// Types mirroring Go structs from pkg/config and cmd/gui/app.go

export interface GlobalConfig {
  folder: string;
  periodic_sync?: string;
  editors?: EditorInfo[];
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

export interface EditorInfo {
  id: string;
  name: string;
  command: string;
}
