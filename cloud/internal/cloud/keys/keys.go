// Package keys declares every credential Carbon Cloud understands.
//
// The build must never require an external key. Instead each capability declares
// which keys it needs; a capability whose keys are absent is reported as disabled
// by the system capability endpoint rather than crashing the process.
package keys

import (
	"sort"
	"strings"
)

// Secret key names. Values are read through the secrets provider, never from
// package-level state.
const (
	// ClerkIssuer is the Clerk instance issuer URL used to derive JWKS.
	ClerkIssuer = "clerk.issuer"
	// ClerkJWKSURL overrides the JWKS location when it is not derived from the issuer.
	ClerkJWKSURL = "clerk.jwks_url"
	// ClerkSecretKey is the Clerk backend API key.
	ClerkSecretKey = "clerk.secret_key"
	// ClerkWebhookSecret verifies Clerk webhook signatures.
	ClerkWebhookSecret = "clerk.webhook_secret"

	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL = "database.url"

	// NodeCAKey is the PEM-encoded control-plane CA private key used to sign
	// node client certificates.
	NodeCAKey = "node.ca_key"
	// NodeCACert is the PEM-encoded control-plane CA certificate.
	NodeCACert = "node.ca_cert"
	// JoinTokenSecret is the HMAC secret used to sign join tokens.
	JoinTokenSecret = "node.join_token_secret"

	// EnvelopeMasterKey is the key-encryption key protecting tenant provider
	// credentials at rest.
	EnvelopeMasterKey = "crypto.envelope_master_key"

	// StateS3Endpoint is the S3-compatible endpoint for Terraform state.
	StateS3Endpoint = "state.s3.endpoint"
	// StateS3Bucket is the bucket holding Terraform state.
	StateS3Bucket = "state.s3.bucket"
	// StateS3AccessKeyID authenticates to the state backend.
	StateS3AccessKeyID = "state.s3.access_key_id"
	// StateS3SecretAccessKey authenticates to the state backend.
	StateS3SecretAccessKey = "state.s3.secret_access_key"

	// ProviderGenericSSHKey is a private key used to reach generic BYO/managed hosts.
	ProviderGenericSSHKey = "provider.generic.ssh_private_key"
	// ProviderHetznerToken is the Hetzner Cloud API token.
	ProviderHetznerToken = "provider.hetzner.token"
	// ProviderAWSAccessKeyID is the AWS access key id.
	ProviderAWSAccessKeyID = "provider.aws.access_key_id"
	// ProviderAWSSecretAccessKey is the AWS secret access key.
	ProviderAWSSecretAccessKey = "provider.aws.secret_access_key"
	// ProviderGCPCredentials is the GCP service-account JSON.
	ProviderGCPCredentials = "provider.gcp.credentials_json"
	// ProviderDigitalOceanToken is the DigitalOcean API token.
	ProviderDigitalOceanToken = "provider.digitalocean.token"
	// ProviderProxmoxToken is the Proxmox API token.
	ProviderProxmoxToken = "provider.proxmox.token"
	// ProviderProxmoxEndpoint is the Proxmox API endpoint.
	ProviderProxmoxEndpoint = "provider.proxmox.endpoint"

	// SMTPURL is the mail relay used for invitations and alerts.
	SMTPURL = "smtp.url"
)

// Capability identifiers reported by the system capability endpoint.
const (
	// CapClerk covers session authentication and membership sync.
	CapClerk = "clerk"
	// CapProvisioning covers Terraform-backed managed node creation.
	CapProvisioning = "provisioning"
	// CapStateBackend covers remote Terraform state.
	CapStateBackend = "state.backend"
	// CapNodeIdentity covers issuing node certificates.
	CapNodeIdentity = "node.identity"
	// CapTenantCredentials covers storing tenant provider credentials.
	CapTenantCredentials = "tenant.credentials"
	// CapEmail covers outbound email.
	CapEmail = "email"
)

// capabilityKeys maps a capability to the keys it requires. A capability is
// enabled when every listed key resolves.
var capabilityKeys = map[string][]string{
	CapClerk:             {ClerkIssuer},
	CapProvisioning:      {ProviderGenericSSHKey},
	CapStateBackend:      {StateS3Bucket, StateS3AccessKeyID, StateS3SecretAccessKey},
	CapNodeIdentity:      {NodeCAKey, NodeCACert, JoinTokenSecret},
	CapTenantCredentials: {EnvelopeMasterKey},
	CapEmail:             {SMTPURL},
}

// Capabilities returns every capability identifier, sorted.
func Capabilities() []string {
	out := make([]string, 0, len(capabilityKeys))
	for cap := range capabilityKeys {
		out = append(out, cap)
	}
	sort.Strings(out)
	return out
}

// RequiredFor returns the keys a capability needs, sorted.
func RequiredFor(capability string) []string {
	out := append([]string(nil), capabilityKeys[capability]...)
	sort.Strings(out)
	return out
}

// All returns every declared key name, sorted.
func All() []string {
	out := []string{
		ClerkIssuer, ClerkJWKSURL, ClerkSecretKey, ClerkWebhookSecret,
		DatabaseURL, NodeCAKey, NodeCACert, JoinTokenSecret, EnvelopeMasterKey,
		StateS3Endpoint, StateS3Bucket, StateS3AccessKeyID, StateS3SecretAccessKey,
		ProviderGenericSSHKey, ProviderHetznerToken,
		ProviderAWSAccessKeyID, ProviderAWSSecretAccessKey, ProviderGCPCredentials,
		ProviderDigitalOceanToken, ProviderProxmoxToken, ProviderProxmoxEndpoint,
		SMTPURL,
	}
	sort.Strings(out)
	return out
}

// LooksLikePlaceholder reports whether a configured value is obviously a
// placeholder rather than a real secret, so misconfiguration fails loudly.
func LooksLikePlaceholder(v string) bool {
	t := strings.TrimSpace(v)
	if t == "" {
		return true
	}
	lower := strings.ToLower(t)
	for _, p := range []string{"changeme", "change_me", "placeholder", "todo", "xxx", "your-", "<", "${"} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
