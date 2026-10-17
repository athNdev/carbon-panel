package auth_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
)

func TestRateLimiter_BypassesSystemEndpoints(t *testing.T) {
	rl := auth.NewRateLimiter(auth.RateLimitConfig{
		Enabled:    true,
		IPRate:     1,
		IPBurst:    1,
		LimiterTTL: time.Minute,
	})
	defer rl.Close()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "192.0.2.1:1234"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s iteration %d, got %d", path, i, rec.Code)
			}
		}
	}
}

func TestRateLimiter_IPLimit(t *testing.T) {
	rl := auth.NewRateLimiter(auth.RateLimitConfig{
		Enabled:    true,
		IPRate:     1,
		IPBurst:    3,
		LimiterTTL: time.Minute,
	})
	defer rl.Close()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	reqIP1 := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/cloud.v1.OrgService/ListOrgs", nil)
		r.RemoteAddr = "198.51.100.1:1000"
		return r
	}

	// First 3 should succeed (burst = 3)
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, reqIP1())
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i, rec.Code)
		}
	}

	// 4th should be throttled
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, reqIP1())
	if rec4.Code != http.StatusTooManyRequests {
		t.Fatalf("request 4 expected 429, got %d", rec4.Code)
	}
	if rec4.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on 429")
	}

	// Different IP should still succeed
	reqIP2 := httptest.NewRequest(http.MethodGet, "/cloud.v1.OrgService/ListOrgs", nil)
	reqIP2.RemoteAddr = "198.51.100.2:1000"
	recIP2 := httptest.NewRecorder()
	handler.ServeHTTP(recIP2, reqIP2)
	if recIP2.Code != http.StatusOK {
		t.Fatalf("different IP expected 200, got %d", recIP2.Code)
	}
}

func TestRateLimiter_KeyLimit(t *testing.T) {
	rl := auth.NewRateLimiter(auth.RateLimitConfig{
		Enabled:    true,
		IPRate:     100, // generous IP rate
		IPBurst:    100,
		KeyRate:    1,
		KeyBurst:   2,
		LimiterTTL: time.Minute,
	})
	defer rl.Close()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(token string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/cloud.v1.NodeService/ListNodes", nil)
		r.RemoteAddr = "198.51.100.5:1000"
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}

	// First 2 for token A should succeed (burst = 2)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq("token-AAA"))
		if rec.Code != http.StatusOK {
			t.Fatalf("token A request %d expected 200, got %d", i, rec.Code)
		}
	}

	// 3rd for token A should be throttled
	recA3 := httptest.NewRecorder()
	handler.ServeHTTP(recA3, makeReq("token-AAA"))
	if recA3.Code != http.StatusTooManyRequests {
		t.Fatalf("token A request 3 expected 429, got %d", recA3.Code)
	}

	// Token B should succeed
	recB1 := httptest.NewRecorder()
	handler.ServeHTTP(recB1, makeReq("token-BBB"))
	if recB1.Code != http.StatusOK {
		t.Fatalf("token B request 1 expected 200, got %d", recB1.Code)
	}
}

func TestRateLimiter_Concurrency(t *testing.T) {
	rl := auth.NewRateLimiter(auth.RateLimitConfig{
		Enabled:    true,
		IPRate:     50,
		IPBurst:    100,
		LimiterTTL: time.Minute,
	})
	defer rl.Close()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "192.0.2.1:1000"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}(i)
	}
	wg.Wait()
}
