# Feature radar

Active feature backlog for gitbox. Managed by the `/wish` skill.

## Status legend

| Status | Meaning |
| --- | --- |
| radar | Idea captured, not yet explored |
| planning | Being designed, codebase explored, plan drafted |
| active | Implementation in progress |
| shipped | Released, code merged |

## Priority legend

| Priority | Meaning |
| --- | --- |
| P1 | Next up — high value, ready to build |
| P2 | Important but not urgent, or needs prerequisite work |
| P3 | Nice to have, park for later |

## Radar

### W6: Open in browser

- **Status:** planning
- **Priority:** P1
- **Size:** S
- **Concept:** Add a context action to open a repository's remote page in the default browser. Parse the origin remote URL (SSH or HTTPS) to construct the web URL. Expose in GUI context menu, TUI keybinding, and CLI command.
- **Notes:** Web URL is `Account.URL + "/" + repoKey` (works for all providers). GUI: add `OpenInBrowser(url)` to `app.go` using same `open`/`xdg-open`/`cmd /c start` pattern as `OpenFileInEditor` (line 443). Wire through `bridge.ts`, add button to `App.svelte` kebab menu (line 1854) and compact view. TUI: add `b` keybinding in `screen_repos.go` (line 234). CLI: add `browse` Cobra command following `fetch_cmd.go` pattern. Tests: URL builder unit test in `helpers_test.go`, TUI keybinding test in `screen_repos_test.go`.

### W2: Bulk branch cleanup (`gitbox sweep`)

- **Status:** radar
- **Priority:** P1
- **Size:** M
- **Concept:** Safely delete local branches that have been merged upstream or removed on the remote, across all repos in a source or across all sources. A `gitbox sweep` CLI command plus a TUI action and GUI panel.
- **Notes:** `pkg/git/git.go` already wraps `git fetch`, `git branch`, and `git remote`. The iteration pattern exists in `cmd/cli/clone_cmd.go` and `cmd/cli/status_cmd.go` (loop over sources and repos). Needs new `git.MergedBranches()` and `git.DeleteBranch()` helpers, a new Cobra command, TUI integration (repo detail screen or dashboard action), and tests.

### W1: Config sync

- **Status:** radar
- **Priority:** P2
- **Size:** L
- **Concept:** Back up `gitbox.json` to a private repo on one of the user's configured providers, enabling multi-machine config portability. A `gitbox config sync` command that pushes config to a hidden `.gitbox-sync` repo and pulls it on new machines. The `credentials/` folder remains strictly machine-local for security.
- **Notes:** Requires `RepoCreator` interface (already implemented by all providers) to create the sync repo. Needs careful design around merge conflicts, credential exclusion, and first-time bootstrap on a new machine. The `config.Load`/`config.Save` path in `pkg/config/` is clean but the sync semantics (push vs pull vs merge) add complexity.

### W3: Dynamic workspaces

- **Status:** radar
- **Priority:** P2
- **Size:** M
- **Concept:** Generate VS Code `.code-workspace` files or tmuxinator profiles from a source's repos, so developers can open an entire source or organization at once.
- **Notes:** Config already has the source-to-repos mapping and the folder structure. This is mostly template generation — read sources, compute paths, write a file. No API calls needed. Low risk but also lower urgency than operational features. A `gitbox workspace generate` command.

### W4: Smart archiving

- **Status:** radar
- **Priority:** P3
- **Size:** L
- **Concept:** Detect when a repository has been archived upstream (e.g., made read-only on GitHub) and offer to free up local disk space by compressing the local clone into a `.tar.gz`.
- **Notes:** `provider.RemoteRepo.Archived` field already exists and is used in discovery UI (both CLI and GUI show "archived" badges). Needs a new `archived` state in the config `Repo` struct, compression logic, and careful UX for the destructive "compress and delete" action. The detection infrastructure is ready but the compress/restore lifecycle is complex.

### W5: Unified dashboard

- **Status:** radar
- **Priority:** P3
- **Size:** L
- **Concept:** Aggregate open PRs and review requests across all configured providers into a single notification center, turning gitbox into a daily developer launchpad.
- **Notes:** Requires new API endpoints on each provider implementation (PR listing, review request queries). The provider interface would need a new method (e.g., `PendingReviews`). This is the largest feature — significant API surface area across 5 providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket) with different PR/MR models.

## Shipped

_None yet._
