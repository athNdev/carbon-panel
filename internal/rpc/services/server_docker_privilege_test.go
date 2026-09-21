package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// Bug 2 regression tests: CreateServer/UpdateServer must reject
// host-breakout-capable DockerOverrides (Privileged mode, CapAdd,
// confinement-weakening SecurityOpt) unless the caller holds the elevated
// ResourceServers/ActionManageDockerPrivileged permission, while leaving
// benign overrides and ordinary server create/update flows untouched.

func newDockerPrivilegeTestService(t *testing.T) (*ServerService, *rbac.Enforcer) {
	t.Helper()
	store := setupTestStore(t)
	log := logger.New()

	enforcer, err := rbac.NewEnforcer(store.DB())
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}
	// "operator" gets ordinary create/update on servers but nothing else.
	if err := enforcer.SetPermissionsForRole("operator", []rbac.Permission{
		{Resource: rbac.ResourceServers, Action: rbac.ActionCreate, ObjectID: "*"},
		{Resource: rbac.ResourceServers, Action: rbac.ActionUpdate, ObjectID: "*"},
	}); err != nil {
		t.Fatalf("failed to seed operator role: %v", err)
	}
	// "docker-admin" additionally has the elevated permission.
	if err := enforcer.SetPermissionsForRole("docker-admin", []rbac.Permission{
		{Resource: rbac.ResourceServers, Action: rbac.ActionCreate, ObjectID: "*"},
		{Resource: rbac.ResourceServers, Action: rbac.ActionUpdate, ObjectID: "*"},
		{Resource: rbac.ResourceServers, Action: rbac.ActionManageDockerPrivileged, ObjectID: "*"},
	}); err != nil {
		t.Fatalf("failed to seed docker-admin role: %v", err)
	}

	cfg := &config.Config{
		Storage: config.StorageConfig{DataDir: t.TempDir()},
	}

	// No phantom default node exists anymore: seed an enabled node so
	// server creation can be placed explicitly in these tests.
	if err := store.CreateNode(context.Background(), &db.Node{
		ID:      "test-node",
		Name:    "Test Node",
		Host:    "tcp://127.0.0.1:2375",
		Enabled: true,
		Status:  db.NodeStatusOnline,
	}); err != nil {
		t.Fatalf("failed to seed test node: %v", err)
	}

	// CreateServer/UpdateServer kick off container creation on a detached
	// background goroutine that this test never waits on. A nil *docker.Client
	// would nil-pointer-panic inside that goroutine (crashing the whole test
	// binary) the moment it dereferences any field, so give it a real Client
	// pointed at a host nothing is listening on: its calls fail fast with a
	// connection error, which the goroutine already handles gracefully by
	// logging and marking the server as errored.
	dockerCli, err := docker.NewClient("tcp://127.0.0.1:1", log)
	if err != nil {
		t.Fatalf("failed to create docker client stub: %v", err)
	}

	svc := NewServerService(store, dockerCli, nil, cfg, nil, nil, nil, nil, nil, log, nil, nil, enforcer)
	return svc, enforcer
}

func ctxWithRole(role string) context.Context {
	return auth.WithUser(context.Background(), &auth.AuthenticatedUser{
		ID: "test-user", Username: "test-user", Roles: []string{role}, Provider: "local",
	})
}

func dangerousOverrides() *v1.DockerOverrides {
	return &v1.DockerOverrides{
		Privileged:  true,
		CapAdd:      []string{"SYS_ADMIN"},
		SecurityOpt: []string{"apparmor:unconfined"},
	}
}

func TestCreateServer_PrivilegedOverrides_RejectedWithoutElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	_, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:            "test-server",
		McVersion:       "1.20.1",
		DockerImage:     "java21",
		Port:            25566,
		DockerOverrides: dangerousOverrides(),
	}))
	if err == nil {
		t.Fatal("expected CreateServer to reject privileged overrides for a caller without elevated permission")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestCreateServer_PrivilegedOverrides_AllowedWithElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("docker-admin")

	resp, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:            "test-server",
		NodeId:          "test-node",
		McVersion:       "1.20.1",
		DockerImage:     "java21",
		Port:            25566,
		DockerOverrides: dangerousOverrides(),
	}))
	if err != nil {
		t.Fatalf("expected CreateServer to succeed for a caller with elevated permission, got %v", err)
	}
	if resp.Msg.Server == nil || resp.Msg.Server.DockerOverrides == nil || !resp.Msg.Server.DockerOverrides.Privileged {
		t.Errorf("expected created server to retain Privileged:true override")
	}
}

func TestCreateServer_BenignOverrides_AllowedWithoutElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	resp, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:      "test-server-benign",
		NodeId:    "test-node",
		McVersion: "1.20.1",
		DockerImage: "java21",
		Port:      25567,
		DockerOverrides: &v1.DockerOverrides{
			PidsLimit:   1024,
			CpusetCpus:  "0,1",
			ReadOnly:    true,
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges:true"},
		},
	}))
	if err != nil {
		t.Fatalf("expected CreateServer to succeed with only benign overrides, got %v", err)
	}
	if resp.Msg.Server == nil || resp.Msg.Server.DockerOverrides == nil || resp.Msg.Server.DockerOverrides.CpusetCpus != "0,1" {
		t.Errorf("expected created server to retain benign overrides")
	}
}

func TestCreateServer_NoOverrides_UnaffectedByPrivilegeCheck(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	_, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:      "plain-server",
		NodeId:    "test-node",
		McVersion: "1.20.1",
		DockerImage: "java21",
		Port:      25568,
	}))
	if err != nil {
		t.Fatalf("expected CreateServer without DockerOverrides to succeed for ordinary operator, got %v", err)
	}
}

func TestUpdateServer_PrivilegedOverrides_RejectedWithoutElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	adminCtx := ctxWithRole("docker-admin")

	created, err := svc.CreateServer(adminCtx, connect.NewRequest(&v1.CreateServerRequest{
		Name:      "update-target",
		NodeId:    "test-node",
		McVersion: "1.20.1",
		DockerImage: "java21",
		Port:      25569,
	}))
	if err != nil {
		t.Fatalf("failed to create server fixture: %v", err)
	}

	operatorCtx := ctxWithRole("operator")
	_, err = svc.UpdateServer(operatorCtx, connect.NewRequest(&v1.UpdateServerRequest{
		Id:              created.Msg.Server.Id,
		DockerOverrides: dangerousOverrides(),
	}))
	if err == nil {
		t.Fatal("expected UpdateServer to reject privileged overrides for a caller without elevated permission")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestUpdateServer_PrivilegedOverrides_AllowedWithElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	adminCtx := ctxWithRole("docker-admin")

	created, err := svc.CreateServer(adminCtx, connect.NewRequest(&v1.CreateServerRequest{
		Name:      "update-target-2",
		NodeId:    "test-node",
		McVersion: "1.20.1",
		DockerImage: "java21",
		Port:      25570,
	}))
	if err != nil {
		t.Fatalf("failed to create server fixture: %v", err)
	}

	resp, err := svc.UpdateServer(adminCtx, connect.NewRequest(&v1.UpdateServerRequest{
		Id:              created.Msg.Server.Id,
		DockerOverrides: dangerousOverrides(),
	}))
	if err != nil {
		t.Fatalf("expected UpdateServer to succeed for a caller with elevated permission, got %v", err)
	}
	if resp.Msg.Server == nil || resp.Msg.Server.DockerOverrides == nil || !resp.Msg.Server.DockerOverrides.Privileged {
		t.Errorf("expected updated server to retain Privileged:true override")
	}
}
