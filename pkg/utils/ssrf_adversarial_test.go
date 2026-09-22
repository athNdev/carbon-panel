package utils

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// TestSafeClientBlocksLoopbackRedirect checks that the SSRF client refuses a
// redirect that points at loopback (a common bypass: initial URL is fine, the
// redirect is not).
func TestSafeClientBlocksLoopbackRedirect(t *testing.T) {
	c := NewSafeHTTPClient(2*time.Second, true)
	if c.CheckRedirect == nil {
		t.Fatal("expected a CheckRedirect guard")
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:9/internal", nil)
	err := c.CheckRedirect(req, []*http.Request{{}})
	if err == nil {
		t.Fatal("CheckRedirect allowed a loopback redirect")
	}
	t.Logf("loopback redirect blocked: %v", err)

	req2, _ := http.NewRequest("GET", "http://169.254.169.254/latest/meta-data/", nil)
	if err := c.CheckRedirect(req2, []*http.Request{{}}); err == nil {
		t.Fatal("CheckRedirect allowed a link-local redirect")
	}
}

// TestValidateIPMappedAndEncoded covers address encodings used to dodge guards.
func TestValidateIPMappedAndEncoded(t *testing.T) {
	cases := []struct {
		ip    string
		allow bool
		want  bool // true == accepted
	}{
		{"::ffff:169.254.169.254", true, false}, // IPv4-mapped link-local
		{"::ffff:127.0.0.1", true, false},       // IPv4-mapped loopback
		{"0:0:0:0:0:ffff:a9fe:a9fe", true, false},
		{"169.254.169.254", true, false},
		{"127.0.0.1", true, false},
		{"10.0.0.5", true, true},
		{"10.0.0.5", false, false},
		{"192.168.1.10", true, true},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		err := ValidateIP(ip, tc.allow)
		got := err == nil
		if got != tc.want {
			t.Errorf("ValidateIP(%s, allowPrivate=%v) accepted=%v, want %v (err=%v)", tc.ip, tc.allow, got, tc.want, err)
		}
	}
}

// TestValidateURLStaticEncodedHosts covers hosts that are not dotted-quad but
// still resolve to a blocked address once parsed by the OS.
func TestValidateURLStaticEncodedHosts(t *testing.T) {
	// Static validation cannot resolve DNS; it must still reject literal IPs in
	// any recognised form and non-http schemes.
	bad := []string{
		"http://169.254.169.254/",
		"http://127.0.0.1/",
		"file:///etc/passwd",
		"gopher://127.0.0.1/",
		"http://[::ffff:169.254.169.254]/",
		"http://0.0.0.0/",
	}
	for _, u := range bad {
		if _, err := ValidateURLStatic(u, true); err == nil {
			t.Errorf("ValidateURLStatic(%q) = nil, want rejection", u)
		}
	}
}
