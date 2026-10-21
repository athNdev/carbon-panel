package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// GenerateTOTPSecret returns a base32-encoded 20-byte secret suitable for TOTP.
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// totpCode computes the TOTP code for the given secret and time using RFC 6238.
func totpCode(secret string, t time.Time, digits int) (string, error) {
	if digits <= 0 {
		digits = 6
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	counter := uint64(t.Unix() / 30)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	mod := 1
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	val := code % mod
	return fmt.Sprintf("%0*d", digits, val), nil
}

// TOTPCode returns the current 6-digit code for secret at t.
func TOTPCode(secret string, t time.Time) (string, error) {
	return totpCode(secret, t, 6)
}

// ValidateTOTP reports whether code matches the secret within +/- window steps.
func ValidateTOTP(secret, code string, t time.Time, window int) bool {
	if window < 0 {
		window = 1
	}
	code = strings.TrimSpace(code)
	for i := -window; i <= window; i++ {
		ct := t.Add(time.Duration(i*30) * time.Second)
		c, err := totpCode(secret, ct, 6)
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(c), []byte(code)) {
			return true
		}
	}
	return false
}

// GenerateRecoveryCodes returns n random recovery codes.
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		n = 8
	}
	out := make([]string, 0, n)
	for len(out) < n {
		nb, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%06d", nb.Int64()))
	}
	return out, nil
}
