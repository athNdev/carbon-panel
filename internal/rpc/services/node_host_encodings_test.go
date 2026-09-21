package services

import "testing"

// TestValidateNodeHostEncodings covers encoded/alternate host forms used to dodge
// the Docker host policy.
func TestValidateNodeHostEncodings(t *testing.T) {
	rejected := []string{
		"tcp://[::ffff:169.254.169.254]:2375", // IPv4-mapped link-local
		"tcp://0x7f000001:2375",               // hex loopback (not parsed as IP -> hostname lookup)
		"tcp://2130706433:2375",               // decimal loopback
		"ssh://user@169.254.169.254",          // link-local over ssh
		"tcp://[fe80::1]:2375",                // IPv6 link-local
		"unix://",                             // no socket path
		"npipe://",                            // no pipe name
		"tcp://0.0.0.0:2375",                  // unspecified
		"http://169.254.169.254/latest/meta-data/",
	}
	for _, h := range rejected {
		if err := validateNodeHost(h); err == nil {
			t.Errorf("validateNodeHost(%q) = nil, want rejection", h)
		}
	}

	accepted := []string{
		"unix:///var/run/docker.sock",
		"tcp://127.0.0.1:2375",
		"tcp://10.0.0.5:2376",
		"tcp://[::1]:2375",
		"ssh://docker@10.0.0.9",
	}
	for _, h := range accepted {
		if err := validateNodeHost(h); err != nil {
			t.Errorf("validateNodeHost(%q) = %v, want nil", h, err)
		}
	}
}
