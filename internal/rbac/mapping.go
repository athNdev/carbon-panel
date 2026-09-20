package rbac

// ProcedurePermission maps an RPC procedure to a resource and action.
type ProcedurePermission struct {
	Resource      string
	Action        string
	ObjectIDField string // Protobuf field name to extract for per-object RBAC (empty = "*")
}

// PublicProcedures lists RPC procedures that require no authentication.
var PublicProcedures = map[string]bool{
	"/carbonpanel.v1.AuthService/GetAuthStatus":   true,
	"/carbonpanel.v1.AuthService/Login":           true,
	"/carbonpanel.v1.AuthService/Register":        true,
	"/carbonpanel.v1.AuthService/GetOIDCLoginURL": true,
	"/carbonpanel.v1.AuthService/ValidateInvite":  true,
	"/carbonpanel.v1.AuthService/UseRecoveryKey":  true,
}

// AuthenticatedOnlyProcedures lists RPC procedures that require authentication
// but no specific resource permission.
var AuthenticatedOnlyProcedures = map[string]bool{
	// AuthService - authenticated user operations
	"/carbonpanel.v1.AuthService/GetCurrentUser": true,
	"/carbonpanel.v1.AuthService/Logout":         true,
	"/carbonpanel.v1.AuthService/ChangePassword": true,
	"/carbonpanel.v1.AuthService/CreateAPIToken": true,
	"/carbonpanel.v1.AuthService/ListAPITokens":  true,
	"/carbonpanel.v1.AuthService/DeleteAPIToken": true,

	// MinecraftService - reference data, no resource ownership
	"/carbonpanel.v1.MinecraftService/GetMinecraftVersions": true,
	"/carbonpanel.v1.MinecraftService/GetModLoaders":        true,
	"/carbonpanel.v1.MinecraftService/GetDockerImages":      true,
}

// ProcedurePermissions maps each RPC procedure path to the resource and action
// required to invoke it, plus an optional ObjectIDField for per-object scoping.
var ProcedurePermissions = map[string]ProcedurePermission{
	// â”€â”€ ServerService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.ServerService/ListServers":          {Resource: ResourceServers, Action: ActionRead},
	"/carbonpanel.v1.ServerService/GetServer":            {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/GetServerLogs":        {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/ClearServerLogs":      {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/GetNextAvailablePort": {Resource: ResourceServers, Action: ActionRead},
	"/carbonpanel.v1.ServerService/CreateServer":         {Resource: ResourceServers, Action: ActionCreate},
	"/carbonpanel.v1.ServerService/UpdateServer":         {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/DeleteServer":         {Resource: ResourceServers, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/StartServer":          {Resource: ResourceServers, Action: ActionStart, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/StopServer":           {Resource: ResourceServers, Action: ActionStop, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/RestartServer":        {Resource: ResourceServers, Action: ActionRestart, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/RecreateServer":       {Resource: ResourceServers, Action: ActionRestart, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/SendCommand":          {Resource: ResourceServers, Action: ActionCommand, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/UploadToMCLogs":       {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ServerService/MigrateServer":        {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},

	// â”€â”€ AuthService (admin) â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.AuthService/GetAuthConfig":      {Resource: ResourceSettings, Action: ActionRead},
	"/carbonpanel.v1.AuthService/UpdateAuthSettings": {Resource: ResourceSettings, Action: ActionUpdate},
	"/carbonpanel.v1.AuthService/CreateInvite":       {Resource: ResourceUsers, Action: ActionCreate},
	"/carbonpanel.v1.AuthService/ListInvites":        {Resource: ResourceUsers, Action: ActionRead},
	"/carbonpanel.v1.AuthService/GetInvite":          {Resource: ResourceUsers, Action: ActionRead},
	"/carbonpanel.v1.AuthService/DeleteInvite":       {Resource: ResourceUsers, Action: ActionDelete},

	// â”€â”€ ConfigService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.ConfigService/GetServerConfig":             {Resource: ResourceServerConfig, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ConfigService/UpdateServerConfig":          {Resource: ResourceServerConfig, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ConfigService/GetGlobalSettings":           {Resource: ResourceSettings, Action: ActionRead},
	"/carbonpanel.v1.ConfigService/UpdateGlobalSettings":        {Resource: ResourceSettings, Action: ActionUpdate},
	"/carbonpanel.v1.ConfigService/SyncGlobalSettingsToServers": {Resource: ResourceSettings, Action: ActionUpdate},

	// â”€â”€ FileService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.FileService/ListFiles":                {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/GetFile":                  {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/SaveUploadedFile":         {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/UpdateFile":               {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/DeleteFile":               {Resource: ResourceFiles, Action: ActionDelete, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/RenameFile":               {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/ExtractArchive":           {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/CreateFolder":             {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/MoveFile":                 {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/CopyFile":                 {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/CreateArchive":            {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/DownloadArchive":          {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/InitFileDownload":         {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/GetExtractionStatus":      {Resource: ResourceFiles, Action: ActionRead},
	"/carbonpanel.v1.FileService/DownloadRemoteArchive":    {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.FileService/GetRemoteArchiveProgress": {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},

	// â”€â”€ ModService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.ModService/ListMods":                       {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModService/GetMod":                         {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModService/ImportUploadedMod":              {Resource: ResourceMods, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModService/UpdateMod":                      {Resource: ResourceMods, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModService/DeleteMod":                      {Resource: ResourceMods, Action: ActionDelete, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModService/GetFabricOptimizationStack":     {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModService/InstallFabricOptimizationStack": {Resource: ResourceMods, Action: ActionUpdate, ObjectIDField: "server_id"},

	// â”€â”€ ModpackService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.ModpackService/SearchModpacks":        {Resource: ResourceModpacks, Action: ActionRead},
	"/carbonpanel.v1.ModpackService/GetModpack":            {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModpackService/GetModpackBySlug":      {Resource: ResourceModpacks, Action: ActionRead},
	"/carbonpanel.v1.ModpackService/GetModpackByURL":       {Resource: ResourceModpacks, Action: ActionRead},
	"/carbonpanel.v1.ModpackService/SyncModpacks":          {Resource: ResourceModpacks, Action: ActionCreate},
	"/carbonpanel.v1.ModpackService/ImportUploadedModpack": {Resource: ResourceModpacks, Action: ActionCreate},
	"/carbonpanel.v1.ModpackService/ImportRemoteModpack":   {Resource: ResourceModpacks, Action: ActionCreate},
	"/carbonpanel.v1.ModpackService/DeleteModpack":         {Resource: ResourceModpacks, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.ModpackService/ToggleFavorite":        {Resource: ResourceModpacks, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.ModpackService/ListFavorites":         {Resource: ResourceModpacks, Action: ActionRead},
	"/carbonpanel.v1.ModpackService/GetIndexerStatus":      {Resource: ResourceModpacks, Action: ActionRead},
	"/carbonpanel.v1.ModpackService/GetModpackConfig":      {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModpackService/GetModpackFiles":       {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModpackService/GetModpackVersions":    {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModpackService/SyncModpackFiles":      {Resource: ResourceModpacks, Action: ActionUpdate, ObjectIDField: "id"},

	// â”€â”€ ModuleService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.ModuleService/ListModuleTemplates":        {Resource: ResourceModuleTemplates, Action: ActionRead},
	"/carbonpanel.v1.ModuleService/GetModuleTemplate":          {Resource: ResourceModuleTemplates, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/CreateModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionCreate},
	"/carbonpanel.v1.ModuleService/UpdateModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/DeleteModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/ListModules":                {Resource: ResourceModules, Action: ActionRead},
	"/carbonpanel.v1.ModuleService/GetModule":                  {Resource: ResourceModules, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/CreateModule":               {Resource: ResourceModules, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ModuleService/UpdateModule":               {Resource: ResourceModules, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/DeleteModule":               {Resource: ResourceModules, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/StartModule":                {Resource: ResourceModules, Action: ActionStart, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/StopModule":                 {Resource: ResourceModules, Action: ActionStop, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/RestartModule":              {Resource: ResourceModules, Action: ActionRestart, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/RecreateModule":             {Resource: ResourceModules, Action: ActionRestart, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/GetModuleLogs":              {Resource: ResourceModules, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.ModuleService/GetNextAvailableModulePort": {Resource: ResourceModules, Action: ActionRead},
	"/carbonpanel.v1.ModuleService/GetAvailableAliases":        {Resource: ResourceModules, Action: ActionRead},
	"/carbonpanel.v1.ModuleService/GetResolvedAliases":         {Resource: ResourceModules, Action: ActionRead},

	// â”€â”€ NodeService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	// Node responses embed the Docker daemon host and TLS client key/cert
	// used to reach it, and Create/Update/Delete let a caller point the
	// panel at an arbitrary Docker host. These were previously absent from
	// this table, which meant any authenticated user fell through the
	// interceptor's "no mapping found" path with zero RBAC enforcement.
	"/carbonpanel.v1.NodeService/ListNodes":  {Resource: ResourceNodes, Action: ActionRead},
	"/carbonpanel.v1.NodeService/GetNode":    {Resource: ResourceNodes, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.NodeService/CreateNode": {Resource: ResourceNodes, Action: ActionCreate},
	"/carbonpanel.v1.NodeService/UpdateNode": {Resource: ResourceNodes, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.NodeService/DeleteNode": {Resource: ResourceNodes, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.NodeService/PingNode":   {Resource: ResourceNodes, Action: ActionRead, ObjectIDField: "id"},

	// â”€â”€ ProxyService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.ProxyService/GetProxyRoutes":             {Resource: ResourceProxy, Action: ActionRead},
	"/carbonpanel.v1.ProxyService/GetProxyStatus":             {Resource: ResourceProxy, Action: ActionRead},
	"/carbonpanel.v1.ProxyService/UpdateProxyConfig":          {Resource: ResourceProxy, Action: ActionUpdate},
	"/carbonpanel.v1.ProxyService/GetProxyListeners":          {Resource: ResourceProxy, Action: ActionRead},
	"/carbonpanel.v1.ProxyService/CreateProxyListener":        {Resource: ResourceProxy, Action: ActionCreate},
	"/carbonpanel.v1.ProxyService/UpdateProxyListener":        {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.ProxyService/DeleteProxyListener":        {Resource: ResourceProxy, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.ProxyService/GetServerRouting":           {Resource: ResourceProxy, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ProxyService/UpdateServerRouting":        {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.ProxyService/GetVelocitySecret":          {Resource: ResourceProxy, Action: ActionRead},
	"/carbonpanel.v1.ProxyService/RotateVelocitySecret":       {Resource: ResourceProxy, Action: ActionUpdate},
	"/carbonpanel.v1.ProxyService/SyncVelocitySecretToServer": {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "server_id"},

	// â”€â”€ TaskService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.TaskService/ListTasks":            {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.TaskService/GetTask":              {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/CreateTask":           {Resource: ResourceTasks, Action: ActionCreate, ObjectIDField: "server_id"},
	"/carbonpanel.v1.TaskService/UpdateTask":           {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/DeleteTask":           {Resource: ResourceTasks, Action: ActionDelete, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/ToggleTask":           {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/TriggerTask":          {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/ListTaskExecutions":   {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "task_id"},
	"/carbonpanel.v1.TaskService/ListServerExecutions": {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "server_id"},
	"/carbonpanel.v1.TaskService/GetTaskExecution":     {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/CancelExecution":      {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/carbonpanel.v1.TaskService/GetSchedulerStatus":   {Resource: ResourceTasks, Action: ActionRead},

	// ── ActivityService ─────────────────────────────────────────────
	"/carbonpanel.v1.ActivityService/ListActivityLogs": {Resource: ResourceActivity, Action: ActionRead, ObjectIDField: "server_id"},

	// â”€â”€ UserService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.UserService/ListUsers":  {Resource: ResourceUsers, Action: ActionRead},
	"/carbonpanel.v1.UserService/GetUser":    {Resource: ResourceUsers, Action: ActionRead},
	"/carbonpanel.v1.UserService/CreateUser": {Resource: ResourceUsers, Action: ActionCreate},
	"/carbonpanel.v1.UserService/UpdateUser": {Resource: ResourceUsers, Action: ActionUpdate},
	"/carbonpanel.v1.UserService/DeleteUser": {Resource: ResourceUsers, Action: ActionDelete},

	// â”€â”€ RoleService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.RoleService/ListRoles":           {Resource: ResourceRoles, Action: ActionRead},
	"/carbonpanel.v1.RoleService/GetRole":             {Resource: ResourceRoles, Action: ActionRead},
	"/carbonpanel.v1.RoleService/CreateRole":          {Resource: ResourceRoles, Action: ActionCreate},
	"/carbonpanel.v1.RoleService/UpdateRole":          {Resource: ResourceRoles, Action: ActionUpdate},
	"/carbonpanel.v1.RoleService/DeleteRole":          {Resource: ResourceRoles, Action: ActionDelete},
	"/carbonpanel.v1.RoleService/GetPermissionMatrix": {Resource: ResourceRoles, Action: ActionRead},
	"/carbonpanel.v1.RoleService/UpdatePermissions":   {Resource: ResourceRoles, Action: ActionUpdate},
	"/carbonpanel.v1.RoleService/AssignRole":          {Resource: ResourceRoles, Action: ActionCreate},
	"/carbonpanel.v1.RoleService/UnassignRole":        {Resource: ResourceRoles, Action: ActionDelete},
	"/carbonpanel.v1.RoleService/GetUserRoles":        {Resource: ResourceRoles, Action: ActionRead},

	// â”€â”€ SupportService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.SupportService/GenerateSupportBundle": {Resource: ResourceSupport, Action: ActionCreate},
	"/carbonpanel.v1.SupportService/DownloadSupportBundle": {Resource: ResourceSupport, Action: ActionRead},
	"/carbonpanel.v1.SupportService/UploadSupportBundle":   {Resource: ResourceSupport, Action: ActionCreate},
	"/carbonpanel.v1.SupportService/GetApplicationLogs":    {Resource: ResourceSupport, Action: ActionRead},

	// â”€â”€ UploadService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/carbonpanel.v1.UploadService/GetUploadStatus": {Resource: ResourceUploads, Action: ActionRead},
	"/carbonpanel.v1.UploadService/InitUpload":      {Resource: ResourceUploads, Action: ActionCreate},
	"/carbonpanel.v1.UploadService/UploadChunk":     {Resource: ResourceUploads, Action: ActionCreate},
	"/carbonpanel.v1.UploadService/CancelUpload":    {Resource: ResourceUploads, Action: ActionDelete},
}
