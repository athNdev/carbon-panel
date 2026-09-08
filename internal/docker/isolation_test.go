package docker

import (
	"context"
	"slices"
	"testing"
)

func TestBuildIsolationRules(t *testing.T) {
	fm := NewFirewallManager(nil, true, 10, 20)
	rules := fm.BuildIsolationRules(25565)

	if len(rules) != 5 {
		t.Fatalf("expected 5 isolation rules, got %d", len(rules))
	}

	// 1. Loopback rule
	loopbackRule := rules[0]
	if !slices.Contains(loopbackRule, "127.0.0.1") || !slices.Contains(loopbackRule, "ACCEPT") {
		t.Errorf("expected loopback accept rule, got %v", loopbackRule)
	}

	// 2. Docker/private subnets
	if !slices.Contains(rules[1], "10.0.0.0/8") || !slices.Contains(rules[1], "ACCEPT") {
		t.Errorf("expected 10.0.0.0/8 accept rule, got %v", rules[1])
	}
	if !slices.Contains(rules[2], "172.16.0.0/12") || !slices.Contains(rules[2], "ACCEPT") {
		t.Errorf("expected 172.16.0.0/12 accept rule, got %v", rules[2])
	}
	if !slices.Contains(rules[3], "192.168.0.0/16") || !slices.Contains(rules[3], "ACCEPT") {
		t.Errorf("expected 192.168.0.0/16 accept rule, got %v", rules[3])
	}

	// 3. Drop all external traffic
	dropRule := rules[4]
	if !slices.Contains(dropRule, "DROP") || !slices.Contains(dropRule, "25565") {
		t.Errorf("expected drop all rule, got %v", dropRule)
	}
}

func TestFirewallManager_IsolationDisabled(t *testing.T) {
	fm := NewFirewallManager(nil, false, 10, 20)
	ctx := context.Background()

	if err := fm.ApplyProxyPortIsolation(ctx, 25565); err != nil {
		t.Errorf("expected nil error on disabled firewall, got %v", err)
	}

	if err := fm.RemoveProxyPortIsolation(ctx, 25565); err != nil {
		t.Errorf("expected nil error on disabled firewall remove, got %v", err)
	}
}
