export namespace config {
	
	export class AIHarnessEntry {
	    name: string;
	    command: string;
	    args?: string[];
	    source?: string;
	    missing?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AIHarnessEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.source = source["source"];
	        this.missing = source["missing"];
	    }
	}
	export class CredentialTransition {
	    rename_to?: string;
	    previous_key?: string;
	    previous_type?: string;
	    staged_id?: string;
	    managed_ssh?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CredentialTransition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rename_to = source["rename_to"];
	        this.previous_key = source["previous_key"];
	        this.previous_type = source["previous_type"];
	        this.staged_id = source["staged_id"];
	        this.managed_ssh = source["managed_ssh"];
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
	    credential_transition?: CredentialTransition;
	
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
	        this.credential_transition = this.convertValues(source["credential_transition"], CredentialTransition);
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
	export class CollapsedState {
	    sources?: string[];
	    repos?: string[];
	    sidebar?: boolean;
	    view?: string;
	
	    static createFrom(source: any = {}) {
	        return new CollapsedState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sources = source["sources"];
	        this.repos = source["repos"];
	        this.sidebar = source["sidebar"];
	        this.view = source["view"];
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
	export class RemoteEntry {
	    alias: string;
	    server_dir?: string;
	    editor?: string;
	    server_os?: string;
	    server_arch?: string;
	    server_version?: string;
	    last_connected?: string;
	    window?: WindowState;
	    compact_window?: WindowState;
	
	    static createFrom(source: any = {}) {
	        return new RemoteEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.server_dir = source["server_dir"];
	        this.editor = source["editor"];
	        this.server_os = source["server_os"];
	        this.server_arch = source["server_arch"];
	        this.server_version = source["server_version"];
	        this.last_connected = source["last_connected"];
	        this.window = this.convertValues(source["window"], WindowState);
	        this.compact_window = this.convertValues(source["compact_window"], WindowState);
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
	export class TerminalProfile {
	    id: string;
	    name: string;
	    terminal: string;
	    shell?: string;
	    args?: string[];
	    default?: boolean;
	    preferred?: boolean;
	    hidden?: boolean;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new TerminalProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.terminal = source["terminal"];
	        this.shell = source["shell"];
	        this.args = source["args"];
	        this.default = source["default"];
	        this.preferred = source["preferred"];
	        this.hidden = source["hidden"];
	        this.source = source["source"];
	    }
	}
	export class ShellEntry {
	    id: string;
	    name: string;
	    command: string;
	    args?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ShellEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	export class TerminalApp {
	    id: string;
	    name: string;
	    command: string;
	    args_template?: string[];
	
	    static createFrom(source: any = {}) {
	        return new TerminalApp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args_template = source["args_template"];
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
	    roots: string[];
	    nested_scan_depth?: number;
	    ignored_clones?: string[];
	    language?: string;
	    periodic_sync?: string;
	    window?: WindowState;
	    compact_window?: WindowState;
	    view_mode?: string;
	    ui_mode?: string;
	    show_pull_plan?: boolean;
	    theme?: string;
	    collapsed?: CollapsedState;
	    credential_ssh?: SSHGlobal;
	    credential_gcm?: GCMGlobal;
	    // Go type: TokenGlobal
	    credential_token?: any;
	    editors?: EditorEntry[];
	    ai_harnesses?: AIHarnessEntry[];
	    terminals?: TerminalEntry[];
	    terminal_apps?: TerminalApp[];
	    shells?: ShellEntry[];
	    terminal_profiles?: TerminalProfile[];
	    pr_badges_enabled?: boolean;
	    pr_include_drafts?: boolean;
	    check_global_gitignore?: boolean;
	    experimental_remote?: boolean;
	    remotes?: RemoteEntry[];
	
	    static createFrom(source: any = {}) {
	        return new GlobalConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.roots = source["roots"];
	        this.nested_scan_depth = source["nested_scan_depth"];
	        this.ignored_clones = source["ignored_clones"];
	        this.language = source["language"];
	        this.periodic_sync = source["periodic_sync"];
	        this.window = this.convertValues(source["window"], WindowState);
	        this.compact_window = this.convertValues(source["compact_window"], WindowState);
	        this.view_mode = source["view_mode"];
	        this.ui_mode = source["ui_mode"];
	        this.show_pull_plan = source["show_pull_plan"];
	        this.theme = source["theme"];
	        this.collapsed = this.convertValues(source["collapsed"], CollapsedState);
	        this.credential_ssh = this.convertValues(source["credential_ssh"], SSHGlobal);
	        this.credential_gcm = this.convertValues(source["credential_gcm"], GCMGlobal);
	        this.credential_token = this.convertValues(source["credential_token"], null);
	        this.editors = this.convertValues(source["editors"], EditorEntry);
	        this.ai_harnesses = this.convertValues(source["ai_harnesses"], AIHarnessEntry);
	        this.terminals = this.convertValues(source["terminals"], TerminalEntry);
	        this.terminal_apps = this.convertValues(source["terminal_apps"], TerminalApp);
	        this.shells = this.convertValues(source["shells"], ShellEntry);
	        this.terminal_profiles = this.convertValues(source["terminal_profiles"], TerminalProfile);
	        this.pr_badges_enabled = source["pr_badges_enabled"];
	        this.pr_include_drafts = source["pr_include_drafts"];
	        this.check_global_gitignore = source["check_global_gitignore"];
	        this.experimental_remote = source["experimental_remote"];
	        this.remotes = this.convertValues(source["remotes"], RemoteEntry);
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
	export class PathConflict {
	    first: string;
	    second: string;
	
	    static createFrom(source: any = {}) {
	        return new PathConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.first = source["first"];
	        this.second = source["second"];
	    }
	}
	
	export class Repo {
	    fetch_all_remotes?: boolean;
	    credential_type?: string;
	    name?: string;
	    email?: string;
	    id_folder?: string;
	    clone_folder?: string;
	    root?: string;
	    container?: boolean;
	    workspace?: string;
	    workspace_preferred?: string;
	
	    static createFrom(source: any = {}) {
	        return new Repo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fetch_all_remotes = source["fetch_all_remotes"];
	        this.credential_type = source["credential_type"];
	        this.name = source["name"];
	        this.email = source["email"];
	        this.id_folder = source["id_folder"];
	        this.clone_folder = source["clone_folder"];
	        this.root = source["root"];
	        this.container = source["container"];
	        this.workspace = source["workspace"];
	        this.workspace_preferred = source["workspace_preferred"];
	    }
	}
	
	
	
	
	
	

}

export namespace credential {
	
	export class Finding {
	    path: string;
	    problem: string;
	    hint: string;
	    exposed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.problem = source["problem"];
	        this.hint = source["hint"];
	        this.exposed = source["exposed"];
	    }
	}
	export class GlobalGCMConfigStatus {
	    hasHelper: boolean;
	    helperValue: string;
	    expectedHelper: string;
	    hasCredentialStore: boolean;
	    credentialStoreValue: string;
	    expectedCredentialStore: string;
	    needsFix: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GlobalGCMConfigStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasHelper = source["hasHelper"];
	        this.helperValue = source["helperValue"];
	        this.expectedHelper = source["expectedHelper"];
	        this.hasCredentialStore = source["hasCredentialStore"];
	        this.credentialStoreValue = source["credentialStoreValue"];
	        this.expectedCredentialStore = source["expectedCredentialStore"];
	        this.needsFix = source["needsFix"];
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
	export class IncomingCommit {
	    hash: string;
	    author: string;
	    subject: string;
	
	    static createFrom(source: any = {}) {
	        return new IncomingCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.author = source["author"];
	        this.subject = source["subject"];
	    }
	}

}

export namespace gitignore {
	
	export class InstallResult {
	    path: string;
	    backupPath: string;
	    setExcludesfile: boolean;
	    updated: boolean;
	    alreadyUpToDate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InstallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.backupPath = source["backupPath"];
	        this.setExcludesfile = source["setExcludesfile"];
	        this.updated = source["updated"];
	        this.alreadyUpToDate = source["alreadyUpToDate"];
	    }
	}
	export class Status {
	    path: string;
	    defaultPath: string;
	    excludesfile: string;
	    excludesfileSet: boolean;
	    fileExists: boolean;
	    blockPresent: boolean;
	    blockUpToDate: boolean;
	    hasDuplicates: boolean;
	    duplicates?: string[];
	    needsAction: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.defaultPath = source["defaultPath"];
	        this.excludesfile = source["excludesfile"];
	        this.excludesfileSet = source["excludesfileSet"];
	        this.fileExists = source["fileExists"];
	        this.blockPresent = source["blockPresent"];
	        this.blockUpToDate = source["blockUpToDate"];
	        this.hasDuplicates = source["hasDuplicates"];
	        this.duplicates = source["duplicates"];
	        this.needsAction = source["needsAction"];
	    }
	}

}

export namespace heal {
	
	export class ProofOverride {
	    scope: string;
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new ProofOverride(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.key = source["key"];
	        this.value = source["value"];
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
	
	export class AccountDeletionImpactDTO {
	    account: string;
	    sources: string[];
	    mirrors: string[];
	    repo_count: number;
	    clone_count: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountDeletionImpactDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account = source["account"];
	        this.sources = source["sources"];
	        this.mirrors = source["mirrors"];
	        this.repo_count = source["repo_count"];
	        this.clone_count = source["clone_count"];
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
	export class AdoptRequestDTO {
	    path: string;
	    account: string;
	    workspaceRoot?: string;
	
	    static createFrom(source: any = {}) {
	        return new AdoptRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.account = source["account"];
	        this.workspaceRoot = source["workspaceRoot"];
	    }
	}
	export class ConfigBackupInfo {
	    path: string;
	    filename: string;
	    timestamp: string;
	    size_bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ConfigBackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.filename = source["filename"];
	        this.timestamp = source["timestamp"];
	        this.size_bytes = source["size_bytes"];
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
	    revision: number;
	    version: number;
	    global: config.GlobalConfig;
	    accounts: Record<string, config.Account>;
	    sources: Record<string, SourceDTO>;
	    mirrors: Record<string, MirrorDTO>;
	    pathConflicts?: config.PathConflict[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.version = source["version"];
	        this.global = this.convertValues(source["global"], config.GlobalConfig);
	        this.accounts = this.convertValues(source["accounts"], config.Account, true);
	        this.sources = this.convertValues(source["sources"], SourceDTO, true);
	        this.mirrors = this.convertValues(source["mirrors"], MirrorDTO, true);
	        this.pathConflicts = this.convertValues(source["pathConflicts"], config.PathConflict);
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
	export class ConfigRepairResult {
	    success: boolean;
	    error?: string;
	    repairs?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigRepairResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	        this.repairs = source["repairs"];
	    }
	}
	export class CredentialSetupResult {
	    ok: boolean;
	    message: string;
	    needsPAT?: boolean;
	    sshPublicKey?: string;
	    sshAddURL?: string;
	    sshVerified?: boolean;
	    findings?: credential.Finding[];
	
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
	        this.findings = this.convertValues(source["findings"], credential.Finding);
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
	export class ProblemDTO {
	    code?: string;
	    hint?: string;
	    detail?: string;
	    retryAt?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProblemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.hint = source["hint"];
	        this.detail = source["detail"];
	        this.retryAt = source["retryAt"];
	    }
	}
	export class CredentialStatus {
	    problem?: ProblemDTO;
	    status: string;
	    message: string;
	    primary: string;
	    primaryMsg: string;
	    pat: string;
	    patMsg: string;
	    expiresAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new CredentialStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.problem = this.convertValues(source["problem"], ProblemDTO);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.primary = source["primary"];
	        this.primaryMsg = source["primaryMsg"];
	        this.pat = source["pat"];
	        this.patMsg = source["patMsg"];
	        this.expiresAt = source["expiresAt"];
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
	export class DoctorToolDTO {
	    name: string;
	    displayName: string;
	    purpose: string;
	    found: boolean;
	    path?: string;
	    version?: string;
	    required: boolean;
	    requiredFor?: string;
	    installHint?: string;
	
	    static createFrom(source: any = {}) {
	        return new DoctorToolDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.purpose = source["purpose"];
	        this.found = source["found"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.required = source["required"];
	        this.requiredFor = source["requiredFor"];
	        this.installHint = source["installHint"];
	    }
	}
	export class DoctorPrecheckDTO {
	    ok: boolean;
	    summary?: string;
	    hint?: string;
	    missing?: DoctorToolDTO[];
	
	    static createFrom(source: any = {}) {
	        return new DoctorPrecheckDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.summary = source["summary"];
	        this.hint = source["hint"];
	        this.missing = this.convertValues(source["missing"], DoctorToolDTO);
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
	export class DoctorReport {
	    credentialFindings: credential.Finding[];
	    tools: DoctorToolDTO[];
	    allOk: boolean;
	    missingReq: number;
	    missingOpt: number;
	
	    static createFrom(source: any = {}) {
	        return new DoctorReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.credentialFindings = this.convertValues(source["credentialFindings"], credential.Finding);
	        this.tools = this.convertValues(source["tools"], DoctorToolDTO);
	        this.allOk = source["allOk"];
	        this.missingReq = source["missingReq"];
	        this.missingOpt = source["missingOpt"];
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
	export class ExternalOpenDTO {
	    needsConfirm: boolean;
	    url?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalOpenDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.needsConfirm = source["needsConfirm"];
	        this.url = source["url"];
	    }
	}
	export class IdentityProofDTO {
	    account: string;
	    server: string;
	    username: string;
	    nameSource: string;
	    emailSource: string;
	    originURL: string;
	    helperChain: string[];
	    credentialFile?: string;
	    overrides: heal.ProofOverride[];
	    // Go type: time
	    lastVerified?: any;
	    verdict: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new IdentityProofDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account = source["account"];
	        this.server = source["server"];
	        this.username = source["username"];
	        this.nameSource = source["nameSource"];
	        this.emailSource = source["emailSource"];
	        this.originURL = source["originURL"];
	        this.helperChain = source["helperChain"];
	        this.credentialFile = source["credentialFile"];
	        this.overrides = this.convertValues(source["overrides"], heal.ProofOverride);
	        this.lastVerified = this.convertValues(source["lastVerified"], null);
	        this.verdict = source["verdict"];
	        this.reason = source["reason"];
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
	export class IncompleteCloneDTO {
	    source: string;
	    repo: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new IncompleteCloneDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	        this.path = source["path"];
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
	
	export class OnboardingRootPreviewDTO {
	    abs: string;
	    exists: boolean;
	    isDir: boolean;
	    writable: boolean;
	    existingClones: number;
	    sampled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OnboardingRootPreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abs = source["abs"];
	        this.exists = source["exists"];
	        this.isDir = source["isDir"];
	        this.writable = source["writable"];
	        this.existingClones = source["existingClones"];
	        this.sampled = source["sampled"];
	    }
	}
	export class OrphanRepoDTO {
	    path: string;
	    relPath: string;
	    remoteURL: string;
	    repoKey: string;
	    matchedAccount: string;
	    reason: string;
	    candidates?: string[];
	    nested: boolean;
	    workspaceRoot?: string;
	
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
	        this.reason = source["reason"];
	        this.candidates = source["candidates"];
	        this.nested = source["nested"];
	        this.workspaceRoot = source["workspaceRoot"];
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
	    partial?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PRSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authored = this.convertValues(source["authored"], PullRequestDTO);
	        this.reviewRequested = this.convertValues(source["reviewRequested"], PullRequestDTO);
	        this.partial = source["partial"];
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
	
	export class PullPlanItem {
	    source: string;
	    repo: string;
	    behind?: number;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new PullPlanItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	        this.behind = source["behind"];
	        this.reason = source["reason"];
	    }
	}
	export class repoRef {
	    source: string;
	    repo: string;
	
	    static createFrom(source: any = {}) {
	        return new repoRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	    }
	}
	export class PullPlanDTO {
	    planID: string;
	    configRevision: number;
	    stale: boolean;
	    willClone: repoRef[];
	    willUpdate: PullPlanItem[];
	    skipped: PullPlanItem[];
	    offline: repoRef[];
	    repairsPending: repoRef[];
	
	    static createFrom(source: any = {}) {
	        return new PullPlanDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.planID = source["planID"];
	        this.configRevision = source["configRevision"];
	        this.stale = source["stale"];
	        this.willClone = this.convertValues(source["willClone"], repoRef);
	        this.willUpdate = this.convertValues(source["willUpdate"], PullPlanItem);
	        this.skipped = this.convertValues(source["skipped"], PullPlanItem);
	        this.offline = this.convertValues(source["offline"], repoRef);
	        this.repairsPending = this.convertValues(source["repairsPending"], repoRef);
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
	
	
	export class RecipeFile {
	    document: string;
	    slots: string[];
	    newAccounts: recipe.AccountRef[];
	
	    static createFrom(source: any = {}) {
	        return new RecipeFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.document = source["document"];
	        this.slots = source["slots"];
	        this.newAccounts = this.convertValues(source["newAccounts"], recipe.AccountRef);
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
	export class WorkspaceDiagnosticDTO {
	    parent: string;
	    file: string;
	    problem: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceDiagnosticDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parent = source["parent"];
	        this.file = source["file"];
	        this.problem = source["problem"];
	    }
	}
	export class WorkspaceFolderDTO {
	    parent: string;
	    name: string;
	    path: string;
	    outsidePath?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceFolderDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parent = source["parent"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.outsidePath = source["outsidePath"];
	    }
	}
	export class WorkspaceChoiceDTO {
	    source: string;
	    repo: string;
	    files: string[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceChoiceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	        this.files = source["files"];
	    }
	}
	export class RepairDTO {
	    source: string;
	    repo: string;
	    fixed?: string[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new RepairDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.repo = source["repo"];
	        this.fixed = source["fixed"];
	        this.warnings = source["warnings"];
	    }
	}
	export class ReconcileDTO {
	    adopted: number;
	    repairs?: RepairDTO[];
	    relinked: number;
	    unmatched: OrphanRepoDTO[];
	    ignored: OrphanRepoDTO[];
	    skippedFolders?: number;
	    skipped?: reconcile.Skip[];
	    background?: boolean;
	    workspacesChanged: boolean;
	    workspaceChoices: WorkspaceChoiceDTO[];
	    placeholders: WorkspaceFolderDTO[];
	    workspaceMembers: string[];
	    workspaceUnknown: string[];
	    workspaceDiagnostics?: WorkspaceDiagnosticDTO[];
	    incomplete: IncompleteCloneDTO[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ReconcileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.adopted = source["adopted"];
	        this.repairs = this.convertValues(source["repairs"], RepairDTO);
	        this.relinked = source["relinked"];
	        this.unmatched = this.convertValues(source["unmatched"], OrphanRepoDTO);
	        this.ignored = this.convertValues(source["ignored"], OrphanRepoDTO);
	        this.skippedFolders = source["skippedFolders"];
	        this.skipped = this.convertValues(source["skipped"], reconcile.Skip);
	        this.background = source["background"];
	        this.workspacesChanged = source["workspacesChanged"];
	        this.workspaceChoices = this.convertValues(source["workspaceChoices"], WorkspaceChoiceDTO);
	        this.placeholders = this.convertValues(source["placeholders"], WorkspaceFolderDTO);
	        this.workspaceMembers = source["workspaceMembers"];
	        this.workspaceUnknown = source["workspaceUnknown"];
	        this.workspaceDiagnostics = this.convertValues(source["workspaceDiagnostics"], WorkspaceDiagnosticDTO);
	        this.incomplete = this.convertValues(source["incomplete"], IncompleteCloneDTO);
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
	export class doctorDaemon {
	    state: string;
	    running: boolean;
	    lastFallback?: string;
	
	    static createFrom(source: any = {}) {
	        return new doctorDaemon(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.running = source["running"];
	        this.lastFallback = source["lastFallback"];
	    }
	}
	export class doctorRoot {
	    path: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new doctorRoot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.state = source["state"];
	    }
	}
	export class serverDoctorReport {
	    version: string;
	    commit: string;
	    os: string;
	    arch: string;
	    home: string;
	    configPath: string;
	    configDirWritable: boolean;
	    configDirExec: boolean;
	    gitVersion: string;
	    credentialFindings: credential.Finding[];
	    roots?: doctorRoot[];
	    daemon?: doctorDaemon;
	
	    static createFrom(source: any = {}) {
	        return new serverDoctorReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.commit = source["commit"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.home = source["home"];
	        this.configPath = source["configPath"];
	        this.configDirWritable = source["configDirWritable"];
	        this.configDirExec = source["configDirExec"];
	        this.gitVersion = source["gitVersion"];
	        this.credentialFindings = this.convertValues(source["credentialFindings"], credential.Finding);
	        this.roots = this.convertValues(source["roots"], doctorRoot);
	        this.daemon = this.convertValues(source["daemon"], doctorDaemon);
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
	export class RemoteDoctorDTO {
	    error?: string;
	    hint?: string;
	    report?: serverDoctorReport;
	
	    static createFrom(source: any = {}) {
	        return new RemoteDoctorDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	        this.hint = source["hint"];
	        this.report = this.convertValues(source["report"], serverDoctorReport);
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
	export class RemoteEntryDTO {
	    alias: string;
	    serverDir?: string;
	    editor?: string;
	    open: boolean;
	    state?: string;
	    serverOS?: string;
	    serverArch?: string;
	    serverVersion?: string;
	    lastConnected?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteEntryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.serverDir = source["serverDir"];
	        this.editor = source["editor"];
	        this.open = source["open"];
	        this.state = source["state"];
	        this.serverOS = source["serverOS"];
	        this.serverArch = source["serverArch"];
	        this.serverVersion = source["serverVersion"];
	        this.lastConnected = source["lastConnected"];
	    }
	}
	export class RemotePathDTO {
	    abs: string;
	    exists: boolean;
	    isDir: boolean;
	    writable: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemotePathDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abs = source["abs"];
	        this.exists = source["exists"];
	        this.isDir = source["isDir"];
	        this.writable = source["writable"];
	        this.error = source["error"];
	    }
	}
	export class RemoteResultDTO {
	    error?: string;
	    hint?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	        this.hint = source["hint"];
	    }
	}
	export class RemoteStateDTO {
	    state: string;
	    code?: string;
	    hint?: string;
	    detail?: string;
	    alias: string;
	    serverOS?: string;
	    serverArch?: string;
	    serverVersion?: string;
	    truncated?: boolean;
	    waiting?: string;
	    remoteEditors: string[];
	    editor?: string;
	    attempt?: number;
	    attempts?: number;
	    resumed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteStateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.code = source["code"];
	        this.hint = source["hint"];
	        this.detail = source["detail"];
	        this.alias = source["alias"];
	        this.serverOS = source["serverOS"];
	        this.serverArch = source["serverArch"];
	        this.serverVersion = source["serverVersion"];
	        this.truncated = source["truncated"];
	        this.waiting = source["waiting"];
	        this.remoteEditors = source["remoteEditors"];
	        this.editor = source["editor"];
	        this.attempt = source["attempt"];
	        this.attempts = source["attempts"];
	        this.resumed = source["resumed"];
	    }
	}
	export class RemoteUninstallDTO {
	    error?: string;
	    hint?: string;
	    needsConfirm: boolean;
	    otherClients: number;
	    done: boolean;
	    rootKept: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteUninstallDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	        this.hint = source["hint"];
	        this.needsConfirm = source["needsConfirm"];
	        this.otherClients = source["otherClients"];
	        this.done = source["done"];
	        this.rootKept = source["rootKept"];
	    }
	}
	export class RemotesDTO {
	    enabled: boolean;
	    current?: string;
	    remotes: RemoteEntryDTO[];
	    remoteEditors: string[];
	
	    static createFrom(source: any = {}) {
	        return new RemotesDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.current = source["current"];
	        this.remotes = this.convertValues(source["remotes"], RemoteEntryDTO);
	        this.remoteEditors = source["remoteEditors"];
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
	export class RemoveRootDTO {
	    inUse: number;
	    forgotten: string[];
	
	    static createFrom(source: any = {}) {
	        return new RemoveRootDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inUse = source["inUse"];
	        this.forgotten = source["forgotten"];
	    }
	}
	
	export class RepoDetail {
	    branch: string;
	    ahead: number;
	    behind: number;
	    changed: git.FileChange[];
	    untracked: string[];
	    incoming: git.IncomingCommit[];
	    problem?: ProblemDTO;
	    incomingProblem?: ProblemDTO;
	    incomingError?: string;
	    error?: string;
	    upstreamGone?: boolean;
	    upstreamError?: string;
	
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
	        this.incoming = this.convertValues(source["incoming"], git.IncomingCommit);
	        this.problem = this.convertValues(source["problem"], ProblemDTO);
	        this.incomingProblem = this.convertValues(source["incomingProblem"], ProblemDTO);
	        this.incomingError = source["incomingError"];
	        this.error = source["error"];
	        this.upstreamGone = source["upstreamGone"];
	        this.upstreamError = source["upstreamError"];
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
	export class RootDTO {
	    path: string;
	    isDefault: boolean;
	    exists: boolean;
	    repos: number;
	
	    static createFrom(source: any = {}) {
	        return new RootDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.isDefault = source["isDefault"];
	        this.exists = source["exists"];
	        this.repos = source["repos"];
	    }
	}
	export class SSHAgentReportDTO {
	    keyFile: string;
	    keyFileFingerprint: string;
	    agentFingerprints: string[];
	    agentForwarded: boolean;
	    match: boolean;
	    identitiesOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SSHAgentReportDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.keyFile = source["keyFile"];
	        this.keyFileFingerprint = source["keyFileFingerprint"];
	        this.agentFingerprints = source["agentFingerprints"];
	        this.agentForwarded = source["agentForwarded"];
	        this.match = source["match"];
	        this.identitiesOnly = source["identitiesOnly"];
	    }
	}
	export class SSHHostDTO {
	    alias: string;
	    names: string[];
	
	    static createFrom(source: any = {}) {
	        return new SSHHostDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.names = source["names"];
	    }
	}
	export class ScheduledTaskDTO {
	    name: string;
	    nextAt: number;
	    inFlight: boolean;
	    lastResult: string;
	
	    static createFrom(source: any = {}) {
	        return new ScheduledTaskDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.nextAt = source["nextAt"];
	        this.inFlight = source["inFlight"];
	        this.lastResult = source["lastResult"];
	    }
	}
	export class ShellInfo {
	    id: string;
	    name: string;
	    command: string;
	    args: string[];
	
	    static createFrom(source: any = {}) {
	        return new ShellInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	    }
	}
	
	export class StartupSnapshot {
	    eventWatermark: number;
	    config: ConfigDTO;
	    nesting: Record<string, string>;
	    configPath: string;
	    appVersion: string;
	    os: string;
	    credentialFindings: credential.Finding[];
	    serverOS: string;
	    serverVersion?: string;
	    remoteAlias?: string;
	
	    static createFrom(source: any = {}) {
	        return new StartupSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.eventWatermark = source["eventWatermark"];
	        this.config = this.convertValues(source["config"], ConfigDTO);
	        this.nesting = source["nesting"];
	        this.configPath = source["configPath"];
	        this.appVersion = source["appVersion"];
	        this.os = source["os"];
	        this.credentialFindings = this.convertValues(source["credentialFindings"], credential.Finding);
	        this.serverOS = source["serverOS"];
	        this.serverVersion = source["serverVersion"];
	        this.remoteAlias = source["remoteAlias"];
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
	    problem?: ProblemDTO;
	    branch?: string;
	    isDefault?: boolean;
	    checkedAt?: string;
	
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
	        this.problem = this.convertValues(source["problem"], ProblemDTO);
	        this.branch = source["branch"];
	        this.isDefault = source["isDefault"];
	        this.checkedAt = source["checkedAt"];
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
	export class SweepDeleteDTO {
	    skipped: string[];
	    deleted: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SweepDeleteDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skipped = source["skipped"];
	        this.deleted = source["deleted"];
	        this.error = source["error"];
	    }
	}
	export class SweepPreviewDTO {
	    id: string;
	    merged: string[];
	    gone: string[];
	    squashed: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SweepPreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.merged = source["merged"];
	        this.gone = source["gone"];
	        this.squashed = source["squashed"];
	        this.error = source["error"];
	    }
	}
	export class TerminalAppInfo {
	    id: string;
	    name: string;
	    command: string;
	    args_template: string[];
	
	    static createFrom(source: any = {}) {
	        return new TerminalAppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args_template = source["args_template"];
	    }
	}
	export class TerminalProfileInfo {
	    id: string;
	    name: string;
	    terminal: string;
	    shell: string;
	    args: string[];
	    default: boolean;
	    preferred: boolean;
	    hidden: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new TerminalProfileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.terminal = source["terminal"];
	        this.shell = source["shell"];
	        this.args = source["args"];
	        this.default = source["default"];
	        this.preferred = source["preferred"];
	        this.hidden = source["hidden"];
	        this.source = source["source"];
	    }
	}
	export class TokenGuideInfo {
	    creationURL: string;
	    scopes: string;
	
	    static createFrom(source: any = {}) {
	        return new TokenGuideInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.creationURL = source["creationURL"];
	        this.scopes = source["scopes"];
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
	
	
	
	
	
	

}

export namespace ops {
	
	export class Step {
	    name: string;
	    ok: boolean;
	    err?: string;
	    repo?: string;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ok = source["ok"];
	        this.err = source["err"];
	        this.repo = source["repo"];
	    }
	}
	export class StepReport {
	    ok: boolean;
	    steps: Step[];
	    sshPublicKey?: string;
	    sshAddURL?: string;
	    sshVerified?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StepReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.steps = this.convertValues(source["steps"], Step);
	        this.sshPublicKey = source["sshPublicKey"];
	        this.sshAddURL = source["sshAddURL"];
	        this.sshVerified = source["sshVerified"];
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

export namespace recipe {
	
	export class AccountRef {
	    server: string;
	    username: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.username = source["username"];
	    }
	}
	export class Identity {
	    name: string;
	    email: string;
	
	    static createFrom(source: any = {}) {
	        return new Identity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.email = source["email"];
	    }
	}
	export class Row {
	    kind: string;
	    item: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new Row(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.item = source["item"];
	        this.state = source["state"];
	    }
	}
	export class ImportPlan {
	    rows: Row[];
	    digest: string;
	    collisions: number;
	    accounts: number;
	    sources: number;
	    repos: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], Row);
	        this.digest = source["digest"];
	        this.collisions = source["collisions"];
	        this.accounts = source["accounts"];
	        this.sources = source["sources"];
	        this.repos = source["repos"];
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
	export class Options {
	    slots: Record<string, string>;
	    identities: Record<string, Identity>;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slots = source["slots"];
	        this.identities = this.convertValues(source["identities"], Identity, true);
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

export namespace reconcile {
	
	export class Skip {
	    path: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new Skip(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.reason = source["reason"];
	    }
	}

}

