// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// ruleTarget returns the -j target of an iptables argument slice ("" if none).
func ruleTarget(rule []string) string {
	for i, tok := range rule {
		if tok == "-j" && i+1 < len(rule) {
			return rule[i+1]
		}
	}
	return ""
}

func indexOfTarget(rules [][]string, target string) int {
	for i, r := range rules {
		if ruleTarget(r) == target {
			return i
		}
	}
	return -1
}

func TestBuildPortRules(t *testing.T) {
	fm := NewFirewallManager(nil, true, 10, 20)
	rules := fm.BuildPortRules(25565, 10, 20)

	if len(rules) != 4 {
		t.Fatalf("expected 4 firewall rules, got %d", len(rules))
	}

	// 1. Per-source burst DROP must come first so repeat offenders are rejected
	//    before their connection is recorded.
	if tgt := ruleTarget(rules[0]); tgt != "DROP" || !contains(rules[0], "--update") {
		t.Errorf("rule 0 should be the per-source recent DROP, got %v", rules[0])
	}

	// 2. Per-source recent tracking.
	if !contains(rules[1], "MC_CONN_25565") || !contains(rules[1], "--set") {
		t.Errorf("rule 1 should be the recent --set tracking rule, got %v", rules[1])
	}

	// 3. Rate-limited ACCEPT must precede the catch-all SYN DROP, otherwise the
	//    port is blackholed.
	if tgt := ruleTarget(rules[2]); tgt != "ACCEPT" || !contains(rules[2], "limit") {
		t.Errorf("rule 2 should be the rate-limited ACCEPT, got %v", rules[2])
	}
	if tgt := ruleTarget(rules[3]); tgt != "DROP" || !contains(rules[3], "SYN") {
		t.Errorf("rule 3 should be the catch-all SYN DROP, got %v", rules[3])
	}
}

// TestPortRulePrecedence is the regression guard for the inverted-chain bug:
// the blanket SYN DROP must never be evaluated before the rate-limited ACCEPT.
func TestPortRulePrecedence(t *testing.T) {
	fm := NewFirewallManager(nil, true, 10, 20)
	rules := fm.BuildPortRules(25565, 10, 20)

	acceptIdx := indexOfTarget(rules, "ACCEPT")
	dropIdx := -1
	for i, r := range rules {
		if ruleTarget(r) == "DROP" && contains(r, "SYN") {
			dropIdx = i
			break
		}
	}
	if acceptIdx == -1 || dropIdx == -1 {
		t.Fatalf("expected both ACCEPT and SYN DROP rules, got %v", rules)
	}
	if acceptIdx > dropIdx {
		t.Fatalf("catch-all SYN DROP (index %d) precedes rate-limited ACCEPT (index %d): port would be blackholed", dropIdx, acceptIdx)
	}
}

// TestBuildPortRulesHonoursRatePerMin guards against the parameter being dead
// (it used to be hardcoded to 30/s and burst 60).
func TestBuildPortRulesHonoursRatePerMin(t *testing.T) {
	fm := NewFirewallManager(nil, true, 10, 20)

	low := fm.BuildPortRules(25565, 10, 20)
	high := fm.BuildPortRules(25565, 240, 40)

	if !contains(low[2], "10/minute") {
		t.Errorf("expected limit 10/minute, got %v", low[2])
	}
	if !contains(high[2], "240/minute") {
		t.Errorf("expected limit 240/minute, got %v", high[2])
	}
	if !contains(high[2], "40") {
		t.Errorf("expected burst 40 in limit-burst, got %v", high[2])
	}
	if contains(low[2], "30/s") || contains(low[2], "60") {
		t.Errorf("hardcoded 30/s / burst 60 still present: %v", low[2])
	}
}

func TestBuildIsolationRulesOrdersAllowBeforeDrop(t *testing.T) {
	fm := NewFirewallManager(nil, true, 10, 20)
	rules := fm.BuildIsolationRules(25565)

	if len(rules) == 0 {
		t.Fatal("no isolation rules generated")
	}
	last := rules[len(rules)-1]
	if ruleTarget(last) != "DROP" {
		t.Fatalf("isolation rules must end with the catch-all DROP, got %v", last)
	}
	for i, r := range rules[:len(rules)-1] {
		if ruleTarget(r) != "ACCEPT" {
			t.Errorf("rule %d should be ACCEPT, got %v", i, r)
		}
	}
}

// TestApplyPortRateLimitingPreservesRuleOrder runs ApplyPortRateLimiting against
// a stub iptables that records every invocation, and asserts rules are inserted
// at increasing positions (1,2,3,...) rather than all at position 1.
func TestApplyPortRateLimitingPreservesRuleOrder(t *testing.T) {
	logPath := t.TempDir() + string(os.PathSeparator) + "iptables.log"
	t.Setenv("CARBONPANEL_IPTABLES_STUB", "1")
	t.Setenv("CARBONPANEL_IPTABLES_STUB_LOG", logPath)
	fm := &FirewallManager{
		enabled: true, defaultRateMin: 10, defaultBurst: 20,
		iptablesPath: os.Args[0], commandPrefix: []string{"-test.run=TestIptablesStubProcess", "--"},
	}
	if err := fm.ApplyPortRateLimiting(context.Background(), 25565, 10, 20); err != nil {
		t.Fatalf("ApplyPortRateLimiting: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	var inserts [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		args := strings.Fields(line)
		for i, a := range args {
			if a == "-I" && i+2 < len(args) {
				inserts = append(inserts, args[i:])
			}
		}
	}

	want := fm.BuildPortRules(25565, 10, 20)
	if len(inserts) != len(want) {
		t.Fatalf("expected %d inserts, got %d: %v", len(want), len(inserts), inserts)
	}
	for i, ins := range inserts {
		if len(ins) < 3 || ins[0] != "-I" || ins[1] != "DOCKER-USER" {
			t.Fatalf("insert %d malformed: %v", i, ins)
		}
		if pos, err := strconv.Atoi(ins[2]); err != nil || pos != i+1 {
			t.Fatalf("insert %d position = %q, want %d (all-at-position-1 reverses the chain)", i, ins[2], i+1)
		}
		// Compare the rule body after the position argument.
		gotRule := ins[3:]
		wantRule := want[i]
		if strings.Join(gotRule, " ") != strings.Join(wantRule, " ") {
			t.Errorf("insert %d rule mismatch:\n got %v\nwant %v", i, gotRule, wantRule)
		}
	}
}

// TestIptablesStubProcess is a portable executable stand-in for iptables.
// The child test process records calls and simulates absent (-C) rules.
func TestIptablesStubProcess(t *testing.T) {
	if os.Getenv("CARBONPANEL_IPTABLES_STUB") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	file, err := os.OpenFile(os.Getenv("CARBONPANEL_IPTABLES_STUB_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Join(args, " ") + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if arg == "-C" {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func TestFirewallManager_DisabledOrNoBinary(t *testing.T) {
	fm := NewFirewallManager(nil, false, 10, 20)
	ctx := context.Background()

	// Should safely return nil without error when disabled
	if err := fm.ApplyPortRateLimiting(ctx, 25565, 10, 20); err != nil {
		t.Errorf("expected nil error on disabled firewall, got %v", err)
	}

	if err := fm.RemovePortRateLimiting(ctx, 25565, 10, 20); err != nil {
		t.Errorf("expected nil error on disabled firewall remove, got %v", err)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
