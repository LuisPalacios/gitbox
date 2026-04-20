// Types mirroring Go structs from pkg/config and cmd/gui/app.go

export interface GlobalConfig {
  folder: string;
  periodic_sync?: string;
  editors?: EditorInfo[];
  terminals?: TerminalInfo[];
  ai_harnesses?: AIHarnessInfo[];
}

export interface SSHConfig {
  host: string;
  hostname: string;
  key_type: string;
}

export interface GCMConfig {
  provider: string;
  use_http_path: boolean;
}

export interface Account {
  provider: string;
  url: string;
  username: string;
  name: string;
  email: string;
  default_credential_type: string;
  ssh?: SSHConfig;
  gcm?: GCMConfig;
}

export interface Repo {
  credential_type?: string;
  name?: string;
  email?: string;
  id_folder?: string;
  clone_folder?: string;
}

export interface SourceDTO {
  account: string;
  folder?: string;
  repos: Record<string, Repo>;
  repoOrder: string[];
}

export interface MirrorRepo {
  direction: string;
  origin: string;
  target_repo?: string;
  last_sync?: string;
  error?: string;
}

export interface MirrorDTO {
  account_src: string;
  account_dst: string;
  repos: Record<string, MirrorRepo>;
  repoOrder: string[];
}

export interface MirrorStatusResult {
  mirrorKey: string;
  repoKey: string;
  direction: string;
  originAcct: string;
  backupAcct: string;
  syncStatus: string;
  headCommit: string;
  originHead: string;
  backupHead: string;
  warning: string;
  error: string;
}

export interface MirrorSetupResult {
  repoKey: string;
  created: boolean;
  mirrored: boolean;
  method: string;
  instructions: string;
  error: string;
}

export interface MirrorCredentialCheck {
  accountKey: string;
  hasMirrorToken: boolean;
  needsPat: boolean;
  message: string;
}

export interface ConfigDTO {
  version: number;
  global: GlobalConfig;
  accounts: Record<string, Account>;
  sources: Record<string, SourceDTO>;
  mirrors: Record<string, MirrorDTO>;
  workspaces: Record<string, WorkspaceDTO>;
  workspaceOrder: string[];
}

// ── Dynamic workspaces (issue #27, GUI slice #49) ──

export interface WorkspaceMemberDTO {
  source: string;
  repo: string;
}

export interface WorkspaceDTO {
  type: 'codeWorkspace' | 'tmuxinator';
  name?: string;
  file?: string;
  layout?: 'windowsPerRepo' | 'splitPanes';
  members: WorkspaceMemberDTO[];
  discovered?: boolean;
}

export interface WorkspaceCreateRequest {
  key: string;
  type: 'codeWorkspace' | 'tmuxinator';
  name?: string;
  file?: string;
  layout?: 'windowsPerRepo' | 'splitPanes';
  members?: WorkspaceMemberDTO[];
}

export interface WorkspaceUpdateRequest {
  name: string;
  layout: string;
  members: WorkspaceMemberDTO[] | null;
}

export interface WorkspaceGenerateResult {
  file: string;
  size: number;
}

export interface WorkspaceListResult {
  workspaces: Record<string, WorkspaceDTO>;
  order: string[];
}

export interface DiscoveredPathDTO {
  path: string;
  candidates: WorkspaceMemberDTO[];
}

export interface DiscoveredWorkspaceDTO {
  key: string;
  type: 'codeWorkspace' | 'tmuxinator';
  layout?: 'windowsPerRepo' | 'splitPanes';
  file: string;
  members?: WorkspaceMemberDTO[];
  ambig?: DiscoveredPathDTO[];
  noMatch?: string[];
  skipped?: string;
}

export interface DiscoverWorkspacesResult {
  adopted: string[];
  newCount: number;
  ambigCount: number;
  skippedCount: number;
  ambiguous?: DiscoveredWorkspaceDTO[];
}

export interface StatusResult {
  source: string;
  repo: string;
  account: string;
  path: string;
  state: string;
  ahead: number;
  behind: number;
  modified: number;
  untracked: number;
  conflicts: number;
  error?: string;
  branch?: string;
  isDefault?: boolean;
}

export interface DiscoverResult {
  fullName: string;
  description: string;
  private: boolean;
  fork: boolean;
  archived: boolean;
}

export interface CredentialStatus {
  status: string;
  message: string;
}

// Frontend-only UI state for a single repo row
export interface RepoState {
  status: 'clean' | 'dirty' | 'behind' | 'ahead' | 'diverged' | 'conflict' | 'not cloned' | 'no upstream' | 'error' | 'syncing' | 'cloning';
  path?: string;
  progress: number;
  behind: number;
  modified: number;
  untracked: number;
  ahead: number;
  error?: string;
  branch?: string;
  isDefault?: boolean;
}

export interface OrphanRepoDTO {
  path: string;
  relPath: string;
  remoteURL: string;
  repoKey: string;
  matchedAccount: string;
  matchedSource: string;
  expectedPath: string;
  needsRelocate: boolean;
  localOnly: boolean;
  ambiguousCandidates?: string[];
}

export interface AdoptResultDTO {
  adopted: number;
  relocated: number;
  skipped: number;
  error?: string;
}

export interface EditorInfo {
  id: string;
  name: string;
  command: string;
}

export interface TerminalInfo {
  id: string;
  name: string;
  command: string;
  args: string[];
}

export interface AIHarnessInfo {
  id: string;
  name: string;
  command: string;
  args: string[];
}

// ── PR / review indicators (issue #29) ──

export interface PullRequestDTO {
  number: number;
  title: string;
  url: string;
  author: string;
  updated: string;    // RFC3339, empty when unknown
  isDraft: boolean;
  repoFull: string;
}

export interface PRSummaryDTO {
  authored: PullRequestDTO[];
  reviewRequested: PullRequestDTO[];
}

export interface PRSettingsDTO {
  enabled: boolean;
  includeDrafts: boolean;
}

export interface PRAccountUpdateDTO {
  accountKey: string;
  byRepo: Record<string, PRSummaryDTO>; // repoFull (lowercase) -> summary
  error?: string;
}
