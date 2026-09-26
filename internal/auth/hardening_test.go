package auth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
)

func newTestManager(t *testing.T, authCfg *config.AuthConfig) (*Manager, *db.Store) {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	m, err := NewManager(store, nil, authCfg)
	if err != nil {
		t.Fatalf("failed to create auth manager: %v", err)
	}
	return m, store
}

// TestNoAuthProviderFailsClosed covers the fail-open fix: with local auth and
// OIDC both disabled, requests must be rejected unless the operator explicitly
// opted in with allow_no_auth.
func TestNoAuthProviderFailsClosed(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: false, AllowRegistration: false},
		OIDC:           config.OIDCConfig{Enabled: false},
		AllowNoAuth:    false,
	})

	if m.IsAnyAuthEnabled() {
		t.Fatal("expected no auth provider to be enabled")
	}
	if _, err := m.AuthenticateFromHeader(context.TODO(), ""); err == nil {
		t.Fatal("expected unauthenticated request to be rejected when no provider is enabled")
	} else if !matchesErrNoAuth(err) {
		t.Fatalf("expected ErrNoAuthProvider, got %v", err)
	}
}

// TestNoAuthProviderAllowedWhenOptedIn asserts the explicit opt-in still works.
func TestNoAuthProviderAllowedWhenOptedIn(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: false},
		OIDC:           config.OIDCConfig{Enabled: false},
		AllowNoAuth:    true,
	})

	user, err := m.AuthenticateFromHeader(context.TODO(), "")
	if err != nil {
		t.Fatalf("expected synthetic admin when allow_no_auth is set, got %v", err)
	}
	if user == nil || len(user.Roles) != 1 || user.Roles[0] != "admin" {
		t.Fatalf("expected admin role, got %+v", user)
	}
}

// TestUpdateSettingsRefusesLastProvider covers the runtime guard: an admin must
// not be able to disable local auth while OIDC is off.
func TestUpdateSettingsRefusesLastProvider(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true},
		OIDC:           config.OIDCConfig{Enabled: false},
		AllowNoAuth:    false,
	})

	disabled := false
	if err := m.UpdateSettings(context.TODO(), &disabled, nil, nil, nil); err == nil {
		t.Fatal("expected UpdateSettings to refuse disabling the last provider")
	} else if !matchesErrNoAuth(err) {
		t.Fatalf("expected ErrNoAuthProvider, got %v", err)
	}
	if !m.config.Local.Enabled {
		t.Fatal("local auth must remain enabled after the refused update")
	}
}

// TestPasswordChangeInvalidatesOtherSessions covers the session-invalidation
// fix: rotating the password must drop every other session.
func TestPasswordChangeInvalidatesOtherSessions(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true},
		AllowNoAuth:    false,
	})

	ctx := t.Context()
	const oldPassword = "OldPassword!2345"
	const newPassword = "NewPassword!2345"

	user, err := m.CreateLocalUser(ctx, "alice", "alice@example.com", oldPassword)
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	// Two independent logins => two sessions.
	_, _, tokenA, _, err := m.Login(ctx, "alice", oldPassword)
	if err != nil {
		t.Fatalf("login A: %v", err)
	}
	_, _, tokenB, _, err := m.Login(ctx, "alice", oldPassword)
	if err != nil {
		t.Fatalf("login B: %v", err)
	}

	if err := m.ChangePassword(ctx, user.ID, oldPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	// Keep tokenA (the acting session) and drop tokenB.
	if err := m.InvalidateUserSessions(ctx, user.ID, tokenA); err != nil {
		t.Fatalf("InvalidateUserSessions: %v", err)
	}

	if _, err := m.ValidateSession(ctx, tokenA); err != nil {
		t.Errorf("acting session must survive: %v", err)
	}
	if _, err := m.ValidateSession(ctx, tokenB); err == nil {
		t.Error("other session must be invalidated after a password change")
	}
}

// TestWeakPasswordRejectedAtCreate covers the password policy wiring.
func TestWeakPasswordRejectedAtCreate(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true},
	})

	if _, err := m.CreateLocalUser(t.Context(), "bob", "", "a"); err == nil {
		t.Fatal("expected weak password to be rejected by CreateLocalUser")
	}
}

func matchesErrNoAuth(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrNoAuthProvider.Error())
}

// TestRapidLoginsProduceDistinctSessions is a regression test for a real bug
// found while writing the test above: two logins by the same user inside the
// same second produced byte-identical JWTs (no jti claim), so the second
// CreateSession failed on the sessions.token UNIQUE constraint.
func TestRapidLoginsProduceDistinctSessions(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true},
	})

	ctx := t.Context()
	const password = "RapidLogin!2345"
	if _, err := m.CreateLocalUser(ctx, "carol", "", password); err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}

	var tokens []string
	for i := 0; i < 5; i++ {
		_, _, token, _, err := m.Login(ctx, "carol", password)
		if err != nil {
			t.Fatalf("rapid login %d failed: %v", i+1, err)
		}
		tokens = append(tokens, token)
	}

	seen := make(map[string]bool, len(tokens))
	for i, tok := range tokens {
		if seen[tok] {
			t.Fatalf("login %d produced a duplicate token", i+1)
		}
		seen[tok] = true
		if _, err := m.ValidateSession(ctx, tok); err != nil {
			t.Fatalf("token %d should validate: %v", i+1, err)
		}
	}
}

// TestThrottleLockoutThenExpiry is a timing test with an injected clock.
func TestThrottleLockoutThenExpiry(t *testing.T) {
	th := NewLoginThrottle()
	now := time.Unix(1_700_000_000, 0)
	th.now = func() time.Time { return now }

	const key = "ip:192.0.2.10"
	for i := 0; i < 5; i++ {
		if allowed, _ := th.Allow(key); !allowed {
			t.Fatalf("attempt %d should be allowed before the threshold", i+1)
		}
		th.Failure(key)
	}

	allowed, retry := th.Allow(key)
	if allowed {
		t.Fatal("key must be locked out after 5 failures")
	}
	if retry <= 0 || retry > 15*time.Minute {
		t.Fatalf("unexpected retry window: %v", retry)
	}

	now = now.Add(16 * time.Minute)
	if allowed, _ := th.Allow(key); !allowed {
		t.Fatal("lockout must expire after the cooldown")
	}
}
