// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// ErrFirewallUnsupported means rate limiting or proxy isolation was requested
// and this host cannot apply DOCKER-USER rules. It is not a successful apply.
var ErrFirewallUnsupported = errors.New("DOCKER-USER firewall enforcement is not available on this host")

// FirewallManager manages DOCKER-USER chain iptables rules for Minecraft ports
type FirewallManager struct {
	log            *logger.Logger
	enabled        bool
	defaultRateMin int
	defaultBurst   int
	iptablesPath   string
	commandPrefix  []string
}

func (f *FirewallManager) iptablesCommand(ctx context.Context, args ...string) *exec.Cmd {
	commandArgs := make([]string, 0, len(f.commandPrefix)+len(args))
	commandArgs = append(commandArgs, f.commandPrefix...)
	commandArgs = append(commandArgs, args...)
	return exec.CommandContext(ctx, f.iptablesPath, commandArgs...)
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

// BuildPortRules generates the DOCKER-USER iptables arguments for SYN flood and
// per-source connection rate limiting on a published game port.
//
// The returned slice is in evaluation order and both ratePerMin and burst are
// honoured. ApplyPortRateLimiting inserts the rules while preserving this
// order, so the per-source DROP and the rate-limited ACCEPT are reached before
// the catch-all SYN DROP.
func (f *FirewallManager) BuildPortRules(port int, ratePerMin, burst int) [][]string {
	if ratePerMin <= 0 {
		ratePerMin = f.defaultRateMin
	}
	if burst <= 0 {
		burst = f.defaultBurst
	}

	portStr := strconv.Itoa(port)
	burstStr := strconv.Itoa(burst)
	limitStr := fmt.Sprintf("%d/minute", ratePerMin)
	recentName := fmt.Sprintf("MC_CONN_%d", port)

	return [][]string{
		// 1. Drop further NEW connections from a source that already opened
		//    `burst` connections within the last 60s (per-source limiter).
		{"-p", "tcp", "--dport", portStr, "-m", "state", "--state", "NEW", "-m", "recent", "--update", "--seconds", "60", "--hitcount", burstStr, "--name", recentName, "-j", "DROP"},
		// 2. Record this source as having opened a NEW connection.
		{"-p", "tcp", "--dport", portStr, "-m", "state", "--state", "NEW", "-m", "recent", "--set", "--name", recentName},
		// 3. Accept SYN packets while within the configured global rate.
		{"-p", "tcp", "--dport", portStr, "--tcp-flags", "SYN,ACK,FIN,RST", "SYN", "-m", "limit", "--limit", limitStr, "--limit-burst", burstStr, "-j", "ACCEPT"},
		// 4. Drop SYN packets that exceeded the global rate (SYN-flood guard).
		{"-p", "tcp", "--dport", portStr, "--tcp-flags", "SYN,ACK,FIN,RST", "SYN", "-j", "DROP"},
	}
}

func (f *FirewallManager) enforceReady() error {
	if f == nil || !f.enabled {
		return nil
	}
	if f.iptablesPath == "" {
		return ErrFirewallUnsupported
	}
	return nil
}

// ApplyPortRateLimiting applies DOCKER-USER chain rules for the given port.
//
// Rules are inserted at increasing positions (1, 2, 3, ...) so the chain order
// matches BuildPortRules' evaluation order. Inserting every rule at position 1
// would reverse the order and put the catch-all DROP first, blackholing the
// port. A requested apply on a host with no iptables returns
// ErrFirewallUnsupported instead of a successful result.
func (f *FirewallManager) ApplyPortRateLimiting(ctx context.Context, port int, ratePerMin, burst int) error {
	if err := f.enforceReady(); err != nil {
		return err
	}
	if f == nil || !f.enabled {
		return nil
	}

	rules := f.BuildPortRules(port, ratePerMin, burst)

	for i, ruleArgs := range rules {
		// Check if rule already exists (-C)
		checkArgs := append([]string{"-C", "DOCKER-USER"}, ruleArgs...)
		cmdCheck := f.iptablesCommand(ctx, checkArgs...)
		if err := cmdCheck.Run(); err == nil {
			// Rule already exists
			continue
		}

		// Insert rule at its intended position
		position := strconv.Itoa(i + 1)
		insertArgs := append([]string{"-I", "DOCKER-USER", position}, ruleArgs...)
		cmdInsert := f.iptablesCommand(ctx, insertArgs...)
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

// RemovePortRateLimiting removes DOCKER-USER rules for a closed/deleted port.
// The same rate/burst values used when applying must be supplied so the
// generated rules match and can be deleted.
func (f *FirewallManager) RemovePortRateLimiting(ctx context.Context, port, ratePerMin, burst int) error {
	if !f.enabled || f.iptablesPath == "" {
		return nil
	}

	rules := f.BuildPortRules(port, ratePerMin, burst)
	for _, ruleArgs := range rules {
		deleteArgs := append([]string{"-D", "DOCKER-USER"}, ruleArgs...)
		cmdDelete := f.iptablesCommand(ctx, deleteArgs...)
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
	if port <= 0 {
		return nil
	}
	if err := f.enforceReady(); err != nil {
		return err
	}
	if f == nil || !f.enabled {
		return nil
	}

	rules := f.BuildIsolationRules(port)
	for i, ruleArgs := range rules {
		checkArgs := append([]string{"-C", "DOCKER-USER"}, ruleArgs...)
		cmdCheck := f.iptablesCommand(ctx, checkArgs...)
		if err := cmdCheck.Run(); err == nil {
			continue
		}

		// Insert at increasing positions so the allow rules are evaluated
		// before the catch-all DROP (see BuildIsolationRules).
		position := strconv.Itoa(i + 1)
		insertArgs := append([]string{"-I", "DOCKER-USER", position}, ruleArgs...)
		cmdInsert := f.iptablesCommand(ctx, insertArgs...)
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
		cmdDelete := f.iptablesCommand(ctx, deleteArgs...)
		_ = cmdDelete.Run()
	}
	return nil
}
