package services

import (
	"testing"
)

// TestValidateNodeHost covers the Docker host policy added for the CreateNode
// SSRF/hijack finding.
func TestValidateNodeHost(t *testing.T) {
	allowed := []string{
		"unix:///var/run/docker.sock",
		"tcp://127.0.0.1:2375",
		"tcp://10.0.0.5:2376",
		"tcp://192.168.0.120:2375",
		"ssh://docker@10.0.0.9",
		"http://10.0.0.5:2375",
	}
	for _, h := range allowed {
		if err := validateNodeHost(h); err != nil {
			t.Errorf("validateNodeHost(%q) = %v, want nil", h, err)
		}
	}

	rejected := []string{
		"",
		"169.254.169.254:2375",       // no scheme
		"ftp://10.0.0.5",             // disallowed scheme
		"tcp://",                     // no address
		"tcp://169.254.169.254:2375", // link-local / metadata
		"http://169.254.169.254/latest/meta-data/", // metadata URL
		"tcp://0.0.0.0:2375",                       // unspecified
	}
	for _, h := range rejected {
		if err := validateNodeHost(h); err == nil {
			t.Errorf("validateNodeHost(%q) = nil, want rejection", h)
		}
	}
}

// TestValidateWebhookConfigRejectsSSRF covers the configuration-time guard.
func TestValidateWebhookConfigRejectsSSRF(t *testing.T) {
	bad := []string{
		`{"url":"http://127.0.0.1:9000/hook"}`,
		`{"url":"http://169.254.169.254/latest/meta-data/"}`,
		`{"url":"file:///etc/passwd"}`,
		`{"url":""}`,
	}
	for _, cfg := range bad {
		if err := validateWebhookConfig(cfg); err == nil {
			t.Errorf("validateWebhookConfig(%s) = nil, want rejection", cfg)
		}
	}

	good := []string{
		`{"url":"http://192.168.0.50:8123/api/webhook/abc"}`,
		`{"url":"https://hooks.example.com/services/x"}`,
	}
	for _, cfg := range good {
		if err := validateWebhookConfig(cfg); err != nil {
			t.Errorf("validateWebhookConfig(%s) = %v, want nil", cfg, err)
		}
	}
}
