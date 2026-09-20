package rbac

// Resource constants
const (
	ResourceServers         = "servers"
	ResourceServerConfig    = "server_config"
	ResourceMods            = "mods"
	ResourceModpacks        = "modpacks"
	ResourceModules         = "modules"
	ResourceModuleTemplates = "module_templates"
	ResourceFiles           = "files"
	ResourceTasks           = "tasks"
	ResourceProxy           = "proxy"
	ResourceUsers           = "users"
	ResourceRoles           = "roles"
	ResourceSettings        = "settings"
	ResourceSupport         = "support"
	ResourceUploads         = "uploads"
	ResourceNodes           = "nodes"
	ResourceActivity        = "activity"
	ResourceSubusers        = "subusers"
	ResourceBackups         = "backups"
)

// Action constants
const (
	ActionRead    = "read"
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionCommand = "command"

	// ActionManageDockerPrivileged gates host-breakout-capable Docker
	// container overrides: full --privileged mode, added Linux
	// capabilities (CapAdd), and confinement-weakening SecurityOpt values
	// (e.g. apparmor:unconfined, seccomp:unconfined, label:disable).
	//
	// Unlike the other actions above, this is not tied 1:1 to an RPC
	// procedure - ServerService/CreateServer and UpdateServer are ordinary
	// ResourceServers create/update operations for most requests, but when
	// the request payload's DockerOverrides contains one of the dangerous
	// fields above, the handler performs an *additional* enforcer check for
	// (ResourceServers, ActionManageDockerPrivileged) before honoring it.
	// Because it is derived from payload content rather than from
	// ProcedurePermissions, it intentionally does not appear in
	// ResourceActionsFromProcedures' output; only the built-in admin role
	// (which matches "*" actions) has it until an operator explicitly
	// grants it to another role.
	ActionManageDockerPrivileged = "manage_docker_privileged"
)

// ResourceActionEntry pairs a resource with its valid actions.
type ResourceActionEntry struct {
	Resource string
	Actions  []string
}

// AllActions in display order.
var AllActions = []string{
	ActionRead, ActionCreate, ActionUpdate, ActionDelete,
	ActionStart, ActionStop, ActionRestart, ActionCommand,
}

// AllResources in display order.
var AllResources = []string{
	ResourceServers, ResourceServerConfig, ResourceMods,
	ResourceModpacks, ResourceModules, ResourceModuleTemplates,
	ResourceFiles, ResourceTasks, ResourceProxy,
	ResourceUsers, ResourceRoles, ResourceSettings,
	ResourceSupport, ResourceUploads, ResourceNodes,
	ResourceActivity, ResourceSubusers, ResourceBackups,
}

// ResourceScopeSource maps each scopeable resource to the resource that
// provides its scope objects. For example, files are scoped by server_id,
// so ResourceFiles → ResourceServers. Resources absent from this map
// (users, roles, settings, support, uploads) have no per-object scoping.
var ResourceScopeSource = map[string]string{
	ResourceServers:         ResourceServers,
	ResourceServerConfig:    ResourceServers,
	ResourceFiles:           ResourceServers,
	ResourceMods:            ResourceServers,
	ResourceModules:         ResourceModules,
	ResourceModuleTemplates: ResourceModuleTemplates,
	ResourceModpacks:        ResourceModpacks,
	ResourceProxy:           ResourceProxy,
	ResourceTasks:           ResourceTasks,
	ResourceNodes:           ResourceNodes,
	ResourceActivity:        ResourceServers,
	ResourceSubusers:        ResourceServers,
	ResourceBackups:         ResourceServers,
}

// ResourceActionsFromProcedures derives which actions are valid for each
// resource by inspecting the ProcedurePermissions mapping. Maintains stable
// resource ordering via AllResources and stable action ordering via AllActions.
func ResourceActionsFromProcedures() []ResourceActionEntry {
	actionSet := make(map[string]map[string]bool)
	for _, pp := range ProcedurePermissions {
		if actionSet[pp.Resource] == nil {
			actionSet[pp.Resource] = make(map[string]bool)
		}
		actionSet[pp.Resource][pp.Action] = true
	}

	entries := make([]ResourceActionEntry, 0, len(AllResources))
	for _, res := range AllResources {
		acts, ok := actionSet[res]
		if !ok {
			continue
		}
		var ordered []string
		for _, a := range AllActions {
			if acts[a] {
				ordered = append(ordered, a)
			}
		}
		entries = append(entries, ResourceActionEntry{Resource: res, Actions: ordered})
	}
	return entries
}
