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

// BackendPortLease is a node-scoped host port held for one server.
// Uniqueness is (node, port), so two creates cannot publish the same bind.
type BackendPortLease struct {
	NodeID   string `json:"node_id" gorm:"primaryKey;column:node_id"`
	Port     int    `json:"port" gorm:"primaryKey"`
	ServerID string `json:"server_id" gorm:"column:server_id;index"`
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
				if err := tx.Create(&BackendPortLease{NodeID: nodeID, Port: p, ServerID: serverID}).Error; err != nil {
					return err
				}
				if err := tx.Create(&BackendPortLease{NodeID: nodeID, Port: p + backendPortGap, ServerID: serverID}).Error; err != nil {
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

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "CONSTRAINT")
}
