package svc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

type staticSecrets struct {
	values map[string]string
}

func (s staticSecrets) Kind() string                 { return "static" }
func (s staticSecrets) Health(context.Context) error { return nil }
func (s staticSecrets) Get(_ context.Context, key string) (string, error) {
	v, ok := s.values[key]
	if !ok || v == "" {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func testCSR(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func TestRenewCredentialsSignsCSR(t *testing.T) {
	ca, err := keys.GenerateDev()
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := ca.KeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	svc := &AgentService{deps: Deps{Secrets: staticSecrets{values: map[string]string{
		keys.NodeCACert: ca.CertPEM(),
		keys.NodeCAKey:  keyPEM,
	}}}}
	resp, err := svc.RenewCredentials(context.Background(), connect.NewRequest(&v1.RenewCredentialsRequest{
		CsrPem: testCSR(t, "node-42"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	id := resp.Msg.GetIdentity()
	if id.GetClientKeyPem() != "" {
		t.Fatal("renew must not return a private key")
	}
	if id.GetClientCertPem() == "" || id.GetCaCertPem() == "" {
		t.Fatal("missing certificate material")
	}
	block, _ := pem.Decode([]byte(id.GetClientCertPem()))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(id.GetCaCertPem())) {
		t.Fatal("ca pem rejected")
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("issued cert does not chain: %v", err)
	}
}

func TestRenewCredentialsMissingCA(t *testing.T) {
	svc := &AgentService{deps: Deps{Secrets: staticSecrets{values: map[string]string{}}}}
	resp, err := svc.RenewCredentials(context.Background(), connect.NewRequest(&v1.RenewCredentialsRequest{
		CsrPem: testCSR(t, "node-42"),
	}))
	if resp != nil {
		t.Fatal("missing CA must not return a certificate")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(err), err)
	}
}
