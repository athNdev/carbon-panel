package services

import (
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// Confinement wiring tests: CreateServer/UpdateServer must reject
// host-breakout DockerOverrides (host-path binds outside the server data
// path, host devices, host/none networking) with CodeInvalidArgument for ALL
// callers — including holders of the elevated docker permission — while the
// legitimate creation path (no overrides, benign overrides, named volumes,
// binds inside the server data path) still succeeds.

func hostileConfinementOverrides() *v1.DockerOverrides {
	return &v1.DockerOverrides{
		Volumes:     []*v1.VolumeMount{{Source: "/", Target: "/host"}},
		Devices:     []string{"/dev/kvm:/dev/kvm"},
		NetworkMode: "host",
	}
}

func TestCreateServer_HostileVolumes_RejectedEvenWithElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("docker-admin")

	_, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:            "evil-volumes",
		NodeId:          "test-node",
		McVersion:       "1.20.1",
		DockerImage:     "java21",
		Port:            25571,
		DockerOverrides: hostileConfinementOverrides(),
	}))
	if err == nil {
		t.Fatal("expected CreateServer to reject host bind mount even for elevated caller")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestCreateServer_NamedVolumes_AllowedWithoutElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	resp, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "named-vol-server",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "java21",
		Port:        25572,
		DockerOverrides: &v1.DockerOverrides{
			Volumes: []*v1.VolumeMount{
				{Source: "mydata", Target: "/data/extra", Type: "volume"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("expected CreateServer with a named volume to succeed, got %v", err)
	}
	if resp.Msg.Server == nil {
		t.Fatal("expected created server in response")
	}
}

func TestUpdateServer_HostileVolumes_RejectedEvenWithElevatedPermission(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	adminCtx := ctxWithRole("docker-admin")

	created, err := svc.CreateServer(adminCtx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "confinement-target",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "java21",
		Port:        25573,
	}))
	if err != nil {
		t.Fatalf("failed to create server fixture: %v", err)
	}

	_, err = svc.UpdateServer(adminCtx, connect.NewRequest(&v1.UpdateServerRequest{
		Id:              created.Msg.Server.Id,
		DockerOverrides: hostileConfinementOverrides(),
	}))
	if err == nil {
		t.Fatal("expected UpdateServer to reject host bind mount even for elevated caller")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestUpdateServer_BindInsideDataPath_Allowed(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	adminCtx := ctxWithRole("docker-admin")

	created, err := svc.CreateServer(adminCtx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "legit-bind-target",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "java21",
		Port:        25574,
	}))
	if err != nil {
		t.Fatalf("failed to create server fixture: %v", err)
	}

	// The server's own data directory remains a legitimate bind source.
	inside := filepath.Join(created.Msg.Server.DataPath, "mods")
	resp, err := svc.UpdateServer(adminCtx, connect.NewRequest(&v1.UpdateServerRequest{
		Id: created.Msg.Server.Id,
		DockerOverrides: &v1.DockerOverrides{
			Volumes: []*v1.VolumeMount{{Source: inside, Target: "/data/mods"}},
		},
	}))
	if err != nil {
		t.Fatalf("expected bind mount inside the server data path to be allowed, got %v", err)
	}
	if resp.Msg.Server == nil {
		t.Fatal("expected updated server in response")
	}
}
