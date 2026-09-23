package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig configures IP-based and key-based token bucket rate limiting.
type RateLimitConfig struct {
	// Enabled determines if rate limiting is active.
	Enabled bool
	// IPRate is the allowed sustained requests per second per IP address.
	IPRate float64
	// IPBurst is the burst limit for an IP address.
	IPBurst int
	// KeyRate is the allowed sustained requests per second per API key / token.
	KeyRate float64
	// KeyBurst is the burst limit for an API key / token.
	KeyBurst int
	// LimiterTTL is the idle duration before an inactive limiter is pruned.
	LimiterTTL time.Duration
}

// DefaultRateLimitConfig returns standard production defaults for rate limiting.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:    true,
		IPRate:     20.0, // 20 requests / sec sustained per IP
		IPBurst:    50,   // up to 50 burst requests
		KeyRate:    50.0, // 50 requests / sec sustained per authenticated key
		KeyBurst:   100,  // up to 100 burst requests
		LimiterTTL: 10 * time.Minute,
	}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter tracks and enforces token-bucket limits.
type RateLimiter struct {
	cfg   RateLimitConfig
	mu    sync.Mutex
	ips   map[string]*clientLimiter
	keys  map[string]*clientLimiter
	close chan struct{}
}

// NewRateLimiter creates a RateLimiter and begins periodic TTL eviction.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	if cfg.LimiterTTL <= 0 {
		cfg.LimiterTTL = 10 * time.Minute
	}
	rl := &RateLimiter{
		cfg:   cfg,
		ips:   make(map[string]*clientLimiter),
		keys:  make(map[string]*clientLimiter),
		close: make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Close stops the background eviction loop.
func (rl *RateLimiter) Close() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	select {
	case <-rl.close:
	default:
		close(rl.close)
	}
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cfg.LimiterTTL / 2)
	defer ticker.Stop()
	for {
		select {
		case <-rl.close:
			return
		case now := <-ticker.C:
			rl.evict(now)
		}
	}
}

func (rl *RateLimiter) evict(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := now.Add(-rl.cfg.LimiterTTL)
	for ip, cl := range rl.ips {
		if cl.lastSeen.Before(cutoff) {
			delete(rl.ips, ip)
		}
	}
	for key, cl := range rl.keys {
		if cl.lastSeen.Before(cutoff) {
			delete(rl.keys, key)
		}
	}
}

func (rl *RateLimiter) getIPLimiter(ip string, now time.Time) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cl, ok := rl.ips[ip]
	if !ok {
		cl = &clientLimiter{
			limiter: rate.NewLimiter(rate.Limit(rl.cfg.IPRate), rl.cfg.IPBurst),
		}
		rl.ips[ip] = cl
	}
	cl.lastSeen = now
	return cl.limiter
}

func (rl *RateLimiter) getKeyLimiter(keyHash string, now time.Time) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cl, ok := rl.keys[keyHash]
	if !ok {
		cl = &clientLimiter{
			limiter: rate.NewLimiter(rate.Limit(rl.cfg.KeyRate), rl.cfg.KeyBurst),
		}
		rl.keys[keyHash] = cl
	}
	cl.lastSeen = now
	return cl.limiter
}

// ExtractIP extracts the client IP address, preferring X-Forwarded-For if present.
func ExtractIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		clientIP := strings.TrimSpace(parts[0])
		if clientIP != "" {
			return clientIP
		}
	}
	xri := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// Middleware returns an http.Handler middleware wrapping next.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	if !rl.cfg.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health, readiness, and metrics never throttle.
		path := r.URL.Path
		if path == "/healthz" || path == "/readyz" || path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		now := time.Now()

		// 1. IP-level rate limiting.
		ip := ExtractIP(r)
		if ip != "" && rl.cfg.IPRate > 0 {
			lim := rl.getIPLimiter(ip, now)
			if !lim.AllowN(now, 1) {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":"resource_exhausted","message":"ip rate limit exceeded"}`))
				return
			}
		}

		// 2. Key-level rate limiting if Authorization header is provided.
		authHdr := r.Header.Get("Authorization")
		if authHdr != "" && rl.cfg.KeyRate > 0 {
			token := strings.TrimSpace(strings.TrimPrefix(authHdr, "Bearer "))
			if token != "" {
				h := sha256.Sum256([]byte(token))
				keyHash := hex.EncodeToString(h[:16])
				lim := rl.getKeyLimiter(keyHash, now)
				if !lim.AllowN(now, 1) {
					w.Header().Set("Retry-After", "1")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"code":"resource_exhausted","message":"key rate limit exceeded"}`))
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}
