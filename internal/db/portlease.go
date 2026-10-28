// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const (
	backendPortStart = 25566
	backendPortEnd   = 30000
	backendPortGap   = 10 // RCON host port is the game port plus this gap
)

// ErrNoBackendPort means every candidate pair on the node is taken.
var ErrNoBackendPort = errors.New("no free backend port on node")

// ErrPortLeaseHeld means another server already holds that node, port, and protocol.
var ErrPortLeaseHeld = errors.New("host port is already leased")

const (
	portProtocolTCP     = "tcp"
	portProtocolUDP     = "udp"
	portLeaseBackend    = "backend"
	portLeaseAdditional = "additional"
)

// HostPort is one advertised bind. An empty protocol means TCP.
type HostPort struct {
	Port     int
	Protocol string
}

// BackendPortLease is a node-scoped host bind held for one server.
// Uniqueness is (node, port, protocol), so TCP and UDP of the same number
// are different claims.
type BackendPortLease struct {
	NodeID   string `json:"node_id" gorm:"primaryKey;column:node_id"`
	Port     int    `json:"port" gorm:"primaryKey"`
	Protocol string `json:"protocol" gorm:"primaryKey;column:protocol;not null;default:tcp"`
	ServerID string `json:"server_id" gorm:"column:server_id;index"`
	Purpose  string `json:"purpose" gorm:"column:purpose;not null;default:''"`
}

func (BackendPortLease) TableName() string { return "backend_port_leases" }

// ReserveBackendPortPair claims a game port and the RCON port ten above it.
// extraUsed includes ports Docker already published on that node. The lease is
// released with ReleaseBackendPorts when the server row is not kept.
func (s *Store) ReserveBackendPortPair(ctx context.Context, nodeID, serverID string, extraUsed map[int]bool) (int, int, error) {
	if nodeID == "" || serverID == "" {
		return 0, 0, fmt.Errorf("node and server are required to reserve a backend port")
	}
	var game, rcon int
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("server_id = ?", serverID).Delete(&BackendPortLease{}).Error; err != nil {
				return err
			}
			used, err := usedBackendPorts(tx, nodeID, serverID)
			if err != nil {
				return err
			}
			for port, taken := range extraUsed {
				if taken {
					used[port] = true
				}
			}
			for p := backendPortStart; p+backendPortGap <= backendPortEnd; p++ {
				if used[p] || used[p+backendPortGap] {
					continue
				}
				if err := tx.Create(&BackendPortLease{NodeID: nodeID, Port: p, Protocol: portProtocolTCP, ServerID: serverID, Purpose: portLeaseBackend}).Error; err != nil {
					return err
				}
				if err := tx.Create(&BackendPortLease{NodeID: nodeID, Port: p + backendPortGap, Protocol: portProtocolTCP, ServerID: serverID, Purpose: portLeaseBackend}).Error; err != nil {
					return err
				}
				game, rcon = p, p+backendPortGap
				return nil
			}
			return ErrNoBackendPort
		})
		if err == nil {
			return game, rcon, nil
		}
		lastErr = err
		if errors.Is(err, ErrNoBackendPort) || !isUniqueViolation(err) {
			return 0, 0, err
		}
	}
	return 0, 0, fmt.Errorf("reserve backend port: %w", lastErr)
}

// ReleaseBackendPorts drops every lease held by serverID.
func (s *Store) ReleaseBackendPorts(ctx context.Context, serverID string) error {
	return s.db.WithContext(ctx).Where("server_id = ?", serverID).Delete(&BackendPortLease{}).Error
}

func usedBackendPorts(tx *gorm.DB, nodeID, exceptServerID string) (map[int]bool, error) {
	used := map[int]bool{}
	var leases []BackendPortLease
	if err := tx.Where("node_id = ?", nodeID).Find(&leases).Error; err != nil {
		return nil, err
	}
	for _, lease := range leases {
		if lease.ServerID == exceptServerID {
			continue
		}
		protocol := lease.Protocol
		if protocol == "" {
			protocol = portProtocolTCP
		}
		if protocol != portProtocolTCP {
			continue
		}
		used[lease.Port] = true
	}
	var servers []Server
	if err := tx.Select("id", "port").Where("node_id = ? AND port > 0", nodeID).Find(&servers).Error; err != nil {
		return nil, err
	}
	for _, server := range servers {
		if server.ID == exceptServerID {
			continue
		}
		used[server.Port] = true
		used[server.Port+backendPortGap] = true
	}
	return used, nil
}

// ReplaceAdditionalPortLeases stores the server's additional binds and
// drops the additional binds it held before. Backend game and RCON leases
// stay. A TCP claim does not occupy the UDP port, and the reverse is also
// true. The call is one transaction, so a conflict leaves the previous
// additional claims in place.
func (s *Store) ReplaceAdditionalPortLeases(ctx context.Context, nodeID, serverID string, ports []HostPort) error {
	if nodeID == "" || serverID == "" {
		return fmt.Errorf("node and server are required to reserve a host port")
	}
	normalized := make([]HostPort, 0, len(ports))
	seen := map[string]bool{}
	for _, port := range ports {
		protocol, err := normalizePortProtocol(port.Protocol)
		if err != nil {
			return err
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("invalid host port %d", port.Port)
		}
		key := fmt.Sprintf("%d/%s", port.Port, protocol)
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, HostPort{Port: port.Port, Protocol: protocol})
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ? AND purpose = ?", serverID, portLeaseAdditional).Delete(&BackendPortLease{}).Error; err != nil {
			return err
		}
		for _, port := range normalized {
			var held int64
			if err := tx.Model(&BackendPortLease{}).
				Where("node_id = ? AND port = ? AND protocol = ? AND server_id <> ?", nodeID, port.Port, port.Protocol, serverID).
				Count(&held).Error; err != nil {
				return err
			}
			if held > 0 {
				return fmt.Errorf("%w: %d/%s on node %s", ErrPortLeaseHeld, port.Port, port.Protocol, nodeID)
			}
			if port.Protocol == portProtocolTCP {
				var servers int64
				if err := tx.Model(&Server{}).
					Where("node_id = ? AND id <> ? AND port = ?", nodeID, serverID, port.Port).
					Count(&servers).Error; err != nil {
					return err
				}
				if servers > 0 {
					return fmt.Errorf("%w: %d/tcp on node %s", ErrPortLeaseHeld, port.Port, nodeID)
				}
			}
			row := BackendPortLease{
				NodeID: nodeID, Port: port.Port, Protocol: port.Protocol,
				ServerID: serverID, Purpose: portLeaseAdditional,
			}
			if err := tx.Create(&row).Error; err != nil {
				if isUniqueViolation(err) {
					return fmt.Errorf("%w: %d/%s on node %s", ErrPortLeaseHeld, port.Port, port.Protocol, nodeID)
				}
				return err
			}
		}
		return nil
	})
	return err
}

// MovePortLeases moves every lease held by serverID onto targetNodeID.
// A bind the target already holds for another server is left on the source.
func (s *Store) MovePortLeases(ctx context.Context, serverID, targetNodeID string) error {
	if serverID == "" || targetNodeID == "" {
		return fmt.Errorf("server and target node are required to move port leases")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var leases []BackendPortLease
		if err := tx.Where("server_id = ?", serverID).Find(&leases).Error; err != nil {
			return err
		}
		if len(leases) == 0 {
			return nil
		}
		for _, lease := range leases {
			protocol := lease.Protocol
			if protocol == "" {
				protocol = portProtocolTCP
			}
			var held int64
			if err := tx.Model(&BackendPortLease{}).
				Where("node_id = ? AND port = ? AND protocol = ? AND server_id <> ?", targetNodeID, lease.Port, protocol, serverID).
				Count(&held).Error; err != nil {
				return err
			}
			if held > 0 {
				return fmt.Errorf("%w: %d/%s on node %s", ErrPortLeaseHeld, lease.Port, protocol, targetNodeID)
			}
		}
		return tx.Model(&BackendPortLease{}).Where("server_id = ?", serverID).Update("node_id", targetNodeID).Error
	})
}

func normalizePortProtocol(protocol string) (string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		protocol = portProtocolTCP
	}
	if protocol != portProtocolTCP && protocol != portProtocolUDP {
		return "", fmt.Errorf("invalid port protocol %q", protocol)
	}
	return protocol, nil
}

// ensurePortLeaseProtocolKey rebuilds a lease table whose primary key is
// only the node and the port. Existing rows become TCP claims. A table that
// already keys protocol is left in place.
func ensurePortLeaseProtocolKey(tx *gorm.DB) error {
	var count int
	if err := tx.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='backend_port_leases'").Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var createSQL string
	if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='backend_port_leases'").Scan(&createSQL).Error; err != nil {
		return err
	}
	normalized := strings.ToLower(createSQL)
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "`", "")
	normalized = strings.ReplaceAll(normalized, "\"", "")
	normalized = strings.ReplaceAll(normalized, "[", "")
	normalized = strings.ReplaceAll(normalized, "]", "")
	if strings.Contains(normalized, "primarykey(node_id,port,protocol)") {
		return tx.Exec("UPDATE backend_port_leases SET protocol = 'tcp' WHERE protocol IS NULL OR protocol = ''").Error
	}
	if err := tx.Exec(`CREATE TABLE backend_port_leases_proto (
		node_id text NOT NULL,
		port integer NOT NULL,
		protocol text NOT NULL DEFAULT 'tcp',
		server_id text,
		purpose text NOT NULL DEFAULT '',
		PRIMARY KEY (node_id, port, protocol)
	)`).Error; err != nil {
		return err
	}
	insert := `INSERT INTO backend_port_leases_proto (node_id, port, protocol, server_id, purpose)
		SELECT node_id, port, 'tcp', server_id, '' FROM backend_port_leases`
	if strings.Contains(normalized, "protocol") {
		purposeExpr := "''"
		if strings.Contains(normalized, "purpose") {
			purposeExpr = "IFNULL(purpose, '')"
		}
		insert = `INSERT INTO backend_port_leases_proto (node_id, port, protocol, server_id, purpose)
			SELECT node_id, port, CASE WHEN protocol IS NULL OR protocol = '' THEN 'tcp' ELSE protocol END, server_id, ` + purposeExpr + ` FROM backend_port_leases`
	}
	if err := tx.Exec(insert).Error; err != nil {
		return err
	}
	if err := tx.Exec("DROP TABLE backend_port_leases").Error; err != nil {
		return err
	}
	if err := tx.Exec("ALTER TABLE backend_port_leases_proto RENAME TO backend_port_leases").Error; err != nil {
		return err
	}
	return tx.Exec("CREATE INDEX IF NOT EXISTS idx_backend_port_leases_server_id ON backend_port_leases (server_id)").Error
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "CONSTRAINT")
}
