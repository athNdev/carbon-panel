package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvLookup(t *testing.T) {
	t.Setenv("CARBONCLOUD_CLERK_ISSUER", "https://issuer.example")
	p := NewEnv("")
	assert.Equal(t, "env", p.Kind())
	v, err := p.Get(context.Background(), keys.ClerkIssuer)
	require.NoError(t, err)
	assert.Equal(t, "https://issuer.example", v)
}

func TestEnvMissingIsNotFound(t *testing.T) {
	_ = os.Unsetenv("CARBONCLOUD_CLERK_SECRET_KEY")
	p := NewEnv("CARBONCLOUD_")
	_, err := p.Get(context.Background(), keys.ClerkSecretKey)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, p.Health(context.Background()))
}

func TestEnvRejectsPlaceholders(t *testing.T) {
	for _, ph := range []string{"", "CHANGEME", "xxx", "<paste-token>", "your-token-here", "TODO fill in", "${TOKEN}"} {
		t.Setenv("CARBONCLOUD_PROVIDER_HETZNER_TOKEN", ph)
		p := NewEnv("")
		_, err := p.Get(context.Background(), keys.ProviderHetznerToken)
		require.ErrorIs(t, err, ErrNotFound, "placeholder %q must be absent", ph)
	}
}

func TestEnvCustomPrefix(t *testing.T) {
	t.Setenv("ACME_CLERK_ISSUER", "https://acme.example")
	p := NewEnv("ACME")
	v, err := p.Get(context.Background(), keys.ClerkIssuer)
	require.NoError(t, err)
	assert.Equal(t, "https://acme.example", v)
}

func TestFileDotenvLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("# comment\nCARBONCLOUD_CLERK_ISSUER=https://file-issuer.example\nclerk.secret_key=\"file-secret\"\n"), 0o600))
	p, err := NewFile(path)
	require.NoError(t, err)
	assert.Equal(t, "file", p.Kind())

	v, err := p.Get(context.Background(), keys.ClerkIssuer)
	require.NoError(t, err)
	assert.Equal(t, "https://file-issuer.example", v)

	v, err = p.Get(context.Background(), keys.ClerkSecretKey)
	require.NoError(t, err)
	assert.Equal(t, "file-secret", v)

	_, err = p.Get(context.Background(), keys.SMTPURL)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, p.Health(context.Background()))
}

func TestFileJSONLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"clerk.issuer": "https://json-issuer.example", "CARBONCLOUD_SMTP_URL": "smtp://json.example"}`), 0o600))
	p, err := NewFile(path)
	require.NoError(t, err)

	v, err := p.Get(context.Background(), keys.ClerkIssuer)
	require.NoError(t, err)
	assert.Equal(t, "https://json-issuer.example", v)

	v, err = p.Get(context.Background(), keys.SMTPURL)
	require.NoError(t, err)
	assert.Equal(t, "smtp://json.example", v)
}

func TestFileRejectsPlaceholders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("CARBONCLOUD_CLERK_ISSUER=CHANGEME\n"), 0o600))
	p, err := NewFile(path)
	require.NoError(t, err)
	_, err = p.Get(context.Background(), keys.ClerkIssuer)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFileErrors(t *testing.T) {
	_, err := NewFile("")
	require.Error(t, err)
	_, err = NewFile(filepath.Join(t.TempDir(), "missing.env"))
	require.Error(t, err)
	bad := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte(`{oops`), 0o600))
	_, err = NewFile(bad)
	require.Error(t, err)
}

func TestChainPrecedence(t *testing.T) {
	t.Setenv("CARBONCLOUD_CLERK_ISSUER", "https://env-issuer.example")
	filePath := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(filePath, []byte("CARBONCLOUD_CLERK_ISSUER=https://file-issuer.example\nCARBONCLOUD_SMTP_URL=smtp://file.example\n"), 0o600))
	file, err := NewFile(filePath)
	require.NoError(t, err)

	chain := NewChain(NewEnv(""), file)
	assert.Equal(t, "chain", chain.Kind())

	// First hit wins: env beats file.
	v, err := chain.Get(context.Background(), keys.ClerkIssuer)
	require.NoError(t, err)
	assert.Equal(t, "https://env-issuer.example", v)

	// Falls through to file when env misses.
	v, err = chain.Get(context.Background(), keys.SMTPURL)
	require.NoError(t, err)
	assert.Equal(t, "smtp://file.example", v)

	_, err = chain.Get(context.Background(), keys.ProviderGCPCredentials)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestResolverMissingRequire(t *testing.T) {
	t.Setenv("CARBONCLOUD_CLERK_ISSUER", "https://issuer.example")
	_ = os.Unsetenv("CARBONCLOUD_NODE_CA_KEY")
	r := NewResolver(NewEnv(""))

	assert.True(t, r.Has(context.Background(), keys.ClerkIssuer))
	assert.False(t, r.Has(context.Background(), "node.ca_key"))

	missing := r.Missing(context.Background(), []string{keys.ClerkIssuer, "node.ca_key"})
	assert.Equal(t, []string{"node.ca_key"}, missing)

	require.NoError(t, r.Require(context.Background(), keys.ClerkIssuer))
	err := r.Require(context.Background(), keys.ClerkIssuer, "node.ca_key", "node.ca_cert")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CARBONCLOUD_NODE_CA_KEY")
	assert.Contains(t, err.Error(), "CARBONCLOUD_NODE_CA_CERT")
}

func TestCapabilitiesClerkDisabledThenEnabled(t *testing.T) {
	_ = os.Unsetenv("CARBONCLOUD_CLERK_ISSUER")
	r := NewResolver(NewEnv(""))
	cap := r.Capability(context.Background(), keys.CapClerk)
	assert.False(t, cap.Enabled)
	assert.Equal(t, []string{keys.ClerkIssuer}, cap.RequiredKeys)
	assert.Contains(t, cap.Detail, "CARBONCLOUD_CLERK_ISSUER")

	t.Setenv("CARBONCLOUD_CLERK_ISSUER", "https://issuer.example")
	cap = r.Capability(context.Background(), keys.CapClerk)
	assert.True(t, cap.Enabled)
	assert.Empty(t, cap.MissingKeys)
}

func TestCapabilitiesListsAll(t *testing.T) {
	r := NewResolver(NewEnv(""))
	caps := r.Capabilities(context.Background())
	assert.Len(t, caps, len(keys.Capabilities()))
	ids := make([]string, 0, len(caps))
	for _, c := range caps {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, keys.Capabilities(), ids)
}

func TestCapabilityUnknownDenied(t *testing.T) {
	r := NewResolver(NewEnv(""))
	cap := r.Capability(context.Background(), "nope")
	assert.False(t, cap.Enabled)
	assert.Contains(t, cap.Detail, "unknown capability")
}

func TestFromConfig(t *testing.T) {
	p, err := FromConfig(config.Secrets{Provider: "env", Prefix: "CARBONCLOUD_"})
	require.NoError(t, err)
	assert.Equal(t, "env", p.Kind())

	p, err = FromConfig(config.Secrets{})
	require.NoError(t, err)
	assert.Equal(t, "env", p.Kind())

	_, err = FromConfig(config.Secrets{Provider: "file"})
	require.Error(t, err)

	_, err = FromConfig(config.Secrets{Provider: "vault"})
	require.Error(t, err)

	filePath := filepath.Join(t.TempDir(), "s.env")
	require.NoError(t, os.WriteFile(filePath, []byte("CARBONCLOUD_SMTP_URL=smtp://x.example\n"), 0o600))
	p, err = FromConfig(config.Secrets{Provider: "chain", File: filePath})
	require.NoError(t, err)
	assert.Equal(t, "chain", p.Kind())
	v, err := p.Get(context.Background(), keys.SMTPURL)
	require.NoError(t, err)
	assert.Equal(t, "smtp://x.example", v)

	// Chain without a file still boots.
	p, err = FromConfig(config.Secrets{Provider: "chain"})
	require.NoError(t, err)
	assert.Equal(t, "chain", p.Kind())
}
