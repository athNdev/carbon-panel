package audit

import (
	"testing"

	cloudv1connect "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/stretchr/testify/require"
)

func TestActionNameExample(t *testing.T) {
	t.Parallel()
	require.Equal(t, "nodes.delete", ActionName(cloudv1connect.NodeServiceDeleteNodeProcedure))
}

func TestEveryProcedureHasAction(t *testing.T) {
	t.Parallel()
	procs := AllProcedures()
	require.Len(t, procs, 67)
	for _, p := range procs {
		a := ActionName(p)
		require.NotEmpty(t, a, "procedure %s has no action", p)
		require.Contains(t, a, ".", "action %s must be <service>.<verb>", a)
	}
}

func TestUnknownProcedure(t *testing.T) {
	t.Parallel()
	require.Empty(t, ActionName("/cloud.v1.NopeService/Nope"))
	require.False(t, IsMutating("/cloud.v1.NopeService/Nope"))
	require.False(t, IsMutating(""))
}

func TestMutatingTable(t *testing.T) {
	t.Parallel()
	for _, p := range MutatingProcedures() {
		require.NotEmpty(t, ActionName(p), "mutating %s must have an action", p)
	}
	// Spot checks: writes mutate, reads do not.
	mutating := []string{
		cloudv1connect.NodeServiceDeleteNodeProcedure,
		cloudv1connect.OrgServiceCreateOrgProcedure,
		cloudv1connect.WorkloadServiceSendWorkloadCommandProcedure,
		cloudv1connect.ProvisionServiceApplyProvisionProcedure,
		cloudv1connect.ApiKeyServiceRotateApiKeyProcedure,
		cloudv1connect.RoleServiceCreateRoleBindingProcedure,
		cloudv1connect.AgentServiceJoinNodeProcedure,
		cloudv1connect.ProvisionServicePlanProvisionProcedure,
	}
	for _, p := range mutating {
		require.True(t, IsMutating(p), p)
	}
	readOnly := []string{
		cloudv1connect.NodeServiceListNodesProcedure,
		cloudv1connect.NodeServiceGetNodeProcedure,
		cloudv1connect.AuditServiceListAuditEventsProcedure,
		cloudv1connect.SystemServiceGetBuildInfoProcedure,
		cloudv1connect.SystemServiceGetCapabilitiesProcedure,
		cloudv1connect.WorkloadServiceStreamWorkloadLogsProcedure,
		cloudv1connect.AgentServiceConnectProcedure,
		cloudv1connect.SessionServiceListMyOrgsProcedure,
	}
	for _, p := range readOnly {
		require.False(t, IsMutating(p), p)
	}
}

func TestDefaultResourceType(t *testing.T) {
	t.Parallel()
	require.Equal(t, "nodes", defaultResourceType(cloudv1connect.NodeServiceListNodesProcedure))
	require.Equal(t, "workloads", defaultResourceType(cloudv1connect.WorkloadServiceStartWorkloadProcedure))
	require.Equal(t, "apikeys", defaultResourceType(cloudv1connect.ApiKeyServiceCreateApiKeyProcedure))
	require.Equal(t, "audit", defaultResourceType(cloudv1connect.AuditServiceListAuditEventsProcedure))
	require.Equal(t, "orgs", defaultResourceType(cloudv1connect.OrgServiceGetOrgProcedure))
	require.Equal(t, "provisions", defaultResourceType(cloudv1connect.ProvisionServiceApplyProvisionProcedure))
	require.Equal(t, "rolebindings", defaultResourceType(cloudv1connect.RoleServiceListRolesProcedure))
	require.Equal(t, "sessions", defaultResourceType(cloudv1connect.SessionServiceGetSessionProcedure))
	require.Equal(t, "system", defaultResourceType(cloudv1connect.SystemServiceGetBuildInfoProcedure))
	require.Equal(t, "files", defaultResourceType(cloudv1connect.FileServiceListFilesProcedure))
	require.Empty(t, defaultResourceType("/cloud.v1.NopeService/Nope"))
}
