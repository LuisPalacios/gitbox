// Types mirroring Go structs from pkg/config and cmd/gui/app.go

export interface GlobalConfig {
  folder: string;
  periodic_sync?: string;
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

export interface TokenConfig {
  env_var: string;
}

export interface Account {
  provider: string;
  url: string;
  username: string;
  name: string;
  email: string;
  default_branch: string;
  default_credential_type: string;
  ssh?: SSHConfig;
  gcm?: GCMConfig;
  token?: TokenConfig;
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

export interface ConfigDTO {
  version: number;
  global: GlobalConfig;
  accounts: Record<string, Account>;
  sources: Record<string, SourceDTO>;
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
  progress: number;
  behind: number;
  modified: number;
  untracked: number;
  ahead: number;
  error?: string;
}
