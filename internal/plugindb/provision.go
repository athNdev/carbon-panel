// Package plugindb provides per-server plugin database provisioning (MINE-191).
package plugindb

import "fmt"

// Database holds connection details for a provisioned plugin database.
type Database struct {
	Host     string
	Name     string
	User     string
	Password string
}

// Provisioner creates and drops plugin databases on a host pool.
type Provisioner struct {
	Hosts []string
}

// CreateDatabase returns a placeholder connection; real provisioning requires a MySQL driver.
func (p *Provisioner) CreateDatabase(serverID string) (Database, error) {
	if len(p.Hosts) == 0 {
		return Database{}, fmt.Errorf("plugindb: no host configured")
	}
	return Database{Host: p.Hosts[0], Name: "mc_" + serverID, User: "mc_" + serverID, Password: "changeme"}, nil
}
