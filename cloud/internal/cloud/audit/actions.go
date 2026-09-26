package audit

import (
	cloudv1connect "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

// actionTable maps every known procedure to a canonical action string.
// Adding an RPC without an action entry makes TestEveryProcedureHasAction fail.
var actionTable = map[string]string{
	cloudv1connect.AgentServiceConnectProcedure:                    "agent.connect",
	cloudv1connect.AgentServiceJoinNodeProcedure:                   "agent.join_node",
	cloudv1connect.AgentServiceRenewCredentialsProcedure:           "agent.renew_credentials",
	cloudv1connect.ApiKeyServiceCreateApiKeyProcedure:              "apikeys.create",
	cloudv1connect.ApiKeyServiceListApiKeysProcedure:               "apikeys.list",
	cloudv1connect.ApiKeyServiceRevokeApiKeyProcedure:              "apikeys.revoke",
	cloudv1connect.ApiKeyServiceRotateApiKeyProcedure:              "apikeys.rotate",
	cloudv1connect.AuditServiceGetAuditEventProcedure:              "audit.get",
	cloudv1connect.AuditServiceListAuditEventsProcedure:            "audit.list",
	cloudv1connect.NodeServiceCreateJoinTokenProcedure:             "nodes.create_join_token",
	cloudv1connect.NodeServiceDeleteNodeProcedure:                  "nodes.delete",
	cloudv1connect.NodeServiceDrainNodeProcedure:                   "nodes.drain",
	cloudv1connect.NodeServiceGetNodeProcedure:                     "nodes.get",
	cloudv1connect.NodeServiceListJoinTokensProcedure:              "nodes.list_join_tokens",
	cloudv1connect.NodeServiceListNodesProcedure:                   "nodes.list",
	cloudv1connect.NodeServiceResumeNodeProcedure:                  "nodes.resume",
	cloudv1connect.NodeServiceRevokeJoinTokenProcedure:             "nodes.revoke_join_token",
	cloudv1connect.NodeServiceUpdateNodeProcedure:                  "nodes.update",
	cloudv1connect.NodeTypeServiceGetNodeTypeProcedure:             "nodetypes.get",
	cloudv1connect.NodeTypeServiceListNodeTypesProcedure:           "nodetypes.list",
	cloudv1connect.NodeTypeServiceListProvidersProcedure:           "nodetypes.list_providers",
	cloudv1connect.OrgServiceCreateOrgProcedure:                    "orgs.create",
	cloudv1connect.OrgServiceDeleteOrgProcedure:                    "orgs.delete",
	cloudv1connect.OrgServiceGetOrgProcedure:                       "orgs.get",
	cloudv1connect.OrgServiceInviteMemberProcedure:                 "orgs.invite_member",
	cloudv1connect.OrgServiceListInvitationsProcedure:              "orgs.list_invitations",
	cloudv1connect.OrgServiceListMembersProcedure:                  "orgs.list_members",
	cloudv1connect.OrgServiceRemoveMemberProcedure:                 "orgs.remove_member",
	cloudv1connect.OrgServiceRevokeInvitationProcedure:             "orgs.revoke_invitation",
	cloudv1connect.OrgServiceUpdateMemberRoleProcedure:             "orgs.update_member_role",
	cloudv1connect.OrgServiceUpdateOrgProcedure:                    "orgs.update",
	cloudv1connect.ProvisionServiceApplyProvisionProcedure:         "provisions.apply",
	cloudv1connect.ProvisionServiceCreateProvisionProcedure:        "provisions.create",
	cloudv1connect.ProvisionServiceDestroyProvisionProcedure:       "provisions.destroy",
	cloudv1connect.ProvisionServiceGetProvisionProcedure:           "provisions.get",
	cloudv1connect.ProvisionServiceListProvisionsProcedure:         "provisions.list",
	cloudv1connect.ProvisionServicePlanProvisionProcedure:          "provisions.plan",
	cloudv1connect.ProvisionServiceStreamProvisionLogsProcedure:    "provisions.stream_logs",
	cloudv1connect.RoleServiceCreateRoleBindingProcedure:           "rbac.create_binding",
	cloudv1connect.RoleServiceDeleteRoleBindingProcedure:           "rbac.delete_binding",
	cloudv1connect.RoleServiceListPermissionsProcedure:             "rbac.list_permissions",
	cloudv1connect.RoleServiceListRoleBindingsProcedure:            "rbac.list_bindings",
	cloudv1connect.RoleServiceListRolesProcedure:                   "rbac.list_roles",
	cloudv1connect.SessionServiceGetSessionProcedure:               "sessions.get",
	cloudv1connect.SessionServiceListMyOrgsProcedure:               "sessions.list_orgs",
	cloudv1connect.SessionServiceListMyPermissionsProcedure:        "sessions.list_permissions",
	cloudv1connect.SystemServiceGetBuildInfoProcedure:              "system.get_build_info",
	cloudv1connect.SystemServiceGetCapabilitiesProcedure:           "system.get_capabilities",
	cloudv1connect.WorkloadServiceCreateWorkloadProcedure:          "workloads.create",
	cloudv1connect.WorkloadServiceDeleteWorkloadProcedure:          "workloads.delete",
	cloudv1connect.WorkloadServiceGetWorkloadProcedure:             "workloads.get",
	cloudv1connect.WorkloadServiceListWorkloadEventsProcedure:      "workloads.list_events",
	cloudv1connect.WorkloadServiceListWorkloadsProcedure:           "workloads.list",
	cloudv1connect.WorkloadServiceRestartWorkloadProcedure:         "workloads.restart",
	cloudv1connect.WorkloadServiceSendWorkloadCommandProcedure:     "workloads.exec",
	cloudv1connect.WorkloadServiceStartWorkloadProcedure:           "workloads.start",
	cloudv1connect.WorkloadServiceStopWorkloadProcedure:            "workloads.stop",
	cloudv1connect.WorkloadServiceStreamWorkloadLogsProcedure:      "workloads.stream_logs",
	cloudv1connect.WorkloadServiceUpdateWorkloadProcedure:          "workloads.update",
	cloudv1connect.WorkloadServiceGetWorkloadConfigProcedure:       "workloads.get_config",
	cloudv1connect.WorkloadServiceUpdateWorkloadConfigProcedure:    "workloads.update_config",
	cloudv1connect.WorkloadServiceCreateWorkloadBackupProcedure:    "workloads.create_backup",
	cloudv1connect.WorkloadServiceListWorkloadBackupsProcedure:     "workloads.list_backups",
	cloudv1connect.WorkloadServiceRestoreWorkloadBackupProcedure:   "workloads.restore_backup",
	cloudv1connect.WorkloadServiceDeleteWorkloadBackupProcedure:    "workloads.delete_backup",
	cloudv1connect.WorkloadServiceSetWorkloadBackupLockedProcedure: "workloads.set_backup_locked",
	// FileService.
	cloudv1connect.FileServiceListFilesProcedure:       "files.list",
	cloudv1connect.FileServiceStatFileProcedure:        "files.stat",
	cloudv1connect.FileServiceReadFileProcedure:        "files.read",
	cloudv1connect.FileServiceWriteFileProcedure:       "files.write",
	cloudv1connect.FileServiceDeleteFileProcedure:      "files.delete",
	cloudv1connect.FileServiceCreateDirectoryProcedure: "files.create_directory",
}

// mutatingTable marks the procedures that change state. Every one of them is
// audited by Interceptor, success or failure. Read-only procedures (list/get,
// build info, capabilities, log streams, session reads) are never written.
var mutatingTable = map[string]bool{
	cloudv1connect.AgentServiceJoinNodeProcedure:                   true,
	cloudv1connect.AgentServiceRenewCredentialsProcedure:           true,
	cloudv1connect.ApiKeyServiceCreateApiKeyProcedure:              true,
	cloudv1connect.ApiKeyServiceRevokeApiKeyProcedure:              true,
	cloudv1connect.ApiKeyServiceRotateApiKeyProcedure:              true,
	cloudv1connect.NodeServiceCreateJoinTokenProcedure:             true,
	cloudv1connect.NodeServiceDeleteNodeProcedure:                  true,
	cloudv1connect.NodeServiceDrainNodeProcedure:                   true,
	cloudv1connect.NodeServiceResumeNodeProcedure:                  true,
	cloudv1connect.NodeServiceRevokeJoinTokenProcedure:             true,
	cloudv1connect.NodeServiceUpdateNodeProcedure:                  true,
	cloudv1connect.OrgServiceCreateOrgProcedure:                    true,
	cloudv1connect.OrgServiceDeleteOrgProcedure:                    true,
	cloudv1connect.OrgServiceInviteMemberProcedure:                 true,
	cloudv1connect.OrgServiceRemoveMemberProcedure:                 true,
	cloudv1connect.OrgServiceRevokeInvitationProcedure:             true,
	cloudv1connect.OrgServiceUpdateMemberRoleProcedure:             true,
	cloudv1connect.OrgServiceUpdateOrgProcedure:                    true,
	cloudv1connect.ProvisionServiceApplyProvisionProcedure:         true,
	cloudv1connect.ProvisionServiceCreateProvisionProcedure:        true,
	cloudv1connect.ProvisionServiceDestroyProvisionProcedure:       true,
	cloudv1connect.ProvisionServicePlanProvisionProcedure:          true,
	cloudv1connect.RoleServiceCreateRoleBindingProcedure:           true,
	cloudv1connect.RoleServiceDeleteRoleBindingProcedure:           true,
	cloudv1connect.WorkloadServiceCreateWorkloadProcedure:          true,
	cloudv1connect.WorkloadServiceDeleteWorkloadProcedure:          true,
	cloudv1connect.WorkloadServiceRestartWorkloadProcedure:         true,
	cloudv1connect.WorkloadServiceSendWorkloadCommandProcedure:     true,
	cloudv1connect.WorkloadServiceStartWorkloadProcedure:           true,
	cloudv1connect.WorkloadServiceStopWorkloadProcedure:            true,
	cloudv1connect.WorkloadServiceUpdateWorkloadProcedure:          true,
	cloudv1connect.WorkloadServiceUpdateWorkloadConfigProcedure:    true,
	cloudv1connect.WorkloadServiceCreateWorkloadBackupProcedure:    true,
	cloudv1connect.WorkloadServiceRestoreWorkloadBackupProcedure:   true,
	cloudv1connect.WorkloadServiceDeleteWorkloadBackupProcedure:    true,
	cloudv1connect.WorkloadServiceSetWorkloadBackupLockedProcedure: true,
	cloudv1connect.FileServiceWriteFileProcedure:                   true,
	cloudv1connect.FileServiceDeleteFileProcedure:                  true,
	cloudv1connect.FileServiceCreateDirectoryProcedure:             true,
}

// ActionName returns the deterministic action for a procedure, or "" when the
// procedure is unknown. Unknown means deny, never a silent bypass.
func ActionName(procedure string) string { return actionTable[procedure] }

// IsMutating reports whether the procedure changes state and therefore must
// be audited. Unknown procedures return false and are never audited silently;
// the RBAC lane denies them before they reach the audit interceptor.
func IsMutating(procedure string) bool { return mutatingTable[procedure] }

// AllProcedures returns every known procedure.
func AllProcedures() []string {
	out := make([]string, 0, len(actionTable))
	for p := range actionTable {
		out = append(out, p)
	}
	return out
}

// MutatingProcedures returns every mutating procedure.
func MutatingProcedures() []string {
	out := make([]string, 0, len(mutatingTable))
	for p := range mutatingTable {
		out = append(out, p)
	}
	return out
}

// defaultResourceType maps a procedure to its resource type for events where
// the handler supplies no explicit resource.
func defaultResourceType(procedure string) string {
	switch actionTable[procedure] {
	case "":
		return ""
	}
	for _, prefix := range []struct {
		action, resource string
	}{
		{"nodes.", "nodes"}, {"apikeys.", "apikeys"}, {"audit.", "audit"},
		{"nodetypes.", "nodetypes"}, {"orgs.", "orgs"}, {"provisions.", "provisions"},
		{"rbac.", "rolebindings"}, {"sessions.", "sessions"}, {"system.", "system"},
		{"workloads.", "workloads"}, {"agent.", "nodes"}, {"files.", "files"},
	} {
		if len(actionTable[procedure]) >= len(prefix.action) &&
			actionTable[procedure][:len(prefix.action)] == prefix.action {
			return prefix.resource
		}
	}
	return ""
}
