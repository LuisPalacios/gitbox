# Gitbox — architecture and design

For the product overview (what Gitbox does, who it's for, and why it exists), see the [README](../readme.md).

---

## 1. Architecture overview

Gitbox is a Go monorepo that produces one binary, the `GitboxApp` desktop app, on top of a shared library in `pkg/`:

<p align="center">
  <img src="diagrams/architecture-overview.png" alt="Architecture Overview" width="800" />
</p>

The GUI is a Wails v2 app with a Svelte frontend. The Go side lives in `cmd/gui/` and is a thin layer of Wails bindings, locking and events. Application-level operations (account lifecycle, credential switching, clone planning, discovery) live in `pkg/ops`. Everything below that (config, credentials, providers, git, status, mirrors) lives in the other `pkg/` packages. The GUI supports GCM, SSH and Token credentials.

The app needs no companion binary. It shells out only to tools already on the system: `git`, Git Credential Manager, `ssh` / `ssh-keygen` / `ssh-add`, `wsl` on Windows, and the user's terminals and editors.

The v1 CLI and its embedded TUI were removed in v2. They live on in the v1 maintenance line on the `release/v1` branch.

## 2. Core concepts

### Accounts, sources, and repos

<p align="center">
  <img src="diagrams/config-model.png" alt="Config Model" width="800" />
</p>

Accounts and sources are separate because one account can have several sources (for example, different GitHub orgs under the same login). Sources group repos logically. Repos use `org/repo` naming, and the org part becomes the folder structure.

### Credential model

[credentials.md](credentials.md) covers user-facing setup. This section covers the design.

Each credential type is **self-sufficient** for the account. Per-repo credential isolation gives every clone a self-contained `.git/config`: an empty `credential.helper =` line cancels inherited (global and system) helpers, then the type-specific helper is set. Clones don't depend on `~/.gitconfig`.

**Design decisions:**

- **Token** stores the PAT in one gitbox credential file per account, `~/.config/gitbox/credentials/<key>` (git-credential-store format; mode `0600` on Unix, ACL-controlled access on Windows). Gitbox reads it for API calls, and every clone of the account points `credential.helper = store --file <path>` at it for git.
- **GCM** uses `helper = manager` with per-host `username` and `provider` scoped per repo, plus `credential.credentialStore`. GCM keeps its secrets in the OS credential store.
- **SSH** only cancels helpers (auth goes through `~/.ssh/config`). Discovery needs an optional PAT, kept in the same credential file as a token account's.

**Credential lifecycle:** switching type removes the old type's artifacts before configuring the new one, then reconfigures existing clones (remote URL and credential config). Renaming an account key migrates every artifact: config keys, the source folder, the credential file, SSH keys, and the SSH config alias.

### Config as local database

The JSON config file (`~/.config/gitbox/gitbox.json`) is the **desired state**: a local database of the accounts, sources and repos that should exist, and of the clone roots they live in.

**Discovery** is a northbound query. It asks the provider API which repos exist and lets the user add them to the config. Discovery only adds, on demand, and never removes repos.

**Reconcile** is its counterpart on disk: Gitbox reads back which clones exist under the clone roots. The config stays the record of intent (accounts, roots, mirrors, overrides, repos wanted but not cloned yet), and `pkg/reconcile` keeps the inventory in step. It adopts the clones it recognises in place, re-links moved ones, and leaves the rest for the user. It never touches files and never forgets a repo whose clone is gone.

### Folder structure

Repos are cloned into a 3-level hierarchy under a clone root. `global.roots` lists the roots, the default first. Each source clones into one of them (`source.root`, empty for the default), and a single repo can live in another (`repo.root`):

```text
~/00.git/                          <- a root in global.roots
  github-personal/                 <- source key (1st level)
    MyOrg/                         <- org from "MyOrg/project-a" (2nd level)
      project-a/                   <- repo name (3rd level)
      project-b/
    other-org/
      tools/                       <- cross-org repo, same credentials
  forgejo-work/
    infra/
      homelab-ops/
    infra-prod/                    <- id_folder override
      homelab/
```

Each level can be overridden:

- **1st level**: `source.folder` overrides the source key.
- **2nd level**: `repo.id_folder` overrides the org part.
- **3rd level**: `repo.clone_folder` overrides the repo name. An absolute value (`/` or `~`) replaces the entire path, and so does one starting with `../`, resolved against the root.

Changing a source's root or the default root pins the root every existing clone already resolves through, so no clone path moves. Clones outside this tree (nested clones inside a multi-repo container, clones found away from the standard layout) always carry an absolute `clone_folder`, so Gitbox never moves them.

---

## 3. Component design

### cmd/gui — Wails bindings

`cmd/gui/app.go` exposes the methods the Svelte frontend calls through auto-generated TypeScript bindings. Bound methods take the config lock for shared state, call `pkg/ops` or another `pkg/` package for the work, save changes, and emit Wails events for progress. Neighbouring files hold GUI-only concerns: terminal profiles (`profiles.go`), multi-repo containers and their workspace files (`containers.go`), clone roots (`roots.go`), disk reconcile (`reconcile.go`), PR badges (`pr.go`), autostart, and per-OS process lifecycle. Every `exec.Command` in `cmd/gui/` goes through `git.HideWindow`, so no console window flashes on Windows.

`main.go` handles two flags before Wails starts: `--version` prints the build version and exits, and `--test-mode` routes the config through a temp directory built from the `test-gitbox.json` fixture (see [testing.md](testing.md)).

Startup returns config, preferences, collapse state and path-derived nesting in one detached snapshot. The frontend renders it before local status arrives and shows pending spinners until checking or fetching finishes. Cross-source children stay under their containers but keep their own source for status, actions and ownership. Editor and terminal-profile detection runs outside the config lock and merges into the current settings, so concurrent edits survive.

`cmd/gui/refresh.go` coordinates refresh work:

- Overlapping callers share one active run of local status, Fetch all, per-clone fetch, and each account's credential check.
- One pending local-status invalidation is kept, results computed from a config that has since changed are dropped, and closing the app cancels refresh work.
- When the account changes or a re-check is requested during a credential check, the check runs again against the current config. Offline results retry with backoff; authentication errors don't.
- A Fetch all cycle publishes local status before waiting for provider work. `fetch:alldone` follows its final status and remote checks.

`cmd/gui/probe.go` shares and throttles the sweeps that check whether each upstream still exists:

- Credentials resolve once per account, and each upstream is probed once per sweep.
- Limits: four requests at a time, 10 seconds per request, 30 seconds per sweep (credential resolution included), and at least 60 seconds after a completed sweep before the next one.
- Errors and cancellation keep the known upstream state. Results superseded by newer fetches or credential changes are discarded.

### pkg/ops — Application operations

The service layer between the GUI bindings and the lower-level packages. It holds the operations that touch several packages at once: `AddAccount`, `RenameAccount`, `DeleteAccount`, `DeleteRepo`, `ChangeCredentialType`, `DeleteCredential`, `RemoveCredentialArtifacts`, `ReconfigureClones` (runs `heal.Repo` on each clone), `PlanClone` / `Clone`, `ListRemoteRepos`, `ListAccountOrgs`, `CreateRemoteRepo`, `AddDiscoveredRepos`, and `ProviderClient`.

Each operation works on a `*config.Config` plus the files it owns on disk (clones, SSH keys, credential files). Operations never save the config and never lock: the caller serializes access and persists afterwards. Operations that touch every clone of an account (`ReconfigureClones`) are separate, so the caller can run them after a successful save.

### pkg/config — Configuration management

Handles the v5 configuration file and migrates v2, v3 and v4 files on load. Core types: `Config`, `Account`, `Source`, `Repo`, `Mirror`. Clone roots (`Global.Roots`, `Source.Root`, `Repo.Root`) and their mutators live in `roots.go`, and `status.RepoPath` resolves every clone path through them. Multi-repo containers (`Repo.Container`, `Repo.Workspace`), the nested-scan depth (`Global.NestedScanDepth`) and the GUI collapse state (`Global.Collapsed`, pruned of stale keys on every `Save`) round out the model. `pkg/config/config.go` holds the struct definitions; [section 4](#4-config-format-v5) is the field reference.

**Key design decisions:**

- **JSON order preservation:** `SourceOrder`, `RepoOrder` and `MirrorOrder` make iteration follow the order of the config file.
- **Credential inheritance:** repos inherit `default_credential_type` from their account unless they override it.
- **CRUD with referential integrity:** `DeleteAccount` fails if any source references the account, and `DeleteSource` cascades to its repos.

### pkg/credential — Credential management

Manages tokens, SSH keys, GCM integration and per-repo credential isolation. See `pkg/credential/credential.go`, `pkg/credential/validate.go` and `pkg/credential/repoconfig.go`.

**Token storage:** `StoreTokenWithAccount` writes the git-credential-store line (`https://user:TOKEN@host`) directly when the account is known, including GUI saves and account renames. `StoreToken` keeps the raw-token compatibility path. `GetToken` reads both formats and `DeleteToken` removes the file `~/.config/gitbox/credentials/<key>` (`CredentialFilePath`). There is no other token store.

**Shared writer:** both storage paths and `WriteCredentialFile` use one private writer. It serializes comparison and replacement, skips identical content, and prepares changed content in a temporary file in the destination directory. It sets mode `0600` before writing, calls `Sync`, closes the file and renames it over the destination; errors before replacement preserve the previous file and clean up the temporary file. A normal write can repair an existing Unix file's mode to `0600`. Windows replacement uses the operating system's rename semantics; this does not promise uniform crash or power-loss durability across filesystems.

**Permission audit:** `AuditCredentialDir` inspects metadata without reading secrets or changing permissions. Unix checks expect directory mode `0700` and file mode `0600`. Windows checks inspect directory and file DACLs, flag grants outside the owner/SYSTEM/Administrators allowlist, and distinguish exposure from permissions they cannot verify. The GUI runs the audit once after loading config, outside the config lock, caches the findings for the startup snapshot, and reruns it through System check. A warning above the full or Compact dashboard highlights exposure; the report includes all findings and suggested actions.

**Token resolution chain (`ResolveToken`):** the `GITBOX_TOKEN_<KEY>` environment variable, then `GIT_TOKEN`, then the credential file.

**API token dispatch:** routes by credential type. `token` uses `ResolveToken`, `gcm` runs `git credential fill` and falls back to `ResolveToken`, and `ssh` tries `ResolveToken` (the optional discovery PAT). `ResolveMirrorToken` requires a portable PAT for mirrors (see [pkg/mirror](#pkgmirror--repository-mirroring)).

**SSH key management:** generates key pairs, writes `~/.ssh/config` entries and tests connections. Naming convention: host alias `gitbox-<account-key>`, key file `gitbox-<account-key>-sshkey`.

**Per-repo credential config** (`repoconfig.go`): `ConfigureRepoCredential()` makes each clone's `.git/config` self-contained, and `WriteCredentialFile()` writes the git-credential-store line for token accounts. `pkg/ops` and `pkg/heal` call the same functions.

`credential.CheckContext` verifies one account and honours cancellation, which reports as offline. GCM and SSH verification subprocesses stop after 15 seconds or the caller's earlier deadline.

### pkg/provider — Repository discovery

Abstraction layer for Git hosting provider APIs. Each provider implements `ListRepos()` returning `RemoteRepo` structs. See `pkg/provider/provider.go` for the interface.

| Provider      | API          | Auth                        | Notes                         |
| ------------- | ------------ | --------------------------- | ----------------------------- |
| GitHub        | REST v3      | Bearer token                | Supports GitHub Enterprise    |
| GitLab        | REST v4      | PRIVATE-TOKEN header        | Self-hosted compatible        |
| Gitea/Forgejo | REST /api/v1 | Token + Basic auth fallback | Same API, same implementation |
| Bitbucket     | REST v2      | HTTP Basic (app password)   | Cloud only                    |

`CheckAuth()` validates a credential, and `TokenCreationURL()` and `TokenRequiredScopes()` give the GUI each provider's PAT page and scopes. `CheckAuth()` also returns the token's expiry: GitHub reads the `GitHub-Authentication-Token-Expiration` header of the existing repo-list call, and GitLab adds one `GET /personal_access_tokens/self` after a successful check. The other providers return no expiry. `credential.StatusResult.ExpiresAt` carries it to the GUI's `CredentialStatus.expiresAt`, and `accountHealth` in `src/lib/status.ts` turns a working token within 14 days of expiry into a warning.

**Optional interfaces** (via type assertions):

- `RepoCreator`: create repos (under the user or an org, with a description) and check existence (all providers).
- `OrgLister`: list the organizations or groups the user belongs to, for the owner dropdown when creating a repo (all providers).
- `PushMirrorProvider`: server-side push mirrors (Gitea/Forgejo, GitLab).
- `PullMirrorProvider`: pull mirrors via the migrate API (Gitea/Forgejo).
- `RepoInfoProvider`: fetch the HEAD commit and visibility for sync comparison (GitHub, GitLab, Gitea/Forgejo).

### pkg/git — Git operations

Thin wrapper around `os/exec` for all Git operations, with no libgit2 dependency. Provides `Clone`, `CloneWithProgress`, `Pull`, `Status`, `Fetch`, `ConfigSet`, `ConfigAdd`, `ConfigUnsetAll` and more (see `pkg/git/git.go`). Multi-value git config keys such as `credential.helper` are managed with `ConfigUnsetAll` + `ConfigAdd`.

I use `IncomingCommits` in `pkg/git/incoming.go` to read `HEAD..@{upstream}` from local refs, returning each incoming commit's hash, author and subject without fetching or modifying the clone. `GetRepoDetail` includes this history when the branch is behind and reports history errors separately from local-file details. The frontend's `repo-detail.ts` loader discards superseded reads so switching clones or refreshing an open panel cannot display an older response.

On macOS, `GitBin()` probes the Homebrew paths (`/opt/homebrew/bin/git`, `/usr/local/bin/git`) before falling back to `PATH`, so the GUI finds a GCM-enabled git even with the minimal `PATH` that macOS GUI apps inherit.

### pkg/status — Sync status checking

Determines the sync state of local clones relative to their upstream. States: Clean, Dirty, Behind, Ahead, Diverged, Conflict, NotCloned, NoUpstream, Error, UpstreamGone, RootOffline. A repo whose clone root is missing reads RootOffline instead of NotCloned (`RepoRootOffline`), so an unplugged drive doesn't look like deleted clones. Priority: Conflict > Dirty > Diverged > Behind > Ahead > NoUpstream > Clean. `ComputeNesting` derives parent → child relationships between container clones and the clones nested inside them. See `pkg/status/status.go`.

### pkg/identity — Git identity management

Manages per-repo git identity (`user.name`, `user.email`) with a resolution chain: repo-level overrides fall back to account-level values. See `pkg/identity/identity.go`. `heal.Repo` checks each clone's identity and repairs drift.

`CheckGlobalIdentity()` and `RemoveGlobalIdentity()` handle the global identity in `~/.gitconfig`. Gitbox encourages removing it, so the per-repo identity set at clone or reconfigure time is always authoritative.

`pkg/credential` does the same for the global GCM helper (`gcm_config.go`). Without a GCM helper in `~/.gitconfig`, `git credential fill` falls through to `/dev/tty` and fails with "Device not configured" in GUI contexts. The design:

- **When:** `IsGlobalGCMConfigNeeded()` gates the check on at least one account using GCM, so SSH- and token-only setups never see the warning.
- **Check:** `CheckGlobalGCMConfig()` expects `credential.helper` to resolve to GCM and `credential.credentialStore` to match (`keychain`, `wincredman` or `secretservice`), taking the expected values from `global.credential_gcm` or the OS defaults. It reads the full multi-value `credential.helper` list and applies git's reset rule (an empty value clears the helpers before it). A helper resolves to GCM when it is `manager`, the legacy `manager-core`, or a path (quoted or not) whose binary is `git-credential-manager`. A stock `git-credential-manager configure` chain (a reset plus an absolute path) therefore passes without a fix.
- **Fix:** `FixGlobalGCMConfig()`, behind the GUI's fix button, first backfills `DefaultCredentialHelper()` and `DefaultCredentialStore()` into `gitbox.json` when `global.credential_gcm` lacks them. It then writes only what the check flags: a helper chain that already resolves to GCM stays untouched, and one that doesn't is replaced as a whole with `--replace-all`, as is a wrong `credentialStore`.

Per-host `[credential "https://…"]` sections, including the per-repo config Gitbox writes, still take precedence over these global values.

### pkg/update — Auto-update

Provides version checking and self-update via GitHub Releases. The design:

- **Check:** `CheckLatest()` queries the GitHub API on every call. The GUI calls it at launch and every six hours, and follows the release GitHub marks as latest. A `MaxMajor` option caps the major version; the v1 CLI on `release/v1` uses it to stay on 1.x.
- **Token:** I send `GITHUB_TOKEN` only over HTTPS to the exact lowercase hosts `api.github.com`, `github.com`, `objects.githubusercontent.com`, `release-assets.githubusercontent.com` and `uploads.github.com`. API requests and downloads remove authorization after a redirect to any other host or to HTTP, including later redirects back to GitHub. I preserve the caller's redirect policy, or Go's default redirect limit when none is set, without restoring tokens that Go removes.
- **Verify:** `DownloadRelease()` first verifies that the release's `checksums.sha256.sig` is an `ssh-keygen -Y sign` signature in the `gitbox-release` namespace, by the key embedded from `release-signing-key.pub`, over `gitbox <tag>` plus the checksums (a small sshsig verifier on `golang.org/x/crypto/ssh`). Only then does it fetch the platform-specific artifact and verify its SHA256 checksum. It fails closed when the signature or `checksums.sha256` is missing, unreadable or invalid, or doesn't list the artifact. See [release-signing.md](release-signing.md).
- **Install:** `ExtractUpdate()` and `InstallExtracted()` unpack the zip and replace what is installed next to the running app: the whole `GitboxApp.app` bundle on macOS, an atomic rename on other Unix systems, and on Windows a rename of the running binary to `.old` first (`CleanupOldBinary()` removes stale `.old` files on the next startup).
- **Relaunch:** `Relaunch()` starts the new version detached with `--wait-pid=<old PID>` (on macOS through `open -n` on the bundle), and the GUI quits. The new process waits in `WaitForExit()` until the old one has exited, so the single-instance lock is free. An elevated Windows update relaunches from its helper script through `explorer.exe`, so the new app isn't elevated.

### pkg/doctor — External-tool detection

Probes the host for every external binary Gitbox may call (`git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, and `wsl` on Windows) and reports path, version and per-OS install hints for anything missing. `PrecheckForCredentialType()` powers the GUI setup flows, so a missing tool shows as a yellow banner with the install command instead of a cryptic runtime error. For GCM the probe asks `git credential-manager --version` first, because git resolves it from its own directories, which need not be on `PATH`. It falls back to the known install folders only when git can't run it. The GUI shows the full report under **Settings → System check**, alongside the credential permission audit from `pkg/credential`.

The probe never runs `C:\Windows\System32\wsl.exe`: Windows ships that file as a stub that prompts to install WSL, in a window of its own, when the package is absent. `doctor.WSLDistros()` reads the distros from the registry, and the version comes from the package's own binary under `Program Files\WSL`. Shell detection uses the same list, so the `wsl` shell only appears when a distro is installed.

### pkg/heal — Repo .git/config self-heal

`heal.Repo(cfg, sourceKey, repoKey)` idempotently reconciles a clone's `.git/config` against the account spec: `user.name`, `user.email`, the canonical `origin` URL (credential-type specific, no embedded secrets), and the credential helper. Every trigger point runs it (clone, fetch, pull, repo move, `ops.ReconfigureClones` and the GUI's periodic sync), so clones that drift out of spec are repaired silently.

Identity, origin and credential checks share one `git.ReadConfigSnapshot`: two Git processes read the effective config (with includes) and the direct local config. An unchanged clone causes no credential-config or credential-file writes. Changed token files use the shared credential writer, which serializes updates, prepares complete replacement files and applies the Unix file mode policy. The permission audit separately reports Windows ACL exposure and any permissions it cannot verify. If a later include or worktree config still overrides a repair, heal reports a maintenance warning and the GUI skips the fetch rather than use the wrong identity, origin or helper chain.

### pkg/move — Cross-account and cross-provider repo move

Relocates a clone from one configured account to another, including across providers (GitHub → GitLab, Gitea → Forgejo). The flow runs in phases: preflight (credential-readiness probe) → fetch → create destination → `git push --mirror` → rewire `origin` → update `gitbox.json` → optional source-remote delete → optional local-clone delete → auto-clone of the destination when the local clone was deleted. Phase callbacks stream progress to the GUI. Phases 1–6 are fatal on failure. The optional deletes are best-effort and surface as warnings, with `provider.InsufficientScopesError` turned into remediation text (for example, "add `delete_repo` scope to your GitHub PAT").

### pkg/gitignore — Global gitignore self-heal

Installs a curated managed block of OS-junk patterns (`.DS_Store`, `Thumbs.db`, `*~`, …) into `~/.gitignore_global` and points `core.excludesfile` at it. Sentinel markers (`# >>> gitbox:global-gitignore >>>` / `# <<< gitbox:global-gitignore <<<`) wrap the block, so re-installs rewrite only the managed region. User entries and negation patterns (`!.DS_Store`) outside the sentinels are preserved, and duplicates of managed patterns outside them are removed. The write is an atomic tmp+rename with a rolling cap of 3 backups (`.bak-YYYYMMDD-HHMMSS`). `global.check_global_gitignore` in `gitbox.json` gates only the automatic startup check; explicit actions always run. The GUI exposes it as a banner with an **Install** button.

### pkg/workspace — Container workspace files

Ties VS Code `.code-workspace` files to the clones that hold them. Gitbox never creates, edits, generates or deletes workspace files.

- `Sync` reads the root of every clone on disk (it doesn't recurse). One workspace file makes the clone a container with that file in `repo.workspace`. Several make it a container that opens the preferred file (`repo.workspace_preferred`, set when the user picks one) when it is there, else keeps its current file, else reports a `Choice` for the GUI to ask about. None clears a stale pick and leaves the container flag alone.
- `Apply` writes those changes into the config.
- `Members` reads the folders a workspace file lists inside its container. `Placeholders` keeps those that aren't on disk, that no configured repo resolves to, and that don't sit inside another clone (a submodule, say). `MemberRepos` lists the configured clones it names, which the GUI shows in the workspace colour.

Workspace files are JSON with comments, so comments and trailing commas are stripped before parsing. The disk reconcile binding runs `Sync` on a snapshot without the config lock, applies the changes under it, then runs the reconcile scan, so a container that just gained a workspace file has its nested clones adopted in the same pass.

### pkg/adopt — Orphan repo discovery

Scans every root in `global.roots` and the working trees of container repos for clones that are not in `gitbox.json`, then scores each against every host-matching account. All signals are additive, and the highest score wins:

| Signal                                                                    | Score | Source                                                             |
| ------------------------------------------------------------------------- | ----- | ------------------------------------------------------------------ |
| Host match (required baseline)                                            | 1     | account URL hostname or SSH alias vs the parsed remote host        |
| Owner equals `account.username`                                           | +3    | owner segment of the remote URL path                               |
| Repo lives under the account's source folder                              | +5    | first path component of the repo relative to the root holding it   |
| HTTPS URL embeds `user@` where user equals `account.username`             | +10   | `url.User.Username()` of the origin remote                         |
| `.git/config` has `credential.<url>.username` equal to `account.username` | +10   | `git config --get-regexp '^credential\..*\.username$'` in the repo |

If two or more accounts share the top score (including a bare host-only tie), the match is ambiguous and no account is picked. A unique match is **confident** when its score is above the host-only baseline, so something beyond the server ties the clone to the account. Adoption never moves files: a clone at its standard spot under a root records that root, and any other clone gets an absolute `clone_folder`. Adopted clones get credential isolation, identity, and a remote URL rewritten to match the credential type.

Ordinary orphan discovery stops at each clone boundary, including a `.git` file used by a worktree. Explicit roots are scanned independently. The container scan uses `nested_scan_depth` to find nested clones, and also scans every folder a container's workspace file lists inside it, at any depth.

### pkg/reconcile — Disk reconcile

`Plan` runs the orphan scan and decides, for each clone the config doesn't track, in this order:

- A linked worktree, or a clone without a usable remote, waits for the user.
- A clone whose repo key and server match exactly one tracked repo whose clone went missing, with its root online, **re-links** that entry to the new path.
- A confident match is **adopted** in place.
- Everything else waits in the Unmatched list with its reason: host-only, ambiguous, unknown host, no remote, already tracked, or worktree.

Paths in `global.ignored_clones` are reported apart. `Apply` writes the decisions into the config; neither step touches files. The GUI runs a pass at startup, on window focus, after a manual Fetch all, after clone-root changes and on a container rescan. Each pass first syncs container workspace files (`pkg/workspace`), then snapshots the config, scans without the lock, re-checks the config (retrying when it changed) and applies under the lock. Repos under a missing root are never re-linked or forgotten.

### pkg/mirror — Repository mirroring

Handles push and pull mirror setup, status checking, discovery and manual setup guides. Mirrors keep backup copies of repos on another provider without cloning locally.

**Mirror types:**

| Type | Direction                       | Use case                                        |
| ---- | ------------------------------- | ----------------------------------------------- |
| Push | Origin server pushes to backup  | Source repo on Forgejo/GitLab; backup on GitHub |
| Pull | Backup server pulls from origin | Source repo on GitHub; backup on Forgejo        |

**Provider automation:**

| Provider      | Create repo | Push mirror              | Pull mirror           |
| ------------- | ----------- | ------------------------ | --------------------- |
| Gitea/Forgejo | Yes         | Yes                      | Yes (via migrate API) |
| GitHub        | Yes         | No (guide only)          | No                    |
| GitLab        | Yes         | Yes (remote mirrors API) | No                    |
| Bitbucket     | Yes         | No (guide only)          | No                    |

**Key design decisions:**

- **Config model:** `mirrors` is an optional top-level section (`omitempty`), so configs without mirrors stay valid. Each mirror group pairs two accounts (`account_src`, `account_dst`) with per-repo direction and origin settings.
- **Mirror tokens:** remote servers need portable PATs, not machine-local GCM OAuth tokens. `ResolveMirrorToken()` enforces this, so GCM accounts must store a separate PAT for mirrors.
- **Immediate sync:** after creating a push mirror on Forgejo/Gitea, the code calls `/push_mirrors-sync`, so the first sync runs immediately instead of waiting for the configured interval.
- **Status comparison:** `CheckStatus()` queries the HEAD commit SHAs of origin and backup via the provider APIs and compares them. Mirror status is derived from these live checks, never stored.
- **Visibility checks:** status warns when backup repos are not private.
- **Discovery:** scans all account pairs for existing mirror relationships, with decreasing confidence: push mirror API (confirmed), pull mirror flag (likely), name match (possible).

### pkg/terminals, pkg/launch, pkg/harness — Open-in actions

`pkg/terminals` owns the compiled-in catalog of supported terminals and shells per OS, host detection, OS-aware Profile composition, and the merge logic that keeps `terminal_apps[]`, `shells[]` and `terminal_profiles[]` in step with what is installed. `pkg/launch.ResolveArgs` expands a Profile's argv template for a folder outside the Wails runtime, so the token rules are unit-tested. `pkg/harness` parses the embedded [`tools-directory.md`](../pkg/harness/tools-directory.md) table to decide which AI CLI harnesses the GUI auto-detects; [agentic-tools-directory.md](agentic-tools-directory.md) documents its rules. See [Terminal profiles](#terminal-profiles) for the detection and launch rules.

### pkg/i18n — UI language

Resolves and normalizes the UI language, in order: the `GITBOX_LANG` environment variable, `global.language`, the OS locale, and English as the fallback. The string catalogs ship in the Svelte frontend (`cmd/gui/frontend/src/lib/i18n.ts`); this package only decides which one to use.

---

## 4. Config format (v5)

The config lives at `~/.config/gitbox/gitbox.json`. The [annotated JSON example](../json/gitbox.jsonc) shows a complete config with comments, and the [JSON Schema](../json/gitbox.schema.json) drives editor validation and autocompletion. The top-level keys are `$schema` (optional), `version`, `global`, `accounts`, `sources` and the optional `mirrors`.

**Versioning:** `config.CurrentVersion` is 5. Gitbox loads v5 strictly and migrates v2, v3 and v4 files on load (in memory, persisted on the next save). The v3 → v4 step turns `global.folder` and `global.extra_folders` into `global.roots`, in that order, so every clone keeps its path. The v4 → v5 step drops the `workspaces` section, and the next disk reconcile records each container's workspace file in `repo.workspace`. Any other version is an error. Additive changes never bump the version.

**Automatic backups:** every meaningful save first writes a dated backup in the same directory (for example, `gitbox-20260401-143025.json`) and keeps the 10 most recent. The GUI's corruption-recovery screen restores any of them in one click. Saves that only change the window position, the layout (`collapsed`) or the theme skip the backup, so cosmetic churn never rotates real pre-corruption copies out of the ring.

**Credential type inheritance:** repos inherit `default_credential_type` from their account unless they set their own `credential_type`.

**Folder resolution:** `root / sourceFolder / idFolder / cloneFolder`, where the root is `repo.root`, else `source.root`, else the first of `global.roots`, with overrides possible at each level. An absolute `clone_folder` replaces the entire hierarchy.

### Global

| Field                             | Type   | Required | Description                                                                                                                                                                                                                                                                                                                                                                                                              |
| --------------------------------- | ------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `roots`                           | array  | Yes      | Clone roots: the folders Gitbox clones into and scans. The first is the default. Supports `~`.                                                                                                                                                                                                                                                                                                                           |
| `nested_scan_depth`               | int    | No       | Levels Gitbox descends below a container repo to find nested clones. Default `1` (immediate children).                                                                                                                                                                                                                                                                                                                   |
| `ignored_clones`                  | array  | No       | Absolute paths of clones under a root that the user chose not to track. Reconcile never adopts or lists them.                                                                                                                                                                                                                                                                                                            |
| `language`                        | string | No       | UI language. Empty means the OS locale (English as the fallback).                                                                                                                                                                                                                                                                                                                                                        |
| `periodic_sync`                   | string | No       | Background fetch interval: off, `5m`, `15m`, or `30m`.                                                                                                                                                                                                                                                                                                                                                                   |
| `view_mode`                       | string | No       | `"full"` or `"compact"`.                                                                                                                                                                                                                                                                                                                                                                                                 |
| `ui_mode`                         | string | No       | `"simple"` or `"advanced"` GUI experience. Absent means advanced; onboarding writes `"simple"` on a new install.                                                                                                                                                                                                                                                                                                         |
| `theme`                           | string | No       | `"light"` or `"dark"` GUI theme. Absent follows the OS. Theme-only saves skip the backup ring.                                                                                                                                                                                                                                                                                                                           |
| `collapsed`                       | object | No       | GUI layout state: folded account groups (`sources`), folded parent clones (`repos`, as `"<source>/<repo>"`), `sidebar` (folded to the icon rail) and `view` (`attention`, `all`, `mirrors` or `account:<key>`). Absent means all expanded and Needs attention. `Save` prunes stale keys, a deleted account's view and the retired `workspaces` view; a rename rewrites the view. Layout-only saves skip the backup ring. |
| `window` / `compact_window`       | object | No       | Saved position and size (`x`, `y`, `width`, `height`) per view mode.                                                                                                                                                                                                                                                                                                                                                     |
| `check_global_gitignore`          | bool   | No       | Run the automatic `~/.gitignore_global` check at startup. Default `true`.                                                                                                                                                                                                                                                                                                                                                |
| `pr_badges_enabled`               | bool   | No       | Fetch and show PR / review indicators on clone rows. Default `true`.                                                                                                                                                                                                                                                                                                                                                     |
| `pr_include_drafts`               | bool   | No       | Count draft PRs in the "my PRs" badge. Default `true`.                                                                                                                                                                                                                                                                                                                                                                   |
| `credential_ssh`                  | object | No       | SSH platform defaults. Presence indicates SSH is available.                                                                                                                                                                                                                                                                                                                                                              |
| `credential_ssh.ssh_folder`       | string | No       | SSH config directory. Default `~/.ssh`.                                                                                                                                                                                                                                                                                                                                                                                  |
| `credential_gcm`                  | object | No       | GCM platform defaults. Presence indicates GCM is available.                                                                                                                                                                                                                                                                                                                                                              |
| `credential_gcm.helper`           | string | No       | Credential helper. Typically `"manager"`.                                                                                                                                                                                                                                                                                                                                                                                |
| `credential_gcm.credential_store` | string | No       | `"wincredman"`, `"keychain"`, or `"secretservice"`.                                                                                                                                                                                                                                                                                                                                                                      |
| `credential_token`                | object | No       | Token/PAT platform defaults (no fields yet). Presence indicates token auth is available.                                                                                                                                                                                                                                                                                                                                 |
| `editors`                         | array  | No       | Code editors for the "Open in" menu. Auto-populated on first launch.                                                                                                                                                                                                                                                                                                                                                     |
| `editors[].name`                  | string | Yes      | Display name (for example, `"VS Code"`).                                                                                                                                                                                                                                                                                                                                                                                 |
| `editors[].command`               | string | Yes      | Full path or command name (for example, `"C:\\...\\code.cmd"`).                                                                                                                                                                                                                                                                                                                                                          |
| `terminals`                       | array  | No       | Legacy flat terminal list from before Terminal Profiles. Read only to migrate old configs into the three arrays below on load; Gitbox never writes it.                                                                                                                                                                                                                                                                   |
| `terminal_apps`                   | array  | No       | Detected terminal emulators, from the `pkg/terminals` catalog (see [Terminal profiles](#terminal-profiles)).                                                                                                                                                                                                                                                                                                             |
| `terminal_apps[].id`              | string | Yes      | Stable id (`"wt"`, `"wezterm"`, `"gnome-terminal"`, `"iterm"`, …), referenced by `terminal_profiles[].terminal`.                                                                                                                                                                                                                                                                                                         |
| `terminal_apps[].name`            | string | Yes      | Display name shown in the Terminals Manager and the per-row launcher.                                                                                                                                                                                                                                                                                                                                                    |
| `terminal_apps[].command`         | string | Yes      | Resolved absolute path, filled in at detect time.                                                                                                                                                                                                                                                                                                                                                                        |
| `terminal_apps[].args_template`   | array  | No       | Argv template that `pkg/launch.ResolveArgs` expands per Profile. Tokens: `{path}` (appended when absent), `{command}` (AI harness argv, empty for plain launches), `{shell_command}` (resolved shell binary), `{shell_args}` (the shell's default args).                                                                                                                                                                 |
| `shells`                          | array  | No       | Detected shells, from the `pkg/terminals` catalog, plus one `wsl-<distro>` row per installed WSL distro on Windows.                                                                                                                                                                                                                                                                                                      |
| `shells[].id`                     | string | Yes      | Stable id (`"cmd"`, `"pwsh"`, `"git-bash"`, `"wsl-ubuntu"`, …), referenced by `terminal_profiles[].shell`.                                                                                                                                                                                                                                                                                                               |
| `shells[].name`                   | string | Yes      | Display name.                                                                                                                                                                                                                                                                                                                                                                                                            |
| `shells[].command`                | string | Yes      | Resolved absolute path.                                                                                                                                                                                                                                                                                                                                                                                                  |
| `shells[].args`                   | array  | No       | Default args spliced where the terminal's `args_template` references `{shell_args}`.                                                                                                                                                                                                                                                                                                                                     |
| `terminal_profiles`               | array  | No       | Launchable Profiles, read by the per-row launcher and the "Open in" menu. Composition is OS-aware (see [OS-aware composition](#os-aware-composition)).                                                                                                                                                                                                                                                                   |
| `terminal_profiles[].id`          | string | Yes      | Stable id (`"wt+pwsh"`, `"wezterm+launchmenu-mybash"`, `"user-1"`, …).                                                                                                                                                                                                                                                                                                                                                   |
| `terminal_profiles[].name`        | string | Yes      | Display name (for example, `"Windows Terminal — PowerShell 7"`).                                                                                                                                                                                                                                                                                                                                                         |
| `terminal_profiles[].terminal`    | string | Yes      | `terminal_apps[].id` to launch. Empty only for bare-shell Profiles on Windows.                                                                                                                                                                                                                                                                                                                                           |
| `terminal_profiles[].shell`       | string | No       | `shells[].id` to run inside the terminal. Empty means the terminal's default, which on macOS and Linux is the host's login shell.                                                                                                                                                                                                                                                                                        |
| `terminal_profiles[].args`        | array  | No       | Override argv. When empty the launcher uses `terminal_apps[].args_template`. WezTerm `launch_menu` rows store the full `start --cwd {path} -- <argv>` shape here.                                                                                                                                                                                                                                                        |
| `terminal_profiles[].default`     | bool   | No       | Marks the Profile the per-row launcher's primary action runs. At most one Profile has it.                                                                                                                                                                                                                                                                                                                                |
| `terminal_profiles[].preferred`   | bool   | No       | Lists the Profile under Open with in the ⋯ menus.                                                                                                                                                                                                                                                                                                                                                                        |
| `terminal_profiles[].hidden`      | bool   | No       | Hides the Profile from menus without deleting it. This is the only way to suppress a non-user Profile, since a deleted one comes back on the next detect cycle.                                                                                                                                                                                                                                                          |
| `terminal_profiles[].source`      | string | No       | **Internal field**, never displayed. Origin tag that gates delete versus hide: only `"user"` Profiles can be deleted; `"detected"`, `"wt-profile"`, `"wezterm-launchmenu"` and `"migrated"` rows can only be hidden.                                                                                                                                                                                                     |
| `ai_harnesses`                    | array  | No       | AI CLI harnesses for the "Open in" menu. Detected in the background (shortly after launch, every 10 minutes, and on window focus) from the embedded catalog plus `~/.local/bin`, Homebrew prefixes and per-tool well-known directories. Launched inside the default Terminal Profile's shell (see `terminal_profiles[].default`).                                                                                        |
| `ai_harnesses[].name`             | string | Yes      | Display name (for example, `"Claude Code"`).                                                                                                                                                                                                                                                                                                                                                                             |
| `ai_harnesses[].command`          | string | Yes      | Absolute path or on-PATH binary (for example, `"claude"`).                                                                                                                                                                                                                                                                                                                                                               |
| `ai_harnesses[].args`             | array  | No       | Optional extra args for the harness. Usually empty.                                                                                                                                                                                                                                                                                                                                                                      |
| `ai_harnesses[].source`           | string | No       | **Internal field.** The harness sync adds `"detected"` entries and may re-resolve their `command` after a reinstall; it never alters `"user"` entries beyond `missing`. Empty on older entries until the first sync classifies them.                                                                                                                                                                                     |
| `ai_harnesses[].missing`          | bool   | No       | Set by the harness sync when the binary can no longer be found. The entry is hidden from menus but kept, so a reinstall restores it with its `args` intact. Cleared when the binary reappears.                                                                                                                                                                                                                           |

### Account

| Field                     | Type    | Required    | Description                                                                        |
| ------------------------- | ------- | ----------- | ---------------------------------------------------------------------------------- |
| `provider`                | string  | Yes         | `"github"`, `"gitlab"`, `"gitea"`, `"forgejo"`, `"bitbucket"`, `"generic"`         |
| `url`                     | string  | Yes         | Server URL (scheme+host, no path).                                                 |
| `username`                | string  | Yes         | Account username.                                                                  |
| `name`                    | string  | Yes         | Default `git user.name`.                                                           |
| `email`                   | string  | Yes         | Default `git user.email`.                                                          |
| `default_credential_type` | string  | No          | Default auth: `"gcm"`, `"ssh"`, or `"token"`.                                      |
| `ssh.host`                | string  | Conditional | SSH Host alias (for example, `"gt-myuser"`). **Mandatory** when SSH is configured. |
| `ssh.hostname`            | string  | No          | Real SSH hostname. Derived from the URL if omitted.                                |
| `ssh.key_type`            | string  | Conditional | `"ed25519"` or `"rsa"`. **Mandatory** when SSH is configured.                      |
| `gcm.provider`            | string  | No          | GCM provider hint.                                                                 |
| `gcm.useHttpPath`         | boolean | No          | Scope credentials by HTTP path.                                                    |

An account is unique by `(hostname, username)`. Deleting an account cascades through every source and mirror that references it.

### Source

| Field     | Type   | Required | Description                                                                         |
| --------- | ------ | -------- | ----------------------------------------------------------------------------------- |
| `account` | string | Yes      | References an account key.                                                          |
| `folder`  | string | No       | Overrides the first-level clone folder. Default: the source key.                    |
| `root`    | string | No       | Clone root it clones into, remembered from the last clone. Default: the first root. |
| `repos`   | object | Yes      | Repos keyed by `org/repo`.                                                          |

### Repo (within source.repos)

| Field                 | Type   | Required | Description                                                                                                          |
| --------------------- | ------ | -------- | -------------------------------------------------------------------------------------------------------------------- |
| `credential_type`     | string | No       | Overrides the auth method. Inherits from the account.                                                                |
| `name`                | string | No       | Overrides `git user.name`.                                                                                           |
| `email`               | string | No       | Overrides `git user.email`.                                                                                          |
| `id_folder`           | string | No       | Overrides the 2nd-level dir (org folder).                                                                            |
| `clone_folder`        | string | No       | Overrides the 3rd-level dir. An absolute value replaces the entire path.                                             |
| `root`                | string | No       | Clone root, set only when it differs from the source's.                                                              |
| `container`           | bool   | No       | Marks a multi-repo container; Gitbox scans inside it for nested clones.                                              |
| `workspace`           | string | No       | The container's `.code-workspace` file, relative to the clone root. `pkg/workspace.Sync` keeps it in step with disk. |
| `workspace_preferred` | string | No       | The file the user picked among several. It wins whenever it is at the root, and survives while it is missing.        |

### Mirrors

```json
{
  "mirrors": {
    "forgejo-github": {
      "account_src": "my-forgejo",
      "account_dst": "github-personal",
      "repos": {
        "infra/homelab": {
          "direction": "push",
          "origin": "src"
        },
        "MyUser/dotfiles": {
          "direction": "pull",
          "origin": "dst"
        }
      }
    }
  }
}
```

| Field                     | Type   | Required | Description                                                  |
| ------------------------- | ------ | -------- | ------------------------------------------------------------ |
| `account_src`             | string | Yes      | Source account key.                                          |
| `account_dst`             | string | Yes      | Destination account key (must differ from `account_src`).    |
| `repos.<key>.direction`   | string | Yes      | `"push"` or `"pull"`.                                        |
| `repos.<key>.origin`      | string | Yes      | `"src"` or `"dst"`: which account holds the source of truth. |
| `repos.<key>.target_repo` | string | No       | Overrides the target repo name (default: same as the key).   |
| `repos.<key>.last_sync`   | string | No       | RFC3339 timestamp of the last known sync.                    |
| `repos.<key>.error`       | string | No       | Last error message.                                          |

Mirror status is not stored: the GUI derives it from live checks (see [pkg/mirror](#pkgmirror--repository-mirroring)).

### Terminal profiles

`pkg/terminals` owns the `terminal_apps[]` + `shells[]` + `terminal_profiles[]` trio. It ships a compiled-in catalog of the terminals and shells Gitbox knows how to detect and launch per OS. Adding a terminal is a code change in `pkg/terminals/catalog.go`. The catalog covers:

- **Windows:** Windows Terminal, WezTerm, Alacritty, Tabby, ConEmu, Hyper, Mintty and ZOC; shells PowerShell 7, PowerShell 5, Command Prompt, Git Bash, and WSL (one `wsl-<distro>` row per installed distro).
- **macOS:** iTerm2, Terminal, Warp, Kitty, Ghostty, WezTerm and Alacritty; shells Zsh, Bash, Fish and Dash.
- **Linux:** GNOME Terminal, Konsole, Terminator, Foot, Alacritty, Kitty, Tilda, Guake and xterm; shells Bash, Zsh, Fish, Ksh and Dash.

On every launch the catalog probes the host and merges the result into `gitbox.json`:

- Installed catalog entries are added to `terminal_apps[]` and `shells[]` only if missing, so existing rows survive re-detection.
- Hidden flags survive re-detection, including after upgrades that grow the catalog.
- User Profiles (`source: "user"`) and migrated legacy Profiles (`source: "migrated"`) are kept verbatim, even when they aren't in the freshly detected set.
- Catalog entries that aren't installed are skipped silently and appear once the binary is installed.

#### OS-aware composition

The auto-derived Profile set follows different rules per platform:

- **Windows:** each Profile pairs a Terminal with a Shell, for every modern Terminal and every Shell. The four directly launchable shells (`pwsh`, `powershell`, `cmd`, `wsl`) also get a bare-shell Profile, hidden by default. When no modern Terminal is installed, every shell gets a visible bare-shell Profile instead, and the Manager shows a banner: "Install Windows Terminal for the best experience."
- **macOS and Linux:** each Profile is Terminal-only (`terminal_profiles[].shell == ""`). The host's login shell is implicit: `pkg/launch.ResolveArgs` collapses the empty shell tokens to zero items, and the Manager shows the login shell as a dim badge next to the Terminal name. A Terminal can still be paired with a non-login Shell through the Manager's `+ Add profile` form, which stamps the row `source: "user"`.

The Add-Profile form follows the same rules: on macOS and Linux the shell selector defaults to a `(login shell)` entry, and on Windows a shell is mandatory.

#### How launch matching works

A `WezTerm — PowerShell 7` or `Windows Terminal — PowerShell 7` Profile doesn't just run the generic per-Terminal template. Gitbox first looks for a matching entry in the user's own terminal config and falls back to the generic template only when none matches.

The lookup runs at every launch, behind an in-process cache invalidated by file mtime and size, so edits to `wezterm.lua` or `settings.json` apply without a restart:

- **WezTerm:** Gitbox parses `wezterm.lua` (`$WEZTERM_CONFIG_FILE`, then `$XDG_CONFIG_HOME/wezterm/wezterm.lua`, then `~/.config/wezterm/wezterm.lua`, then `~/.wezterm.lua`) and looks for a `config.launch_menu` entry whose label matches the Gitbox shell. On a hit it launches `wezterm-gui.exe start --cwd <path> -- <entry args>` and applies the entry's `set_environment_variables` on top of the parent environment. The parser binds only to the documented `config.launch_menu` table. Entries kept in a custom variable (`local profiles = { … }`) that drives a custom picker stay invisible; adding `config.launch_menu = profiles` at the end of `wezterm.lua` exposes them without changing the picker. Lua picker callbacks (per-entry `color_scheme`, `mux.spawn_window` overrides, `window-focus-changed` handlers) are not reproduced, since they fire only when an entry is picked from WezTerm's own launcher, never when a pane is spawned externally.
- **Windows Terminal:** Gitbox parses `settings.json` (Store install, then Preview install, then the unpackaged install under `%LOCALAPPDATA%`) and looks for a profile in `profiles.list` whose `name` matches the Gitbox shell. Hidden WT profiles and profiles whose `source` appears in WT's top-level `disabledProfileSources` never match. On a hit it runs `wt.exe -w 0 nt --profile "<name>" -d <path>`, and `wt.exe` reads the profile's `commandline`, font, colours and starting flags itself. The `-w 0 nt` prefix opens a new tab in the most recent WT window (or a new window if none exists), so `firstWindowPreference: persistedWindowLayout` doesn't spawn a second window when WT was closed with saved tabs.
- **No match, no config, or terminal not installed:** Gitbox falls back to the generic argv template (`wezterm-gui.exe start --cwd <path> -- <shell> <args>`, `wt.exe -d <path> <shell> <args>`, and so on), which suits shells not wired into the terminal config.

Bare-shell Profiles (no terminal) skip the lookup: there is no terminal config to consult, so running the shell directly is correct. On Windows they launch through `cmd.exe /C start "" /D <path> <shell>`, which gives the shell a fresh console and its starting folder.

The shell-name matcher is forgiving:

- **Direct match:** the entry's normalised name equals the Gitbox shell's display name (`"PowerShell 7"`, `"WSL — Ubuntu-24.04"`).
- **Em-dash suffix:** for Gitbox names like `"WSL — Ubuntu-24.04"`, an entry labelled just `"Ubuntu-24.04"` matches too.
- **Pattern fallback:** `pwsh` matches entries containing `"powershell 7"`, `"powershell core"` or `"pwsh"`; `powershell` matches `"powershell 5"` or `"windows powershell"`; `cmd` matches `"command prompt"` or `"cmd exe"`; `git-bash` matches `"git bash"`; `wsl-<distro>` matches the bare distro slug (`"ubuntu 24 04"`).

#### AI harness launches

An AI harness runs inside a shell of the default Terminal Profile, so it inherits the user's rc files (where nvm, npm and Git Bash put most harness CLIs on `PATH`), and the shell stays open when the harness exits. `pkg/launch.HarnessInShell` wraps the harness per shell family:

- **PowerShell:** `-NoExit -EncodedCommand <base64>`, which survives WT's argv rebuild and the `cmd.exe start` wrapper without quoting.
- **cmd:** `/K <harness> <args>`.
- **fish:** `-C '<harness> <args>'`.
- **WSL:** `-- sh -lc '<harness>; exec "${SHELL:-sh}" -l'`.
- **Other POSIX shells** (bash, zsh, sh, dash, ksh, Git Bash): the shell's args plus `-i -c '<harness>; exec <shell> <args>'`.

On Windows, Git Bash receives npm `.cmd` shims by bare name (npm also installs an extensionless shim on the same `PATH`) and other binaries by path with forward slashes, and WSL always gets the bare name. Native Windows shells get the resolved path.

`pkg/terminals.ResolveLaunch` places the wrapped shell:

- A Profile without a shell (the macOS and Linux default) wraps `$SHELL`, falling back to `/bin/sh`.
- A matched WT profile gets the wrapped shell as WT's trailing commandline, and a matched WezTerm `launch_menu` entry is wrapped around the shell from the entry's args.
- macOS Terminal and iTerm are driven through AppleScript (`osascript`), which runs `cd <folder> && <harness>` in a new window.
- Terminals whose template can't host a command (Warp, Kitty, Ghostty and Alacritty on macOS) return an error that asks for a Profile that hosts a shell.

Harness detection resolves binaries through `doctor.Lookup` without changing the process `PATH`, so it is safe while fetches and clones run.

---

## 5. Credential architecture

<p align="center">
  <img src="diagrams/credential-flow.png" alt="Credential Flow" width="800" />
</p>

<p align="center">
  <img src="diagrams/credential-types.png" alt="Credential Types" width="800" />
</p>

### Token flow

The user picks Token in the account's credential settings → the app shows the provider-specific PAT creation URL with the required scopes → the user pastes the token → the app validates it via the provider API and stores it directly in git-credential-store format in the credential file (`~/.config/gitbox/credentials/<key>`). Before cloning, Gitbox ensures that file matches the resolved credential and passes an empty credential-helper reset followed by the account's store-file helper to Git. The clone URL and process arguments never contain the token. Each clone's `.git/config` gets `credential.helper = store --file <path>` pointing at that same file, so `git push` and `git pull` from any terminal work without GCM.

### GCM flow

The user picks GCM in the account's credential settings → the app runs `git credential fill`, which opens GCM's browser sign-in → the app runs `git credential approve` to persist the result → it tests API access with the GCM token. Clones use HTTPS with the username. Each clone's `.git/config` gets `credential.helper = manager` plus per-host `username` and `provider` and `credential.credentialStore`, making it self-contained. API access extracts the OAuth token via `git credential fill`.

### SSH flow

The user picks SSH in the account's credential settings → the app writes a `~/.ssh/config` entry and generates a key pair (`ssh.key_type`, ed25519 by default) → it shows the public key for the user to register at the provider → it tests the SSH connection. Clones use `git@<host-alias>:repo.git` URLs routed through the SSH config. API access optionally uses a PAT for discovery, stored in the credential file. Each clone's `.git/config` sets an empty `credential.helper =` to cancel any global credential helper.

### Credential type switching

When an account's credential type changes, `ops.ChangeCredentialType` and `ops.ReconfigureClones`:

1. **Remove the old type's artifacts** (`ops.RemoveCredentialArtifacts`).
2. **Update the account config** with the new type and credential sub-object.
3. **Reconfigure every existing clone**: remote URL and per-repo credential config.

What step 1 removes depends only on the old type, so no ghost credentials outlive a switch. `ops.DeleteCredential` runs the same cleanup.

| Old type | Credential file `credentials/<key>` | GCM cached credential (`git credential reject`) | SSH key pair + `~/.ssh/config` host |
| -------- | ----------------------------------- | ----------------------------------------------- | ----------------------------------- |
| Token    | Deleted (the PAT)                   | ---                                             | ---                                 |
| GCM      | Deleted if present (mirror PAT)     | Deleted                                         | ---                                 |
| SSH      | Deleted if present (discovery PAT)  | ---                                             | Deleted                             |

---

## 6. GUI architecture

The Go backend (`cmd/gui/app.go`) exposes methods the Svelte frontend calls through auto-generated TypeScript bindings, wrapped by `cmd/gui/frontend/src/lib/bridge.ts`. Long-running operations (clone, status refresh, pull, mirror discovery, repo moves) run in goroutines and push progress to the frontend through Wails events.

**Frontend structure:**

- `App.svelte` composes the shell and holds the app-level state, the event listeners and the actions several parts share.
- `src/lib/components/` holds the dashboard parts (top bar, activity strip, banners, settings panel, sidebar, the Needs attention, All repos, account and Mirrors views, account card and menu, repo tree, rows and detail panel, footer, notices, compact view). They render from props.
- `src/lib/modals/` holds one component per modal, each with its own state. App opens a modal through a small exported function and binds only the state it shares, such as the credential flow common to Add account and Change credential. `modals.css` holds the dialog shell, buttons and form controls the modals share; `src/style.css` holds the theme tokens.
- `src/lib/status.ts` is the one place that maps a repo status to its shape, colour, glossary label, Simple kind and attention rank, and an account's credential to its health words and fix action. `src/lib/views.ts` resolves the saved view and builds the Needs attention list and counts. `src/lib/activity.ts` tracks Fetch all and Pull all runs and their summaries. `src/lib/notices.ts` holds the result cards and error messages that replace `window.alert()`. `src/lib/repo-tree.ts` builds the nested clone tree and sorts siblings (`compareRepoKeys`). Each has a `node:test` file.
- Every user-facing string goes through `$t()` from `src/lib/i18n.ts`, which holds the English and Spanish catalogs with the same keys. A sentence with an inline config key, bold or emphasis stays one catalog entry with `` `code` ``, `**strong**` and `*em*` markers, rendered by `src/lib/Rich.svelte`. `src/lib/i18n.test.mjs` fails on hard-coded template text, on keys missing in either language, and on keys the code asks for that don't exist.

**Layout structure** (the [GUI guide](gui-guide.md) describes each part from the user's side):

- **Top bar:** the GitBox logo (the sidebar toggle, ⌘B / Ctrl+B), the repo health ring, the mirror health ring (Advanced, when mirrors exist), Pull all and Fetch all, then Delete mode, Compact view and theme in Advanced, the Simple/Advanced switch and the Settings gear.
- **Experience:** Simple shows plain-language states and hides git detail, Mirrors and the advanced controls; Advanced shows everything. Saved as `global.ui_mode`; new installs start in Simple.
- **Activity strip:** below the top bar while Fetch all or Pull all runs, with its scope and a progress bar.
- **Sidebar:** Needs attention and All repos with their counts, one entry per account (provider logo, health, attention count), Add account, Found on disk (orphans), and Mirrors in Advanced. It folds to an icon rail, is always the rail below 780 px, and remembers the view and the rail in `global.collapsed`.
- **Views:** Needs attention (accounts with a credential to fix, then repos grouped by what they need), All repos (a tree of nested clones per account group, with a container's missing workspace folders as dimmed rows), the account view (account card and its clones), and Mirrors (Advanced only). There is no Workspaces view: workspaces live on their container in the clone tree.
- **Result cards:** what Fetch all or Pull all did, and any action's error, bottom right, above modals.
- **Summary footer:** aggregated counts for repos and mirrors, plus the update pill when a newer release exists.
- **Compact view:** a narrow window with the health ring, account pills and a mirror summary pill.

**Cross-cutting behaviour:**

- **Config auto-backup:** see [Automatic backups](#4-config-format-v5).
- **Window state:** position and size are saved per view mode (`window` and `compact_window`) and restored on launch.
- **Autostart:** platform-specific registration (macOS launch agent, Windows registry), configurable from the GUI.
- **Create repo:** repos can be created on the provider (under the user or an org) from the account card or menu, with the owner dropdown filled via `OrgLister`.
- **External edits:** when `gitbox.json` changes on disk, the GUI reloads it when the window regains focus.

---

## 7. UX design principles

The user doesn't need to know Git internals: actions are verbs that do what they say.

- **Live feedback:** each repo's status updates as soon as it is known, not in a batch. Long operations show progress, then snap to the final state; Fetch all and Pull all state their scope while they run and leave a result card.
- **Quiet by default:** the UI highlights errors, warnings and repos that need attention; clean repos stay calm.
- **Consistent colours:** status colours match across rings, badges and rows. The [GUI guide](gui-guide.md#automatic-checking) lists what each state and colour means.
- **Predictable behaviour:** lists follow config file order, actions are idempotent, errors say what to do next, and tokens are never displayed.

---

## 8. Security

- **No secrets in the config file.** `gitbox.json` holds only URLs, usernames, folder paths and preference flags. Provider API calls read tokens from credential files or GCM at runtime.
- **PATs** live in gitbox credential files (`~/.config/gitbox/credentials/<key>`), plaintext in git-credential-store format. Unix files use mode `0600` in a `0700` directory; Windows ACLs determine access. The metadata-only permission audit reports exposure and unverifiable permissions without repairing them.
- **GCM OAuth tokens** live in the OS credential store (Windows Credential Manager, macOS Keychain, Linux Secret Service), managed by Git Credential Manager.
- **SSH private keys** are standard `~/.ssh/` files with 0600 permissions.
- **Secret-free clone URLs:** cloning and later git operations authenticate through the per-repo credential helper. URLs and process arguments never contain the token. If a clone fails, Gitbox leaves any remaining destination in place and reports its path without rewriting the origin.
- **Per-repo credential isolation:** each clone's `.git/config` cancels global credential helpers and sets its own, which prevents credential leakage between accounts and ghost credentials from GCM's OAuth refresh tokens.
- **No secrets in output:** tokens are never displayed, not even in error messages.
- **Public repository:** tracked files contain no real hostnames, usernames, emails or tokens.

---

## 9. Diagrams

The diagrams live in `docs/diagrams/` as editable `.drawio` files, which open in [draw.io](https://app.diagrams.net/) or the VS Code drawio extension:

- `architecture-overview.drawio`: high-level system components.
- `credential-flow.drawio`: per-type credential resolution flow.
- `credential-types.drawio`: which secrets power discovery, git operations and mirrors per credential type.
- `config-model.drawio`: the accounts, sources, repos and mirrors data model.
