package services

import (
	"net/http"
	"testing"
)

func TestRealClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		peerAddr string
		want     string
	}{
		{
			name:     "direct public peer ignores forwarded headers",
			headers:  map[string]string{"X-Forwarded-For": "203.0.113.9"},
			peerAddr: "198.51.100.7:54321",
			want:     "198.51.100.7",
		},
		{
			name:     "trusted loopback proxy honours X-Forwarded-For first hop",
			headers:  map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.5"},
			peerAddr: "127.0.0.1:40000",
			want:     "203.0.113.9",
		},
		{
			name:     "trusted private proxy honours X-Real-IP when no XFF",
			headers:  map[string]string{"X-Real-IP": "203.0.113.11"},
			peerAddr: "10.0.0.5:40000",
			want:     "203.0.113.11",
		},
		{
			name:     "loopback peer without forwarded headers",
			headers:  nil,
			peerAddr: "127.0.0.1:40000",
			want:     "127.0.0.1",
		},
		{
			name:     "malformed peer address passed through",
			headers:  nil,
			peerAddr: "not-an-address",
			want:     "not-an-address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdr := http.Header{}
			for k, v := range tc.headers {
				hdr.Set(k, v)
			}
			if got := realClientIP(hdr, tc.peerAddr); got != tc.want {
				t.Errorf("realClientIP(%v, %q) = %q, want %q", tc.headers, tc.peerAddr, got, tc.want)
			}
		})
	}
}
