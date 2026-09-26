// Package db is the Carbon Cloud control-plane store: GORM models,
// migrations, org scoping and default seeding.
//
// Every tenant-owned table carries OrgID and must be reached through
// Store.Org(ctx), which refuses to run without an org in the context.
// NodeType is the only global table.
package db

import (
	"context"
	"errors"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrNoOrg is returned when an org-scoped query is attempted without an org
// in the context. Deny by default: callers must select a tenant first.
var ErrNoOrg = errors.New("db: no org in context")

// Options configures Store.Open. It is deliberately narrow so the cfg lane can
// be written in parallel: the cfg lane (or the orchestrator) adapts
// config.Database into this struct. Do not import the config package here.
type Options struct {
	Driver          string // "postgres" | "sqlite"
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
}

// Store wraps the GORM handle. Reach tenant tables via Org(ctx); use
// Unscoped only for global tables (node_types) and migrations.
type Store struct {
	db *gorm.DB
}

// Open connects with opts and optionally runs Migrate.
func Open(opts Options) (*Store, error) {
	var (
		dial gorm.Dialector
	)
	switch opts.Driver {
	case "postgres":
		dial = postgres.Open(opts.DSN)
	case "sqlite", "":
		dial = sqlite.Open(opts.DSN)
	default:
		return nil, errors.New("db: unknown driver " + opts.Driver)
	}

	gdb, err := gorm.Open(dial, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, err
	}

	if sqlDB, err := gdb.DB(); err == nil {
		if opts.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
		}
		if opts.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
		}
		if opts.ConnMaxLifetime > 0 {
			sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)
		}
	}

	s := &Store{db: gdb}
	if opts.AutoMigrate {
		if err := s.Migrate(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// DB returns the raw GORM handle. Prefer Org(ctx) for tenant tables.
func (s *Store) DB() *gorm.DB { return s.db }

// Org returns a query already constrained to the caller's org. It fails with
// ErrNoOrg when the context carries no org. This is the only sanctioned way
// to reach tenant tables.
func (s *Store) Org(ctx context.Context) (*gorm.DB, error) {
	orgID := principal.OrgID(ctx)
	if orgID == "" {
		return nil, ErrNoOrg
	}
	return s.db.WithContext(ctx).Where("org_id = ?", orgID), nil
}

// Unscoped returns the bare handle for genuinely global tables (node_types)
// and for migrations. Never use it to touch tenant-owned rows.
func (s *Store) Unscoped() *gorm.DB { return s.db }
