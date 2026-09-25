package db

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migrations is the append-only schema history. New migrations are appended
// with a new string id; existing entries are never edited.
func migrations() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		{
			ID: "0001_initial_schema",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(
					&Org{},
					&Member{},
					&ApiKey{},
					&JoinToken{},
					&Node{},
					&NodeType{},
					&Provision{},
					&Workload{},
					&WorkloadEvent{},
					&AuditEvent{},
					&RoleBinding{},
				); err != nil {
					return err
				}
				// Composite + named uniques beyond what AutoMigrate derives
				// from single-column tags.
				return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_members_org_user ON members (org_id, user_id)`).Error
			},
		},
		{
			ID: "0002_unique_names_per_org",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_nodes_org_name ON nodes (org_id, name)`).Error; err != nil {
					return err
				}
				return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_workloads_org_name ON workloads (org_id, name)`).Error
			},
		},
		{
			ID: "0003_workload_backups",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&WorkloadBackup{})
			},
		},
		{
			ID: "0004_blueprints",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&Blueprint{}); err != nil {
					return err
				}
				return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_blueprints_org_name ON blueprints (org_id, name)`).Error
			},
		},
	}
}

// Migrate applies the append-only migration list. It is idempotent: running
// twice is a no-op.
func (s *Store) Migrate() error {
	m := gormigrate.New(s.db, &gormigrate.Options{
		TableName:                 "cloud_migrations",
		IDColumnName:              "id",
		IDColumnSize:              200,
		UseTransaction:            true,
		ValidateUnknownMigrations: true,
	}, migrations())
	return m.Migrate()
}

// tenantModels lists every model carrying an OrgID, used by tests and by
// future lanes to audit scoping coverage.
func tenantModels() []any {
	return []any{
		&Org{},
		&Member{},
		&ApiKey{},
		&JoinToken{},
		&Node{},
		&Provision{},
		&Workload{},
		&WorkloadEvent{},
		&WorkloadBackup{},
		&Blueprint{},
		&AuditEvent{},
		&RoleBinding{},
	}
}
