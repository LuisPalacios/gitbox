export namespace config {
	
	export class AIHarnessEntry {
	    name: string;
	    command: string;
	    args?: string[];
	
	    static createFrom(source: any = {}) {
	        return new AIHarnessEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	export class GCMConfig {
	    provider?: string;
	    useHttpPath: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GCMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.useHttpPath = source["useHttpPath"];
	    }
	}
	export class SSHConfig {
	    host?: string;
	    hostname?: string;
	    key_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.hostname = source["hostname"];
	        this.key_type = source["key_type"];
	    }
	}
	export class Account {
	    provider: string;
	    url: string;
	    username: string;
	    name: string;
	    email: string;
	    default_credential_type?: string;
	    ssh?: SSHConfig;
	    gcm?: GCMConfig;
	
	    static createFrom(source: any = {}) {
	        return new Account(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.url = source["url"];
	        this.username = source["username"];
	        this.name = source["name"];
	        this.email = source["email"];
	        this.default_credential_type = source["default_credential_type"];
	        this.ssh = this.convertValues(source["ssh"], SSHConfig);
	        this.gcm = this.convertValues(source["gcm"], GCMConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EditorEntry {
	    name: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new EditorEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	    }
	}
	
	export class GCMGlobal {
	    helper?: string;
	    credential_store?: string;
	
	    static createFrom(source: any = {}) {
	        return new GCMGlobal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.helper = source["helper"];
	        this.credential_store = source["credential_store"];
	    }
	}
	export class TerminalEntry {
	    name: string;
	    command: string;
	    args?: string[];
	
	    static createFrom(source: any = {}) {
	        return new TerminalEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	export class TokenGlobal {
	
	
	    static createFrom(source: any = {}) {
	        return new TokenGlobal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SSHGlobal {
	    ssh_folder?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHGlobal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ssh_folder = source["ssh_folder"];
	    }
	}
	export class WindowState {
	    x: number;
	    y: number;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new WindowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class GlobalConfig {
	    folder: string;
	    periodic_sync?: string;
	    window?: WindowState;
	    compact_window?: WindowState;
	    view_mode?: string;
	    credential_ssh?: SSHGlobal;
	    credential_gcm?: GCMGlobal;
	    // Go type: TokenGlobal
	    credential_token?: any;
	    editors?: EditorEntry[];
	    terminals?: TerminalEntry[];
	    ai_harnesses?: AIHarnessEntry[];
	    pr_badges_enabled?: boolean;
	    pr_include_drafts?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GlobalConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folder = source["folder"];
	        this.periodic_sync = source["periodic_sync"];
	        this.window = this.convertValues(source["window"], WindowState);
	        this.compact_window = this.convertValues(source["compact_window"], WindowState);
	        this.view_mode = source["view_mode"];
	        this.credential_ssh = this.convertValues(source["credential_ssh"], SSHGlobal);
	        this.credential_gcm = this.convertValues(source["credential_gcm"], GCMGlobal);
	        this.credential_token = this.convertValues(source["credential_token"], null);
	        this.editors = this.convertValues(source["editors"], EditorEntry);
	        this.terminals = this.convertValues(source["terminals"], TerminalEntry);
	        this.ai_harnesses = this.convertValues(source["ai_harnesses"], AIHarnessEntry);
	        this.pr_badges_enabled = source["pr_badges_enabled"];
	        this.pr_include_drafts = source["pr_include_drafts"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MirrorRepo {
	    direction: string;
	    origin: string;
	    target_repo?: string;
	    last_sync?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorRepo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.direction = source["direction"];
	        this.origin = source["origin"];
	        this.target_repo = source["target_repo"];
	        this.last_sync = source["last_sync"];
	        this.error = source["error"];
	    }
	}
	export class Repo {
	    credential_type?: string;
	    name?: string;
	    email?: string;
	    id_folder?: string;
	    clone_folder?: string;
	
	    static createFrom(source: any = {}) {
	        return new Repo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.credential_type = source["credential_type"];
	        this.name = source["name"];
	        this.email = source["email"];
	        this.id_folder = source["id_folder"];
	        this.clone_folder = source["clone_folder"];
	    }
	}
	
	
	

}

export namespace git {
	
	export class FileChange {
	    kind: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.path = source["path"];
	    }
	}

}

export namespace identity {
	
	export class GlobalIdentityStatus {
	    hasName: boolean;
	    hasEmail: boolean;
	    name: string;
	    email: string;
	
	    static createFrom(source: any = {}) {
	        return new GlobalIdentityStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasName = source["hasName"];
	        this.hasEmail = source["hasEmail"];
	        this.name = source["name"];
	        this.email = source["email"];
	    }
	}

}

export namespace main {
	
	export class AIHarnessInfo {
	    id: string;
	    name: string;
	    command: string;
	    args: string[];
	
	    static createFrom(source: any = {}) {
	        return new AIHarnessInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	export class AddAccountRequest {
	    key: string;
	    provider: string;
	    url: string;
	    username: string;
	    name: string;
	    email: string;
	    credentialType: string;
	
	    static createFrom(source: any = {}) {
	        return new AddAccountRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.provider = source["provider"];
	        this.url = source["url"];
	        this.username = source["username"];
	        this.name = source["name"];
	        this.email = source["email"];
	        this.credentialType = source["credentialType"];
	    }
	}
	export class AdoptResultDTO {
	    adopted: number;
	    relocated: number;
	    skipped: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AdoptResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.adopted = source["adopted"];
	        this.relocated = source["relocated"];
	        this.skipped = source["skipped"];
	        this.error = source["error"];
	    }
	}
	export class WorkspaceMemberDTO {
	    source: string;
	    repo: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceMemberDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	    }
	}
	export class WorkspaceDTO {
	    type: string;
	    name?: string;
	    file?: string;
	    layout?: string;
	    members: WorkspaceMemberDTO[];
	    discovered?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.name = source["name"];
	        this.file = source["file"];
	        this.layout = source["layout"];
	        this.members = this.convertValues(source["members"], WorkspaceMemberDTO);
	        this.discovered = source["discovered"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MirrorDTO {
	    account_src: string;
	    account_dst: string;
	    repos: Record<string, config.MirrorRepo>;
	    repoOrder: string[];
	
	    static createFrom(source: any = {}) {
	        return new MirrorDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account_src = source["account_src"];
	        this.account_dst = source["account_dst"];
	        this.repos = this.convertValues(source["repos"], config.MirrorRepo, true);
	        this.repoOrder = source["repoOrder"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SourceDTO {
	    account: string;
	    folder?: string;
	    repos: Record<string, config.Repo>;
	    repoOrder: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account = source["account"];
	        this.folder = source["folder"];
	        this.repos = this.convertValues(source["repos"], config.Repo, true);
	        this.repoOrder = source["repoOrder"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConfigDTO {
	    version: number;
	    global: config.GlobalConfig;
	    accounts: Record<string, config.Account>;
	    sources: Record<string, SourceDTO>;
	    mirrors: Record<string, MirrorDTO>;
	    workspaces: Record<string, WorkspaceDTO>;
	    workspaceOrder: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.global = this.convertValues(source["global"], config.GlobalConfig);
	        this.accounts = this.convertValues(source["accounts"], config.Account, true);
	        this.sources = this.convertValues(source["sources"], SourceDTO, true);
	        this.mirrors = this.convertValues(source["mirrors"], MirrorDTO, true);
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceDTO, true);
	        this.workspaceOrder = source["workspaceOrder"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CredentialSetupResult {
	    ok: boolean;
	    message: string;
	    needsPAT?: boolean;
	    sshPublicKey?: string;
	    sshAddURL?: string;
	    sshVerified?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CredentialSetupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.needsPAT = source["needsPAT"];
	        this.sshPublicKey = source["sshPublicKey"];
	        this.sshAddURL = source["sshAddURL"];
	        this.sshVerified = source["sshVerified"];
	    }
	}
	export class CredentialStatus {
	    status: string;
	    message: string;
	    primary: string;
	    primaryMsg: string;
	    pat: string;
	    patMsg: string;
	
	    static createFrom(source: any = {}) {
	        return new CredentialStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.primary = source["primary"];
	        this.primaryMsg = source["primaryMsg"];
	        this.pat = source["pat"];
	        this.patMsg = source["patMsg"];
	    }
	}
	export class DiscoverResult {
	    fullName: string;
	    description: string;
	    private: boolean;
	    fork: boolean;
	    archived: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiscoverResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fullName = source["fullName"];
	        this.description = source["description"];
	        this.private = source["private"];
	        this.fork = source["fork"];
	        this.archived = source["archived"];
	    }
	}
	export class EditorInfo {
	    id: string;
	    name: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new EditorInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	    }
	}
	export class MirrorCredentialCheck {
	    accountKey: string;
	    hasMirrorToken: boolean;
	    needsPat: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorCredentialCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accountKey = source["accountKey"];
	        this.hasMirrorToken = source["hasMirrorToken"];
	        this.needsPat = source["needsPat"];
	        this.message = source["message"];
	    }
	}
	
	export class OrphanRepoDTO {
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
	
	    static createFrom(source: any = {}) {
	        return new OrphanRepoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.relPath = source["relPath"];
	        this.remoteURL = source["remoteURL"];
	        this.repoKey = source["repoKey"];
	        this.matchedAccount = source["matchedAccount"];
	        this.matchedSource = source["matchedSource"];
	        this.expectedPath = source["expectedPath"];
	        this.needsRelocate = source["needsRelocate"];
	        this.localOnly = source["localOnly"];
	        this.ambiguousCandidates = source["ambiguousCandidates"];
	    }
	}
	export class PRSettingsDTO {
	    enabled: boolean;
	    includeDrafts: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PRSettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.includeDrafts = source["includeDrafts"];
	    }
	}
	export class PullRequestDTO {
	    number: number;
	    title: string;
	    url: string;
	    author: string;
	    updated: string;
	    isDraft: boolean;
	    repoFull: string;
	
	    static createFrom(source: any = {}) {
	        return new PullRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.author = source["author"];
	        this.updated = source["updated"];
	        this.isDraft = source["isDraft"];
	        this.repoFull = source["repoFull"];
	    }
	}
	export class PRSummaryDTO {
	    authored: PullRequestDTO[];
	    reviewRequested: PullRequestDTO[];
	
	    static createFrom(source: any = {}) {
	        return new PRSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authored = this.convertValues(source["authored"], PullRequestDTO);
	        this.reviewRequested = this.convertValues(source["reviewRequested"], PullRequestDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RepoDetail {
	    branch: string;
	    ahead: number;
	    behind: number;
	    changed: git.FileChange[];
	    untracked: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.changed = this.convertValues(source["changed"], git.FileChange);
	        this.untracked = source["untracked"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class StatusResult {
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
	
	    static createFrom(source: any = {}) {
	        return new StatusResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	        this.account = source["account"];
	        this.path = source["path"];
	        this.state = source["state"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.modified = source["modified"];
	        this.untracked = source["untracked"];
	        this.conflicts = source["conflicts"];
	        this.error = source["error"];
	        this.branch = source["branch"];
	        this.isDefault = source["isDefault"];
	    }
	}
	export class SweepDeleteDTO {
	    deleted: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SweepDeleteDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deleted = source["deleted"];
	        this.error = source["error"];
	    }
	}
	export class SweepPreviewDTO {
	    merged: string[];
	    gone: string[];
	    squashed: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SweepPreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.merged = source["merged"];
	        this.gone = source["gone"];
	        this.squashed = source["squashed"];
	        this.error = source["error"];
	    }
	}
	export class TerminalInfo {
	    id: string;
	    name: string;
	    command: string;
	    args: string[];
	
	    static createFrom(source: any = {}) {
	        return new TerminalInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	export class TokenGuideInfo {
	    creationURL: string;
	    scopes: string;
	    guide: string;
	
	    static createFrom(source: any = {}) {
	        return new TokenGuideInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.creationURL = source["creationURL"];
	        this.scopes = source["scopes"];
	        this.guide = source["guide"];
	    }
	}
	export class UpdateAccountRequest {
	    key: string;
	    provider: string;
	    url: string;
	    username: string;
	    name: string;
	    email: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateAccountRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.provider = source["provider"];
	        this.url = source["url"];
	        this.username = source["username"];
	        this.name = source["name"];
	        this.email = source["email"];
	    }
	}
	export class WindowStateDTO {
	    x: number;
	    y: number;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new WindowStateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class WorkspaceCreateRequest {
	    key: string;
	    type: string;
	    name?: string;
	    file?: string;
	    layout?: string;
	    members?: WorkspaceMemberDTO[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceCreateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.file = source["file"];
	        this.layout = source["layout"];
	        this.members = this.convertValues(source["members"], WorkspaceMemberDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class WorkspaceGenerateResult {
	    file: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceGenerateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.size = source["size"];
	    }
	}
	export class WorkspaceListResult {
	    workspaces: Record<string, WorkspaceDTO>;
	    order: string[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceDTO, true);
	        this.order = source["order"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class WorkspaceUpdateRequest {
	    name: string;
	    layout: string;
	    members: WorkspaceMemberDTO[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceUpdateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.layout = source["layout"];
	        this.members = this.convertValues(source["members"], WorkspaceMemberDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

