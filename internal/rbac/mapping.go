package rbac

// ProcedurePermission maps an RPC procedure to a resource and action.
type ProcedurePermission struct {
	Resource      string
	Action        string
	ObjectIDField string // Protobuf field name to extract for per-object RBAC (empty = "*")
}

// PublicProcedures lists RPC procedures that require no authentication.
var PublicProcedures = map[string]bool{
	"/mineserver.v1.AuthService/GetAuthStatus":   true,
	"/mineserver.v1.AuthService/Login":           true,
	"/mineserver.v1.AuthService/Register":        true,
	"/mineserver.v1.AuthService/GetOIDCLoginURL": true,
	"/mineserver.v1.AuthService/ValidateInvite":  true,
	"/mineserver.v1.AuthService/UseRecoveryKey":  true,
}

// AuthenticatedOnlyProcedures lists RPC procedures that require authentication
// but no specific resource permission.
var AuthenticatedOnlyProcedures = map[string]bool{
	// AuthService - authenticated user operations
	"/mineserver.v1.AuthService/GetCurrentUser": true,
	"/mineserver.v1.AuthService/Logout":         true,
	"/mineserver.v1.AuthService/ChangePassword": true,
	"/mineserver.v1.AuthService/CreateAPIToken": true,
	"/mineserver.v1.AuthService/ListAPITokens":  true,
	"/mineserver.v1.AuthService/DeleteAPIToken": true,

	// MinecraftService - reference data, no resource ownership
	"/mineserver.v1.MinecraftService/GetMinecraftVersions": true,
	"/mineserver.v1.MinecraftService/GetModLoaders":        true,
	"/mineserver.v1.MinecraftService/GetDockerImages":      true,
}

// ProcedurePermissions maps each RPC procedure path to the resource and action
// required to invoke it, plus an optional ObjectIDField for per-object scoping.
var ProcedurePermissions = map[string]ProcedurePermission{
	// â”€â”€ ServerService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.ServerService/ListServers":          {Resource: ResourceServers, Action: ActionRead},
	"/mineserver.v1.ServerService/GetServer":            {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/GetServerLogs":        {Resource: ResourceServers, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/ClearServerLogs":      {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/GetNextAvailablePort": {Resource: ResourceServers, Action: ActionRead},
	"/mineserver.v1.ServerService/CreateServer":         {Resource: ResourceServers, Action: ActionCreate},
	"/mineserver.v1.ServerService/UpdateServer":         {Resource: ResourceServers, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/DeleteServer":         {Resource: ResourceServers, Action: ActionDelete, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/StartServer":          {Resource: ResourceServers, Action: ActionStart, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/StopServer":           {Resource: ResourceServers, Action: ActionStop, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/RestartServer":        {Resource: ResourceServers, Action: ActionRestart, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/RecreateServer":       {Resource: ResourceServers, Action: ActionRestart, ObjectIDField: "id"},
	"/mineserver.v1.ServerService/SendCommand":          {Resource: ResourceServers, Action: ActionCommand, ObjectIDField: "id"},

	// â”€â”€ AuthService (admin) â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.AuthService/GetAuthConfig":      {Resource: ResourceSettings, Action: ActionRead},
	"/mineserver.v1.AuthService/UpdateAuthSettings": {Resource: ResourceSettings, Action: ActionUpdate},
	"/mineserver.v1.AuthService/CreateInvite":       {Resource: ResourceUsers, Action: ActionCreate},
	"/mineserver.v1.AuthService/ListInvites":        {Resource: ResourceUsers, Action: ActionRead},
	"/mineserver.v1.AuthService/GetInvite":          {Resource: ResourceUsers, Action: ActionRead},
	"/mineserver.v1.AuthService/DeleteInvite":       {Resource: ResourceUsers, Action: ActionDelete},

	// â”€â”€ ConfigService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.ConfigService/GetServerConfig":      {Resource: ResourceServerConfig, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.ConfigService/UpdateServerConfig":   {Resource: ResourceServerConfig, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/mineserver.v1.ConfigService/GetGlobalSettings":          {Resource: ResourceSettings, Action: ActionRead},
	"/mineserver.v1.ConfigService/UpdateGlobalSettings":       {Resource: ResourceSettings, Action: ActionUpdate},
	"/mineserver.v1.ConfigService/SyncGlobalSettingsToServers": {Resource: ResourceSettings, Action: ActionUpdate},

	// â”€â”€ FileService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.FileService/ListFiles":           {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/GetFile":             {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/SaveUploadedFile":    {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/UpdateFile":          {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/DeleteFile":          {Resource: ResourceFiles, Action: ActionDelete, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/RenameFile":          {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/ExtractArchive":      {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/CreateFolder":        {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/MoveFile":            {Resource: ResourceFiles, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/CopyFile":            {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/CreateArchive":       {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/DownloadArchive":          {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/InitFileDownload":         {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/GetExtractionStatus":      {Resource: ResourceFiles, Action: ActionRead},
	"/mineserver.v1.FileService/DownloadRemoteArchive":    {Resource: ResourceFiles, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.FileService/GetRemoteArchiveProgress": {Resource: ResourceFiles, Action: ActionRead, ObjectIDField: "server_id"},

	// â”€â”€ ModService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.ModService/ListMods":          {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.ModService/GetMod":            {Resource: ResourceMods, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.ModService/ImportUploadedMod": {Resource: ResourceMods, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.ModService/UpdateMod":         {Resource: ResourceMods, Action: ActionUpdate, ObjectIDField: "server_id"},
	"/mineserver.v1.ModService/DeleteMod":         {Resource: ResourceMods, Action: ActionDelete, ObjectIDField: "server_id"},

	// â”€â”€ ModpackService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.ModpackService/SearchModpacks":        {Resource: ResourceModpacks, Action: ActionRead},
	"/mineserver.v1.ModpackService/GetModpack":            {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModpackService/GetModpackBySlug":      {Resource: ResourceModpacks, Action: ActionRead},
	"/mineserver.v1.ModpackService/GetModpackByURL":       {Resource: ResourceModpacks, Action: ActionRead},
	"/mineserver.v1.ModpackService/SyncModpacks":          {Resource: ResourceModpacks, Action: ActionCreate},
	"/mineserver.v1.ModpackService/ImportUploadedModpack": {Resource: ResourceModpacks, Action: ActionCreate},
	"/mineserver.v1.ModpackService/ImportRemoteModpack":   {Resource: ResourceModpacks, Action: ActionCreate},
	"/mineserver.v1.ModpackService/DeleteModpack":         {Resource: ResourceModpacks, Action: ActionDelete, ObjectIDField: "id"},
	"/mineserver.v1.ModpackService/ToggleFavorite":        {Resource: ResourceModpacks, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.ModpackService/ListFavorites":         {Resource: ResourceModpacks, Action: ActionRead},
	"/mineserver.v1.ModpackService/GetIndexerStatus":      {Resource: ResourceModpacks, Action: ActionRead},
	"/mineserver.v1.ModpackService/GetModpackConfig":      {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModpackService/GetModpackFiles":       {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModpackService/GetModpackVersions":    {Resource: ResourceModpacks, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModpackService/SyncModpackFiles":      {Resource: ResourceModpacks, Action: ActionUpdate, ObjectIDField: "id"},

	// â”€â”€ ModuleService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.ModuleService/ListModuleTemplates":        {Resource: ResourceModuleTemplates, Action: ActionRead},
	"/mineserver.v1.ModuleService/GetModuleTemplate":          {Resource: ResourceModuleTemplates, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/CreateModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionCreate},
	"/mineserver.v1.ModuleService/UpdateModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/DeleteModuleTemplate":       {Resource: ResourceModuleTemplates, Action: ActionDelete, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/ListModules":                {Resource: ResourceModules, Action: ActionRead},
	"/mineserver.v1.ModuleService/GetModule":                  {Resource: ResourceModules, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/CreateModule":               {Resource: ResourceModules, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.ModuleService/UpdateModule":               {Resource: ResourceModules, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/DeleteModule":               {Resource: ResourceModules, Action: ActionDelete, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/StartModule":                {Resource: ResourceModules, Action: ActionStart, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/StopModule":                 {Resource: ResourceModules, Action: ActionStop, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/RestartModule":              {Resource: ResourceModules, Action: ActionRestart, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/RecreateModule":             {Resource: ResourceModules, Action: ActionRestart, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/GetModuleLogs":              {Resource: ResourceModules, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.ModuleService/GetNextAvailableModulePort": {Resource: ResourceModules, Action: ActionRead},
	"/mineserver.v1.ModuleService/GetAvailableAliases":        {Resource: ResourceModules, Action: ActionRead},
	"/mineserver.v1.ModuleService/GetResolvedAliases":         {Resource: ResourceModules, Action: ActionRead},

	// â”€â”€ ProxyService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.ProxyService/GetProxyRoutes":      {Resource: ResourceProxy, Action: ActionRead},
	"/mineserver.v1.ProxyService/GetProxyStatus":      {Resource: ResourceProxy, Action: ActionRead},
	"/mineserver.v1.ProxyService/UpdateProxyConfig":   {Resource: ResourceProxy, Action: ActionUpdate},
	"/mineserver.v1.ProxyService/GetProxyListeners":   {Resource: ResourceProxy, Action: ActionRead},
	"/mineserver.v1.ProxyService/CreateProxyListener": {Resource: ResourceProxy, Action: ActionCreate},
	"/mineserver.v1.ProxyService/UpdateProxyListener": {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.ProxyService/DeleteProxyListener": {Resource: ResourceProxy, Action: ActionDelete, ObjectIDField: "id"},
	"/mineserver.v1.ProxyService/GetServerRouting":    {Resource: ResourceProxy, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.ProxyService/UpdateServerRouting": {Resource: ResourceProxy, Action: ActionUpdate, ObjectIDField: "server_id"},

	// â”€â”€ TaskService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.TaskService/ListTasks":            {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.TaskService/GetTask":              {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/CreateTask":           {Resource: ResourceTasks, Action: ActionCreate, ObjectIDField: "server_id"},
	"/mineserver.v1.TaskService/UpdateTask":           {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/DeleteTask":           {Resource: ResourceTasks, Action: ActionDelete, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/ToggleTask":           {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/TriggerTask":          {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/ListTaskExecutions":   {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "task_id"},
	"/mineserver.v1.TaskService/ListServerExecutions": {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "server_id"},
	"/mineserver.v1.TaskService/GetTaskExecution":     {Resource: ResourceTasks, Action: ActionRead, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/CancelExecution":      {Resource: ResourceTasks, Action: ActionUpdate, ObjectIDField: "id"},
	"/mineserver.v1.TaskService/GetSchedulerStatus":   {Resource: ResourceTasks, Action: ActionRead},

	// â”€â”€ UserService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.UserService/ListUsers":  {Resource: ResourceUsers, Action: ActionRead},
	"/mineserver.v1.UserService/GetUser":    {Resource: ResourceUsers, Action: ActionRead},
	"/mineserver.v1.UserService/CreateUser": {Resource: ResourceUsers, Action: ActionCreate},
	"/mineserver.v1.UserService/UpdateUser": {Resource: ResourceUsers, Action: ActionUpdate},
	"/mineserver.v1.UserService/DeleteUser": {Resource: ResourceUsers, Action: ActionDelete},

	// â”€â”€ RoleService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.RoleService/ListRoles":           {Resource: ResourceRoles, Action: ActionRead},
	"/mineserver.v1.RoleService/GetRole":             {Resource: ResourceRoles, Action: ActionRead},
	"/mineserver.v1.RoleService/CreateRole":          {Resource: ResourceRoles, Action: ActionCreate},
	"/mineserver.v1.RoleService/UpdateRole":          {Resource: ResourceRoles, Action: ActionUpdate},
	"/mineserver.v1.RoleService/DeleteRole":          {Resource: ResourceRoles, Action: ActionDelete},
	"/mineserver.v1.RoleService/GetPermissionMatrix": {Resource: ResourceRoles, Action: ActionRead},
	"/mineserver.v1.RoleService/UpdatePermissions":   {Resource: ResourceRoles, Action: ActionUpdate},
	"/mineserver.v1.RoleService/AssignRole":          {Resource: ResourceRoles, Action: ActionCreate},
	"/mineserver.v1.RoleService/UnassignRole":        {Resource: ResourceRoles, Action: ActionDelete},
	"/mineserver.v1.RoleService/GetUserRoles":        {Resource: ResourceRoles, Action: ActionRead},

	// â”€â”€ SupportService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.SupportService/GenerateSupportBundle": {Resource: ResourceSupport, Action: ActionCreate},
	"/mineserver.v1.SupportService/DownloadSupportBundle": {Resource: ResourceSupport, Action: ActionRead},
	"/mineserver.v1.SupportService/UploadSupportBundle":   {Resource: ResourceSupport, Action: ActionCreate},
	"/mineserver.v1.SupportService/GetApplicationLogs":    {Resource: ResourceSupport, Action: ActionRead},

	// â”€â”€ UploadService â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	"/mineserver.v1.UploadService/GetUploadStatus": {Resource: ResourceUploads, Action: ActionRead},
	"/mineserver.v1.UploadService/InitUpload":      {Resource: ResourceUploads, Action: ActionCreate},
	"/mineserver.v1.UploadService/UploadChunk":     {Resource: ResourceUploads, Action: ActionCreate},
	"/mineserver.v1.UploadService/CancelUpload":    {Resource: ResourceUploads, Action: ActionDelete},
}
