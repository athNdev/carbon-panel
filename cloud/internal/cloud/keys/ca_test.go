package keys

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"
)

func TestCASignCSRChainsToCA(t *testing.T) {
	ca, err := GenerateDev()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "node-1"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	certPEM, serial, notAfter, err := ca.SignCSR(string(csrPEM), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if serial == "" || notAfter.Before(time.Now()) {
		t.Fatalf("serial %q notAfter %s", serial, notAfter)
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("signed cert is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "node-1" {
		t.Fatalf("cn = %s", cert.Subject.CommonName)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ca.CertPEM())) {
		t.Fatal("CA pem rejected")
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("cert does not chain to CA: %v", err)
	}
}

func TestParseCARoundTrip(t *testing.T) {
	ca, err := GenerateDev()
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := ca.KeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCA(ca.CertPEM(), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CertPEM() == "" {
		t.Fatal("empty cert")
	}
}

func TestParseCAMissing(t *testing.T) {
	if _, err := ParseCA("", ""); !errorsIsMissing(err) {
		t.Fatalf("err = %v", err)
	}
}

func errorsIsMissing(err error) bool {
	return err == ErrCAMissing
}
