package logger

import (
	"strings"
	"testing"
)

func TestSanitizeLogMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No secrets",
			input:    "Server started successfully on port 25565",
			expected: "Server started successfully on port 25565",
		},
		{
			name:     "RCON URI password",
			input:    "Connecting to rcon://admin:supersecret@localhost:25575",
			expected: "Connecting to rcon://admin:********@localhost:25575",
		},
		{
			name:     "Password assignment with equals",
			input:    "Starting server with password=mySecretPassword123 and user=admin",
			expected: "Starting server with password=******** and user=admin",
		},
		{
			name:     "Password with colon",
			input:    "Config parsed password: mySecretPassword123",
			expected: "Config parsed password: ********",
		},
		{
			name:     "Client secret in JSON-like log",
			input:    `OIDC client_secret: "superSecretClientSecret123"`,
			expected: `OIDC client_secret: "********"`,
		},
		{
			name:     "API key",
			input:    "Using api_key=ak_live_123456789 for external service",
			expected: "Using api_key=******** for external service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeLogMessage(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeLogMessage(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLoggerRedactsSecretsInBuffer(t *testing.T) {
	l := New()
	l.Info("Starting server with password=hidden123 and port=25565")
	recent := l.GetRecentLogs()
	if len(recent) == 0 {
		t.Fatal("expected log entry in buffer")
	}
	lastLog := recent[len(recent)-1]
	if strings.Contains(lastLog, "hidden123") {
		t.Errorf("expected secret to be redacted, got: %s", lastLog)
	}
	if !strings.Contains(lastLog, "password=********") {
		t.Errorf("expected placeholder in log, got: %s", lastLog)
	}
}
