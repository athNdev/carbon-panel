package provision

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// Runner executes terraform/tofu. The fake backs every test so no Terraform
// binary, network or credential is ever required.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// ExecRunner shells out with os/exec. Binary is config.Provisioner.TerraformPath.
type ExecRunner struct {
	Binary string
}

// Run executes Binary args... in dir and returns combined output.
func (r ExecRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	if r.Binary == "" {
		return "", fmt.Errorf("provision: terraform binary is not configured")
	}
	cmd := exec.CommandContext(ctx, r.Binary, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("provision: %s %v: %w: %s", r.Binary, args, err, buf.String())
	}
	return buf.String(), nil
}

// FakeRunner is the test double. Handle, when set, answers every call;
// otherwise calls are logged and answered with canned per-command output.
type FakeRunner struct {
	mu     sync.Mutex
	Calls  []FakeCall
	Handle func(ctx context.Context, dir string, args []string) (string, error)
}

// FakeCall records one invocation.
type FakeCall struct {
	Dir  string
	Args []string
}

// Run records the call and returns canned output.
func (f *FakeRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	f.mu.Lock()
	cp := append([]string(nil), args...)
	f.Calls = append(f.Calls, FakeCall{Dir: dir, Args: cp})
	h := f.Handle
	f.mu.Unlock()
	if h != nil {
		return h(ctx, dir, cp)
	}
	if len(cp) == 0 {
		return "", nil
	}
	switch cp[0] {
	case "plan":
		return "Plan: 1 to add, 0 to change, 0 to destroy.", nil
	case "apply":
		return "Apply complete! Resources: 1 added, 0 changed, 0 destroyed.", nil
	case "destroy":
		return "Destroy complete! Resources: 1 destroyed.", nil
	case "output":
		return `{"public_ip":{"value":"203.0.113.10"}}`, nil
	default:
		return "ok", nil
	}
}

// CommandNames returns the first arg of every recorded call.
func (f *FakeRunner) CommandNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.Calls))
	for _, c := range f.Calls {
		if len(c.Args) > 0 {
			out = append(out, c.Args[0])
		}
	}
	return out
}
