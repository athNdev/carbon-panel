package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// FirewallManager manages DOCKER-USER chain iptables rules for Minecraft ports
type FirewallManager struct {
	log            *logger.Logger
	enabled        bool
	defaultRateMin int
	defaultBurst   int
	iptablesPath   string
}

// NewFirewallManager creates a new DOCKER-USER firewall manager
func NewFirewallManager(log *logger.Logger, enabled bool, rateMin, burst int) *FirewallManager {
	if rateMin <= 0 {
		rateMin = 10
	}
	if burst <= 0 {
		burst = 20
	}

	iptablesPath, _ := exec.LookPath("iptables")

	return &FirewallManager{
		log:            log,
		enabled:        enabled,
		defaultRateMin: rateMin,
		defaultBurst:   burst,
		iptablesPath:   iptablesPath,
	}
}

// BuildPortRules generates the DOCKER-USER iptables arguments for SYN flood and rate limiting
func (f *FirewallManager) BuildPortRules(port int, ratePerMin, burst int) [][]string {
	if ratePerMin <= 0 {
		ratePerMin = f.defaultRateMin
	}
	if burst <= 0 {
		burst = f.defaultBurst
	}

	portStr := strconv.Itoa(port)
	burstStr := strconv.Itoa(burst)
	recentName := fmt.Sprintf("MC_CONN_%d", port)

	return [][]string{
		// 1. Accept valid TCP SYN packets under burst limit (30/s burst 60)
		{"-p", "tcp", "--dport", portStr, "--tcp-flags", "SYN,ACK,FIN,RST", "SYN", "-m", "limit", "--limit", "30/s", "--limit-burst", "60", "-j", "ACCEPT"},
		// 2. Drop SYN flood attacks exceeding limit before container socket
		{"-p", "tcp", "--dport", portStr, "--tcp-flags", "SYN,ACK,FIN,RST", "SYN", "-j", "DROP"},
		// 3. Track new connections per source IP in recent list
		{"-p", "tcp", "--dport", portStr, "-m", "state", "--state", "NEW", "-m", "recent", "--set", "--name", recentName},
		// 4. Drop connections exceeding burst limit per minute from same source IP
		{"-p", "tcp", "--dport", portStr, "-m", "state", "--state", "NEW", "-m", "recent", "--update", "--seconds", "60", "--hitcount", burstStr, "--name", recentName, "-j", "DROP"},
	}
}

// ApplyPortRateLimiting applies DOCKER-USER chain rules for the given port
func (f *FirewallManager) ApplyPortRateLimiting(ctx context.Context, port int, ratePerMin, burst int) error {
	if !f.enabled || f.iptablesPath == "" {
		if f.log != nil {
			f.log.Debug("iptables DOCKER-USER rate limiting skipped (enabled=%v, iptables=%s)", f.enabled, f.iptablesPath)
		}
		return nil
	}

	rules := f.BuildPortRules(port, ratePerMin, burst)

	for _, ruleArgs := range rules {
		// Check if rule already exists (-C)
		checkArgs := append([]string{"-C", "DOCKER-USER"}, ruleArgs...)
		cmdCheck := exec.CommandContext(ctx, f.iptablesPath, checkArgs...)
		if err := cmdCheck.Run(); err == nil {
			// Rule already exists
			continue
		}

		// Insert rule (-I DOCKER-USER 1)
		insertArgs := append([]string{"-I", "DOCKER-USER", "1"}, ruleArgs...)
		cmdInsert := exec.CommandContext(ctx, f.iptablesPath, insertArgs...)
		if out, err := cmdInsert.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if f.log != nil {
				f.log.Warn("Failed to apply DOCKER-USER iptables rule for port %d: %s (%v)", port, msg, err)
			}
			return fmt.Errorf("iptables rule insert failed: %w (%s)", err, msg)
		}
	}

	if f.log != nil {
		f.log.Info("Successfully applied DOCKER-USER rate limiting & SYN flood protection for port %d", port)
	}

	return nil
}

// RemovePortRateLimiting removes DOCKER-USER rules for a closed/deleted port
func (f *FirewallManager) RemovePortRateLimiting(ctx context.Context, port int) error {
	if !f.enabled || f.iptablesPath == "" {
		return nil
	}

	rules := f.BuildPortRules(port, f.defaultRateMin, f.defaultBurst)
	for _, ruleArgs := range rules {
		deleteArgs := append([]string{"-D", "DOCKER-USER"}, ruleArgs...)
		cmdDelete := exec.CommandContext(ctx, f.iptablesPath, deleteArgs...)
		_ = cmdDelete.Run()
	}

	return nil
}

// BuildIsolationRules generates iptables rules to drop all external traffic to the proxied server port,
// allowing only local loopback (127.0.0.1) and internal private/docker subnets.
func (f *FirewallManager) BuildIsolationRules(port int) [][]string {
	portStr := strconv.Itoa(port)
	return [][]string{
		// 1. Allow connections from loopback (127.0.0.1)
		{"-p", "tcp", "--dport", portStr, "-s", "127.0.0.1", "-j", "ACCEPT"},
		// 2. Allow connections from docker bridge & internal private subnets
		{"-p", "tcp", "--dport", portStr, "-s", "10.0.0.0/8", "-j", "ACCEPT"},
		{"-p", "tcp", "--dport", portStr, "-s", "172.16.0.0/12", "-j", "ACCEPT"},
		{"-p", "tcp", "--dport", portStr, "-s", "192.168.0.0/16", "-j", "ACCEPT"},
		// 3. Drop all outside traffic attempting to bypass Velocity/proxy
		{"-p", "tcp", "--dport", portStr, "-j", "DROP"},
	}
}

// ApplyProxyPortIsolation isolates a proxied server port in the DOCKER-USER chain
func (f *FirewallManager) ApplyProxyPortIsolation(ctx context.Context, port int) error {
	if !f.enabled || f.iptablesPath == "" || port <= 0 {
		return nil
	}

	rules := f.BuildIsolationRules(port)
	for _, ruleArgs := range rules {
		checkArgs := append([]string{"-C", "DOCKER-USER"}, ruleArgs...)
		cmdCheck := exec.CommandContext(ctx, f.iptablesPath, checkArgs...)
		if err := cmdCheck.Run(); err == nil {
			continue
		}

		insertArgs := append([]string{"-I", "DOCKER-USER", "1"}, ruleArgs...)
		cmdInsert := exec.CommandContext(ctx, f.iptablesPath, insertArgs...)
		if out, err := cmdInsert.CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			if f.log != nil {
				f.log.Warn("Failed to apply DOCKER-USER isolation rule for port %d: %s (%v)", port, msg, err)
			}
			return fmt.Errorf("iptables isolation rule insert failed: %w (%s)", err, msg)
		}
	}

	if f.log != nil {
		f.log.Info("Successfully applied DOCKER-USER proxy port isolation guard for port %d", port)
	}
	return nil
}

// RemoveProxyPortIsolation removes proxy port isolation rules
func (f *FirewallManager) RemoveProxyPortIsolation(ctx context.Context, port int) error {
	if !f.enabled || f.iptablesPath == "" || port <= 0 {
		return nil
	}

	rules := f.BuildIsolationRules(port)
	for _, ruleArgs := range rules {
		deleteArgs := append([]string{"-D", "DOCKER-USER"}, ruleArgs...)
		cmdDelete := exec.CommandContext(ctx, f.iptablesPath, deleteArgs...)
		_ = cmdDelete.Run()
	}
	return nil
}

