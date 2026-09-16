package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/athNdev/carbon-panel/internal/command"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/internal/metrics"
	"github.com/athNdev/carbon-panel/internal/module"
	"github.com/athNdev/carbon-panel/internal/proxy"
	"github.com/athNdev/carbon-panel/internal/reconciler"
	"github.com/athNdev/carbon-panel/internal/rpc"
	"github.com/athNdev/carbon-panel/internal/scheduler"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func main() {
	var configPath = flag.String("config", "./config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Init logger
	logConfig := &logger.Config{
		Enabled:    cfg.Logging.Enabled,
		FilePath:   cfg.Logging.FilePath,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAge:     cfg.Logging.MaxAge,
		Compress:   cfg.Logging.Compress,
	}
	log := logger.NewWithConfig(logConfig)
	defer log.Close()

	// Create required directories
	dirs := []string{
		cfg.Storage.DataDir,
		cfg.Storage.BackupDir,
		cfg.Storage.TempDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatal("Failed to create directory %s: %v", dir, err)
		}
	}

	// Initialize storage w/ migrations and seeding
	store, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		log.Fatal("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Initialize Docker client with configuration
	dockerClient, err := docker.NewClient(cfg.Docker.Host, log, docker.ClientConfig{
		APIVersion:      cfg.Docker.Version,
		NetworkName:     cfg.Docker.NetworkName,
		RegistryURL:     cfg.Docker.RegistryURL,
		DNS:             cfg.Docker.DNS,
		Labels:          cfg.Docker.Labels,
		EnableRateLimit: cfg.Docker.EnableRateLimit,
		RateLimitPerMin: cfg.Docker.RateLimitPerMin,
		RateLimitBurst:  cfg.Docker.RateLimitBurst,
	})
	if err != nil {
		log.Fatal("Failed to initialize Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Ensure Docker network exists
	if err := dockerClient.EnsureNetwork(); err != nil {
		log.Error("Failed to ensure Docker network: %v", err)
	}

	// Reconcile and clean up containers on startup. This is a two-phase, adopt-then-purge
	// process: containers that match a live DB record (even with a drifted/empty ContainerID)
	// are adopted (DB updated to match reality) and never touched further; only containers with
	// no matching DB record at all are treated as genuine orphans and removed. If the DB can't
	// be read, ReconcileAndCleanupContainers aborts before any deletion happens.
	log.Info("Reconciling containers with database state...")
	if err := docker.ReconcileAndCleanupContainers(ctx, store, dockerClient, log); err != nil {
		log.Error("Failed to reconcile/cleanup containers: %v", err)
	}

	// Load proxy configuration from database
	proxyConfig, isNew, err := store.GetProxyConfig(ctx)
	if err != nil {
		log.Warn("Failed to load proxy config from database, using file config: %v", err)
	} else {
		if isNew {
			proxyConfig.Enabled = cfg.Proxy.Enabled
			proxyConfig.BaseURL = cfg.Proxy.BaseURL
			err = store.SaveProxyConfig(ctx, proxyConfig)
			if err != nil {
				log.Error("Failed to set proxy configs from startup configuration values: %v", err)
			}
		} else {
			cfg.Proxy.Enabled = proxyConfig.Enabled
			cfg.Proxy.BaseURL = proxyConfig.BaseURL
		}

		// Load listeners and build ports array
		listeners, err := store.GetProxyListeners(ctx)
		if err == nil && len(listeners) > 0 {
			listenPorts := make([]int, 0, len(listeners))
			for _, l := range listeners {
				if l.Enabled {
					listenPorts = append(listenPorts, l.Port)
				}
			}
			if len(listenPorts) > 0 {
				cfg.Proxy.ListenPorts = listenPorts
				cfg.Proxy.ListenPort = listenPorts[0]
			}
		}

		log.Info("Loaded proxy configuration from database: enabled=%v, base_url=%v, listeners=%d",
			cfg.Proxy.Enabled, cfg.Proxy.BaseURL, len(cfg.Proxy.ListenPorts))
	}

	// Initialize proxy manager
	proxyManager := proxy.NewManager(store, cfg, log, dockerClient.GetDockerClient())

	// Start proxy if enabled
	if err := proxyManager.Start(); err != nil {
		log.Error("Failed to start proxy manager: %v", err)
	}
	defer proxyManager.Stop()

	// Initialize Docker client pool for multi-node support (created early so
	// downstream components like the command sender, metrics collector, and
	// module manager can resolve per-node Docker clients rather than always
	// targeting the local daemon).
	clientPool := docker.NewClientPool(store, dockerClient, log)
	clientPool.StartHealthChecker(30 * time.Second)
	defer clientPool.StopHealthChecker()

	// Initialize command sender
	sender := command.NewSender(store, cfg, dockerClient, clientPool)

	// Initialize the central event bus
	eventBus := events.NewBus(log)

	// Initialize metrics collector
	metricsCollector := metrics.NewCollectorWithPool(store, dockerClient, clientPool, sender, cfg, eventBus, log)

	// Initialize task scheduler
	taskScheduler := scheduler.NewScheduler(store, dockerClient, sender, cfg, metricsCollector, log, scheduler.Config{
		CheckInterval: time.Duration(cfg.Docker.SyncInterval) * time.Second, // Use same interval as container status monitor
	})

	// Start the scheduler
	if err := taskScheduler.Start(); err != nil {
		log.Error("Failed to start task scheduler: %v", err)
	}
	defer taskScheduler.Stop()

	// Initialize builtin module templates
	if err := module.InitBuiltinTemplates(store); err != nil {
		log.Error("Failed to initialize builtin module templates: %v", err)
	}

	// Initialize module manager
	moduleManager := module.NewManagerWithPool(store, dockerClient, clientPool, sender, cfg, proxyManager, log)
	if err := moduleManager.Start(); err != nil {
		log.Error("Failed to start module manager: %v", err)
	}
	defer moduleManager.Stop()

	// Register event consumers on the event bus - EVENT CONSUMERS REGISTER HERE...
	eventBus.Subscribe(moduleManager.HandleServerEvent)
	eventBus.Subscribe(taskScheduler.HandleServerEvent)

	// Start the metrics collector now that consumers are subscribed
	if err := metricsCollector.Start(); err != nil {
		log.Error("Failed to start metrics collector: %v", err)
	}
	defer metricsCollector.Stop()

	// Initialize placement engine for multi-node support
	placementEngine := docker.NewPlacementEngine(store)

	// Initialize RPC server with full configuration
	rpcServer := rpc.NewServer(store, dockerClient, sender, cfg, proxyManager, taskScheduler, metricsCollector, moduleManager, eventBus, log, clientPool, placementEngine)

	// Print recovery key
	if key := rpcServer.RecoveryKey(); key != "" {
		fmt.Fprintf(os.Stderr, "\nâ•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•\n")
		fmt.Fprintf(os.Stderr, "RECOVERY KEY (use to reset panel access if locked out)\n")
		fmt.Fprintf(os.Stderr, "%s\n", key)
		fmt.Fprintf(os.Stderr, "â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•\n\n")
		keyPath := filepath.Join(cfg.Storage.DataDir, "recovery.key")
		if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
			log.Error("Failed to write recovery key file: %v", err)
		}
	}

	// Auto-start servers that have auto_start enabled
	log.Info("Checking for servers with auto-start enabled...")
	autoStartServers, err := store.ListServers(ctx)
	if err != nil {
		log.Warn("Failed to auto-start server instances due to error: %v\n", err)
	}

	for i := range autoStartServers {
		if autoStartServers[i].AutoStart && !autoStartServers[i].Detached {
			server := autoStartServers[i]
			log.Info("Auto-starting server: %s", server.Name)
			go func() {
				// Wait a moment for everything to initialize
				time.Sleep(2 * time.Second)

				// Get server config
				_, err := store.GetServerConfig(ctx, server.ID)
				if err != nil {
					log.Error("Failed to get config for auto-start server %s: %v", server.Name, err)
					return
				}

				status, err := dockerClient.GetContainerStatus(ctx, server.ContainerID)
				if err != nil {
					log.Error("Failed to find existing container for auto-start server %s: %v", server.Name, err)
					return
				}

				if status == storage.StatusStopped {
					// Start the container
					if err := dockerClient.StartContainer(ctx, server.ContainerID); err != nil {
						log.Error("Failed to start container for auto-start server %s: %v", server.Name, err)
						return
					}
				}

				// Start log streaming for this container (whether it was just started or already running)
				if status == storage.StatusRunning || status == storage.StatusStopped {
					if err := rpcServer.StartLogStreaming(server.ContainerID); err != nil {
						log.Error("Failed to start log streaming for auto-started server %s: %v", server.Name, err)
					}
				}

				// Update server status
				server.Status = storage.StatusRunning
				now := time.Now()
				server.LastStarted = &now
				if err := store.UpdateServer(ctx, server); err != nil {
					log.Error("Failed to update auto-start server %s: %v", server.Name, err)
				}

				// Update proxy route if enabled
				if server.ProxyHostname != "" {
					if err := proxyManager.UpdateServerRoute(server); err != nil {
						log.Error("Failed to update proxy route for auto-started server %s: %v", server.Name, err)
					}
				}

				log.Info("Successfully auto-started server: %s", server.Name)

				// Emit the server-start event
				eventBus.Emit(ctx, events.Event{
					Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
					ServerID: server.ID,
				})
			}()
		}
	}

	// Clean expired sessions on startup, then periodically
	if err := store.CleanExpiredSessions(ctx); err != nil {
		log.Error("Failed to clean expired sessions on startup: %v", err)
	}
	stopSessionCleanup := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := store.CleanExpiredSessions(context.Background()); err != nil {
					log.Error("Failed to clean expired sessions: %v", err)
				}
			case <-stopSessionCleanup:
				return
			}
		}
	}()

	// Start the level-triggered, self-healing reconciler (MINE-107). It replaces
	// the legacy fixed-interval polling monitor: Docker lifecycle events from the
	// multi-node watcher schedule per-server convergence passes (coalesced into a
	// quiet window), backed by a periodic sweep so a dropped event cannot leave a
	// server permanently divergent.
	reconcileCtx, stopReconciler := context.WithCancel(context.Background())

	eventSupervisor, err := reconciler.NewSupervisorFromNodeStore(reconcileCtx, clientPool, store, log)
	if err != nil {
		log.Error("Failed to initialize Docker events supervisor; reconciler will run on sweep only: %v", err)
	}

	var reconcileEvents <-chan reconciler.ContainerEvent
	if eventSupervisor != nil {
		if err := eventSupervisor.Start(reconcileCtx); err != nil {
			log.Error("Failed to start Docker events supervisor: %v", err)
		}
		reconcileEvents = eventSupervisor.Events()
	}

	reconcilerEngine := reconciler.New(
		store,
		reconciler.PoolResolver{Pool: clientPool},
		proxyManager,
		eventBus,
		// The RPC server owns both the log streamer and the WebSocket hub, so it
		// is the concrete LogMigrator that keeps console subscriptions attached
		// across a container recreation (MINE-108).
		rpcServer,
		log,
		reconciler.Config{},
	)
	reconcilerEngine.Start(reconcileCtx, reconcileEvents)
	defer func() {
		stopReconciler()
		reconcilerEngine.Stop()
		reconcilerEngine.Wait()
	}()

	// Setup HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      rpcServer.Handler(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info("Starting Carbon Panel on %s:%s", cfg.Server.Host, cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
	close(stopSessionCleanup)

	// Halt reconciliation before we start stopping containers, so the reconciler
	// cannot fight the deliberate shutdown sequence below.
	reconcilerEngine.Stop()
	stopReconciler()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop managed containers if auto-stop is enabled
	log.Info("Checking for managed containers...")
	managedServers, lsErr := store.ListServers(ctx)
	if lsErr != nil {
		log.Error("Unable to list managed containers prior to shutdown: %v", lsErr)
	}

	for _, server := range managedServers {
		if server.Detached {
			log.Info("Skipping shutdown of detached server: %s", server.Name)
		} else if server.Status == storage.StatusRunning {
			log.Info("Stopping managed container for server: %s", server.Name)
			if _, err := dockerClient.StopContainer(ctx, server.ContainerID); err != nil {
				log.Error("Failed to stop container %s: %v", server.ContainerID, err)
			}
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown: %v", err)
	}

	log.Info("Server stopped\n")
}
