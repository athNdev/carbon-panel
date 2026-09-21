// Package secrets provides credential lookup for Carbon Cloud.
//
// Values are read through a Provider (env, file, or a chain of providers).
// Placeholder values (see keys.LooksLikePlaceholder) are treated as absent so
// a half-filled .env behaves like a missing key instead of silently sending
// garbage to an API. Missing keys disable capabilities; they never crash the
// process (see ADR 0006).
package secrets

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
)

// ErrNotFound is returned when a key is absent or holds a placeholder value.
var ErrNotFound = errors.New("secrets: not found")

// DefaultPrefix is used when a provider is constructed without an explicit prefix.
const DefaultPrefix = "CARBONCLOUD_"

// Provider resolves credential values by key name.
type Provider interface {
	Kind() string
	Get(ctx context.Context, key string) (string, error)
	Health(ctx context.Context) error
}

// envProvider reads credentials from process environment.
type envProvider struct {
	prefix string
}

// NewEnv returns a Provider that maps a dotted key to PREFIX_DOTTED_PATH,
// e.g. clerk.issuer with prefix CARBONCLOUD_ reads CARBONCLOUD_CLERK_ISSUER.
// An empty prefix selects the default.
func NewEnv(prefix string) Provider {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	return &envProvider{prefix: withTrailingUnderscore(prefix)}
}

func (p *envProvider) Kind() string { return "env" }

// EnvName returns the environment variable name for a dotted key.
func (p *envProvider) EnvName(key string) string {
	return p.prefix + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

func (p *envProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := os.LookupEnv(p.EnvName(key))
	if !ok || keys.LooksLikePlaceholder(v) {
		return "", ErrNotFound
	}
	return v, nil
}

func (p *envProvider) Health(_ context.Context) error { return nil }

// fileProvider reads credentials from a dotenv (KEY=VALUE) or JSON file.
type fileProvider struct {
	path   string
	values map[string]string
}

// NewFile loads path as JSON (object of string values) when it starts with
// '{', otherwise as dotenv KEY=VALUE lines. Entries may use dotted key names
// (clerk.issuer) or env-style names (CARBONCLOUD_CLERK_ISSUER); both resolve.
func NewFile(path string) (Provider, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("secrets: file provider requires a path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", path, err)
	}
	values, err := parseSecretFile(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: parse %s: %w", path, err)
	}
	return &fileProvider{path: path, values: values}, nil
}

func (p *fileProvider) Kind() string { return "file" }

func (p *fileProvider) Get(_ context.Context, key string) (string, error) {
	candidates := []string{
		key,
		DefaultPrefix + strings.ToUpper(strings.ReplaceAll(key, ".", "_")),
		strings.ToUpper(strings.ReplaceAll(key, ".", "_")),
	}
	for _, c := range candidates {
		for _, want := range []string{c, strings.ToLower(c)} {
			if v, ok := p.values[want]; ok {
				if keys.LooksLikePlaceholder(v) {
					return "", ErrNotFound
				}
				return v, nil
			}
		}
	}
	return "", ErrNotFound
}

func (p *fileProvider) Health(_ context.Context) error {
	if _, err := os.Stat(p.path); err != nil {
		return fmt.Errorf("secrets: file %s unreadable: %w", p.path, err)
	}
	return nil
}

func parseSecretFile(raw []byte) (map[string]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]string{}, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
		out := make(map[string]string, len(obj))
		for k, v := range obj {
			out[k] = fmt.Sprintf("%v", v)
		}
		return out, nil
	}
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(trimmed))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[name] = value
	}
	return out, sc.Err()
}

// chainProvider returns the first hit across providers in order.
type chainProvider struct {
	providers []Provider
}

// NewChain returns a Provider that queries each provider in order and returns
// the first value found. All-miss yields ErrNotFound.
func NewChain(providers ...Provider) Provider {
	return &chainProvider{providers: append([]Provider(nil), providers...)}
}

func (p *chainProvider) Kind() string { return "chain" }

func (p *chainProvider) Get(ctx context.Context, key string) (string, error) {
	for _, inner := range p.providers {
		v, err := inner.Get(ctx, key)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return "", err
		}
	}
	return "", ErrNotFound
}

func (p *chainProvider) Health(ctx context.Context) error {
	var first error
	for _, inner := range p.providers {
		if err := inner.Health(ctx); err == nil {
			return nil
		} else if first == nil {
			first = err
		}
	}
	if first != nil {
		return first
	}
	return nil
}

// Resolver answers presence questions over a Provider.
type Resolver struct {
	provider Provider
}

// NewResolver wraps p for capability-style lookups.
func NewResolver(p Provider) *Resolver { return &Resolver{provider: p} }

// Get resolves a single key.
func (r *Resolver) Get(ctx context.Context, key string) (string, error) {
	return r.provider.Get(ctx, key)
}

// Has reports whether a key resolves to a real (non-placeholder) value.
func (r *Resolver) Has(ctx context.Context, key string) bool {
	_, err := r.provider.Get(ctx, key)
	return err == nil
}

// Missing returns the subset of key names that do not resolve.
func (r *Resolver) Missing(ctx context.Context, keyNames []string) []string {
	var missing []string
	for _, k := range keyNames {
		if !r.Has(ctx, k) {
			missing = append(missing, k)
		}
	}
	return missing
}

// Require returns an error naming every missing key, or nil when all resolve.
func (r *Resolver) Require(ctx context.Context, keyNames ...string) error {
	if missing := r.Missing(ctx, keyNames); len(missing) > 0 {
		envs := make([]string, 0, len(missing))
		for _, k := range missing {
			envs = append(envs, envName(k))
		}
		sort.Strings(envs)
		return fmt.Errorf("secrets: missing required keys: %s", strings.Join(envs, ", "))
	}
	return nil
}

// Capability describes whether one capability from keys.Capabilities is usable.
type Capability struct {
	ID           string
	Enabled      bool
	RequiredKeys []string
	MissingKeys  []string
	Detail       string
}

// Capability evaluates a single capability id. Unknown ids are disabled
// (deny by default).
func (r *Resolver) Capability(ctx context.Context, id string) Capability {
	required := keys.RequiredFor(id)
	known := false
	for _, c := range keys.Capabilities() {
		if c == id {
			known = true
			break
		}
	}
	if !known {
		return Capability{
			ID:           id,
			Enabled:      false,
			RequiredKeys: required,
			MissingKeys:  required,
			Detail:       fmt.Sprintf("disabled: unknown capability %q", id),
		}
	}
	var missing []string
	for _, k := range required {
		if !r.Has(ctx, k) {
			missing = append(missing, envName(k))
		}
	}
	sort.Strings(missing)
	cap := Capability{
		ID:           id,
		RequiredKeys: required,
		MissingKeys:  missing,
		Enabled:      len(missing) == 0,
	}
	if cap.Enabled {
		cap.Detail = "enabled: all required keys present"
	} else {
		cap.Detail = fmt.Sprintf("disabled: missing %s", strings.Join(missing, ", "))
	}
	return cap
}

// Capabilities evaluates every capability declared by keys.Capabilities.
func (r *Resolver) Capabilities(ctx context.Context) []Capability {
	ids := keys.Capabilities()
	out := make([]Capability, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.Capability(ctx, id))
	}
	return out
}

// FromConfig builds the Provider selected by cfg. "chain" composes env then
// file (the file leg is skipped when no path is configured so the process
// still boots without secrets). An empty provider name means "env".
func FromConfig(cfg config.Secrets) (Provider, error) {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = DefaultPrefix
	}
	switch cfg.Provider {
	case "", "env":
		return NewEnv(prefix), nil
	case "file":
		return NewFile(cfg.File)
	case "chain":
		providers := []Provider{NewEnv(prefix)}
		if strings.TrimSpace(cfg.File) != "" {
			f, err := NewFile(cfg.File)
			if err != nil {
				return nil, err
			}
			providers = append(providers, f)
		}
		return NewChain(providers...), nil
	default:
		return nil, fmt.Errorf("secrets: unknown provider %q (want env|file|chain)", cfg.Provider)
	}
}

func envName(key string) string {
	return DefaultPrefix + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

func withTrailingUnderscore(prefix string) string {
	if strings.HasSuffix(prefix, "_") {
		return prefix
	}
	return prefix + "_"
}
