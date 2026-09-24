// Command cloudcontrold is the Carbon Cloud control-plane binary.
//
// It loads config, initialises observability, opens the store, runs
// migrations plus seeds, builds the auth verifier, RBAC engine and audit
// store, then serves the Connect-RPC API until SIGTERM/SIGINT.
//
// Subcommand:
//
//	cloudcontrold healthcheck [url]   probe /healthz, exit 0/1 (compose healthcheck)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/billing"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/config"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/httpapi"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/node"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodetype"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/notify"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/obs"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provision"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/rbac"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/reconcile"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/svc"
)

// Build metadata, injected via ldflags:
// -X main.version=X -X main.commit=X -X main.buildTime=X.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "healthcheck" {
		url := "http://127.0.0.1:8080/healthz"
		if len(args) > 1 && args[1] != "" {
			url = args[1]
		}
		if err := probeHealth(url); err != nil {
			_, _ = fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
			return 1
		}
		return 0
	}

	flags := flag.NewFlagSet("cloudcontrold", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "config.yaml", "path to configuration file")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if err := serve(*configPath, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "cloudcontrold fatal: %v\n", err)
		return 1
	}
	return 0
}

// probeHealth GETs url and requires HTTP 200 with a body naming status ok.
func probeHealth(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), `"ok"`) {
		return fmt.Errorf("GET %s: unhealthy body", url)
	}
	return nil
}

func serve(configPath string, stderr io.Writer) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := obs.NewLogger(cfg.Telemetry)
	logger.Info("starting cloudcontrold", "version", version, "commit", commit)

	store, err := db.Open(db.Options{
		Driver:          cfg.Database.Driver,
		DSN:             cfg.Database.URL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	ctx := context.Background()
	if err := store.SeedDefaults(ctx); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	secProvider, err := secrets.FromConfig(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secrets: %w", err)
	}

	catalog := nodetype.NewCatalog(store)
	if err := catalog.EnsureSeeded(ctx); err != nil {
		return fmt.Errorf("seed node types: %w", err)
	}

	// Clerk sessions are a capability, not a boot requirement: without an
	// issuer only API-key (and public) callers authenticate.
	var verifier auth.Verifier
	if strings.TrimSpace(cfg.Clerk.Issuer) != "" {
		v, err := auth.NewClerkVerifier(auth.ClerkConfig{
			Issuer:            cfg.Clerk.Issuer,
			JWKSURL:           cfg.Clerk.JWKSURL,
			Audience:          cfg.Clerk.Audience,
			AuthorizedParties: cfg.Clerk.AuthorizedParties,
			CacheTTL:          cfg.Clerk.JWKSCacheTTL,
		})
		if err != nil {
			return fmt.Errorf("clerk verifier: %w", err)
		}
		verifier = v
	} else {
		logger.Warn("clerk issuer unset: session JWTs disabled, API keys only")
	}

	nodeDeps := node.Deps{Store: store, Secrets: secProvider}
	nodes := node.NewService(nodeDeps)
	joins := node.NewJoinTokenService(nodeDeps)

	// The provisioner degrades to Unavailable (not fatal) when its
	// workspace config is incomplete: the registry/API stay up.
	var provSvc *provision.Service
	provOpts := provision.OptionsFromConfig(cfg.Provisioner, store, secProvider, provision.ExecRunner{Binary: cfg.Provisioner.TerraformPath})
	if ps, err := provision.NewService(provOpts); err != nil {
		logger.Warn("provisioner disabled", "err", err)
	} else {
		provSvc = ps
	}

	audits := audit.NewGormStore(store)

	engine, err := rbac.New(rbac.Options{Source: &httpapi.BindingSource{Store: store}})
	if err != nil {
		return fmt.Errorf("rbac engine: %w", err)
	}

	billingEnforcer := billing.NewQuotaEnforcer(billing.NewCatalog())
	notifier := notify.NewDispatcher(nil)
	outbox := notify.NewOutbox(nil)

	reconciler := reconcile.New(reconcile.Options{
		Store:    store,
		Notifier: notifier,
		Outbox:   outbox,
		Logger:   logger,
	})

	recCtx, stopRec := context.WithCancel(context.Background())
	defer stopRec()
	go reconciler.Run(recCtx, 15*time.Second)

	services, err := svc.New(svc.Deps{
		Store:           store,
		Nodes:           nodes,
		JoinTokens:      joins,
		Catalog:         catalog,
		Provision:       provSvc,
		Audits:          audits,
		Billing:         billingEnforcer,
		Notifier:        notifier,
		Outbox:          outbox,
		ControlPlaneURL: cfg.Server.PublicURL,
		Version:         version,
		Commit:          commit,
		BuildTime:       buildTime,
	})
	if err != nil {
		return fmt.Errorf("services: %w", err)
	}

	rateLimiter := auth.NewRateLimiter(auth.DefaultRateLimitConfig())
	defer rateLimiter.Close()
	httpapi.RateLimit = rateLimiter.Middleware

	srv, err := httpapi.New(httpapi.Options{
		Store:    store,
		Services: services,
		Verifier: verifier,
		Engine:   engine,
		Audits:   audits,
		Metrics:  obs.NewMetrics(),
		Logger:   logger,
		Version:  version,
	})
	if err != nil {
		return fmt.Errorf("httpapi: %w", err)
	}

	addr := cfg.Server.Addr
	if addr == "" {
		addr = ":8080"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	defer func() { _ = ln.Close() }()

	// Connect-RPC needs HTTP/2 (gRPC wire). When TLS terminates at an
	// upstream edge (Traefik, Cloudflare) we speak h2c so standard HTTP/2
	// clients work without TLS on the loopback.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	readTimeout := cfg.Server.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 10 * time.Second
	}
	httpServer := &http.Server{
		Handler:           srv.Handler(),
		Protocols:         protocols,
		ReadHeaderTimeout: readTimeout,
		IdleTimeout:       120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("cloudcontrold listening", "addr", addr)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case sig := <-stop:
		logger.Info("shutting down on signal", "signal", sig.String())
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("cloudcontrold stopped cleanly")
	return nil
}
