// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package webhook

import (
	"context"
	"strings"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

func TestDeliver_RetryExhaustionCountsAttempts(t *testing.T) {
	// Loopback is refused by the SSRF guard: deterministic fast failure
	// that exercises the retry loop without any network.
	res := Deliver(context.Background(), Config{
		URL:          "http://127.0.0.1:9/hook",
		MaxRetries:   2,
		RetryDelayMs: 1,
		TimeoutMs:    500,
	}, &Payload{Event: "test"})
	if res.Success {
		t.Fatal("loopback delivery succeeded, want failure")
	}
	if res.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3 (initial + 2 retries)", res.Attempts)
	}
	if res.ErrorMessage == "" {
		t.Error("missing error message on exhausted delivery")
	}
}

func TestDeliver_ContextCancelAbortsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := Deliver(ctx, Config{
		URL:          "http://127.0.0.1:9/hook",
		MaxRetries:   5,
		RetryDelayMs: 1000,
		TimeoutMs:    500,
	}, &Payload{Event: "test"})
	if res.Success {
		t.Fatal("cancelled delivery succeeded")
	}
}

func TestValidateTemplate(t *testing.T) {
	if err := ValidateTemplate(""); err != nil {
		t.Errorf("empty template = %v, want nil", err)
	}
	if err := ValidateTemplate(`{"event":"{{.Event}}","server":"{{.Server.Name}}"}`); err != nil {
		t.Errorf("valid template = %v, want nil", err)
	}
	if err := ValidateTemplate(`{{.Nope`); err == nil {
		t.Error("broken template accepted")
	}
	if err := ValidateTemplate(`{{.Server.MissingField}}`); err == nil {
		t.Error("template with missing field accepted (sample render should fail)")
	}
}

func TestRenderBody_DefaultAndTemplate(t *testing.T) {
	payload := &Payload{Event: "server_start"}

	body, err := renderBody(Config{}, payload)
	if err != nil {
		t.Fatalf("default render failed: %v", err)
	}
	if !strings.Contains(string(body), "server_start") {
		t.Errorf("default body = %s", body)
	}

	body, err = renderBody(Config{PayloadTemplate: `{"ping":"{{.event}}","server":"{{.server_name}}"}`}, payload)
	if err != nil {
		t.Fatalf("template render failed: %v", err)
	}
	if string(body) != `{"ping":"server_start","server":""}` {
		t.Errorf("template body = %q", body)
	}

	if _, err := renderBody(Config{PayloadTemplate: `{{.Nope`}, payload); err == nil {
		t.Error("broken template rendered without error")
	}
}

func TestSign_Deterministic(t *testing.T) {
	a := sign([]byte("body"), "secret")
	b := sign([]byte("body"), "secret")
	if a == "" || a != b {
		t.Errorf("sign not deterministic: %q vs %q", a, b)
	}
	if sign([]byte("body"), "other") == a {
		t.Error("different secret produced identical signature")
	}
}

func TestBuildPayload_ServerSnapshot(t *testing.T) {
	p := BuildPayload("server_stop", &storage.Server{
		ID: "srv-1", Name: "S", MCVersion: "1.21",
	}, map[string]any{"k": "v"})
	if p.Event != "server_stop" || p.Server == nil || p.Server.ID != "srv-1" {
		t.Errorf("payload = %+v", p)
	}
	if p.Data["k"] != "v" {
		t.Errorf("data = %v", p.Data)
	}

	bare := BuildPayload("ping", nil, nil)
	if bare.Server != nil {
		t.Errorf("nil server payload = %+v", bare.Server)
	}
}
