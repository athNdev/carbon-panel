package svc

import "errors"

// Typed dependency and state errors. Handlers wrap these in connect errors
// with the narrowest applicable code; the messages carry no internals.
var (
	errNoStore   = errors.New("svc: db store is required")
	errNoNode    = errors.New("svc: node registry is required")
	errNoCatalog = errors.New("svc: node-type catalog is required")
	errNoAudit   = errors.New("svc: audit store is required")

	// errNodeExecutes is returned by control-plane RPCs whose execution
	// belongs to the node agent (w3-noded lane). The control plane records
	// intent and streams commands; the agent applies them.
	errNodeExecutes = errors.New("svc: execution belongs to the node agent")

	errNoPrincipal     = errors.New("svc: no authenticated principal")
	errNoOrgCtx        = errors.New("svc: no org selected")
	errOrgName         = errors.New("svc: org name is required")
	errOrgCreate       = errors.New("svc: could not create org")
	errOrgNotFound     = errors.New("svc: org not found")
	errOrgUpdate       = errors.New("svc: could not update org")
	errOrgEmptyUpdate  = errors.New("svc: nothing to update")
	errOrgConfirm      = errors.New("svc: confirm_org_id must match the active org")
	errOrgDelete       = errors.New("svc: could not delete org")
	errOrgMembers      = errors.New("svc: could not list members")
	errOrgBadRole      = errors.New("svc: unknown role")
	errOrgNoUser       = errors.New("svc: user_id is required")
	errOrgMemberNotFound = errors.New("svc: member not found")
	errOrgBadEmail     = errors.New("svc: valid email is required")
	errOrgInvite       = errors.New("svc: could not create invitation")
	errOrgNoInvite     = errors.New("svc: invitation id is required")

	errRoleBindings    = errors.New("svc: could not list role bindings")
	errRoleSubject     = errors.New("svc: subject_type and subject_id are required")
	errRolePerms       = errors.New("svc: at least one permission is required")
	errRoleUnknownPerm = errors.New("svc: unknown permission")
	errRoleCreate      = errors.New("svc: could not create role binding")
	errRoleNoID        = errors.New("svc: binding id is required")
	errRoleDelete      = errors.New("svc: could not delete role binding")

	errNodeTypes        = errors.New("svc: could not list node types")
	errNodeTypeNotFound = errors.New("svc: node type not found")

	errNodeList     = errors.New("svc: could not list nodes")
	errNodeNotFound = errors.New("svc: node not found")
	errNodeSelfOnly = errors.New("svc: update targets the calling node only")
	errJoinIssue    = errors.New("svc: could not issue join token")
	errJoinList     = errors.New("svc: could not list join tokens")
	errJoinNotFound = errors.New("svc: join token not found")

	errKeyList     = errors.New("svc: could not list API keys")
	errKeyName     = errors.New("svc: key name is required")
	errKeyMint     = errors.New("svc: could not mint API key")
	errKeyCreate   = errors.New("svc: could not store API key")
	errKeyNoID     = errors.New("svc: key id is required")
	errKeyRevoke   = errors.New("svc: could not revoke API key")
	errKeyNotFound = errors.New("svc: API key not found")

	errAuditPrefix   = errors.New("svc: action_prefix does not support wildcards")
	errAuditList     = errors.New("svc: could not list audit events")
	errAuditNoID     = errors.New("svc: audit event id is required")
	errAuditNotFound = errors.New("svc: audit event not found")

	errNoProvisioner      = errors.New("svc: provisioner is not configured")
	errProvisionNodeType  = errors.New("svc: unknown node type")
	errProvisionProvider  = errors.New("svc: provider is required")
	errProvisionFields    = errors.New("svc: name, region and node_type_id are required")
	errProvisionCreate    = errors.New("svc: could not create provision")
	errProvisionPlan      = errors.New("svc: provision plan failed")
	errProvisionApply     = errors.New("svc: provision apply failed")
	errProvisionDestroy   = errors.New("svc: provision destroy failed")
	errProvisionNotFound  = errors.New("svc: provision not found")
	errProvisionStalePlan = errors.New("svc: plan changed since review: confirm_plan_hash mismatch")
	errProvisionConfirm   = errors.New("svc: confirm_id must match the provision id")
	errProvisionList      = errors.New("svc: could not list provisions")

	errWorkloadList    = errors.New("svc: could not list workloads")
	errWorkloadNotFound = errors.New("svc: workload not found")
	errWorkloadName    = errors.New("svc: workload name is required")
	errWorkloadNode    = errors.New("svc: node_id is required")
	errWorkloadCreate  = errors.New("svc: could not create workload")
	errWorkloadUpdate  = errors.New("svc: could not update workload")
	errWorkloadDelete  = errors.New("svc: could not delete workload")
	errWorkloadEvents  = errors.New("svc: could not list workload events")

	errAgentJoin  = errors.New("svc: node join failed")
	errAgentCreds = errors.New("svc: credential renewal is not implemented by the control plane")
)
