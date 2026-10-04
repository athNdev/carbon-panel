package rbac

import "testing"

// TestListModulesIsScopedToServer pins the object-scoping of ListModules.
//
// ListModules takes an OPTIONAL server_id filter. Without an ObjectIDField the
// interceptor enforced modules:read against "*", which is wrong in both
// directions:
//
//   - a role scoped to a single server could not list even its own modules
//     (a server-scoped grant does not satisfy "*"), and
//   - the scoping intent was invisible, so any future role granted
//     modules:read on "*" would silently read every server's modules.
//
// With ObjectIDField "server_id", extractObjectID returns the supplied server
// id, or "*" when the filter is omitted - so a scoped role is checked against
// the server it asked for, and an omitted filter falls back to the global
// check rather than bypassing it.
func TestListModulesIsScopedToServer(t *testing.T) {
	perm, ok := ProcedurePermissions["/carbonpanel.v1.ModuleService/ListModules"]
	if !ok {
		t.Fatal("ListModules has no permission mapping")
	}
	if perm.ObjectIDField != "server_id" {
		t.Fatalf("ListModules ObjectIDField = %q, want %q", perm.ObjectIDField, "server_id")
	}
	if perm.Resource != ResourceModules || perm.Action != ActionRead {
		t.Fatalf("ListModules mapping = %s/%s, want modules/read", perm.Resource, perm.Action)
	}
}

// TestObjectScopedProceduresUseServerID guards the procedures that take a
// server_id directly. These are the ones where a missing ObjectIDField would
// mean a cross-server read or write.
func TestObjectScopedProceduresUseServerID(t *testing.T) {
	want := []string{
		"/carbonpanel.v1.ModuleService/CreateModule",
		"/carbonpanel.v1.TaskService/ListTasks",
		"/carbonpanel.v1.TaskService/CreateTask",
		"/carbonpanel.v1.TaskService/ListServerExecutions",
		"/carbonpanel.v1.BackupService/ListBackups",
		"/carbonpanel.v1.FileService/ListFiles",
		"/carbonpanel.v1.FileService/GetFile",
		"/carbonpanel.v1.SubuserService/ListSubusers",
		"/carbonpanel.v1.SubuserService/SetSubuser",
		"/carbonpanel.v1.SubuserService/RemoveSubuser",
	}
	for _, proc := range want {
		perm, ok := ProcedurePermissions[proc]
		if !ok {
			t.Errorf("%s has no permission mapping", proc)
			continue
		}
		if perm.ObjectIDField != "server_id" {
			t.Errorf("%s ObjectIDField = %q, want server_id", proc, perm.ObjectIDField)
		}
	}
}

// TestRecordIDProceduresCannotUseServerIDField documents WHY the backup and
// task-by-id procedures cannot simply be given ObjectIDField "server_id":
// their requests carry the RECORD's id, not a server id, so the interceptor
// would compare a server-scoped grant against an unrelated identifier.
//
// This is a real (fail-closed) limitation, not a hole: a role scoped to
// srv-A holding tasks:update is checked against a task UUID, which does not
// match, so the request is denied. Those procedures are authorized at the
// service layer instead - see BackupService.authorizeServerBackup.
func TestRecordIDProceduresCannotUseServerIDField(t *testing.T) {
	recordIDProcedures := []string{
		"/carbonpanel.v1.TaskService/UpdateTask",
		"/carbonpanel.v1.TaskService/DeleteTask",
		"/carbonpanel.v1.ModuleService/GetModule",
		"/carbonpanel.v1.ModuleService/DeleteModule",
	}
	for _, proc := range recordIDProcedures {
		perm, ok := ProcedurePermissions[proc]
		if !ok {
			t.Errorf("%s has no permission mapping", proc)
			continue
		}
		if perm.ObjectIDField == "server_id" {
			t.Errorf("%s must not claim server_id; its request carries a record id", proc)
		}
	}

	// The three backup procedures are authorized in the service layer because
	// of exactly this reason.
	for _, proc := range []string{
		"/carbonpanel.v1.BackupService/DeleteBackup",
		"/carbonpanel.v1.BackupService/RestoreBackup",
		"/carbonpanel.v1.BackupService/SetBackupLocked",
	} {
		perm, ok := ProcedurePermissions[proc]
		if !ok {
			t.Errorf("%s has no permission mapping", proc)
			continue
		}
		if perm.ObjectIDField != "" {
			t.Errorf("%s ObjectIDField = %q; expected empty (service-layer enforcement)", proc, perm.ObjectIDField)
		}
	}
}
