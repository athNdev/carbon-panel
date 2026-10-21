package auth

import (
	"testing"
	"time"
)

func TestTOTP_RoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code length %d", len(code))
	}
	if !ValidateTOTP(secret, code, now, 1) {
		t.Fatal("valid code rejected")
	}
	if ValidateTOTP(secret, "000000", now, 0) && code != "000000" {
		t.Fatal("wrong code accepted")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 4 {
		t.Fatalf("got %d", len(codes))
	}
}
