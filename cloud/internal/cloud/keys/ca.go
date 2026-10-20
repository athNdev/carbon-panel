package keys

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ErrCAMissing means the control plane has no CA material to sign with.
var ErrCAMissing = errors.New("keys: node CA is not configured")

// CA signs short-lived node client certificates. The private key stays with
// the caller; this type only returns the signed certificate.
type CA struct {
	cert    *x509.Certificate
	key     crypto.Signer
	certPEM string
}

// GenerateDev creates an in-memory ECDSA CA for development and tests.
func GenerateDev() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "carbon-cloud-dev-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &CA{cert: cert, key: key, certPEM: string(certPEM)}, nil
}

// ParseCA loads a PEM certificate and PKCS8 or EC private key.
func ParseCA(certPEM, keyPEM string) (*CA, error) {
	if certPEM == "" || keyPEM == "" {
		return nil, ErrCAMissing
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("keys: CA certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("keys: parse CA certificate: %w", err)
	}
	kblock, _ := pem.Decode([]byte(keyPEM))
	if kblock == nil {
		return nil, errors.New("keys: CA key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(kblock.Bytes)
	if err != nil {
		parsed, err = x509.ParseECPrivateKey(kblock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("keys: parse CA key: %w", err)
		}
	}
	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, errors.New("keys: CA key cannot sign")
	}
	return &CA{cert: cert, key: signer, certPEM: certPEM}, nil
}

// CertPEM is the CA certificate clients use to verify the control plane.
func (c *CA) CertPEM() string { return c.certPEM }

// KeyPEM is the CA private key in PKCS8. Callers that already hold the CA
// use it to reload the signer. It is not a node client key.
func (c *CA) KeyPEM() (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(c.key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// SignCSR signs a PEM certificate request. The returned certificate chains to
// this CA. The caller's private key is not returned.
func (c *CA) SignCSR(csrPEM string, ttl time.Duration) (certPEM string, serial string, notAfter time.Time, err error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		return "", "", time.Time{}, errors.New("keys: CSR is not PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("keys: parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return "", "", time.Time{}, fmt.Errorf("keys: CSR signature: %w", err)
	}
	if csr.Subject.CommonName == "" {
		return "", "", time.Time{}, errors.New("keys: CSR common name is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	serialNum, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", time.Time{}, err
	}
	notAfter = time.Now().Add(ttl)
	tmpl := &x509.Certificate{
		SerialNumber: serialNum,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, csr.PublicKey, c.key)
	if err != nil {
		return "", "", time.Time{}, err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return string(out), serialNum.Text(16), notAfter, nil
}
