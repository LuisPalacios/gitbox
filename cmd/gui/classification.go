package main

// The classification of every entry point for remote mode (#233). The guard
// tests in classification_test.go fail when a method is added to *App, or an
// event name is emitted, without a row here.
//
//   - REMOTE: the work runs where the data is, behind Backend.
//   - LOCAL: it only touches the machine that shows the window. It never
//     goes through Backend.
//   - MIXED: both halves exist. The method is on Backend with its full
//     signature, and its client half stays in the bound method.
//
// Owner is the remote-mode phase that implements the remote side or adapts
// the entry for remote mode. Mutating marks REMOTE and MIXED methods that
// change state on the host; request-ID replay is built from it.

type entryClass string

const (
	classLocal  entryClass = "LOCAL"
	classRemote entryClass = "REMOTE"
	classMixed  entryClass = "MIXED"
)

type methodInfo struct {
	Class    entryClass
	Owner    string
	Mutating bool
}

type eventInfo struct {
	Class entryClass
	Owner string
}

// lifecycleHooks are the exported methods Wails calls that are not bound.
var lifecycleHooks = []string{"BeforeClose", "DomReady", "Shutdown", "Startup"}

// The AI harness detection (RefreshAIHarnesses, SyncAIHarnesses) runs on
// the host, where the harnesses run (#243). It is not marked Mutating: a
// pass is idempotent (it finds the same harnesses every time), so a call
// sent twice needs no request-id replay, and on a read-only server what it
// finds stays in memory and never reaches the host's gitbox.json
// (TestReadOnlyServerSavesNothing).

// methodClass covers the Wails-bound methods of *App and the lifecycle hooks.
var methodClass = map[string]methodInfo{
	"AccountDeletionImpact":          {classRemote, "P4", false},
	"AcknowledgeConfigError":         {classRemote, "P5", true},
	"AddAccount":                     {classRemote, "P5", true},
	"AddDiscoveredRepos":             {classRemote, "P5", true},
	"AddDiscoveredReposToFolder":     {classRemote, "P5", true},
	"AddMirrorGroup":                 {classRemote, "P5", true},
	"AddMirrorRepo":                  {classRemote, "P5", true},
	"AddRemote":                      {classLocal, "P3b", false},
	"AddRoot":                        {classRemote, "P5", true},
	"AdoptClones":                    {classRemote, "P5", true},
	"AdoptFirstProject":              {classRemote, "P5", true},
	"ApplyDiscoveredMirrors":         {classRemote, "P5", true},
	"ApplyRecipeImport":              {classLocal, "fleet recipes", false},
	"ApplyUpdate":                    {classLocal, "P1a", false},
	"BeforeClose":                    {classMixed, "P1a", false},
	"BeginStatus":                    {classRemote, "P4", false},
	"ChangeCredentialType":           {classRemote, "P5b", true},
	"CheckGlobalGCMConfig":           {classRemote, "P4", false},
	"CheckGlobalGitignore":           {classRemote, "P4", false},
	"CheckGlobalIdentity":            {classRemote, "P4", false},
	"CheckMirrorCredentials":         {classRemote, "P4", false},
	"CheckMoveReadiness":             {classRemote, "P4", false},
	"CheckUpdateRecovery":            {classLocal, "P1a", false},
	"CloneRepo":                      {classRemote, "P5", true},
	"ConfirmSweep":                   {classRemote, "P5", true},
	"ConnectRemote":                  {classLocal, "P3b", false},
	"CreateNewRepo":                  {classRemote, "P5", true},
	"CredentialDelete":               {classRemote, "P5b", true},
	"CredentialRegenerateSSH":        {classRemote, "P7", true},
	"CredentialSSHAgentReport":       {classRemote, "P7", false},
	"CredentialSetupGCM":             {classMixed, "P5b", true},
	"CredentialSetupSSH":             {classRemote, "P7", true},
	"CredentialStoreToken":           {classRemote, "P5b", true},
	"CredentialVerify":               {classRemote, "P4", false},
	"DeleteAccount":                  {classRemote, "P5", true},
	"DeleteMirrorGroup":              {classRemote, "P5", true},
	"DeleteMirrorRepo":               {classRemote, "P5", true},
	"DeleteRepoFolder":               {classRemote, "P5", true},
	"StopTrackingRepo":               {classRemote, "P5", true},
	"DetectEditors":                  {classLocal, "P1b", false},
	"DiscardAccount":                 {classRemote, "P5", true},
	"Discover":                       {classRemote, "P4", false},
	"DiscoverMirrors":                {classRemote, "P4", false},
	"DoctorPrecheck":                 {classRemote, "P4", false},
	"DoctorRun":                      {classRemote, "P4", false},
	"DomReady":                       {classMixed, "P1a", false},
	"ExportDiagnostics":              {classLocal, "diagnostics", false},
	"ExportRecipe":                   {classLocal, "fleet recipes", false},
	"FetchAllRepos":                  {classRemote, "P4", false},
	"FetchRepo":                      {classRemote, "P4", false},
	"FixGlobalGCMConfig":             {classRemote, "P5", true},
	"GetAllStatus":                   {classRemote, "P4", false},
	"GetAppVersion":                  {classMixed, "P2", false},
	"GetAutostart":                   {classLocal, "P1a", false},
	"GetCheckGlobalGitignore":        {classRemote, "P4", false},
	"GetCollapsed":                   {classMixed, "P1b", false},
	"GetConfig":                      {classMixed, "P1b", false},
	"GetConfigLoadError":             {classRemote, "P4", false},
	"GetConfigPath":                  {classRemote, "P4", false},
	"GetLanguage":                    {classLocal, "P1b", false},
	"GetIdentityProof":               {classRemote, "identity proof", false},
	"GetMirrorStatus":                {classRemote, "P4", false},
	"GetNestedScanDepth":             {classRemote, "P4", false},
	"GetOS":                          {classMixed, "P4", false},
	"GetPRSettings":                  {classRemote, "P4", false},
	"GetPRsForRepo":                  {classRemote, "P4", false},
	"GetPeriodicSync":                {classMixed, "P1b", false},
	"GetRepoDetail":                  {classRemote, "P4", false},
	"GetScheduledTasks":              {classLocal, "scheduler", false},
	"GetStartupSnapshot":             {classMixed, "P1b", false},
	"GetStartupTiming":               {classLocal, "startup", false},
	"GetTokenGuide":                  {classRemote, "P4", false},
	"GetUIMode":                      {classLocal, "P1b", false},
	"GetViewMode":                    {classLocal, "P1b", false},
	"GetWindowMode":                  {classLocal, "P1a", false},
	"IgnoreClone":                    {classRemote, "P5", true},
	"InitRoot":                       {classMixed, "P5", true},
	"InstallGlobalGitignore":         {classRemote, "P5", true},
	"IsFirstRun":                     {classRemote, "P4", false},
	"IsGlobalGCMConfigNeeded":        {classRemote, "P4", false},
	"IsPositionOnScreen":             {classLocal, "P1a", false},
	"ListAccountOrgs":                {classRemote, "P4", false},
	"ListConfigBackups":              {classRemote, "P4", false},
	"ListMoveDestinationOwners":      {classRemote, "P4", false},
	"ListRemoteRepos":                {classRemote, "P4", false},
	"ListRemotes":                    {classLocal, "P3b", false},
	"ListRoots":                      {classRemote, "P4", false},
	"ListShells":                     {classLocal, "P1b", false},
	"ListTerminalApps":               {classLocal, "P1b", false},
	"ListTerminalProfiles":           {classLocal, "P1b", false},
	"MarkStartup":                    {classLocal, "startup", false},
	"MissingModernTerminal":          {classLocal, "P1a", false},
	"MoveRepo":                       {classRemote, "P5", true},
	"OpenAccountFolder":              {classMixed, "P6", false},
	"OpenAccountInAIHarness":         {classMixed, "P6", false},
	"OpenAccountInApp":               {classMixed, "P6", false},
	"OpenAccountInBrowser":           {classMixed, "P4", false},
	"OpenAccountProfile":             {classMixed, "P6", false},
	"OpenFileInEditor":               {classLocal, "P6", false},
	"OpenInAIHarness":                {classLocal, "P6", false},
	"OpenInApp":                      {classLocal, "P6", false},
	"OpenInBrowser":                  {classLocal, "P4", false},
	"OpenInExplorer":                 {classLocal, "P6", false},
	"OpenProfile":                    {classLocal, "P6", false},
	"OpenRepoConfigInEditor":         {classLocal, "identity proof", false},
	"OpenRecipeImport":               {classLocal, "fleet recipes", false},
	"OpenRepoInBrowser":              {classMixed, "P4", false},
	"OpenTerminalsManagerWindow":     {classLocal, "P1a", false},
	"PickFolder":                     {classLocal, "P5", false},
	"PreflightMove":                  {classRemote, "P4", false},
	"PreviewClonePath":               {classRemote, "P4", false},
	"PreviewDiagnostics":             {classLocal, "diagnostics", false},
	"PreviewRecipeExport":            {classLocal, "fleet recipes", false},
	"PreviewRecipeImport":            {classLocal, "fleet recipes", false},
	"PreviewPullAll":                 {classRemote, "P4", false},
	"PreviewRoot":                    {classRemote, "P4", false},
	"PreviewSweep":                   {classRemote, "P4", false},
	"PullAll":                        {classRemote, "P5", true},
	"PullRepo":                       {classRemote, "P5", true},
	"RecommendedGlobalGitignoreBody": {classRemote, "P4", false},
	"Reconcile":                      {classRemote, "P5", true},
	"RedetectProfiles":               {classMixed, "P1b", false},
	"RefreshAIHarnesses":             {classRemote, "P6", false}, // not Mutating: see the harness detection note
	"RefreshAllPRs":                  {classRemote, "P4", false},
	"RefreshStatus":                  {classRemote, "P4", false},
	"ReloadConfig":                   {classMixed, "P1b", false},
	"RemoteDisconnect":               {classLocal, "P3b", false},
	"RemoteInfo":                     {classLocal, "P3b", false},
	"RemoteLog":                      {classLocal, "P3b", false},
	"RemoteReconnect":                {classLocal, "P3b", false},
	"RemoteReinstall":                {classLocal, "P3b", false},
	"RemoteRunDoctor":                {classLocal, "P3b", false},
	"RemoteUninstall":                {classLocal, "P3b", false},
	"RemoveGlobalIdentity":           {classRemote, "P5", true},
	"RemoveRemote":                   {classLocal, "P3b", false},
	"RemoveRoot":                     {classRemote, "P5", true},
	"RenameAccount":                  {classRemote, "P5", true},
	"RepairConfig":                   {classRemote, "P5", true},
	"RepoNesting":                    {classRemote, "P4", false},
	"RepoWorkspaceFiles":             {classRemote, "P4", false},
	"ResolveUpdateRecovery":          {classLocal, "P1a", false},
	"RepairRepoIdentity":             {classRemote, "identity proof", true},
	"RestoreFromBackup":              {classRemote, "P5", true},
	"SaveTerminalProfiles":           {classLocal, "P1b", false},
	"SaveTerminalProfilesDTO":        {classLocal, "P1b", false},
	"RunTask":                        {classLocal, "scheduler", false},
	"ScanFolderForClones":            {classRemote, "P4", false},
	"SetAutostart":                   {classLocal, "P1a", false},
	"SetCheckGlobalGitignore":        {classRemote, "P5", true},
	"SetCollapsed":                   {classMixed, "P1b", false},
	"SetDefaultRoot":                 {classRemote, "P5", true},
	"SetLanguage":                    {classLocal, "P1b", false},
	"SetNestedScanDepth":             {classRemote, "P5", true},
	"SetPRBadgesEnabled":             {classRemote, "P5", true},
	"SetPRIncludeDrafts":             {classRemote, "P5", true},
	"SetPeriodicSync":                {classMixed, "P1b", false},
	"SetRemoteEditor":                {classLocal, "P6", false},
	"SetRepoContainer":               {classRemote, "P5", true},
	"SetRepoWorkspace":               {classRemote, "P5", true},
	"SetShowPullPlan":                {classLocal, "P1b", false},
	"SetTheme":                       {classLocal, "P1b", false},
	"SetUIMode":                      {classLocal, "P1b", false},
	"SetViewMode":                    {classLocal, "P1b", false},
	"SetupMirrorRepo":                {classRemote, "P5", true},
	"ShowWindow":                     {classLocal, "P1a", false},
	"Shutdown":                       {classMixed, "P1a", false},
	"Startup":                        {classMixed, "P1a", false},
	"SuggestAccountKey":              {classRemote, "P4", false},
	"SyncAIHarnesses":                {classRemote, "P6", false}, // not Mutating: see the harness detection note
	"SyncEditors":                    {classLocal, "P1b", false},
	"SyncProfiles":                   {classLocal, "P1b", false},
	"UpdateAccount":                  {classRemote, "P5", true},
	"ValidateRemotePath":             {classRemote, "P5", false},
}

// backendOnlyClass covers the Backend methods that are not Wails-bound: the
// server serves them, and a bound method of *App calls them.
var backendOnlyClass = map[string]methodInfo{
	// AccountFolder is the host half of the account launchers of a remote
	// window: the host resolves the folder, this computer opens it (#243).
	"AccountFolder": {classRemote, "P6", false},
	"RunFleetCheck": {classRemote, "scheduler", false},
	// The interval and state of the fleet checks a host's daemon runs
	// while no window is connected (#247). Setting the interval changes
	// only the daemon's memory, and the same value twice changes nothing.
	"SetFleetSchedule": {classRemote, "P10", false},
	"FleetSchedule":    {classRemote, "P10", false},
}

// rememberedMethod reports whether a host remembers a call of method by
// its request id and answers a repeat with the first outcome: every
// mutation (#294), and the fleet checks, whose repeat after a reconnect
// joins the run the first attempt started instead of starting another
// (#247).
func rememberedMethod(method string) bool {
	info, ok := classOf(method)
	return ok && (info.Mutating || method == "RunFleetCheck")
}

// classOf is the classification of a Backend method, bound or not.
func classOf(method string) (methodInfo, bool) {
	if info, ok := methodClass[method]; ok {
		return info, true
	}
	info, ok := backendOnlyClass[method]
	return info, ok
}

// eventClass covers every event name passed to EventSink.Emit.
var eventClass = map[string]eventInfo{
	"clone:start":              {classRemote, "P5"},
	"clone:done":               {classRemote, "P5"},
	"clone:progress":           {classRemote, "P5"},
	"credentials:updated":      {classRemote, "P4"},
	"discover:done":            {classRemote, "P4"},
	"fetch:alldone":            {classRemote, "P4"},
	"fetch:allstart":           {classRemote, "P4"},
	"fetch:done":               {classRemote, "P4"},
	"fetch:start":              {classRemote, "P4"},
	"harnesses:updated":        {classRemote, "P6"},
	"mirror:discover:done":     {classRemote, "P4"},
	"mirror:discover:progress": {classRemote, "P4"},
	"mirror:setup:done":        {classRemote, "P5"},
	"mirror:status":            {classRemote, "P4"},
	"move:done":                {classRemote, "P5"},
	"move:progress":            {classRemote, "P5"},
	"network:state":            {classRemote, "P4"},
	"pr:refreshed":             {classRemote, "P4"},
	"profiles:reloaded":        {classLocal, "P1a"},
	"pull:start":               {classRemote, "P5"},
	"pull:allstart":            {classRemote, "P5"},
	"pull:alldone":             {classRemote, "P5"},
	"pull:done":                {classRemote, "P5"},
	"reconcile:done":           {classRemote, "P5"},
	"remote:log":               {classLocal, "P3b"},
	"remote:state":             {classLocal, "P3b"},
	"status:one":               {classRemote, "P5"},
	"status:batch":             {classRemote, "P4"},
	"status:updated":           {classRemote, "P4"},
	"tools:updated":            {classLocal, "P1b"},
	"update:available":         {classLocal, "P1a"},
	"update:done":              {classLocal, "P1a"},
	"update:progress":          {classLocal, "P1a"},
	"update:quit":              {classLocal, "P1a"},
	"update:recoveryExpired":   {classLocal, "P1a"},
	"update:restarting":        {classLocal, "P1a"},
}
