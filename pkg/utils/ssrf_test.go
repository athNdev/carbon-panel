package utils

import (
	"errors"
	"net"
	"testing"
)

func TestValidateIP(t *testing.T) {
	tests := []struct {
		name         string
		ip           string
		allowPrivate bool
		wantErr      bool
	}{
		// Loopback
		{"IPv4 loopback", "127.0.0.1", true, true},
		{"IPv4 loopback range", "127.255.0.1", true, true},
		{"IPv6 loopback", "::1", true, true},

		// Link-local / Cloud metadata
		{"IPv4 link-local (cloud metadata)", "169.254.169.254", true, true},
		{"IPv6 link-local", "fe80::1", true, true},

		// Unspecified
		{"IPv4 unspecified", "0.0.0.0", true, true},
		{"IPv6 unspecified", "::", true, true},

		// Private RFC1918 denied by default
		{"10.x.x.x denied", "10.0.0.1", false, true},
		{"172.16.x.x denied", "172.16.0.1", false, true},
		{"192.168.x.x denied", "192.168.1.1", false, true},

		// Private RFC1918 allowed when flag is true
		{"192.168.x.x allowed", "192.168.1.1", true, false},
		{"10.0.x.x allowed", "10.0.0.5", true, false},

		// Public routable IPs
		{"Cloudflare DNS", "1.1.1.1", false, false},
		{"Google DNS", "8.8.8.8", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tc.ip)
			}
			err := ValidateIP(ip, tc.allowPrivate)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateIP(%s, %v) error = %v, wantErr %v", tc.ip, tc.allowPrivate, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrBlockedIP) {
				t.Errorf("expected ErrBlockedIP, got %v", err)
			}
		})
	}
}

func TestValidateURL_InvalidScheme(t *testing.T) {
	invalidURLs := []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:70",
		"ftp://evil.com/payload",
		"ssh://127.0.0.1:22",
	}

	for _, u := range invalidURLs {
		_, err := ValidateURL(u, false)
		if !errors.Is(err, ErrInvalidScheme) {
			t.Errorf("expected ErrInvalidScheme for %s, got %v", u, err)
		}
	}
}

func TestValidateURL_IPLiterals(t *testing.T) {
	// Blocked
	blockedURLs := []string{
		"http://127.0.0.1:8080/test",
		"http://169.254.169.254/latest/meta-data/",
		"http://192.168.1.50/modpack.zip",
	}

	for _, u := range blockedURLs {
		_, err := ValidateURL(u, false)
		if err == nil {
			t.Errorf("expected error for URL %s, got nil", u)
		}
	}
}
