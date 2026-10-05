package auth

import (
	"context"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/tidwall/gjson"
)

func testOIDCHandler(t *testing.T, cfg *config.OIDCConfig) (*Manager, *OIDCHandler) {
	t.Helper()
	m, store := newTestManager(t, testAuthConfig())
	h, err := NewOIDCHandler(m, store, cfg, logger.New())
	if err != nil {
		t.Fatalf("NewOIDCHandler: %v", err)
	}
	return m, h
}

func TestOIDCDisabledHandler(t *testing.T) {
	_, h := testOIDCHandler(t, &config.OIDCConfig{Enabled: false})
	if h.IsEnabled() {
		t.Fatal("disabled handler must report disabled")
	}
}

func TestResolveClaimRoles(t *testing.T) {
	_, h := testOIDCHandler(t, &config.OIDCConfig{RoleClaim: "groups"})

	if got := h.resolveClaimRoles(map[string]any{}); got != nil {
		t.Fatalf("missing claim=%v", got)
	}
	// Array claim, no mapping, unmapped allowed: passthrough.
	got := h.resolveClaimRoles(map[string]any{"groups": []any{"devs", 42, "ops"}})
	if len(got) != 2 || got[0] != "devs" || got[1] != "ops" {
		t.Fatalf("passthrough=%v", got)
	}
	// JSON-encoded string claim.
	got = h.resolveClaimRoles(map[string]any{"groups": `["a","b"]`})
	if len(got) != 2 {
		t.Fatalf("json claim=%v", got)
	}
	// Plain string claim.
	got = h.resolveClaimRoles(map[string]any{"groups": "admins"})
	if len(got) != 1 || got[0] != "admins" {
		t.Fatalf("string claim=%v", got)
	}

	// Mapping with case-insensitive match.
	_, mapped := testOIDCHandler(t, &config.OIDCConfig{
		RoleClaim:   "groups",
		RoleMapping: map[string]string{"Devs": "developer"},
	})
	got = mapped.resolveClaimRoles(map[string]any{"groups": []any{"DEVS", "other"}})
	if len(got) != 1 || got[0] != "developer" {
		t.Fatalf("mapped=%v", got)
	}

	// Reject-unmapped drops everything without a mapping.
	_, strict := testOIDCHandler(t, &config.OIDCConfig{
		RoleClaim:      "groups",
		RoleMapping:    map[string]string{"devs": "developer"},
		RejectUnmapped: true,
	})
	if got := strict.resolveClaimRoles(map[string]any{"groups": []any{"strangers"}}); len(got) != 0 {
		t.Fatalf("strict=%v", got)
	}

	// Empty role claim name disables resolution.
	_, noClaim := testOIDCHandler(t, &config.OIDCConfig{})
	if got := noClaim.resolveClaimRoles(map[string]any{"groups": []any{"x"}}); got != nil {
		t.Fatalf("no-claim=%v", got)
	}
}

func TestCheckRequiredClaim(t *testing.T) {
	_, h := testOIDCHandler(t, &config.OIDCConfig{
		RequiredClaim:  "hd",
		RequiredValues: []string{"example.com"},
	})
	if h.checkRequiredClaim(map[string]any{}) {
		t.Fatal("missing claim must fail")
	}
	if !h.checkRequiredClaim(map[string]any{"hd": "example.com"}) {
		t.Fatal("string match must pass")
	}
	if h.checkRequiredClaim(map[string]any{"hd": "other.com"}) {
		t.Fatal("string mismatch must fail")
	}
	if !h.checkRequiredClaim(map[string]any{"hd": []any{"other.com", "example.com"}}) {
		t.Fatal("array match must pass")
	}
	if !h.checkRequiredClaim(map[string]any{"hd": []string{"example.com"}}) {
		t.Fatal("string-slice match must pass")
	}
	if h.checkRequiredClaim(map[string]any{"hd": 42}) {
		t.Fatal("numeric mismatch must fail")
	}
}

func TestGjsonToAny(t *testing.T) {
	v := gjsonToAny(gjson.Parse(`{"a":[1,"x"],"b":{"c":true},"d":null}`))
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("type=%T", v)
	}
	if arr, ok := m["a"].([]any); !ok || len(arr) != 2 {
		t.Fatalf("a=%v", m["a"])
	}
	if inner, ok := m["b"].(map[string]any); !ok || inner["c"] != true {
		t.Fatalf("b=%v", m["b"])
	}
	if scalar := gjsonToAny(gjson.Parse(`"hi"`)); scalar != "hi" {
		t.Fatalf("scalar=%v", scalar)
	}
}

func TestGenerateState(t *testing.T) {
	a, err := generateState()
	if err != nil || a == "" {
		t.Fatalf("state=%q err=%v", a, err)
	}
	b, err := generateState()
	if err != nil || b == a {
		t.Fatal("states must be unique")
	}
}

func TestThrottleAllowFailureReset(t *testing.T) {
	th := NewLoginThrottleWithLimits(2, time.Minute, time.Minute)
	if ok, _ := th.Allow("k"); !ok {
		t.Fatal("fresh key must be allowed")
	}
	th.Failure("k")
	th.Failure("k")
	if ok, wait := th.Allow("k"); ok || wait <= 0 {
		t.Fatalf("locked key ok=%v wait=%v", ok, wait)
	}
	th.Reset("k")
	if ok, _ := th.Allow("k"); !ok {
		t.Fatal("reset key must be allowed")
	}
	th.Reset("missing") // no-op, must not panic
}

func TestThrottleLockExpiry(t *testing.T) {
	th := NewLoginThrottleWithLimits(1, 20*time.Millisecond, 30*time.Millisecond)
	th.Failure("k")
	th.Failure("k") // second failure within window locks
	if ok, _ := th.Allow("k"); ok {
		t.Fatal("must be locked")
	}
	time.Sleep(60 * time.Millisecond)
	if ok, _ := th.Allow("k"); !ok {
		t.Fatal("lock must expire")
	}
}

func TestGenerateModuleToken(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	id := mustLocalUser(t, m, "ivan")
	plaintext, rec, err := m.GenerateModuleToken(context.Background(), id, "backup", "mod-1")
	if err != nil {
		t.Fatalf("GenerateModuleToken: %v", err)
	}
	if !rec.IsModuleToken {
		t.Fatal("must be flagged as module token")
	}
	u, err := m.ValidateAPIToken(context.Background(), plaintext)
	if err != nil || u.Username != "ivan" {
		t.Fatalf("validate=%+v err=%v", u, err)
	}
}

func TestAuthenticateFromHeader(t *testing.T) {
	m, _ := newTestManager(t, testAuthConfig())
	mustLocalUser(t, m, "judy")
	token := mustLogin(t, m, "judy", testPassword)
	apiToken := mustAPIToken(t, m, func() string {
		u, _ := m.store.GetUserByUsernameAndProvider(context.Background(), "judy", "local")
		return u.ID
	}())

	ctx := context.Background()
	if _, err := m.AuthenticateFromHeader(ctx, "Bearer "+token); err != nil {
		t.Fatalf("session bearer: %v", err)
	}
	if _, err := m.AuthenticateFromHeader(ctx, "Bearer "+apiToken); err != nil {
		t.Fatalf("api bearer: %v", err)
	}
	if _, err := m.AuthenticateFromHeader(ctx, ""); err == nil {
		t.Fatal("empty header must fail without anonymous access")
	}
	if _, err := m.AuthenticateFromHeader(ctx, "Bearer junk"); err == nil {
		t.Fatal("junk bearer must fail")
	}
}
