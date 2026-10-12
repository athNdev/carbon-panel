// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
)

func newLifecycleTestManager(t *testing.T, cfg *config.AuthConfig) (*Manager, string, string) {
	t.Helper()

	if cfg == nil {
		cfg = &config.AuthConfig{
			SessionTimeout: 3600,
			JWTSecret:      "test-secret-value-0123456789",
			Local:          config.LocalConfig{Enabled: true, AllowRegistration: false},
			OIDC:           config.OIDCConfig{Enabled: false},
		}
	}
	m, store := newTestManager(t, cfg)
	ctx := context.Background()

	user, err := m.CreateLocalUser(ctx, "lifecycle", "life@example.com", "Str0ng!Password-1")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_, _, token, _, err := m.Login(ctx, "lifecycle", "Str0ng!Password-1")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}
	_ = store
	return m, user.ID, token
}

func TestSession_LoginLogoutRoundTrip(t *testing.T) {
	m, _, token := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	got, err := m.ValidateSession(ctx, token)
	if err != nil || got.Username != "lifecycle" {
		t.Fatalf("ValidateSession = %+v, %v", got, err)
	}
	if err := m.Logout(ctx, token); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if _, err := m.ValidateSession(ctx, token); err == nil {
		t.Fatal("session still valid after logout")
	}
	if _, err := m.ValidateSession(ctx, ""); err == nil {
		t.Fatal("empty token validated")
	}
}

func TestAPIToken_GenerateValidateExpire(t *testing.T) {
	m, userID, _ := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	thirty := int32(30)
	plaintext, record, err := m.GenerateAPIToken(ctx, userID, "ci-token", &thirty)
	if err != nil {
		t.Fatalf("GenerateAPIToken failed: %v", err)
	}
	if !strings.HasPrefix(plaintext, "dp_") || record.UserID != userID {
		t.Errorf("unexpected token record: %+v", record)
	}

	got, err := m.ValidateAPIToken(ctx, plaintext)
	if err != nil || got.Username != "lifecycle" {
		t.Fatalf("ValidateAPIToken = %+v, %v", got, err)
	}

	if _, err := m.ValidateAPIToken(ctx, "dp_not-a-real-token"); err == nil {
		t.Fatal("unknown API token validated")
	}

	// Backdate expiry, then validation must fail as expired.
	past := time.Now().Add(-time.Hour)
	record.ExpiresAt = &past
	if err := m.store.DB().WithContext(ctx).Save(record).Error; err != nil {
		t.Fatalf("failed to backdate token: %v", err)
	}
	if _, err := m.ValidateAPIToken(ctx, plaintext); err == nil {
		t.Fatal("expired API token validated")
	}
}

func TestPasswordChange_InvalidatesOtherSessions(t *testing.T) {
	m, userID, token1 := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	_, _, token2, _, err := m.Login(ctx, "lifecycle", "Str0ng!Password-1")
	if err != nil {
		t.Fatalf("second login failed: %v", err)
	}

	if err := m.ChangePassword(ctx, userID, "Str0ng!Password-1", "An0ther!Password-2"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}
	// Old password no longer works.
	if _, _, _, _, err := m.Login(ctx, "lifecycle", "Str0ng!Password-1"); err == nil {
		t.Fatal("login with old password succeeded after rotation")
	}

	// Rotate out every session except the second login's.
	if err := m.InvalidateUserSessions(ctx, userID, token2); err != nil {
		t.Fatalf("InvalidateUserSessions failed: %v", err)
	}
	if _, err := m.ValidateSession(ctx, token1); err == nil {
		t.Fatal("first session survived invalidation")
	}
	if _, err := m.ValidateSession(ctx, token2); err != nil {
		t.Errorf("excepted session invalidated: %v", err)
	}
}

func TestRecoveryKey_UseOnce(t *testing.T) {
	m, _, _ := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	key := m.GetRecoveryKey()
	if key == "" {
		t.Fatal("empty recovery key")
	}
	if err := m.UseRecoveryKey(ctx, "wrong-key"); err == nil {
		t.Fatal("wrong recovery key accepted")
	}
	if err := m.UseRecoveryKey(ctx, key); err != nil {
		t.Fatalf("UseRecoveryKey failed: %v", err)
	}
	// Key is single-use and users were reset: login must fail.
	if err := m.UseRecoveryKey(ctx, key); err == nil {
		t.Fatal("reused recovery key accepted")
	}
	if _, _, _, _, err := m.Login(ctx, "lifecycle", "An0ther!Password-2"); err == nil {
		t.Fatal("login succeeded after user reset")
	}
}

func TestManager_FlagGetters(t *testing.T) {
	m, _, _ := newLifecycleTestManager(t, nil)

	if !m.IsLocalAuthEnabled() {
		t.Error("IsLocalAuthEnabled = false, want true")
	}
	if m.NoAuthAllowed() {
		t.Error("NoAuthAllowed = true, want false")
	}
	if m.IsRegistrationAllowed() {
		t.Error("IsRegistrationAllowed = true, want false")
	}
	if !m.IsAnyAuthEnabled() {
		t.Error("IsAnyAuthEnabled = false, want true")
	}
	if m.IsAnonymousAccessEnabled() {
		t.Error("IsAnonymousAccessEnabled = true, want false")
	}
	if m.GetConfig() == nil {
		t.Error("GetConfig = nil")
	}

	anon := m.AnonymousUser()
	if anon.ID != "anonymous" || len(anon.Roles) != 1 || anon.Roles[0] != "anonymous" {
		t.Errorf("AnonymousUser = %+v", anon)
	}
}

func TestUserContext_RoundTrip(t *testing.T) {
	if got := GetUserFromContext(context.Background()); got != nil {
		t.Errorf("empty context user = %+v, want nil", got)
	}
	want := &AuthenticatedUser{ID: "u1", Username: "tester", Roles: []string{"user"}}
	got := GetUserFromContext(WithUser(context.Background(), want))
	if got == nil || got.ID != "u1" || got.Username != "tester" {
		t.Errorf("context round-trip = %+v", got)
	}
}

func TestUpdateSettings_ValidationAndPersistence(t *testing.T) {
	m, _, _ := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	short := int32(60)
	if err := m.UpdateSettings(ctx, nil, nil, nil, &short); err == nil {
		t.Error("sub-minimum session timeout accepted")
	}
	off := false
	if err := m.UpdateSettings(ctx, &off, nil, nil, nil); err == nil {
		t.Error("disabling last auth provider accepted")
	}

	on, timeout := true, int32(7200)
	if err := m.UpdateSettings(ctx, nil, &on, &on, &timeout); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}
	if !m.IsRegistrationAllowed() || !m.IsAnonymousAccessEnabled() {
		t.Error("settings not applied to running config")
	}
	if m.GetConfig().SessionTimeout != 7200 {
		t.Errorf("session timeout = %d, want 7200", m.GetConfig().SessionTimeout)
	}
}

func TestGenerateModuleToken_Validates(t *testing.T) {
	m, userID, _ := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	plaintext, record, err := m.GenerateModuleToken(ctx, userID, "geyser", "mod-1")
	if err != nil {
		t.Fatalf("GenerateModuleToken failed: %v", err)
	}
	if !strings.HasPrefix(plaintext, "dp_") || !record.IsModuleToken {
		t.Errorf("unexpected module token record: %+v", record)
	}
	if _, err := m.ValidateAPIToken(ctx, plaintext); err != nil {
		t.Errorf("module token did not validate: %v", err)
	}
}

func TestAuthenticateFromHeader_APIToken(t *testing.T) {
	m, userID, sessionToken := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	plaintext, _, err := m.GenerateAPIToken(ctx, userID, "hdr", nil)
	if err != nil {
		t.Fatalf("GenerateAPIToken failed: %v", err)
	}
	if got, err := m.AuthenticateFromHeader(ctx, "Bearer "+plaintext); err != nil || got.Username != "lifecycle" {
		t.Errorf("bearer API token = %+v, %v", got, err)
	}
	if got, err := m.AuthenticateFromHeader(ctx, "Bearer "+sessionToken); err != nil || got.Username != "lifecycle" {
		t.Errorf("bearer session token = %+v, %v", got, err)
	}
	if _, err := m.AuthenticateFromHeader(ctx, "Bearer dp_nope"); err == nil {
		t.Error("bogus bearer token accepted")
	}
}

func TestSettingOverrides_LoadOnStartup(t *testing.T) {
	m, _, _ := newLifecycleTestManager(t, nil)
	ctx := context.Background()

	on := true
	timeout := int32(5400)
	if err := m.UpdateSettings(ctx, nil, &on, &on, &timeout); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	// A fresh manager on the same store must pick up persisted overrides.
	m2, err := NewManager(m.store, nil, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true},
		OIDC:           config.OIDCConfig{Enabled: false},
	})
	if err != nil {
		t.Fatalf("second manager failed: %v", err)
	}
	if !m2.IsRegistrationAllowed() || !m2.IsAnonymousAccessEnabled() {
		t.Error("persisted overrides not loaded on startup")
	}
	if m2.GetConfig().SessionTimeout != 5400 {
		t.Errorf("session timeout = %d, want 5400", m2.GetConfig().SessionTimeout)
	}
}
