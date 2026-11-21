package main

import (
	"context"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/LuisPalacios/gitbox/pkg/identity"
)

// Backend is the work half of the app: config, git, credentials and
// providers, on the host that has the data. The frontend never knows which
// implementation it talks to. localBackend runs the work in this process;
// remote mode (#233) adds an implementation that calls a gitbox-server over
// SSH. Client-only behaviour (window, updater, dialogs, launching local
// apps) stays outside, in the local shell (shell.go).
type Backend interface {
	// Init loads the host's config. Start launches background workers.
	// Close stops them. None is Wails-bound.
	Init(ctx context.Context)
	Start()
	Close()

	// The REMOTE and MIXED entry points of classification.go, with the
	// signatures of their Wails-bound methods on *App.
	AccountDeletionImpact(accountKey string) AccountDeletionImpactDTO
	AcknowledgeConfigError()
	AddAccount(req AddAccountRequest) error
	AddDiscoveredRepos(key string, repoNames []string) error
	AddDiscoveredReposToFolder(key string, repoNames []string, cloneFolder string) error
	AddMirrorGroup(key, accountSrc, accountDst string) error
	AddMirrorRepo(mirrorKey, repoKey, direction, origin string) error
	AddRoot(path string) error
	AdoptClones(reqs []AdoptRequestDTO) ReconcileDTO
	ApplyDiscoveredMirrors() error
	ChangeCredentialType(accountKey, newType string) error
	CheckGlobalGCMConfig() credential.GlobalGCMConfigStatus
	CheckGlobalGitignore() (gitignore.Status, error)
	CheckGlobalIdentity() identity.GlobalIdentityStatus
	CheckMirrorCredentials(accountKey string) MirrorCredentialCheck
	CheckMoveReadiness(sourceSourceKey, destAccountKey string, deleteSource bool) MoveReadinessDTO
	CloneRepo(sourceKey, repoKey, root string)
	ConfirmSweep(sourceKey, repoKey string) SweepDeleteDTO
	CreateNewRepo(accountKey, owner, repoName, description string, private, cloneAfter bool, root string) error
	CredentialDelete(accountKey string) CredentialSetupResult
	CredentialRegenerateSSH(accountKey string) CredentialSetupResult
	CredentialSetupGCM(accountKey string) CredentialSetupResult
	CredentialSetupSSH(accountKey string) CredentialSetupResult
	CredentialStoreToken(accountKey, token string) CredentialSetupResult
	CredentialVerify(key string) CredentialStatus
	DeleteAccount(accountKey string) error
	DeleteMirrorGroup(key string) error
	DeleteMirrorRepo(mirrorKey, repoKey string) error
	DeleteRepo(sourceKey, repoKey string) error
	DiscardAccount(accountKey string) error
	Discover(accountKey string)
	DiscoverMirrors()
	DoctorPrecheck(credentialType string) DoctorPrecheckDTO
	DoctorRun() DoctorReport
	FetchAllRepos() error
	FetchRepo(sourceKey, repoKey string)
	FixGlobalGCMConfig() error
	GetAllStatus() []StatusResult
	GetAppVersion() string
	GetCheckGlobalGitignore() bool
	GetCollapsed() config.CollapsedState
	GetConfig() (ConfigDTO, error)
	GetConfigLoadError() string
	GetConfigPath() string
	GetMirrorStatus(mirrorKey string)
	GetNestedScanDepth() int
	GetOS() string
	GetPRSettings() PRSettingsDTO
	GetPRsForRepo(sourceKey, repoKey string) PRSummaryDTO
	GetPeriodicSync() string
	GetRepoDetail(sourceKey, repoKey string) RepoDetail
	GetStartupSnapshot() (StartupSnapshot, error)
	GetTokenGuide(accountKey string) TokenGuideInfo
	IgnoreClone(path string, ignore bool) error
	InitRoot(folder string) error
	InstallGlobalGitignore() (gitignore.InstallResult, error)
	IsFirstRun() bool
	IsGlobalGCMConfigNeeded() bool
	ListAccountOrgs(accountKey string) ([]string, error)
	ListConfigBackups() []ConfigBackupInfo
	ListMoveDestinationOwners(sourceKey string) []MoveOwnerOption
	ListRemoteRepos(accountKey string) []DiscoverResult
	ListRoots() []RootDTO
	MoveRepo(req MoveRequestDTO)
	OpenAccountFolder(accountKey string) error
	OpenAccountInAIHarness(accountKey string, command string, args []string) error
	OpenAccountInApp(accountKey string, command string) error
	OpenAccountInBrowser(accountKey string) error
	OpenAccountProfile(accountKey, profileID string) error
	OpenRepoInBrowser(sourceKey, repoKey string) error
	PreflightMove(req MoveRequestDTO) MovePreflightDTO
	PreviewSweep(sourceKey, repoKey string) SweepPreviewDTO
	PullRepo(sourceKey, repoKey string)
	RecommendedGlobalGitignoreBody() string
	Reconcile() ReconcileDTO
	RedetectProfiles() (ConfigDTO, error)
	RefreshAIHarnesses()
	RefreshAllPRs()
	RefreshStatus()
	ReloadConfig() (ConfigDTO, error)
	RemoveGlobalIdentity() error
	RemoveRoot(path string, forget bool) (RemoveRootDTO, error)
	RenameAccount(oldKey, newKey string) error
	RepairConfig() ConfigRepairResult
	RepoNesting() map[string]string
	RepoWorkspaceFiles(sourceKey, repoKey string) []string
	RestoreFromBackup(backupPath string) ConfigRepairResult
	ScanFolderForClones(path string) []OrphanRepoDTO
	SetCheckGlobalGitignore(enabled bool) error
	SetCollapsed(state config.CollapsedState) error
	SetDefaultRoot(path string) error
	SetNestedScanDepth(depth int) error
	SetPRBadgesEnabled(enabled bool) error
	SetPRIncludeDrafts(enabled bool) error
	SetPeriodicSync(interval string) error
	SetRepoContainer(sourceKey, repoKey string, container bool) error
	SetRepoWorkspace(sourceKey, repoKey, file string) error
	SetupMirrorRepo(mirrorKey, repoKey string)
	SuggestAccountKey(provider, username string) string
	SyncAIHarnesses() bool
	UpdateAccount(req UpdateAccountRequest) error
}

// localBackend serves Backend from this process. It owns no state: every
// method calls the original body, which stays on *App.
type localBackend struct{ a *App }

func (b localBackend) Init(ctx context.Context) { b.a.initLocal(ctx) }
func (b localBackend) Start()                   { b.a.startLocal() }
func (b localBackend) Close()                   { b.a.closeLocal() }

func (b localBackend) AccountDeletionImpact(accountKey string) AccountDeletionImpactDTO {
	return b.a.accountDeletionImpactLocal(accountKey)
}
func (b localBackend) AcknowledgeConfigError()                { b.a.acknowledgeConfigErrorLocal() }
func (b localBackend) AddAccount(req AddAccountRequest) error { return b.a.addAccountLocal(req) }
func (b localBackend) AddDiscoveredRepos(key string, repoNames []string) error {
	return b.a.addDiscoveredReposLocal(key, repoNames)
}
func (b localBackend) AddDiscoveredReposToFolder(key string, repoNames []string, cloneFolder string) error {
	return b.a.addDiscoveredReposToFolderLocal(key, repoNames, cloneFolder)
}
func (b localBackend) AddMirrorGroup(key, accountSrc, accountDst string) error {
	return b.a.addMirrorGroupLocal(key, accountSrc, accountDst)
}
func (b localBackend) AddMirrorRepo(mirrorKey, repoKey, direction, origin string) error {
	return b.a.addMirrorRepoLocal(mirrorKey, repoKey, direction, origin)
}
func (b localBackend) AddRoot(path string) error { return b.a.addRootLocal(path) }
func (b localBackend) AdoptClones(reqs []AdoptRequestDTO) ReconcileDTO {
	return b.a.adoptClonesLocal(reqs)
}
func (b localBackend) ApplyDiscoveredMirrors() error { return b.a.applyDiscoveredMirrorsLocal() }
func (b localBackend) ChangeCredentialType(accountKey, newType string) error {
	return b.a.changeCredentialTypeLocal(accountKey, newType)
}
func (b localBackend) CheckGlobalGCMConfig() credential.GlobalGCMConfigStatus {
	return b.a.checkGlobalGCMConfigLocal()
}
func (b localBackend) CheckGlobalGitignore() (gitignore.Status, error) {
	return b.a.checkGlobalGitignoreLocal()
}
func (b localBackend) CheckGlobalIdentity() identity.GlobalIdentityStatus {
	return b.a.checkGlobalIdentityLocal()
}
func (b localBackend) CheckMirrorCredentials(accountKey string) MirrorCredentialCheck {
	return b.a.checkMirrorCredentialsLocal(accountKey)
}
func (b localBackend) CheckMoveReadiness(sourceSourceKey, destAccountKey string, deleteSource bool) MoveReadinessDTO {
	return b.a.checkMoveReadinessLocal(sourceSourceKey, destAccountKey, deleteSource)
}
func (b localBackend) CloneRepo(sourceKey, repoKey, root string) {
	b.a.cloneRepoLocal(sourceKey, repoKey, root)
}
func (b localBackend) ConfirmSweep(sourceKey, repoKey string) SweepDeleteDTO {
	return b.a.confirmSweepLocal(sourceKey, repoKey)
}
func (b localBackend) CreateNewRepo(accountKey, owner, repoName, description string, private, cloneAfter bool, root string) error {
	return b.a.createNewRepoLocal(accountKey, owner, repoName, description, private, cloneAfter, root)
}
func (b localBackend) CredentialDelete(accountKey string) CredentialSetupResult {
	return b.a.credentialDeleteLocal(accountKey)
}
func (b localBackend) CredentialRegenerateSSH(accountKey string) CredentialSetupResult {
	return b.a.credentialRegenerateSSHLocal(accountKey)
}
func (b localBackend) CredentialSetupGCM(accountKey string) CredentialSetupResult {
	return b.a.credentialSetupGCMLocal(accountKey)
}
func (b localBackend) CredentialSetupSSH(accountKey string) CredentialSetupResult {
	return b.a.credentialSetupSSHLocal(accountKey)
}
func (b localBackend) CredentialStoreToken(accountKey, token string) CredentialSetupResult {
	return b.a.credentialStoreTokenLocal(accountKey, token)
}
func (b localBackend) CredentialVerify(key string) CredentialStatus {
	return b.a.credentialVerifyLocal(key)
}
func (b localBackend) DeleteAccount(accountKey string) error {
	return b.a.deleteAccountLocal(accountKey)
}
func (b localBackend) DeleteMirrorGroup(key string) error { return b.a.deleteMirrorGroupLocal(key) }
func (b localBackend) DeleteMirrorRepo(mirrorKey, repoKey string) error {
	return b.a.deleteMirrorRepoLocal(mirrorKey, repoKey)
}
func (b localBackend) DeleteRepo(sourceKey, repoKey string) error {
	return b.a.deleteRepoLocal(sourceKey, repoKey)
}
func (b localBackend) DiscardAccount(accountKey string) error {
	return b.a.discardAccountLocal(accountKey)
}
func (b localBackend) Discover(accountKey string) { b.a.discoverLocal(accountKey) }
func (b localBackend) DiscoverMirrors()           { b.a.discoverMirrorsLocal() }
func (b localBackend) DoctorPrecheck(credentialType string) DoctorPrecheckDTO {
	return b.a.doctorPrecheckLocal(credentialType)
}
func (b localBackend) DoctorRun() DoctorReport             { return b.a.doctorRunLocal() }
func (b localBackend) FetchAllRepos() error                { return b.a.fetchAllReposLocal() }
func (b localBackend) FetchRepo(sourceKey, repoKey string) { b.a.fetchRepoLocal(sourceKey, repoKey) }
func (b localBackend) FixGlobalGCMConfig() error           { return b.a.fixGlobalGCMConfigLocal() }
func (b localBackend) GetAllStatus() []StatusResult        { return b.a.getAllStatusLocal() }
func (b localBackend) GetAppVersion() string               { return b.a.getAppVersionLocal() }
func (b localBackend) GetCheckGlobalGitignore() bool       { return b.a.getCheckGlobalGitignoreLocal() }
func (b localBackend) GetCollapsed() config.CollapsedState { return b.a.getCollapsedLocal() }
func (b localBackend) GetConfig() (ConfigDTO, error)       { return b.a.getConfigLocal() }
func (b localBackend) GetConfigLoadError() string          { return b.a.getConfigLoadErrorLocal() }
func (b localBackend) GetConfigPath() string               { return b.a.getConfigPathLocal() }
func (b localBackend) GetMirrorStatus(mirrorKey string)    { b.a.getMirrorStatusLocal(mirrorKey) }
func (b localBackend) GetNestedScanDepth() int             { return b.a.getNestedScanDepthLocal() }
func (b localBackend) GetOS() string                       { return b.a.getOSLocal() }
func (b localBackend) GetPRSettings() PRSettingsDTO        { return b.a.getPRSettingsLocal() }
func (b localBackend) GetPRsForRepo(sourceKey, repoKey string) PRSummaryDTO {
	return b.a.getPRsForRepoLocal(sourceKey, repoKey)
}
func (b localBackend) GetPeriodicSync() string { return b.a.getPeriodicSyncLocal() }
func (b localBackend) GetRepoDetail(sourceKey, repoKey string) RepoDetail {
	return b.a.getRepoDetailLocal(sourceKey, repoKey)
}
func (b localBackend) GetStartupSnapshot() (StartupSnapshot, error) {
	return b.a.getStartupSnapshotLocal()
}
func (b localBackend) GetTokenGuide(accountKey string) TokenGuideInfo {
	return b.a.getTokenGuideLocal(accountKey)
}
func (b localBackend) IgnoreClone(path string, ignore bool) error {
	return b.a.ignoreCloneLocal(path, ignore)
}
func (b localBackend) InitRoot(folder string) error { return b.a.initRootLocal(folder) }
func (b localBackend) InstallGlobalGitignore() (gitignore.InstallResult, error) {
	return b.a.installGlobalGitignoreLocal()
}
func (b localBackend) IsFirstRun() bool              { return b.a.isFirstRunLocal() }
func (b localBackend) IsGlobalGCMConfigNeeded() bool { return b.a.isGlobalGCMConfigNeededLocal() }
func (b localBackend) ListAccountOrgs(accountKey string) ([]string, error) {
	return b.a.listAccountOrgsLocal(accountKey)
}
func (b localBackend) ListConfigBackups() []ConfigBackupInfo { return b.a.listConfigBackupsLocal() }
func (b localBackend) ListMoveDestinationOwners(sourceKey string) []MoveOwnerOption {
	return b.a.listMoveDestinationOwnersLocal(sourceKey)
}
func (b localBackend) ListRemoteRepos(accountKey string) []DiscoverResult {
	return b.a.listRemoteReposLocal(accountKey)
}
func (b localBackend) ListRoots() []RootDTO        { return b.a.listRootsLocal() }
func (b localBackend) MoveRepo(req MoveRequestDTO) { b.a.moveRepoLocal(req) }
func (b localBackend) OpenAccountFolder(accountKey string) error {
	return b.a.openAccountFolderLocal(accountKey)
}
func (b localBackend) OpenAccountInAIHarness(accountKey string, command string, args []string) error {
	return b.a.openAccountInAIHarnessLocal(accountKey, command, args)
}
func (b localBackend) OpenAccountInApp(accountKey string, command string) error {
	return b.a.openAccountInAppLocal(accountKey, command)
}
func (b localBackend) OpenAccountInBrowser(accountKey string) error {
	return b.a.openAccountInBrowserLocal(accountKey)
}
func (b localBackend) OpenAccountProfile(accountKey, profileID string) error {
	return b.a.openAccountProfileLocal(accountKey, profileID)
}
func (b localBackend) OpenRepoInBrowser(sourceKey, repoKey string) error {
	return b.a.openRepoInBrowserLocal(sourceKey, repoKey)
}
func (b localBackend) PreflightMove(req MoveRequestDTO) MovePreflightDTO {
	return b.a.preflightMoveLocal(req)
}
func (b localBackend) PreviewSweep(sourceKey, repoKey string) SweepPreviewDTO {
	return b.a.previewSweepLocal(sourceKey, repoKey)
}
func (b localBackend) PullRepo(sourceKey, repoKey string) { b.a.pullRepoLocal(sourceKey, repoKey) }
func (b localBackend) RecommendedGlobalGitignoreBody() string {
	return b.a.recommendedGlobalGitignoreBodyLocal()
}
func (b localBackend) Reconcile() ReconcileDTO              { return b.a.reconcileLocal() }
func (b localBackend) RedetectProfiles() (ConfigDTO, error) { return b.a.redetectProfilesLocal() }
func (b localBackend) RefreshAIHarnesses()                  { b.a.refreshAIHarnessesLocal() }
func (b localBackend) RefreshAllPRs()                       { b.a.refreshAllPRsLocal() }
func (b localBackend) RefreshStatus()                       { b.a.refreshStatusLocal() }
func (b localBackend) ReloadConfig() (ConfigDTO, error)     { return b.a.reloadConfigLocal() }
func (b localBackend) RemoveGlobalIdentity() error          { return b.a.removeGlobalIdentityLocal() }
func (b localBackend) RemoveRoot(path string, forget bool) (RemoveRootDTO, error) {
	return b.a.removeRootLocal(path, forget)
}
func (b localBackend) RenameAccount(oldKey, newKey string) error {
	return b.a.renameAccountLocal(oldKey, newKey)
}
func (b localBackend) RepairConfig() ConfigRepairResult { return b.a.repairConfigLocal() }
func (b localBackend) RepoNesting() map[string]string   { return b.a.repoNestingLocal() }
func (b localBackend) RepoWorkspaceFiles(sourceKey, repoKey string) []string {
	return b.a.repoWorkspaceFilesLocal(sourceKey, repoKey)
}
func (b localBackend) RestoreFromBackup(backupPath string) ConfigRepairResult {
	return b.a.restoreFromBackupLocal(backupPath)
}
func (b localBackend) ScanFolderForClones(path string) []OrphanRepoDTO {
	return b.a.scanFolderForClonesLocal(path)
}
func (b localBackend) SetCheckGlobalGitignore(enabled bool) error {
	return b.a.setCheckGlobalGitignoreLocal(enabled)
}
func (b localBackend) SetCollapsed(state config.CollapsedState) error {
	return b.a.setCollapsedLocal(state)
}
func (b localBackend) SetDefaultRoot(path string) error   { return b.a.setDefaultRootLocal(path) }
func (b localBackend) SetNestedScanDepth(depth int) error { return b.a.setNestedScanDepthLocal(depth) }
func (b localBackend) SetPRBadgesEnabled(enabled bool) error {
	return b.a.setPRBadgesEnabledLocal(enabled)
}
func (b localBackend) SetPRIncludeDrafts(enabled bool) error {
	return b.a.setPRIncludeDraftsLocal(enabled)
}
func (b localBackend) SetPeriodicSync(interval string) error {
	return b.a.setPeriodicSyncLocal(interval)
}
func (b localBackend) SetRepoContainer(sourceKey, repoKey string, container bool) error {
	return b.a.setRepoContainerLocal(sourceKey, repoKey, container)
}
func (b localBackend) SetRepoWorkspace(sourceKey, repoKey, file string) error {
	return b.a.setRepoWorkspaceLocal(sourceKey, repoKey, file)
}
func (b localBackend) SetupMirrorRepo(mirrorKey, repoKey string) {
	b.a.setupMirrorRepoLocal(mirrorKey, repoKey)
}
func (b localBackend) SuggestAccountKey(provider, username string) string {
	return b.a.suggestAccountKeyLocal(provider, username)
}
func (b localBackend) SyncAIHarnesses() bool { return b.a.syncAIHarnessesLocal() }
func (b localBackend) UpdateAccount(req UpdateAccountRequest) error {
	return b.a.updateAccountLocal(req)
}

// be returns the installed Backend, or the local one when none is set, so
// an App built as a bare struct (tests) keeps working.
func (a *App) be() Backend {
	if a.backend == nil {
		return localBackend{a}
	}
	return a.backend
}

// selectBackend picks the Backend for the main window. Without the
// --experimental-remote flag only the local backend can be built. With it,
// the result is still local: RemoteBackend exists, but nothing can reach a
// gitbox-server until the SSH connection lands (#233).
func selectBackend(a *App, experimentalRemote bool) Backend {
	_ = experimentalRemote
	return localBackend{a}
}
