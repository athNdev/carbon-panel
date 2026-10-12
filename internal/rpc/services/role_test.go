package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func setupRoleTestService(t *testing.T) *RoleService {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate())

	enforcer, err := rbac.NewEnforcer(store.DB())
	require.NoError(t, err)
	require.NoError(t, enforcer.SeedDefaultPolicies(false))

	return NewRoleService(store, enforcer, logger.New())
}

func connectCode(t *testing.T, err error) connect.Code {
	t.Helper()
	require.Error(t, err)
	connectErr, ok := err.(*connect.Error)
	require.True(t, ok, "expected connect error, got %T", err)
	return connectErr.Code()
}

func TestRoleService_CreateGetList(t *testing.T) {
	svc := setupRoleTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateRole(ctx, connect.NewRequest(&v1.CreateRoleRequest{})); err == nil {
		t.Error("empty name accepted")
	} else {
		assert.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))
	}

	created, err := svc.CreateRole(ctx, connect.NewRequest(&v1.CreateRoleRequest{
		Name: "operator", Description: "ops",
		Permissions: []*v1.Permission{{Resource: "servers", Action: "read", ObjectId: "*"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, "operator", created.Msg.Role.Name)

	got, err := svc.GetRole(ctx, connect.NewRequest(&v1.GetRoleRequest{Id: created.Msg.Role.Id}))
	require.NoError(t, err)
	assert.Len(t, got.Msg.Role.Permissions, 1)

	if _, err := svc.GetRole(ctx, connect.NewRequest(&v1.GetRoleRequest{})); err == nil {
		t.Error("empty id accepted")
	} else {
		assert.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))
	}
	if _, err := svc.GetRole(ctx, connect.NewRequest(&v1.GetRoleRequest{Id: "missing"})); err == nil {
		t.Error("missing role fetched")
	} else {
		assert.Equal(t, connect.CodeNotFound, connectCode(t, err))
	}

	listed, err := svc.ListRoles(ctx, connect.NewRequest(&v1.ListRolesRequest{}))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listed.Msg.Roles), 1)
}

func TestRoleService_UpdateDelete(t *testing.T) {
	svc := setupRoleTestService(t)
	ctx := context.Background()

	created, err := svc.CreateRole(ctx, connect.NewRequest(&v1.CreateRoleRequest{Name: "temp"}))
	require.NoError(t, err)
	id := created.Msg.Role.Id

	desc := "updated desc"
	updated, err := svc.UpdateRole(ctx, connect.NewRequest(&v1.UpdateRoleRequest{
		Id: id, Description: &desc,
	}))
	require.NoError(t, err)
	assert.Equal(t, "updated desc", updated.Msg.Role.Description)

	if _, err := svc.UpdateRole(ctx, connect.NewRequest(&v1.UpdateRoleRequest{})); err == nil {
		t.Error("empty id accepted")
	} else {
		assert.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))
	}
	if _, err := svc.UpdateRole(ctx, connect.NewRequest(&v1.UpdateRoleRequest{Id: "missing"})); err == nil {
		t.Error("missing role updated")
	} else {
		assert.Equal(t, connect.CodeNotFound, connectCode(t, err))
	}

	del, err := svc.DeleteRole(ctx, connect.NewRequest(&v1.DeleteRoleRequest{Id: id}))
	require.NoError(t, err)
	assert.NotEmpty(t, del.Msg.Message)

	if _, err := svc.DeleteRole(ctx, connect.NewRequest(&v1.DeleteRoleRequest{})); err == nil {
		t.Error("empty id accepted")
	} else {
		assert.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))
	}
	if _, err := svc.DeleteRole(ctx, connect.NewRequest(&v1.DeleteRoleRequest{Id: id})); err == nil {
		t.Error("double delete succeeded")
	} else {
		assert.Equal(t, connect.CodeNotFound, connectCode(t, err))
	}
}

func TestRoleService_PermissionMatrix(t *testing.T) {
	svc := setupRoleTestService(t)

	res, err := svc.GetPermissionMatrix(context.Background(), connect.NewRequest(&v1.GetPermissionMatrixRequest{}))
	require.NoError(t, err)
	assert.NotEmpty(t, res.Msg.ResourceActions)
}
