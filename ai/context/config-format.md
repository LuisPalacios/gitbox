---
read-when: Before changing gitbox.json keys, versioning or migration, or code in `pkg/config`, `pkg/reconcile` or `pkg/workspace`.
---

# Config format

Config file: `~/.config/gitbox/gitbox.json` — `accounts` + `sources`, real booleans, `gcm`/`ssh`/`token` credential types, nested SSH/GCM objects, `provider` field, `version: 5`. Optional `mirrors` section for push/pull mirror configuration between providers.

**Versioning & migration:** `config.CurrentVersion` is 5. `config.Parse` and `LoadWithRepair` share one `decode` that loads v5 strictly and migrates v2, v3 and v4 transparently on load (in-memory; persisted on next save). The v3→v4 step turns `global.folder` + `global.extra_folders` into `global.roots`, in that order, so every clone keeps its path. The v4→v5 step **drops the `workspaces` section** (`Config` no longer has the field); each container's `repo.workspace` is re-derived from disk. Accounts, sources, mirrors, and global settings are preserved; legacy `global.terminals` is converted by the existing terminal migrator. Any other version is an error.

**Keys:**

- `global.roots` (`[]string`, v4, required) — the clone roots: folders gitbox clones into and scans for clones. The first is the default. A clone lives at `<root>/<source folder>/<org>/<repo>`.
- `source.root` / `repo.root` (v4) — the root a source clones into (remembered from the last clone; empty = the default) and, only when it differs, the root one repo lives in. Every path resolves through `status.RepoPath`. The mutators in `pkg/config/roots.go` (`SetSourceRoot`, `SetDefaultRoot`, `RemoveRoot`, `PlaceRepo`) pin the roots clones already resolve through, so no clone path moves; never set these fields by hand in code.
- `global.ignored_clones` (`[]string`) — clones found under a root that I chose not to track; the disk reconcile (`pkg/reconcile`) never adopts or lists them. Reconcile runs from the GUI at startup, on focus, after a manual Fetch all and after root changes: it adopts confident matches in place, re-links moved clones and leaves the rest in Unmatched clones; repos under a missing root read `root offline`.
- `global.nested_scan_depth` (`int`, default 1 via `NestedScanDepthOrDefault`) — how many levels gitbox descends below a container repo to find nested clones.
- `repo.container` (`bool`) — marks a clone as a multi-repo parent; gitbox descends into its working tree to discover and onboard nested clones the user provisioned there. A clone with a `.code-workspace` at its root becomes a container on its own (see `repo.workspace`). Nested clones onboard as plain repos under their real account/org with an absolute `clone_folder`, never relocated. Parent→child nesting is derived from paths by `status.ComputeNesting`.
- `global.collapsed` (`{sources: [], repos: [], sidebar: bool, view: string}`) — GUI layout state: folded account groups (source keys), folded parent clones (`"source/repo"` keys), the sidebar folded to its icon rail, and the last selected view (`attention`, `all`, `mirrors` or `account:<key>`). Records what is collapsed, so absent means all expanded and Needs attention. `config.Save` prunes keys that no longer resolve, a view of a missing account and the retired `workspaces` view (`PruneCollapsed`), `RenameAccount` rewrites `account:<old>` to `account:<new>`, and the backup ring ignores layout-only changes, like window state. The old `cards_open` key is gone; `Parse` ignores it.
- `global.theme` (`"light"`, `"dark"`, absent = follow the OS) — the GUI theme, set through `SetTheme`; theme-only saves skip the backup ring too.
- `global.experimental_remote` (`bool`) and `global.remotes` (`[{alias, server_dir, editor, window, compact_window}]`) — remote mode (#233) on this machine and its destinations. `alias` is an `ssh_config` `Host` alias that only the system `ssh` resolves (1 to 64 of `[A-Za-z0-9._-]`, not starting with `-`, unique); `server_dir` is absolute or starts with `~/`; a remote window saves its own geometry in its entry. `validate` rejects a bad entry through `ValidateRemotes`.
- `repo.clone_folder` (existing) — an absolute value (`/`, `~`) places a clone anywhere on disk; this is how non-standard and nested clones are stored. A `../` value is resolved against the repo's root.
- `repo.workspace` (`string`, v5) — the `.code-workspace` file, relative to the clone root, that a container opens with. Never set it by hand in code: `pkg/workspace.Sync` keeps it in step with disk on every reconcile pass (one root file is recorded, several produce a `Choice` the GUI asks about, a vanished file is cleared) and `SetRepoWorkspace` records my pick.
- `repo.workspace_preferred` (`string`) — the file I picked among several (`SetRepoWorkspace` sets it with `repo.workspace`). `Sync` opens it whenever it is at the root, lets another file stand in while it is missing and never clears it.

**Workspaces live in the clone tree** (since `version: 5`): there is no Workspaces view and no cache. Every folder a container's workspace lists inside it is scanned for nested clones at any depth (`adopt.FindOrphansIn`); listed folders missing on disk come back as `placeholders` in the reconcile result and show as dimmed rows, and the clones it lists come back as `workspaceMembers`, shown with the container in the workspace colour (`--text-workspace`). The editor button opens the workspace file in VS Code-family editors. gitbox never creates, edits, generates, or deletes `.code-workspace` files, and tmuxinator support is removed.

**Client preferences and domain data:** `config.ClientPrefs` (`pkg/config/prefs.go`) names the global keys that belong to the machine showing the window. In remote mode the client keeps them in its own gitbox.json and lays them over the config from the server; in local mode both are the same file.

| Key | Class | Reason |
| --- | --- | --- |
| `theme`, `language`, `ui_mode`, `view_mode`, `window`, `compact_window` | client | how this machine shows the window |
| `collapsed.sidebar` | client | layout of this window |
| `collapsed.view`, `collapsed.sources`, `collapsed.repos` | domain | they name accounts, sources and repos; `PruneCollapsed` and `RenameAccount` keep them in step with the accounts |
| `editors`, `terminal_apps`, `shells`, `terminal_profiles`, legacy `terminals` | client | apps on this machine |
| `periodic_sync` | client | the timer runs in the frontend |
| `experimental_remote`, `remotes` | client | this machine's features and destinations |
| `ai_harnesses` | domain | detected where the CLI tools run |
| `roots`, `ignored_clones`, `nested_scan_depth`, `pr_badges_enabled`, `pr_include_drafts`, `check_global_gitignore`, `credential_gcm`, `credential_ssh`, `credential_token` | domain | they control work on the host that has the data |

- A new global key needs a row in `globalKeyClass` (`pkg/config/prefs_test.go`); a client key also goes in `ClientPrefs`, `ExtractPrefs` and `ApplyPrefs`. `TestStripClientPrefs` and `TestSchemaDeclaresGlobalKeys` fail otherwise.
- In `cmd/gui`, read and write client keys only through `a.clientPrefs()` (`prefsStore`), never through `a.cfg` or `be()`.
- A server strips client keys from what it sends (`StripClientPrefs`) and keeps the ones on disk when it saves (`PreserveClientPrefs`); it never zeroes them on disk.

**Optional keys do not bump the version:** `decode` uses plain `json.Unmarshal`, so a v5 loader ignores keys it does not know, and `TestUnknownKeysIgnoredV5` pins that. Adding `remotes` and `experimental_remote` therefore kept `version: 5`. The cost is a downgrade: an older GitboxApp that saves a config written by a newer one silently drops the keys it does not know, which here means re-adding aliases and loses no domain data. Bumping to v6 would be worse, because older binaries refuse any version other than 5 and would not start.
