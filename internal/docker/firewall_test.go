package docker

import (
	"context"
	"slices"
	"testing"
)

func TestBuildPortRules(t *testing.T) {
	fm := NewFirewallManager(nil, true, 10, 20)
	rules := fm.BuildPortRules(25565, 10, 20)

	if len(rules) != 4 {
		t.Fatalf("expected 4 firewall rules, got %d", len(rules))
	}

	// Verify SYN flood accept rule
	synAccept := rules[0]
	if !slices.Contains(synAccept, "25565") || !slices.Contains(synAccept, "ACCEPT") || !slices.Contains(synAccept, "limit") {
		t.Errorf("unexpected SYN accept rule: %v", synAccept)
	}

	// Verify SYN flood drop rule
	synDrop := rules[1]
	if !slices.Contains(synDrop, "25565") || !slices.Contains(synDrop, "DROP") {
		t.Errorf("unexpected SYN drop rule: %v", synDrop)
	}

	// Verify per-IP recent tracking rule
	recentSet := rules[2]
	if !slices.Contains(recentSet, "MC_CONN_25565") || !slices.Contains(recentSet, "--set") {
		t.Errorf("unexpected recent set rule: %v", recentSet)
	}

	// Verify per-IP burst drop rule
	recentDrop := rules[3]
	if !slices.Contains(recentDrop, "20") || !slices.Contains(recentDrop, "DROP") || !slices.Contains(recentDrop, "--update") {
		t.Errorf("unexpected recent drop rule: %v", recentDrop)
	}
}

func TestFirewallManager_DisabledOrNoBinary(t *testing.T) {
	fm := NewFirewallManager(nil, false, 10, 20)
	ctx := context.Background()

	// Should safely return nil without error when disabled
	if err := fm.ApplyPortRateLimiting(ctx, 25565, 10, 20); err != nil {
		t.Errorf("expected nil error on disabled firewall, got %v", err)
	}

	if err := fm.RemovePortRateLimiting(ctx, 25565); err != nil {
		t.Errorf("expected nil error on disabled firewall remove, got %v", err)
	}
}
