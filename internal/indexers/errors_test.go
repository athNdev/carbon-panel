// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package indexers

import (
	"errors"
	"net"
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
			t.Errorf("kind %d = %q, want %q", kind, got, want)
		}
	}
}

func TestNewAPIError_Classification(t *testing.T) {
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
		err := NewAPIError("modrinth", tc.status, "https://x", "body")
		if err.Kind != tc.kind {
			t.Errorf("status %d kind = %v, want %v", tc.status, err.Kind, tc.kind)
		}
		if err.StatusCode != tc.status || err.Indexer != "modrinth" {
			t.Errorf("fields = %+v", err)
		}
	}
}

func TestIndexerError_MessageAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	err := &IndexerError{
		Kind:       ErrAPI,
		Indexer:    "curseforge",
		StatusCode: 500,
		URL:        "https://x",
		Body:       "oops",
		Err:        cause,
	}
	msg := err.Error()
	for _, want := range []string{"curseforge", "500", "https://x", "oops", "boom"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap does not expose cause")
	}

	plain := &IndexerError{Kind: ErrNotFound, Indexer: "i"}
	if plain.Error() == "" {
		t.Error("empty message")
	}
}

func TestErrorPredicates(t *testing.T) {
	rate := NewAPIError("i", 429, "", "")
	if !IsRateLimit(rate) || IsAuthError(rate) || IsNotFound(rate) || IsNetworkError(rate) {
		t.Errorf("rate predicates wrong: %+v", rate)
	}
	authErr := NewAuthConfigError("i", "no key")
	if !IsAuthError(authErr) || IsRateLimit(authErr) {
		t.Errorf("auth predicates wrong: %+v", authErr)
	}
	netErr := NewNetworkError("i", "https://x", &net.DNSError{Err: "nope", Name: "x"})
	if !IsNetworkError(netErr) {
		t.Errorf("network predicate wrong: %+v", netErr)
	}
	plain := errors.New("plain")
	if IsRateLimit(plain) || IsAuthError(plain) || IsNotFound(plain) || IsNetworkError(plain) {
		t.Error("plain error classified")
	}
	if IsRateLimit(nil) || IsAuthError(nil) {
		t.Error("nil classified")
	}
}

func TestNewDecodeError(t *testing.T) {
	cause := errors.New("bad json")
	err := NewDecodeError("i", "https://x", cause)
	if err.Kind != ErrDecode || !errors.Is(err, cause) {
		t.Errorf("decode = %+v", err)
	}
}
