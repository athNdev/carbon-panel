package auth

import "testing"

func TestValidatePasswordAcceptsStrongPasswords(t *testing.T) {
	strong := []string{
		"CorrectHorse42!",
		"multi-word-passphrase-9",
		"aB3$aB3$aB3$",
		"Tr0ub4dor&3xtra",
	}
	for _, pw := range strong {
		if err := ValidatePassword(pw); err != nil {
			t.Errorf("ValidatePassword(%q) = %v, want nil", pw, err)
		}
	}
}

func TestValidatePasswordRejectsWeakPasswords(t *testing.T) {
	weak := []string{
		"a",                   // too short
		"1",                   // too short
		"123456",              // too short
		"password",            // too short + common
		"abc",                 // too short
		"aaaaaaaaaaaa",        // long enough, single class
		"123456789012",        // long enough, single class + common
		"passwordpassword",    // long enough but single class
		"shortPass1",          // 10 chars
		"aaaaaaaaaaaaaaaaaaa", // single class, long
	}
	for _, pw := range weak {
		if err := ValidatePassword(pw); err == nil {
			t.Errorf("ValidatePassword(%q) = nil, want rejection", pw)
		}
	}
}

func TestValidatePasswordRejectsOverlongInput(t *testing.T) {
	long := ""
	for i := 0; i < MaxPasswordLength+1; i++ {
		long += "a"
	}
	long = "Aa1!" + long
	if err := ValidatePassword(long); err == nil {
		t.Fatal("expected over-long password to be rejected (bcrypt truncates at 72 bytes)")
	}
}
