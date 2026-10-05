package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	appconfig "github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// TestBackupAuthorizationIsScopedToOwningServer is the regression test for the
// cross-server backup escalation.
//
// DeleteBackup, RestoreBackup and SetBackupLocked take a BACKUP id, not a
// server id, so rbac.ProcedurePermissions could not set an ObjectIDField for
// them and the interceptor enforced against "*". A caller holding
// backups:delete anywhere could therefore delete, restore or lock a backup
// belonging to a server they had no rights on.
//
// The service now resolves the owning server from the record and enforces
// against it. This test proves that a caller scoped to server A cannot delete
// or lock a backup on server B.
func TestBackupAuthorizationIsScopedToOwningServer(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()
	log := logger.New()

	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := enforcer.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, log)
	svc.SetEnforcer(enforcer)

	dir := t.TempDir()
	mineArchive := filepath.Join(dir, "mine.zip")
	if err := os.WriteFile(mineArchive, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	theirsArchive := filepath.Join(dir, "theirs.zip")
	if err := os.WriteFile(theirsArchive, []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two servers, two backups. The caller only has rights on "srv-mine".
	if err := store.CreateBackupRecord(context.Background(),
		newBackupRecord("b-mine", "srv-mine", mineArchive)); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBackupRecord(context.Background(),
		newBackupRecord("b-theirs", "srv-theirs", theirsArchive)); err != nil {
		t.Fatal(err)
	}

	// A caller granted backups:* on exactly one server via a custom role.
	scoped := enforcer
	if err := scoped.SetPermissionsForRole("scoped-backup-user", []rbac.Permission{
		{Resource: rbac.ResourceBackups, Action: rbac.ActionRead, ObjectID: "srv-mine"},
		{Resource: rbac.ResourceBackups, Action: rbac.ActionDelete, ObjectID: "srv-mine"},
		{Resource: rbac.ResourceBackups, Action: rbac.ActionUpdate, ObjectID: "srv-mine"},
	}); err != nil {
		t.Fatalf("SetPermissionsForRole: %v", err)
	}

	ctx := auth.WithUser(context.Background(), &auth.AuthenticatedUser{
		ID: "u-scoped", Username: "scoped", Roles: []string{"scoped-backup-user"},
	})

	t.Run("cannot delete another server's backup", func(t *testing.T) {
		_, err := svc.DeleteBackup(ctx, connect.NewRequest(&v1.DeleteBackupRequest{Id: "b-theirs"}))
		if err == nil {
			t.Fatal("cross-server delete was allowed")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("expected permission_denied, got %v", connect.CodeOf(err))
		}
		// The archive must still exist: authorization must happen BEFORE the
		// os.Remove, not after.
		if _, statErr := os.Stat(theirsArchive); statErr != nil {
			t.Fatalf("victim archive was deleted despite denial: %v", statErr)
		}
		if _, recErr := store.GetBackupRecord(context.Background(), "b-theirs"); recErr != nil {
			t.Fatalf("victim backup record was deleted despite denial: %v", recErr)
		}
	})

	t.Run("cannot lock another server's backup", func(t *testing.T) {
		_, err := svc.SetBackupLocked(ctx, connect.NewRequest(&v1.SetBackupLockedRequest{
			Id: "b-theirs", Locked: true,
		}))
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("expected permission_denied, got %v (%v)", connect.CodeOf(err), err)
		}
		rec, recErr := store.GetBackupRecord(context.Background(), "b-theirs")
		if recErr != nil {
			t.Fatalf("get record: %v", recErr)
		}
		if rec.Locked {
			t.Fatal("victim backup was locked despite denial")
		}
	})

	t.Run("cannot restore another server's backup", func(t *testing.T) {
		_, err := svc.RestoreBackup(ctx, connect.NewRequest(&v1.RestoreBackupRequest{Id: "b-theirs"}))
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("expected permission_denied, got %v (%v)", connect.CodeOf(err), err)
		}
	})

	t.Run("positive control: own server still works", func(t *testing.T) {
		if _, err := svc.SetBackupLocked(ctx, connect.NewRequest(&v1.SetBackupLockedRequest{
			Id: "b-mine", Locked: true,
		})); err != nil {
			t.Fatalf("caller should be able to lock a backup on their own server: %v", err)
		}
		rec, err := store.GetBackupRecord(context.Background(), "b-mine")
		if err != nil {
			t.Fatalf("get record: %v", err)
		}
		if !rec.Locked {
			t.Fatal("expected b-mine to be locked")
		}
	})
}

// TestBackupAuthorizationFailsClosedWithoutEnforcer pins the fail-closed
// behaviour: an unavailable enforcer must deny, not silently allow. A nil
// enforcer previously meant the permission check was skipped entirely.
func TestBackupAuthorizationFailsClosedWithoutEnforcer(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, logger.New())
	// NOTE: SetEnforcer deliberately NOT called.

	archive := filepath.Join(t.TempDir(), "x.zip")
	if err := os.WriteFile(archive, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBackupRecord(context.Background(),
		newBackupRecord("b1", "srv-1", archive)); err != nil {
		t.Fatal(err)
	}

	ctx := auth.WithUser(context.Background(), &auth.AuthenticatedUser{
		ID: "u", Username: "u", Roles: []string{"admin"},
	})

	// Even an admin is denied: with no enforcer we cannot know, and failing
	// open is exactly the bug this guards.
	if _, err := svc.DeleteBackup(ctx, connect.NewRequest(&v1.DeleteBackupRequest{Id: "b1"})); err == nil {
		t.Fatal("expected denial when the enforcer is unavailable")
	} else if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected permission_denied, got %v", connect.CodeOf(err))
	}
	if _, statErr := os.Stat(archive); statErr != nil {
		t.Fatal("archive removed despite fail-closed denial")
	}
}

// TestBackupAuthorizationRequiresAuthenticatedUser covers the unauthenticated
// path, which the interceptor normally prevents but the service must not rely
// on exclusively.
func TestBackupAuthorizationRequiresAuthenticatedUser(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if err := enforcer.SeedDefaultPolicies(false); err != nil {
		t.Fatalf("SeedDefaultPolicies: %v", err)
	}
	svc := NewBackupService(store, nil, nil, appconfig.S3Config{}, logger.New())
	svc.SetEnforcer(enforcer)

	archive := filepath.Join(t.TempDir(), "y.zip")
	if err := os.WriteFile(archive, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBackupRecord(context.Background(),
		newBackupRecord("b2", "srv-1", archive)); err != nil {
		t.Fatal(err)
	}

	// Bare context: no user attached.
	_, err = svc.DeleteBackup(context.Background(), connect.NewRequest(&v1.DeleteBackupRequest{Id: "b2"}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected unauthenticated, got %v (%v)", connect.CodeOf(err), err)
	}
}
