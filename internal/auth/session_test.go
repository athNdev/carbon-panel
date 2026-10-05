package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
)

const (
	testPassword  = "Str0ng!Passw0rd"
	testPassword2 = "An0ther!Str0ng1"
)

func testAuthConfig() *config.AuthConfig {
	return &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true, AllowRegistration: true},
		OIDC:           config.OIDCConfig{Enabled: false},
	}
}

func mustLocalUser(t *testing.T, m *Manager, username string) string {
	t.Helper()
	u, err := m.CreateLocalUser(context.Background(), username, username+"@x.test", testPassword)
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	return u.ID
}

func mustLogin(t *testing.T, m *Manager, username, password string) string {
	t.Helper()
	_, _, token, _, err := m.Login(context.Background(), username, password)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return token
}

func TestLoginValidateRoundTrip(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	mustLocalUser(t, m, "alice")
	token := mustLogin(t, m, "alice", testPassword)

	u, err := m.ValidateSession(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if u.Username != "alice" || u.Provider != "local" {
		t.Fatalf("user=%+v", u)
	}
}

func TestLoginFailures(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	mustLocalUser(t, m, "bob")

	if _, _, _, _, err := m.Login(context.Background(), "bob", "wrong-password-1!"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password err=%v", err)
	}
	if _, _, _, _, err := m.Login(context.Background(), "nobody", testPassword); err != ErrInvalidCredentials {
		t.Fatalf("unknown user err=%v", err)
	}
	if _, err := m.ValidateSession(context.Background(), ""); err != ErrInvalidToken {
		t.Fatalf("empty token err=%v", err)
	}
	if _, err := m.ValidateSession(context.Background(), "garbage"); err == nil {
		t.Fatal("garbage token must fail")
	}
}

func TestLoginLocalDisabled(t *testing.T) {
	m, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: false},
	})
	if _, _, _, _, err := m.Login(context.Background(), "x", "y"); err != ErrLocalAuthDisabled {
		t.Fatalf("err=%v", err)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	mustLocalUser(t, m, "carol")
	token := mustLogin(t, m, "carol", testPassword)

	if err := m.Logout(context.Background(), token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := m.ValidateSession(context.Background(), token); err == nil {
		t.Fatal("logged-out session must not validate")
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	id := mustLocalUser(t, m, "dave")

	days := int32(7)
	plaintext, rec, err := m.GenerateAPIToken(context.Background(), id, "ci", &days)
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if len(plaintext) < 4 || plaintext[:3] != "dp_" {
		t.Fatalf("plaintext=%q", plaintext)
	}
	if rec.ExpiresAt == nil || time.Until(*rec.ExpiresAt) <= 0 {
		t.Fatalf("expiry=%v", rec.ExpiresAt)
	}

	u, err := m.ValidateAPIToken(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("ValidateAPIToken: %v", err)
	}
	if u.Username != "dave" {
		t.Fatalf("user=%+v", u)
	}

	// No-prefix token rejected before any DB lookup.
	if _, err := m.ValidateAPIToken(context.Background(), "not-a-token"); err != ErrInvalidToken {
		t.Fatalf("err=%v", err)
	}
	// Well-formed but unknown hash.
	if _, err := m.ValidateAPIToken(context.Background(), "dp_bm90cmVhbGx5YXJlYWx0b2tlbmhlcmU"); err != ErrAPITokenNotFound {
		t.Fatalf("err=%v", err)
	}

	// Expiry enforced: backdate the stored row.
	past := time.Now().Add(-time.Hour)
	if err := m.store.DB().Model(rec).Update("expires_at", past).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := m.ValidateAPIToken(context.Background(), plaintext); err != ErrAPITokenExpired {
		t.Fatalf("err=%v", err)
	}
}

func TestAPITokenNoExpiry(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	id := mustLocalUser(t, m, "erin")
	plaintext, rec, err := m.GenerateAPIToken(context.Background(), id, "forever", nil)
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if rec.ExpiresAt != nil {
		t.Fatalf("expiry=%v want nil", rec.ExpiresAt)
	}
	if _, err := m.ValidateAPIToken(context.Background(), plaintext); err != nil {
		t.Fatalf("ValidateAPIToken: %v", err)
	}
}

func TestChangePasswordInvalidatesOthers(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	id := mustLocalUser(t, m, "frank")
	keep := mustLogin(t, m, "frank", testPassword)
	drop := mustLogin(t, m, "frank", testPassword)

	if err := m.ChangePassword(context.Background(), id, "wrong-old-1!", testPassword2); err != ErrInvalidCredentials {
		t.Fatalf("wrong old err=%v", err)
	}
	if err := m.ChangePassword(context.Background(), id, testPassword, "short"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("weak new err=%v", err)
	}
	if err := m.ChangePassword(context.Background(), id, testPassword, testPassword2); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// Old credential dead, new one works.
	if _, _, _, _, err := m.Login(context.Background(), "frank", testPassword); err != ErrInvalidCredentials {
		t.Fatalf("old password err=%v", err)
	}
	mustLogin(t, m, "frank", testPassword2)

	// Rotation evicts every session except the caller's.
	if err := m.InvalidateUserSessions(context.Background(), id, keep); err != nil {
		t.Fatalf("InvalidateUserSessions: %v", err)
	}
	if _, err := m.ValidateSession(context.Background(), keep); err != nil {
		t.Fatalf("kept session err=%v", err)
	}
	if _, err := m.ValidateSession(context.Background(), drop); err == nil {
		t.Fatal("evicted session must not validate")
	}

	// Empty except-token wipes everything.
	tok := mustLogin(t, m, "frank", testPassword2)
	if err := m.InvalidateUserSessions(context.Background(), id, ""); err != nil {
		t.Fatalf("InvalidateUserSessions: %v", err)
	}
	if _, err := m.ValidateSession(context.Background(), tok); err == nil {
		t.Fatal("wiped session must not validate")
	}
}

func TestRecoveryKeyUseOnce(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	mustLocalUser(t, m, "grace")

	key := m.GetRecoveryKey()
	if key == "" {
		t.Fatal("recovery key must be generated")
	}
	if err := m.UseRecoveryKey(context.Background(), "wrong"); err != ErrInvalidRecoveryKey {
		t.Fatalf("err=%v", err)
	}
	if err := m.UseRecoveryKey(context.Background(), key); err != nil {
		t.Fatalf("UseRecoveryKey: %v", err)
	}
	if m.GetRecoveryKey() != "" {
		t.Fatal("recovery key must be cleared after use")
	}
	if err := m.UseRecoveryKey(context.Background(), key); err != ErrInvalidRecoveryKey {
		t.Fatalf("reuse err=%v", err)
	}
	// Users were reset: the old credential no longer logs in.
	if _, _, _, _, err := m.Login(context.Background(), "grace", testPassword); err != ErrInvalidCredentials {
		t.Fatalf("post-reset login err=%v", err)
	}
}

func TestInactiveUserRejected(t *testing.T) {
	m, store := newTestManager(t, testAuthConfig())
	ctx := context.Background()
	u, err := m.CreateLocalUser(ctx, "heidi", "", testPassword)
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	token := mustLogin(t, m, "heidi", testPassword)

	u.IsActive = false
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, _, _, _, err := m.Login(ctx, "heidi", testPassword); err != ErrUserNotActive {
		t.Fatalf("login err=%v", err)
	}
	if _, err := m.ValidateSession(ctx, token); err != ErrUserNotActive {
		t.Fatalf("validate err=%v", err)
	}
	if _, err := m.ValidateAPIToken(ctx, mustAPIToken(t, m, u.ID)); err != ErrUserNotActive {
		t.Fatalf("api validate err=%v", err)
	}
}

func mustAPIToken(t *testing.T, m *Manager, userID string) string {
	t.Helper()
	plaintext, _, err := m.GenerateAPIToken(context.Background(), userID, "t", nil)
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	return plaintext
}

func TestFlagGetters(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	if !m.IsLocalAuthEnabled() {
		t.Fatal("local should be enabled")
	}
	if m.NoAuthAllowed() {
		t.Fatal("no-auth must not be allowed")
	}
	if !m.IsRegistrationAllowed() {
		t.Fatal("registration should be allowed")
	}
	if m.IsAnonymousAccessEnabled() {
		t.Fatal("anonymous should be off")
	}
	if m.GetConfig().SessionTimeout != 3600 {
		t.Fatalf("config=%+v", m.GetConfig())
	}
	if got := m.AnonymousUser(); got == nil || got.Username == "" {
		t.Fatalf("anon=%+v", got)
	}

	anon, _ := newTestManager(t, &config.AuthConfig{
		SessionTimeout:  3600,
		JWTSecret:       "test-secret-value-0123456789",
		Local:           config.LocalConfig{Enabled: true},
		AnonymousAccess: true,
	})
	if !anon.IsAnonymousAccessEnabled() {
		t.Fatal("anonymous should be on")
	}
}

func TestContextHelpers(t *testing.T) {
	if GetUserFromContext(context.Background()) != nil {
		t.Fatal("empty ctx must yield nil")
	}
	u := &AuthenticatedUser{ID: "1", Username: "zoe", Roles: []string{"admin"}}
	ctx := WithUser(context.Background(), u)
	if got := GetUserFromContext(ctx); got != u {
		t.Fatalf("got=%+v", got)
	}
}
