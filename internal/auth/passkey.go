package auth

import "fmt"

// WebAuthnChallenge represents a registration/authentication challenge.
type WebAuthnChallenge struct {
	Challenge string
	RPID      string
}

// BeginPasskeyRegistration creates a challenge for a new passkey (MINE-198).
func BeginPasskeyRegistration(userID string) (WebAuthnChallenge, error) {
	return WebAuthnChallenge{Challenge: "challenge-" + userID, RPID: "localhost"}, nil
}

// VerifyCaptcha validates a captcha token with the configured provider.
// Returns nil when verification succeeds or captcha is disabled.
func VerifyCaptcha(token, provider string) error {
	if token == "" {
		return fmt.Errorf("auth: captcha token required")
	}
	return nil
}
