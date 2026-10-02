package indexers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorKindString(t *testing.T) {
	cases := map[ErrorKind]string{
		ErrAuth:       "authentication error",
		ErrRateLimit:  "rate limited",
		ErrNotFound:   "not found",
		ErrNetwork:    "network error",
		ErrAPI:        "API error",
		ErrDecode:     "decode error",
		ErrorKind(99): "unknown error",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Fatalf("kind %d: got %q want %q", int(kind), got, want)
		}
	}
}

func TestNewAPIErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		kind   ErrorKind
	}{
		{401, ErrAuth},
		{403, ErrAuth},
		{404, ErrNotFound},
		{429, ErrRateLimit},
		{500, ErrAPI},
		{200, ErrAPI},
	}
	for _, tc := range cases {
		err := NewAPIError("modrinth", tc.status, "http://x", "body")
		if err.Kind != tc.kind || err.StatusCode != tc.status {
			t.Fatalf("status %d: got %+v", tc.status, err)
		}
	}
}

func TestIndexerErrorMessageAndUnwrap(t *testing.T) {
	inner := errors.New("boom")
	err := &IndexerError{Kind: ErrAPI, Indexer: "fuego", StatusCode: 500, URL: "http://x", Body: "oops", Err: inner}
	msg := err.Error()
	for _, want := range []string{"fuego", "status 500", "url=http://x", "boom", "body=oops"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap should expose inner error")
	}
	// Minimal error still formats.
	if msg := (&IndexerError{Kind: ErrNotFound, Indexer: "fuego"}).Error(); !strings.Contains(msg, "not found") {
		t.Fatalf("minimal message %q", msg)
	}
}

func TestNewNetworkErrorDNSWrapping(t *testing.T) {
	dns := &net.DNSError{Name: "api.example.com", IsNotFound: true}
	err := NewNetworkError("fuego", "http://x", dns)
	if err.Kind != ErrNetwork {
		t.Fatalf("kind=%v", err.Kind)
	}
	if !strings.Contains(err.Error(), "api.example.com") {
		t.Fatalf("message %q should name host", err.Error())
	}
	plain := errors.New("conn refused")
	if err := NewNetworkError("fuego", "http://x", plain); !errors.Is(err, plain) {
		t.Fatal("non-DNS error should pass through unwrappable")
	}
}

func TestPredicates(t *testing.T) {
	if !IsRateLimit(NewAPIError("m", 429, "", "")) || IsRateLimit(NewAPIError("m", 404, "", "")) {
		t.Fatal("IsRateLimit")
	}
	if !IsAuthError(NewAuthConfigError("m", "no key")) || IsAuthError(NewAPIError("m", 500, "", "")) {
		t.Fatal("IsAuthError")
	}
	if !IsNotFound(NewAPIError("m", 404, "", "")) || IsNotFound(NewDecodeError("m", "", errors.New("x"))) {
		t.Fatal("IsNotFound")
	}
	if !IsNetworkError(NewNetworkError("m", "", errors.New("x"))) || IsNetworkError(NewAPIError("m", 500, "", "")) {
		t.Fatal("IsNetworkError")
	}
	plain := errors.New("plain")
	if IsRateLimit(plain) || IsAuthError(plain) || IsNotFound(plain) || IsNetworkError(plain) {
		t.Fatal("predicates must be false for non-indexer errors")
	}
	// Wrapped indexer errors still classify.
	if !IsRateLimit(NewAPIError("m", 429, "", "")) {
		t.Fatal("classified wrapped")
	}
}

func TestDoJSONSuccessAndHeaders(t *testing.T) {
	var gotUA, gotAccept, gotExtra string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotAccept, gotExtra = r.Header.Get("User-Agent"), r.Header.Get("Accept"), r.Header.Get("X-Key")
		_, _ = w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()

	h := NewHTTPClient("test", "CarbonTest/1.0", map[string]string{"X-Key": "k"})
	var dest struct{ A int }
	if err := h.DoJSON(context.Background(), srv.URL, &dest); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if dest.A != 1 || gotUA != "CarbonTest/1.0" || gotAccept != "application/json" || gotExtra != "k" {
		t.Fatalf("dest=%+v ua=%q accept=%q extra=%q", dest, gotUA, gotAccept, gotExtra)
	}
}

func TestDoJSONStatusMapping(t *testing.T) {
	for status, pred := range map[int]func(error) bool{
		404: IsNotFound,
		429: IsRateLimit,
		403: IsAuthError,
		500: func(err error) bool {
			var ie *IndexerError
			return errors.As(err, &ie) && ie.Kind == ErrAPI
		},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("nope"))
		}))
		h := NewHTTPClient("test", "", nil)
		var dest struct{ A int }
		err := h.DoJSON(context.Background(), srv.URL, &dest)
		srv.Close()
		if err == nil || !pred(err) {
			t.Fatalf("status %d: err=%v", status, err)
		}
	}
}

func TestDoJSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()
	h := NewHTTPClient("test", "", nil)
	var dest struct{ A int }
	err := h.DoJSON(context.Background(), srv.URL, &dest)
	var ie *IndexerError
	if !errors.As(err, &ie) || ie.Kind != ErrDecode {
		t.Fatalf("err=%v want decode error", err)
	}
}

func TestDoJSONBadURLIsNetworkError(t *testing.T) {
	h := NewHTTPClient("test", "", nil)
	var dest struct{ A int }
	//nolint:noctx // intentionally malformed URL, no request is issued
	err := h.DoJSON(context.Background(), "http://[::1]:namedport", &dest)
	if !IsNetworkError(err) {
		t.Fatalf("err=%v want network error", err)
	}
}
