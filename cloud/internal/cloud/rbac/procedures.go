package rbac

import (
	cloudv1connect "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

// procedurePermissions maps every Connect procedure to the permission it
// requires. The keys reference the generated *Procedure constants so that a
// renamed RPC breaks compilation here instead of silently opening a hole.
// An absent entry denies; never add a procedure without a permission.
var procedurePermissions = map[string]Permission{
	// AgentService. JoinNode is public (see PublicProcedures) but still
	// mapped so TestEveryProcedureIsMapped fails if it is ever removed.
	cloudv1connect.AgentServiceConnectProcedure:          PermAgentConnect,
	cloudv1connect.AgentServiceJoinNodeProcedure:         PermNodesJoin,
	cloudv1connect.AgentServiceRenewCredentialsProcedure: PermAgentCredentials,
	// ApiKeyService.
	cloudv1connect.ApiKeyServiceCreateApiKeyProcedure: PermKeysManage,
	cloudv1connect.ApiKeyServiceListApiKeysProcedure:  PermKeysManage,
	cloudv1connect.ApiKeyServiceRevokeApiKeyProcedure: PermKeysManage,
	cloudv1connect.ApiKeyServiceRotateApiKeyProcedure: PermKeysManage,
	// AuditService.
	cloudv1connect.AuditServiceGetAuditEventProcedure:   PermAuditRead,
	cloudv1connect.AuditServiceListAuditEventsProcedure: PermAuditRead,
	// NodeService.
	cloudv1connect.NodeServiceCreateJoinTokenProcedure: PermNodesJoin,
	cloudv1connect.NodeServiceDeleteNodeProcedure:      PermNodesWrite,
	cloudv1connect.NodeServiceDrainNodeProcedure:       PermNodesWrite,
	cloudv1connect.NodeServiceGetNodeProcedure:         PermNodesRead,
	cloudv1connect.NodeServiceListJoinTokensProcedure:  PermNodesJoin,
	cloudv1connect.NodeServiceListNodesProcedure:       PermNodesRead,
	cloudv1connect.NodeServiceResumeNodeProcedure:      PermNodesWrite,
	cloudv1connect.NodeServiceRevokeJoinTokenProcedure: PermNodesJoin,
	cloudv1connect.NodeServiceUpdateNodeProcedure:      PermNodesWrite,
	// NodeTypeService.
	cloudv1connect.NodeTypeServiceGetNodeTypeProcedure:   PermNodeTypesRead,
	cloudv1connect.NodeTypeServiceListNodeTypesProcedure: PermNodeTypesRead,
	cloudv1connect.NodeTypeServiceListProvidersProcedure: PermNodeTypesRead,
	// OrgService. CreateOrg runs before a tenant is selected, so it needs
	// authentication but no org (see RequiresOrg).
	cloudv1connect.OrgServiceCreateOrgProcedure:        PermOrgWrite,
	cloudv1connect.OrgServiceDeleteOrgProcedure:        PermOrgDelete,
	cloudv1connect.OrgServiceGetOrgProcedure:           PermOrgRead,
	cloudv1connect.OrgServiceInviteMemberProcedure:     PermOrgMembersWrite,
	cloudv1connect.OrgServiceListInvitationsProcedure:  PermOrgRead,
	cloudv1connect.OrgServiceListMembersProcedure:      PermOrgRead,
	cloudv1connect.OrgServiceRemoveMemberProcedure:     PermOrgMembersWrite,
	cloudv1connect.OrgServiceRevokeInvitationProcedure: PermOrgMembersWrite,
	cloudv1connect.OrgServiceUpdateMemberRoleProcedure: PermOrgMembersWrite,
	cloudv1connect.OrgServiceUpdateOrgProcedure:        PermOrgWrite,
	// ProvisionService.
	cloudv1connect.ProvisionServiceApplyProvisionProcedure:      PermProvisionApply,
	cloudv1connect.ProvisionServiceCreateProvisionProcedure:     PermProvisionWrite,
	cloudv1connect.ProvisionServiceDestroyProvisionProcedure:    PermProvisionApply,
	cloudv1connect.ProvisionServiceGetProvisionProcedure:        PermProvisionRead,
	cloudv1connect.ProvisionServiceListProvisionsProcedure:      PermProvisionRead,
	cloudv1connect.ProvisionServicePlanProvisionProcedure:       PermProvisionWrite,
	cloudv1connect.ProvisionServiceStreamProvisionLogsProcedure: PermProvisionRead,
	// RoleService.
	cloudv1connect.RoleServiceCreateRoleBindingProcedure: PermRBACWrite,
	cloudv1connect.RoleServiceDeleteRoleBindingProcedure: PermRBACWrite,
	cloudv1connect.RoleServiceListPermissionsProcedure:   PermRBACRead,
	cloudv1connect.RoleServiceListRoleBindingsProcedure:  PermRBACRead,
	cloudv1connect.RoleServiceListRolesProcedure:         PermRBACRead,
	// SessionService. Self-scoped bootstrap RPCs: authenticated, no org.
	cloudv1connect.SessionServiceGetSessionProcedure:        PermProfileRead,
	cloudv1connect.SessionServiceListMyOrgsProcedure:        PermProfileRead,
	cloudv1connect.SessionServiceListMyPermissionsProcedure: PermProfileRead,
	// SystemService. Public (see PublicProcedures) but still mapped.
	cloudv1connect.SystemServiceGetBuildInfoProcedure:    PermSystemRead,
	cloudv1connect.SystemServiceGetCapabilitiesProcedure: PermSystemRead,
	// WorkloadService.
	cloudv1connect.WorkloadServiceCreateWorkloadProcedure:      PermWorkloadsWrite,
	cloudv1connect.WorkloadServiceDeleteWorkloadProcedure:      PermWorkloadsWrite,
	cloudv1connect.WorkloadServiceGetWorkloadProcedure:         PermWorkloadsRead,
	cloudv1connect.WorkloadServiceListWorkloadEventsProcedure:  PermWorkloadsRead,
	cloudv1connect.WorkloadServiceListWorkloadsProcedure:       PermWorkloadsRead,
	cloudv1connect.WorkloadServiceRestartWorkloadProcedure:     PermWorkloadsWrite,
	cloudv1connect.WorkloadServiceSendWorkloadCommandProcedure: PermWorkloadsExec,
	cloudv1connect.WorkloadServiceStartWorkloadProcedure:       PermWorkloadsWrite,
	cloudv1connect.WorkloadServiceStopWorkloadProcedure:        PermWorkloadsWrite,
	cloudv1connect.WorkloadServiceStreamWorkloadLogsProcedure:  PermWorkloadsRead,
	cloudv1connect.WorkloadServiceUpdateWorkloadProcedure:      PermWorkloadsWrite,
	// FileService.
	cloudv1connect.FileServiceListFilesProcedure:       PermWorkloadsRead,
	cloudv1connect.FileServiceStatFileProcedure:        PermWorkloadsRead,
	cloudv1connect.FileServiceReadFileProcedure:        PermWorkloadsRead,
	cloudv1connect.FileServiceWriteFileProcedure:       PermWorkloadsWrite,
	cloudv1connect.FileServiceDeleteFileProcedure:      PermWorkloadsWrite,
	cloudv1connect.FileServiceCreateDirectoryProcedure: PermWorkloadsWrite,
}

// PermissionForProcedure returns the permission required by a Connect
// procedure name such as "/cloud.v1.NodeService/ListNodes". The second
// return is false for unknown procedures, which the interceptor denies.
func PermissionForProcedure(procedure string) (Permission, bool) {
	perm, ok := procedurePermissions[procedure]
	if !ok || perm == "" {
		return "", false
	}
	return perm, true
}

// AllProcedures enumerates every known RPC procedure from the generated
// service descriptors. It is an explicit list over the generated *Procedure
// constants: adding an RPC without extending this list and the mapping table
// makes TestEveryProcedureIsMapped fail.
func AllProcedures() []string {
	return []string{
		cloudv1connect.AgentServiceConnectProcedure,
		cloudv1connect.AgentServiceJoinNodeProcedure,
		cloudv1connect.AgentServiceRenewCredentialsProcedure,
		cloudv1connect.ApiKeyServiceCreateApiKeyProcedure,
		cloudv1connect.ApiKeyServiceListApiKeysProcedure,
		cloudv1connect.ApiKeyServiceRevokeApiKeyProcedure,
		cloudv1connect.ApiKeyServiceRotateApiKeyProcedure,
		cloudv1connect.AuditServiceGetAuditEventProcedure,
		cloudv1connect.AuditServiceListAuditEventsProcedure,
		cloudv1connect.NodeServiceCreateJoinTokenProcedure,
		cloudv1connect.NodeServiceDeleteNodeProcedure,
		cloudv1connect.NodeServiceDrainNodeProcedure,
		cloudv1connect.NodeServiceGetNodeProcedure,
		cloudv1connect.NodeServiceListJoinTokensProcedure,
		cloudv1connect.NodeServiceListNodesProcedure,
		cloudv1connect.NodeServiceResumeNodeProcedure,
		cloudv1connect.NodeServiceRevokeJoinTokenProcedure,
		cloudv1connect.NodeServiceUpdateNodeProcedure,
		cloudv1connect.NodeTypeServiceGetNodeTypeProcedure,
		cloudv1connect.NodeTypeServiceListNodeTypesProcedure,
		cloudv1connect.NodeTypeServiceListProvidersProcedure,
		cloudv1connect.OrgServiceCreateOrgProcedure,
		cloudv1connect.OrgServiceDeleteOrgProcedure,
		cloudv1connect.OrgServiceGetOrgProcedure,
		cloudv1connect.OrgServiceInviteMemberProcedure,
		cloudv1connect.OrgServiceListInvitationsProcedure,
		cloudv1connect.OrgServiceListMembersProcedure,
		cloudv1connect.OrgServiceRemoveMemberProcedure,
		cloudv1connect.OrgServiceRevokeInvitationProcedure,
		cloudv1connect.OrgServiceUpdateMemberRoleProcedure,
		cloudv1connect.OrgServiceUpdateOrgProcedure,
		cloudv1connect.ProvisionServiceApplyProvisionProcedure,
		cloudv1connect.ProvisionServiceCreateProvisionProcedure,
		cloudv1connect.ProvisionServiceDestroyProvisionProcedure,
		cloudv1connect.ProvisionServiceGetProvisionProcedure,
		cloudv1connect.ProvisionServiceListProvisionsProcedure,
		cloudv1connect.ProvisionServicePlanProvisionProcedure,
		cloudv1connect.ProvisionServiceStreamProvisionLogsProcedure,
		cloudv1connect.RoleServiceCreateRoleBindingProcedure,
		cloudv1connect.RoleServiceDeleteRoleBindingProcedure,
		cloudv1connect.RoleServiceListPermissionsProcedure,
		cloudv1connect.RoleServiceListRoleBindingsProcedure,
		cloudv1connect.RoleServiceListRolesProcedure,
		cloudv1connect.SessionServiceGetSessionProcedure,
		cloudv1connect.SessionServiceListMyOrgsProcedure,
		cloudv1connect.SessionServiceListMyPermissionsProcedure,
		cloudv1connect.SystemServiceGetBuildInfoProcedure,
		cloudv1connect.SystemServiceGetCapabilitiesProcedure,
		cloudv1connect.WorkloadServiceCreateWorkloadProcedure,
		cloudv1connect.WorkloadServiceDeleteWorkloadProcedure,
		cloudv1connect.WorkloadServiceGetWorkloadProcedure,
		cloudv1connect.WorkloadServiceListWorkloadEventsProcedure,
		cloudv1connect.WorkloadServiceListWorkloadsProcedure,
		cloudv1connect.WorkloadServiceRestartWorkloadProcedure,
		cloudv1connect.WorkloadServiceSendWorkloadCommandProcedure,
		cloudv1connect.WorkloadServiceStartWorkloadProcedure,
		cloudv1connect.WorkloadServiceStopWorkloadProcedure,
		cloudv1connect.WorkloadServiceStreamWorkloadLogsProcedure,
		cloudv1connect.WorkloadServiceUpdateWorkloadProcedure,
		cloudv1connect.FileServiceListFilesProcedure,
		cloudv1connect.FileServiceStatFileProcedure,
		cloudv1connect.FileServiceReadFileProcedure,
		cloudv1connect.FileServiceWriteFileProcedure,
		cloudv1connect.FileServiceDeleteFileProcedure,
		cloudv1connect.FileServiceCreateDirectoryProcedure,
	}
}

// publicProcedures is the explicit unauthenticated allowlist. Everything not
// in it requires authentication and a mapped permission.
var publicProcedures = map[string]struct{}{
	cloudv1connect.AgentServiceJoinNodeProcedure:         {},
	cloudv1connect.SystemServiceGetBuildInfoProcedure:    {},
	cloudv1connect.SystemServiceGetCapabilitiesProcedure: {},
}

// PublicProcedures returns the unauthenticated procedure allowlist.
func PublicProcedures() []string {
	return []string{
		cloudv1connect.AgentServiceJoinNodeProcedure,
		cloudv1connect.SystemServiceGetBuildInfoProcedure,
		cloudv1connect.SystemServiceGetCapabilitiesProcedure,
	}
}

// IsPublic reports whether procedure bypasses authentication.
func IsPublic(procedure string) bool {
	_, ok := publicProcedures[procedure]
	return ok
}

// orgOptionalProcedures are authenticated RPCs that legitimately run without
// a selected tenant: org bootstrap and self-scoped session reads. Every
// other non-public procedure requires a non-empty org.
var orgOptionalProcedures = map[string]struct{}{
	cloudv1connect.OrgServiceCreateOrgProcedure:             {},
	cloudv1connect.SessionServiceGetSessionProcedure:        {},
	cloudv1connect.SessionServiceListMyOrgsProcedure:        {},
	cloudv1connect.SessionServiceListMyPermissionsProcedure: {},
}

// RequiresOrg reports whether procedure needs a selected tenant. Public
// procedures never do; unknown procedures are treated as requiring one so a
// typo cannot widen access (they are denied earlier anyway).
func RequiresOrg(procedure string) bool {
	if IsPublic(procedure) {
		return false
	}
	_, optional := orgOptionalProcedures[procedure]
	return !optional
}

// nodeIdentityProcedures authenticate by node identity (mTLS client
// certificate, surfaced as a KindNode principal) rather than by org role.
var nodeIdentityProcedures = map[string]struct{}{
	cloudv1connect.AgentServiceConnectProcedure:          {},
	cloudv1connect.AgentServiceRenewCredentialsProcedure: {},
}

// IsNodeIdentityProcedure reports whether procedure accepts a node-identity
// principal in place of an org-role check.
func IsNodeIdentityProcedure(procedure string) bool {
	_, ok := nodeIdentityProcedures[procedure]
	return ok
}
