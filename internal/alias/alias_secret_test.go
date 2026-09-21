package alias

import (
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

// TestSecretConfigAliasesAreRedacted guards the fix for the alias-endpoint
// secret disclosure: ModuleService/GetResolvedAliases (and GetAvailableAliases)
// are reachable with only modules:read — which the default "user" and
// "anonymous" roles hold — so credential-bearing config aliases must never
// carry a value out of the API.
func TestSecretConfigAliasesAreRedacted(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Port = "8080"
	cfg.Auth.JWTSecret = "super-secret-jwt-value"
	cfg.Auth.OIDC.ClientSecret = "oidc-client-secret"
	cfg.Storage.S3.AccessKey = "AKIAEXAMPLEACCESSKEY"
	cfg.Storage.S3.SecretKey = "s3-secret-value"
	cfg.Proxy.ValkeyURL = "redis://user:pass@127.0.0.1:6379"

	ctx := &Context{Config: cfg}

	seenSecret := 0
	resolved := GetResolvedAliases(ctx)
	for alias, val := range resolved {
		if IsSecretPath(alias) {
			seenSecret++
			if val != "" {
				t.Errorf("secret alias %s returned a non-empty value", alias)
			}
		}
		if strings.Contains(val, "super-secret-jwt-value") ||
			strings.Contains(val, "oidc-client-secret") ||
			strings.Contains(val, "s3-secret-value") ||
			strings.Contains(val, "AKIAEXAMPLEACCESSKEY") {
			t.Errorf("raw secret leaked through alias %s", alias)
		}
	}
	if seenSecret == 0 {
		t.Fatal("no secret aliases were enumerated; the redaction test is not exercising anything")
	}

	// Non-secret aliases must still resolve so the endpoint stays useful.
	if got := resolved["{{config.server.port}}"]; got != "8080" {
		t.Fatalf("non-secret alias {{config.server.port}} = %q, want \"8080\"", got)
	}
}

// TestSubstituteStillResolvesSecretsInternally asserts the redaction is scoped
// to the introspection API: module template substitution (Substitute) runs
// server-side and must still receive the real values.
func TestSubstituteStillResolvesSecretsInternally(t *testing.T) {
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = "internal-secret"

	out := Substitute("v={{config.auth.jwt_secret}}", &Context{Config: cfg})
	if out != "v=internal-secret" {
		t.Fatalf("internal substitution regressed: got %q, want %q", out, "v=internal-secret")
	}
}

func TestIsSecretPath(t *testing.T) {
	secret := []string{
		"config.auth.jwt_secret",
		"config.auth.oidc.client_secret",
		"config.storage.s3.access_key",
		"config.storage.s3.secret_key",
		"config.proxy.valkey_url",
		"auth.password",
		"some.field.token",
	}
	for _, p := range secret {
		if !IsSecretPath(p) {
			t.Errorf("IsSecretPath(%q) = false, want true", p)
		}
	}

	notSecret := []string{
		"config.server.port",
		"config.server.host",
		"config.proxy.base_url",
		"server.id",
		"module.auto_start",
	}
	for _, p := range notSecret {
		if IsSecretPath(p) {
			t.Errorf("IsSecretPath(%q) = true, want false", p)
		}
	}
}
