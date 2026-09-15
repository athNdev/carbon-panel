package rpc

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/command"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/internal/metrics"
	"github.com/athNdev/carbon-panel/internal/module"
	"github.com/athNdev/carbon-panel/internal/packwiz"
	"github.com/athNdev/carbon-panel/internal/proxy"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/internal/rpc/handlers"
	"github.com/athNdev/carbon-panel/internal/rpc/services"
	"github.com/athNdev/carbon-panel/internal/scheduler"
	"github.com/athNdev/carbon-panel/internal/ws"
	"github.com/athNdev/carbon-panel/pkg/download"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"github.com/athNdev/carbon-panel/pkg/upload"
	web "github.com/athNdev/carbon-panel/web/carbon-panel"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Server represents the Connect RPC server
type Server struct {
	store            *storage.Store
	docker           *docker.Client
	clientPool       *docker.ClientPool
	placementEngine  *docker.PlacementEngine
	sender           *command.Sender
	config           *config.Config
	log              *logger.Logger
	handler          http.Handler
	proxyManager     *proxy.Manager
	authManager      *auth.Manager
	enforcer         *rbac.Enforcer
	oidcHandler      *auth.OIDCHandler
	logStreamer      *logger.LogStreamer
	scheduler        *scheduler.Scheduler
	metricsCollector *metrics.Collector
	moduleManager    *module.Manager
	bus              *events.Bus
	uploadManager    *upload.Manager
	downloadManager  *download.Manager
	wsHub            *ws.Hub
}

// Creates new Connect RPC server
func NewServer(
	store *storage.Store,
	dockerCli *docker.Client,
	sender *command.Sender,
	cfg *config.Config,
	proxyManager *proxy.Manager,
	sched *scheduler.Scheduler,
	metricsCollector *metrics.Collector,
	moduleManager *module.Manager,
	bus *events.Bus,
	log *logger.Logger,
	clientPool *docker.ClientPool,
	placementEngine *docker.PlacementEngine,
) *Server {
	// Initialize RBAC enforcer
	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		log.Error("Failed to initialize RBAC enforcer: %v", err)
	}
	if enforcer != nil {
		if err := enforcer.SeedDefaultPolicies(cfg.Auth.AnonymousAccess); err != nil {
			log.Error("Failed to seed default policies: %v", err)
		}
	}

	// Initialize auth manager
	authManager, err := auth.NewManager(store, enforcer, &cfg.Auth)
	if err != nil {
		log.Error("Failed to initialize auth manager: %v", err)
	}

	// Initialize OIDC handler
	oidcHandler, err := auth.NewOIDCHandler(authManager, store, &cfg.Auth.OIDC, log)
	if err != nil {
		log.Warn("Failed to initialize OIDC handler: %v", err)
		oidcHandler, _ = auth.NewOIDCHandler(authManager, store, &config.OIDCConfig{}, log)
	}

	// Initialize log streamer
	var logStreamer *logger.LogStreamer
	if dockerCli != nil {
		logStreamer = logger.NewLogStreamer(dockerCli.GetDockerClient(), log, 10000)
		dockerCli.SetLogStreamer(logStreamer)
	}

	// Initialize upload manager
	uploadTTL := time.Duration(cfg.Upload.SessionTTL) * time.Minute
	uploadManager := upload.NewManager(cfg.Storage.TempDir, uploadTTL, cfg.Upload.MaxUploadSize, log)

	// Initialize download manager
	downloadManager := download.NewManager(cfg.Storage.TempDir, uploadTTL, log)

	// Initialize WebSocket hub
	wsHub := ws.NewHub(logStreamer, authManager, enforcer, store, dockerCli, sender, log)
	go wsHub.Run()

	if clientPool == nil && dockerCli != nil {
		clientPool = docker.NewClientPool(store, dockerCli, log)
	}
	if placementEngine == nil && store != nil {
		placementEngine = docker.NewPlacementEngine(store)
	}

	s := &Server{
		store:            store,
		docker:           dockerCli,
		clientPool:       clientPool,
		placementEngine:  placementEngine,
		sender:           sender,
		config:           cfg,
		log:              log,
		proxyManager:     proxyManager,
		authManager:      authManager,
		enforcer:         enforcer,
		oidcHandler:      oidcHandler,
		logStreamer:      logStreamer,
		scheduler:        sched,
		metricsCollector: metricsCollector,
		moduleManager:    moduleManager,
		bus:              bus,
		uploadManager:    uploadManager,
		downloadManager:  downloadManager,
		wsHub:            wsHub,
	}

	s.setupHandler()
	return s
}

// Setup all Connect RPC handlers
func (s *Server) setupHandler() {
	mux := http.NewServeMux()

	// Configure Connect options
	interceptors := []connect.Interceptor{
		s.loggingInterceptor(),
		s.authInterceptor(),
	}

	opts := []connect.HandlerOption{
		connect.WithInterceptors(interceptors...),
		// Enable gRPC, gRPC-Web, and Connect protocols
		connect.WithHandlerOptions(
			connect.WithCompression("gzip", nil, nil),
		),
	}

	// Register all service handlers
	s.registerServices(mux, opts)

	// Add reflection for gRPC clients
	reflector := grpcreflect.NewStaticReflector(
		carbonpanelv1connect.AuthServiceName,
		carbonpanelv1connect.ConfigServiceName,
		carbonpanelv1connect.FileServiceName,
		carbonpanelv1connect.MinecraftServiceName,
		carbonpanelv1connect.ModServiceName,
		carbonpanelv1connect.ModpackServiceName,
		carbonpanelv1connect.ModuleServiceName,
		carbonpanelv1connect.ProxyServiceName,
		carbonpanelv1connect.RoleServiceName,
		carbonpanelv1connect.ServerServiceName,
		carbonpanelv1connect.SupportServiceName,
		carbonpanelv1connect.TaskServiceName,
		carbonpanelv1connect.UploadServiceName,
		carbonpanelv1connect.UserServiceName,
	)
	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	// Register WebSocket handler
	mux.Handle("/ws", s.wsHub)

	// Register OIDC HTTP handlers
	if s.oidcHandler != nil && s.oidcHandler.IsEnabled() {
		mux.HandleFunc("/api/v1/auth/oidc/login", s.oidcHandler.HandleLogin)
		mux.HandleFunc("/api/v1/auth/oidc/callback", s.oidcHandler.HandleCallback)
	}

	// Streaming file upload endpoint
	mux.Handle("/api/v1/upload/", handlers.NewUploadStreamHandler(s.uploadManager, s.authManager, s.enforcer, s.log))

	// Streaming file download endpoint
	mux.Handle("/api/v1/download/", handlers.NewDownloadStreamHandler(s.downloadManager, s.authManager, s.enforcer, s.log))

	// Serve dynamic OpenAPI spec
	mux.HandleFunc("/api/v1/openapi.yaml", handlers.NewOpenAPIHandler(s.log, s.authManager.IsAnyAuthEnabled))

	// API key and credentials validation endpoint (MINE-24)
	mux.Handle("/api/v1/settings/validate-key", handlers.NewKeyValidatorHandler(s.authManager, s.enforcer, s.log))

	// Docker daemon auto-detect scan across local network interfaces
	mux.Handle("/api/v1/nodes/scan", handlers.NewNodeScanHandler(s.store, s.authManager, s.enforcer, s.log))

	// Online mod search and 1-click install endpoints (MINE-25)
	mux.Handle("/api/v1/servers/", handlers.NewModOnlineManager(s.store, s.log, s.authManager, s.enforcer))

	// Packwiz Modpack Studio and Server Deployment Engine (MINE-30, MINE-31)
	packwizDir := filepath.Join(s.config.Storage.DataDir, "packwiz")
	packwizManager := packwiz.NewManager(packwizDir, s.log)
	mux.Handle("/api/v1/packwiz/", handlers.NewPackwizHandler(packwizManager, s.store, s.log, s.authManager, s.enforcer))

	// Scheduled/staged config rollout for instances (staged diffs applied on restart or cron)
	mux.Handle("/api/v1/staged-config/", handlers.NewStagedConfigHandler(s.store, s.log, s.authManager, s.enforcer))

	// Serve frontend for non-RPC routes
	s.setupFrontend(mux)

	// h2c HTTP/2 cleartext
	s.handler = h2c.NewHandler(mux, &http2.Server{})
}

// Registers all Connect RPC service handlers
func (s *Server) registerServices(mux *http.ServeMux, opts []connect.HandlerOption) {
	// Create service instances
	authService := services.NewAuthService(s.store, s.authManager, s.enforcer, s.oidcHandler, s.log)
	configService := services.NewConfigService(s.store, s.config, s.docker, s.log)
	fileService := services.NewFileService(s.store, s.docker, s.uploadManager, s.downloadManager, s.log)
	minecraftService := services.NewMinecraftService(s.store, s.docker, s.log)
	modService := services.NewModService(s.store, s.docker, s.uploadManager, s.log)
	modpackService := services.NewModpackService(s.store, s.config, s.uploadManager, s.log)
	nodeService := services.NewNodeService(s.store, s.clientPool, s.log)
	proxyService := services.NewProxyService(s.store, s.docker, s.proxyManager, s.config, s.logStreamer, s.log)
	serverService := services.NewServerService(s.store, s.docker, s.sender, s.config, s.proxyManager, s.logStreamer, s.metricsCollector, s.moduleManager, s.bus, s.log, s.clientPool, s.placementEngine, s.enforcer)
	supportService := services.NewSupportService(s.store, s.docker, s.config, s.log)
	taskService := services.NewTaskService(s.store, s.scheduler, s.log)
	userService := services.NewUserService(s.store, s.authManager, s.log)
	roleService := services.NewRoleService(s.store, s.enforcer, s.log)
	moduleService := services.NewModuleService(s.store, s.docker, s.moduleManager, s.proxyManager, s.authManager, s.config, s.logStreamer, s.log)
	uploadService := services.NewUploadService(s.uploadManager, s.config, s.log)

	// Register service handlers
	authPath, authHandler := carbonpanelv1connect.NewAuthServiceHandler(authService, opts...)
	mux.Handle(authPath, authHandler)

	configPath, configHandler := carbonpanelv1connect.NewConfigServiceHandler(configService, opts...)
	mux.Handle(configPath, configHandler)

	filePath, fileHandler := carbonpanelv1connect.NewFileServiceHandler(fileService, opts...)
	mux.Handle(filePath, fileHandler)

	minecraftPath, minecraftHandler := carbonpanelv1connect.NewMinecraftServiceHandler(minecraftService, opts...)
	mux.Handle(minecraftPath, minecraftHandler)

	modPath, modHandler := carbonpanelv1connect.NewModServiceHandler(modService, opts...)
	mux.Handle(modPath, modHandler)

	modpackPath, modpackHandler := carbonpanelv1connect.NewModpackServiceHandler(modpackService, opts...)
	mux.Handle(modpackPath, modpackHandler)

	proxyPath, proxyHandler := carbonpanelv1connect.NewProxyServiceHandler(proxyService, opts...)
	mux.Handle(proxyPath, proxyHandler)

	serverPath, serverHandler := carbonpanelv1connect.NewServerServiceHandler(serverService, opts...)
	mux.Handle(serverPath, serverHandler)

	nodePath, nodeHandler := carbonpanelv1connect.NewNodeServiceHandler(nodeService, opts...)
	mux.Handle(nodePath, nodeHandler)

	supportPath, supportHandler := carbonpanelv1connect.NewSupportServiceHandler(supportService, opts...)
	mux.Handle(supportPath, supportHandler)

	taskPath, taskHandler := carbonpanelv1connect.NewTaskServiceHandler(taskService, opts...)
	mux.Handle(taskPath, taskHandler)

	userPath, userHandler := carbonpanelv1connect.NewUserServiceHandler(userService, opts...)
	mux.Handle(userPath, userHandler)

	rolePath, roleHandler := carbonpanelv1connect.NewRoleServiceHandler(roleService, opts...)
	mux.Handle(rolePath, roleHandler)

	modulePath, moduleHandler := carbonpanelv1connect.NewModuleServiceHandler(moduleService, opts...)
	mux.Handle(modulePath, moduleHandler)

	uploadPath, uploadHandler := carbonpanelv1connect.NewUploadServiceHandler(uploadService, opts...)
	mux.Handle(uploadPath, uploadHandler)
}

// The HTTP handler for the server
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Creates a Connect interceptor for logging
func (s *Server) loggingInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Skip logging for polling endpoints
			if !s.isPollingProcedure(req.Spec().Procedure) {
				s.log.Info("RPC %s %s", req.Peer().Addr, req.Spec().Procedure)
			}
			return next(ctx, req)
		}
	}
}

// Creates a Connect interceptor for authentication and authorization
func (s *Server) authInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			procedure := req.Spec().Procedure

			// Public procedures - no auth required
			if rbac.PublicProcedures[procedure] {
				return next(ctx, req)
			}

			// Authenticate via shared auth logic
			user, err := s.authManager.AuthenticateFromHeader(ctx, req.Header().Get("Authorization"))
			if err != nil {
				s.log.Debug("Auth: Token validation failed for %s: %v", procedure, err)
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			// Set user in context
			ctx = auth.WithUser(ctx, user)

			// Authenticated-only procedures (no specific resource permission needed)
			if rbac.AuthenticatedOnlyProcedures[procedure] {
				return next(ctx, req)
			}

			// Check resource permission. Every procedure reaching this point
			// must be explicitly covered by rbac.ProcedurePermissions - any
			// procedure that is not public, not authenticated-only, and not
			// in ProcedurePermissions is DENIED by default (fail closed).
			// This also applies if the RBAC enforcer itself failed to
			// initialize: previously a nil enforcer silently skipped the
			// permission check entirely (fail open), which is equally
			// unsafe.
			perm, ok := rbac.ProcedurePermissions[procedure]
			if !ok {
				s.log.Warn("RBAC: denying request for unmapped procedure %s (no public/authenticated-only/permission entry)", procedure)
				return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("procedure %s has no permission mapping and is denied by default", procedure))
			}

			if s.enforcer == nil {
				s.log.Error("RBAC: denying request for %s because the RBAC enforcer is unavailable", procedure)
				return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("rbac enforcer unavailable"))
			}

			objectID := "*"
			if perm.ObjectIDField != "" {
				objectID = extractObjectID(req, perm.ObjectIDField)
			}
			allowed, err := s.enforcer.Enforce(user.Roles, perm.Resource, perm.Action, objectID)
			if err != nil {
				s.log.Error("RBAC enforcement error: %v", err)
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			if !allowed {
				return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("insufficient permissions for %s/%s", perm.Resource, perm.Action))
			}

			return next(ctx, req)
		}
	}
}

// pollingProcedures lists endpoints that are called frequently and should be excluded from logging.
var pollingProcedures = []string{
	"/carbonpanel.v1.AuthService/GetAuthStatus",
	"/carbonpanel.v1.ServerService/ListServers",
	"/carbonpanel.v1.ServerService/GetServer",
	"/carbonpanel.v1.ServerService/GetServerLogs",
	"/carbonpanel.v1.ProxyService/GetProxyStatus",
	"/carbonpanel.v1.SupportService/GetApplicationLogs",
	"/carbonpanel.v1.UploadService/UploadChunk",
	"/carbonpanel.v1.UploadService/GetUploadStatus",
	"/carbonpanel.v1.FileService/GetExtractionStatus",
}

// Checks if a procedure is a polling endpoint or high-frequency endpoint
func (s *Server) isPollingProcedure(procedure string) bool {
	return slices.Contains(pollingProcedures, procedure)
}

// Frontend serving
func (s *Server) setupFrontend(mux *http.ServeMux) {
	// Get frontend source
	fs := s.getFrontendFS()
	if fs == nil {
		s.log.Warn("No frontend found - API only mode")
		return
	}

	// Serve frontend for root path
	mux.Handle("/", s.createFrontendHandler(fs))
}

// Get frontend fs
func (s *Server) getFrontendFS() http.FileSystem {
	// Try embedded frontend first
	if buildFS, err := web.BuildFS(); err == nil {
		s.log.Info("Using embedded frontend")
		return http.FS(buildFS)
	}
	return nil
}

// Create frontend handler
func (s *Server) createFrontendHandler(fs http.FileSystem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only serve frontend for non-Connect paths
		if isConnectPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file directly (static assets like JS, CSS, images)
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		file, err := fs.Open(path)
		if err == nil {
			defer file.Close()
			stat, _ := file.Stat()
			http.ServeContent(w, r, path, stat.ModTime(), file)
			return
		}

		// Serve index.html for client-side routing
		indexFile, err := fs.Open("/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer indexFile.Close()

		stat, _ := indexFile.Stat()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "/index.html", stat.ModTime(), indexFile)
	}
}

// Checks if a path is a Connect RPC path
func isConnectPath(path string) bool {
	// Connect paths start with service names
	connectPrefixes := []string{
		"/carbonpanel.v1.",
		"/grpc.reflection.",
		"/connect.",
	}

	for _, prefix := range connectPrefixes {
		if len(path) > len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// extractObjectID extracts a named string field from a protobuf request message
// using reflection. Falls back to "*" if the field is missing or empty.
func extractObjectID(req connect.AnyRequest, fieldName string) string {
	msg, ok := req.Any().(proto.Message)
	if !ok {
		return "*"
	}
	fd := msg.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(fieldName))
	if fd == nil {
		return "*"
	}
	val := msg.ProtoReflect().Get(fd)
	if str := val.String(); str != "" {
		return str
	}
	return "*"
}

// RecoveryKey returns the current recovery key from the auth manager.
func (s *Server) RecoveryKey() string {
	return s.authManager.GetRecoveryKey()
}

// Starts log streaming for a container
func (s *Server) StartLogStreaming(containerID string) error {
	return s.logStreamer.StartStreaming(containerID)
}
