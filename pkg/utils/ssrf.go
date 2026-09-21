package utils

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"
)

var (
	ErrBlockedIP     = errors.New("connection blocked by SSRF guard")
	ErrInvalidScheme = errors.New("invalid URL scheme: must be http or https")
	ErrMissingHost   = errors.New("url is missing host")
)

// ValidateIP checks whether an IP address belongs to denied ranges (loopback, link-local, private RFC1918)
func ValidateIP(ip net.IP, allowPrivate bool) error {
	if ip == nil {
		return fmt.Errorf("%w: invalid IP", ErrBlockedIP)
	}

	// Always deny loopback (127.0.0.0/8, ::1) unless explicitly permitted for tests
	if ip.IsLoopback() {
		if os.Getenv("DISCO_ALLOW_LOOPBACK_TEST") == "1" {
			return nil
		}
		return fmt.Errorf("%w: loopback address %s is prohibited", ErrBlockedIP, ip)
	}

	// Always deny link-local (169.254.0.0/16, fe80::/10)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%w: link-local address %s is prohibited", ErrBlockedIP, ip)
	}

	// Always deny unspecified (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return fmt.Errorf("%w: unspecified address %s is prohibited", ErrBlockedIP, ip)
	}

	// Always deny multicast
	if ip.IsMulticast() {
		return fmt.Errorf("%w: multicast address %s is prohibited", ErrBlockedIP, ip)
	}

	// Deny private RFC1918 / RFC4193 unless explicitly allowed
	if !allowPrivate && ip.IsPrivate() {
		return fmt.Errorf("%w: private network address %s is prohibited", ErrBlockedIP, ip)
	}

	return nil
}

// ValidateURLStatic validates a URL without performing DNS resolution. It
// enforces the scheme and, when the host is an IP literal, the address policy.
// Use it where a DNS lookup must not happen (e.g. saving configuration);
// ValidateURL remains the resolving variant for actual outbound requests.
func ValidateURLStatic(rawURL string, allowPrivate bool) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrInvalidScheme
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, ErrMissingHost
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if err := ValidateIP(ip, allowPrivate); err != nil {
			return nil, err
		}
	}
	return parsed, nil
}

// ValidateURL performs static and DNS resolution validation on a URL
func ValidateURL(rawURL string, allowPrivate bool) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrInvalidScheme
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, ErrMissingHost
	}

	// If hostname is directly an IP literal
	if ip := net.ParseIP(hostname); ip != nil {
		if err := ValidateIP(ip, allowPrivate); err != nil {
			return nil, err
		}
		return parsed, nil
	}

	// Resolve DNS records
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %w", hostname, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses resolved for %s", hostname)
	}

	for _, ip := range ips {
		if err := ValidateIP(ip, allowPrivate); err != nil {
			return nil, err
		}
	}

	return parsed, nil
}

// NewSafeHTTPClient returns an http.Client equipped with SSRF guards at connection time,
// socket dialing, and redirect following.
func NewSafeHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		ControlContext: func(ctx context.Context, network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip != nil {
				return ValidateIP(ip, allowPrivate)
			}
			return nil
		},
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			_, err := ValidateURL(req.URL.String(), allowPrivate)
			return err
		},
	}
}
