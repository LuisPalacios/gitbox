package main

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/LuisPalacios/gitbox/pkg/identity"
)

// The Backend methods of RemoteBackend. Each one calls the server method of
// the same name with the same arguments; the ones that return a long list
// cut it (capList, capConfig). TestRemoteBackendCallsEachMethodByName
// fails when a line names the wrong method or drops an argument, and the
// compiler fails when Backend gains a method that has no line here.

func (r *RemoteBackend) AccountDeletionImpact(accountKey string) AccountDeletionImpactDTO {
	return remoteValue[AccountDeletionImpactDTO](r, "AccountDeletionImpact", accountKey)
}
func (r *RemoteBackend) AcknowledgeConfigError()                { r.callVoid("AcknowledgeConfigError") }
func (r *RemoteBackend) AddAccount(req AddAccountRequest) error { return r.callErr("AddAccount", req) }
func (r *RemoteBackend) AddDiscoveredRepos(key string, repoNames []string) error {
	return r.callErr("AddDiscoveredRepos", key, repoNames)
}
func (r *RemoteBackend) AddDiscoveredReposToFolder(key string, repoNames []string, cloneFolder string) error {
	return r.callErr("AddDiscoveredReposToFolder", key, repoNames, cloneFolder)
}
func (r *RemoteBackend) AddMirrorGroup(key, accountSrc, accountDst string) error {
	return r.callErr("AddMirrorGroup", key, accountSrc, accountDst)
}
func (r *RemoteBackend) AddMirrorRepo(mirrorKey, repoKey, direction, origin string) error {
	return r.callErr("AddMirrorRepo", mirrorKey, repoKey, direction, origin)
}
func (r *RemoteBackend) AddRoot(path string) error { return r.callErr("AddRoot", path) }
func (r *RemoteBackend) AdoptClones(reqs []AdoptRequestDTO) ReconcileDTO {
	return remoteValue[ReconcileDTO](r, "AdoptClones", reqs)
}
func (r *RemoteBackend) ApplyDiscoveredMirrors() error { return r.callErr("ApplyDiscoveredMirrors") }
func (r *RemoteBackend) ChangeCredentialType(accountKey, newType string) error {
	return r.callErr("ChangeCredentialType", accountKey, newType)
}
func (r *RemoteBackend) CheckGlobalGCMConfig() credential.GlobalGCMConfigStatus {
	return remoteValue[credential.GlobalGCMConfigStatus](r, "CheckGlobalGCMConfig")
}
func (r *RemoteBackend) CheckGlobalGitignore() (gitignore.Status, error) {
	return remoteCall[gitignore.Status](r, "CheckGlobalGitignore")
}
func (r *RemoteBackend) CheckGlobalIdentity() identity.GlobalIdentityStatus {
	return remoteValue[identity.GlobalIdentityStatus](r, "CheckGlobalIdentity")
}
func (r *RemoteBackend) CheckMirrorCredentials(accountKey string) MirrorCredentialCheck {
	return remoteValue[MirrorCredentialCheck](r, "CheckMirrorCredentials", accountKey)
}
func (r *RemoteBackend) CheckMoveReadiness(sourceSourceKey, destAccountKey string, deleteSource bool) MoveReadinessDTO {
	return remoteValue[MoveReadinessDTO](r, "CheckMoveReadiness", sourceSourceKey, destAccountKey, deleteSource)
}
func (r *RemoteBackend) CloneRepo(sourceKey, repoKey, root string) {
	r.callVoid("CloneRepo", sourceKey, repoKey, root)
}
func (r *RemoteBackend) ConfirmSweep(sourceKey, repoKey string) SweepDeleteDTO {
	return remoteValue[SweepDeleteDTO](r, "ConfirmSweep", sourceKey, repoKey)
}
func (r *RemoteBackend) CreateNewRepo(accountKey, owner, repoName, description string, private, cloneAfter bool, root string) error {
	return r.callErr("CreateNewRepo", accountKey, owner, repoName, description, private, cloneAfter, root)
}
func (r *RemoteBackend) CredentialDelete(accountKey string) CredentialSetupResult {
	return remoteValue[CredentialSetupResult](r, "CredentialDelete", accountKey)
}
func (r *RemoteBackend) CredentialRegenerateSSH(accountKey string) CredentialSetupResult {
	return remoteValue[CredentialSetupResult](r, "CredentialRegenerateSSH", accountKey)
}
func (r *RemoteBackend) CredentialSetupGCM(accountKey string) CredentialSetupResult {
	return remoteValue[CredentialSetupResult](r, "CredentialSetupGCM", accountKey)
}
func (r *RemoteBackend) CredentialSetupSSH(accountKey string) CredentialSetupResult {
	return remoteValue[CredentialSetupResult](r, "CredentialSetupSSH", accountKey)
}
func (r *RemoteBackend) CredentialStoreToken(accountKey, token string) CredentialSetupResult {
	return remoteValue[CredentialSetupResult](r, "CredentialStoreToken", accountKey, token)
}
func (r *RemoteBackend) CredentialVerify(key string) CredentialStatus {
	return remoteValue[CredentialStatus](r, "CredentialVerify", key)
}
func (r *RemoteBackend) DeleteAccount(accountKey string) error {
	return r.callErr("DeleteAccount", accountKey)
}
func (r *RemoteBackend) DeleteMirrorGroup(key string) error {
	return r.callErr("DeleteMirrorGroup", key)
}
func (r *RemoteBackend) DeleteMirrorRepo(mirrorKey, repoKey string) error {
	return r.callErr("DeleteMirrorRepo", mirrorKey, repoKey)
}
func (r *RemoteBackend) DeleteRepo(sourceKey, repoKey string) error {
	return r.callErr("DeleteRepo", sourceKey, repoKey)
}
func (r *RemoteBackend) DiscardAccount(accountKey string) error {
	return r.callErr("DiscardAccount", accountKey)
}
func (r *RemoteBackend) Discover(accountKey string) { r.callVoid("Discover", accountKey) }
func (r *RemoteBackend) DiscoverMirrors()           { r.callVoid("DiscoverMirrors") }
func (r *RemoteBackend) DoctorPrecheck(credentialType string) DoctorPrecheckDTO {
	return remoteValue[DoctorPrecheckDTO](r, "DoctorPrecheck", credentialType)
}
func (r *RemoteBackend) DoctorRun() DoctorReport {
	return remoteValue[DoctorReport](r, "DoctorRun")
}
func (r *RemoteBackend) FetchAllRepos() error { return r.callErr("FetchAllRepos") }
func (r *RemoteBackend) FetchRepo(sourceKey, repoKey string) {
	r.callVoid("FetchRepo", sourceKey, repoKey)
}
func (r *RemoteBackend) FixGlobalGCMConfig() error { return r.callErr("FixGlobalGCMConfig") }
func (r *RemoteBackend) GetAllStatus() []StatusResult {
	return capList(r, "statuses", remoteValue[[]StatusResult](r, "GetAllStatus"), maxRemoteRepos)
}
func (r *RemoteBackend) GetAppVersion() string {
	return remoteValue[string](r, "GetAppVersion")
}
func (r *RemoteBackend) GetCheckGlobalGitignore() bool {
	return remoteValue[bool](r, "GetCheckGlobalGitignore")
}
func (r *RemoteBackend) GetCollapsed() config.CollapsedState {
	return remoteValue[config.CollapsedState](r, "GetCollapsed")
}
func (r *RemoteBackend) GetConfig() (ConfigDTO, error) {
	dto, err := remoteCall[ConfigDTO](r, "GetConfig")
	capConfig(r, &dto)
	return dto, err
}
func (r *RemoteBackend) GetConfigLoadError() string {
	return remoteValue[string](r, "GetConfigLoadError")
}
func (r *RemoteBackend) GetConfigPath() string {
	return remoteValue[string](r, "GetConfigPath")
}
func (r *RemoteBackend) GetMirrorStatus(mirrorKey string) { r.callVoid("GetMirrorStatus", mirrorKey) }
func (r *RemoteBackend) GetNestedScanDepth() int {
	return remoteValue[int](r, "GetNestedScanDepth")
}
func (r *RemoteBackend) GetOS() string {
	return remoteValue[string](r, "GetOS")
}
func (r *RemoteBackend) GetPRSettings() PRSettingsDTO {
	return remoteValue[PRSettingsDTO](r, "GetPRSettings")
}
func (r *RemoteBackend) GetPRsForRepo(sourceKey, repoKey string) PRSummaryDTO {
	return remoteValue[PRSummaryDTO](r, "GetPRsForRepo", sourceKey, repoKey)
}
func (r *RemoteBackend) GetPeriodicSync() string {
	return remoteValue[string](r, "GetPeriodicSync")
}
func (r *RemoteBackend) GetRepoDetail(sourceKey, repoKey string) RepoDetail {
	return remoteValue[RepoDetail](r, "GetRepoDetail", sourceKey, repoKey)
}
func (r *RemoteBackend) GetStartupSnapshot() (StartupSnapshot, error) {
	snap, err := remoteCall[StartupSnapshot](r, "GetStartupSnapshot")
	capConfig(r, &snap.Config)
	return snap, err
}
func (r *RemoteBackend) GetTokenGuide(accountKey string) TokenGuideInfo {
	return remoteValue[TokenGuideInfo](r, "GetTokenGuide", accountKey)
}
func (r *RemoteBackend) IgnoreClone(path string, ignore bool) error {
	return r.callErr("IgnoreClone", path, ignore)
}
func (r *RemoteBackend) InitRoot(folder string) error { return r.callErr("InitRoot", folder) }
func (r *RemoteBackend) InstallGlobalGitignore() (gitignore.InstallResult, error) {
	return remoteCall[gitignore.InstallResult](r, "InstallGlobalGitignore")
}
func (r *RemoteBackend) IsFirstRun() bool {
	return remoteValue[bool](r, "IsFirstRun")
}
func (r *RemoteBackend) IsGlobalGCMConfigNeeded() bool {
	return remoteValue[bool](r, "IsGlobalGCMConfigNeeded")
}
func (r *RemoteBackend) ListAccountOrgs(accountKey string) ([]string, error) {
	return remoteCall[[]string](r, "ListAccountOrgs", accountKey)
}
func (r *RemoteBackend) ListConfigBackups() []ConfigBackupInfo {
	return remoteValue[[]ConfigBackupInfo](r, "ListConfigBackups")
}
func (r *RemoteBackend) ListMoveDestinationOwners(sourceKey string) []MoveOwnerOption {
	return remoteValue[[]MoveOwnerOption](r, "ListMoveDestinationOwners", sourceKey)
}
func (r *RemoteBackend) ListRemoteRepos(accountKey string) []DiscoverResult {
	return capList(r, "remote repos", remoteValue[[]DiscoverResult](r, "ListRemoteRepos", accountKey), maxRemoteRepos)
}
func (r *RemoteBackend) ListRoots() []RootDTO {
	return remoteValue[[]RootDTO](r, "ListRoots")
}
func (r *RemoteBackend) MoveRepo(req MoveRequestDTO) { r.callVoid("MoveRepo", req) }
func (r *RemoteBackend) AccountFolder(accountKey string) (string, error) {
	return remoteCall[string](r, "AccountFolder", accountKey)
}
func (r *RemoteBackend) OpenAccountFolder(accountKey string) error {
	return r.callErr("OpenAccountFolder", accountKey)
}
func (r *RemoteBackend) OpenAccountInAIHarness(accountKey string, command string, args []string) error {
	return r.callErr("OpenAccountInAIHarness", accountKey, command, args)
}
func (r *RemoteBackend) OpenAccountInApp(accountKey string, command string) error {
	return r.callErr("OpenAccountInApp", accountKey, command)
}
func (r *RemoteBackend) OpenAccountInBrowser(accountKey string) (string, error) {
	return remoteCall[string](r, "OpenAccountInBrowser", accountKey)
}
func (r *RemoteBackend) OpenAccountProfile(accountKey, profileID string) error {
	return r.callErr("OpenAccountProfile", accountKey, profileID)
}
func (r *RemoteBackend) OpenRepoInBrowser(sourceKey, repoKey string) (string, error) {
	return remoteCall[string](r, "OpenRepoInBrowser", sourceKey, repoKey)
}
func (r *RemoteBackend) PreflightMove(req MoveRequestDTO) MovePreflightDTO {
	return remoteValue[MovePreflightDTO](r, "PreflightMove", req)
}
func (r *RemoteBackend) PreviewSweep(sourceKey, repoKey string) SweepPreviewDTO {
	return remoteValue[SweepPreviewDTO](r, "PreviewSweep", sourceKey, repoKey)
}
func (r *RemoteBackend) PullRepo(sourceKey, repoKey string) {
	r.callVoid("PullRepo", sourceKey, repoKey)
}
func (r *RemoteBackend) RecommendedGlobalGitignoreBody() string {
	return remoteValue[string](r, "RecommendedGlobalGitignoreBody")
}
func (r *RemoteBackend) Reconcile() ReconcileDTO {
	return remoteValue[ReconcileDTO](r, "Reconcile")
}
func (r *RemoteBackend) RedetectProfiles() (ConfigDTO, error) {
	return remoteCall[ConfigDTO](r, "RedetectProfiles")
}
func (r *RemoteBackend) RefreshAIHarnesses() { r.callVoid("RefreshAIHarnesses") }
func (r *RemoteBackend) RefreshAllPRs()      { r.callVoid("RefreshAllPRs") }
func (r *RemoteBackend) RefreshStatus()      { r.callVoid("RefreshStatus") }
func (r *RemoteBackend) ReloadConfig() (ConfigDTO, error) {
	dto, err := remoteCall[ConfigDTO](r, "ReloadConfig")
	capConfig(r, &dto)
	return dto, err
}
func (r *RemoteBackend) RemoveGlobalIdentity() error { return r.callErr("RemoveGlobalIdentity") }
func (r *RemoteBackend) RemoveRoot(path string, forget bool) (RemoveRootDTO, error) {
	return remoteCall[RemoveRootDTO](r, "RemoveRoot", path, forget)
}
func (r *RemoteBackend) RenameAccount(oldKey, newKey string) error {
	return r.callErr("RenameAccount", oldKey, newKey)
}
func (r *RemoteBackend) RepairConfig() ConfigRepairResult {
	return remoteValue[ConfigRepairResult](r, "RepairConfig")
}
func (r *RemoteBackend) RepoNesting() map[string]string {
	return remoteValue[map[string]string](r, "RepoNesting")
}
func (r *RemoteBackend) RepoWorkspaceFiles(sourceKey, repoKey string) []string {
	return remoteValue[[]string](r, "RepoWorkspaceFiles", sourceKey, repoKey)
}
func (r *RemoteBackend) RestoreFromBackup(backupPath string) ConfigRepairResult {
	return remoteValue[ConfigRepairResult](r, "RestoreFromBackup", backupPath)
}
func (r *RemoteBackend) ScanFolderForClones(path string) []OrphanRepoDTO {
	return remoteValue[[]OrphanRepoDTO](r, "ScanFolderForClones", path)
}
func (r *RemoteBackend) SetCheckGlobalGitignore(enabled bool) error {
	return r.callErr("SetCheckGlobalGitignore", enabled)
}
func (r *RemoteBackend) SetCollapsed(state config.CollapsedState) error {
	return r.callErr("SetCollapsed", state)
}
func (r *RemoteBackend) SetDefaultRoot(path string) error { return r.callErr("SetDefaultRoot", path) }
func (r *RemoteBackend) SetNestedScanDepth(depth int) error {
	return r.callErr("SetNestedScanDepth", depth)
}
func (r *RemoteBackend) SetPRBadgesEnabled(enabled bool) error {
	return r.callErr("SetPRBadgesEnabled", enabled)
}
func (r *RemoteBackend) SetPRIncludeDrafts(enabled bool) error {
	return r.callErr("SetPRIncludeDrafts", enabled)
}
func (r *RemoteBackend) SetPeriodicSync(interval string) error {
	return r.callErr("SetPeriodicSync", interval)
}
func (r *RemoteBackend) SetRepoContainer(sourceKey, repoKey string, container bool) error {
	return r.callErr("SetRepoContainer", sourceKey, repoKey, container)
}
func (r *RemoteBackend) SetRepoWorkspace(sourceKey, repoKey, file string) error {
	return r.callErr("SetRepoWorkspace", sourceKey, repoKey, file)
}
func (r *RemoteBackend) SetupMirrorRepo(mirrorKey, repoKey string) {
	r.callVoid("SetupMirrorRepo", mirrorKey, repoKey)
}
func (r *RemoteBackend) SuggestAccountKey(provider, username string) string {
	return remoteValue[string](r, "SuggestAccountKey", provider, username)
}
func (r *RemoteBackend) SyncAIHarnesses() bool {
	return remoteValue[bool](r, "SyncAIHarnesses")
}
func (r *RemoteBackend) UpdateAccount(req UpdateAccountRequest) error {
	return r.callErr("UpdateAccount", req)
}
