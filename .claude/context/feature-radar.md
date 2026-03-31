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

### W8: Orphan repo discovery and adoption (`gitbox scan` / `gitbox adopt`)

- **Status:** planning
- **Priority:** P1
- **Size:** L
- **Concept:** Discover git repos under the gitbox parent folder that aren't tracked in `gitbox.json` and adopt them into the gitbox world. Two-phase approach: `gitbox scan` (read-only, reports orphans with proposed actions) then `gitbox adopt` (executes the plan). Handles all cases: account exists or needs creation (inferred from remote URL), repo in the right folder or needs relocation (always ask before moving). Sanitizes `.git/config` on adoption — sets credential helper, local identity, and remote URL format per account config. CLI-first (`gitbox scan` + `gitbox adopt`), with TUI/GUI integration later designed to feel seamless and transparent.
- **Notes:** Pending codebase exploration during planning phase.

### W7: Branch-aware status and UX

- **Status:** planning
- **Priority:** P2
- **Size:** M
- **Concept:** Make gitbox branch-aware across all views. Currently the dashboard (TUI and GUI) shows status relative to the current branch's upstream but never tells the user WHICH branch that is — a repo on `feature-x` that is clean looks identical to one on `main`. Local branches without upstream show alarming `~ No upstream` with no explanation. Detached HEAD is not handled. The fix is pure observability: show a branch badge in dashboard rows only when the current branch differs from default, soften "No upstream" labeling for local branches, handle detached HEAD explicitly, report skipped repos during bulk pull, and stop inflating account issue counts for normal feature-branch work. No new git write operations — gitbox is a fleet observer, not a repo-level client.
- **Notes:** Plan: (1) Add `Branch string` + `IsDefault bool` to `status.RepoStatus`, populate in `Check()` from existing `git.Status().Branch` + `git.DefaultBranch()`. (2) TUI: branch badge `[feature-x]` in dashboard rows when not on default, soften `formatStatusDetail` for NoUpstream on non-default branch to "local branch", fix `computeAccountStats` to not count feature-branch NoUpstream as issue, detached HEAD display in `screen_repos.go`. (3) GUI: add fields to `StatusResult` + `toStatusResult()` in `app.go`, propagate through `types.ts` → `stores.ts` → `App.svelte`, branch badge in repo rows, "Local branch" vs "No upstream" conditional, fix `accountStats` issue counting. (4) CLI: branch badge in `status_cmd.go`, improved skip reporting in `pull_cmd.go` with branch name. 4 commits: data layer → TUI+CLI → GUI → docs.


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

### W2: Bulk branch cleanup (`gitbox sweep`)

- **Status:** shipped
- **Shipped:** 2026-04-09
- **Priority:** P1
- **Size:** M
- **Concept:** Safely delete local branches that have been merged upstream or removed on the remote, across all repos in a source or across all sources. A `gitbox sweep` CLI command plus a TUI action and GUI panel.
- **Notes:** Implemented with `StaleBranches`/`DeleteBranch` git helpers, CLI `sweep` command with `--source`/`--repo`/`--dry-run` flags, TUI `s` keybinding, and GUI "Sweep branches" in kebab menu. Includes squash-merge detection via cherry/patch-id comparison.

### W6: Open in browser

- **Status:** shipped
- **Shipped:** 2026-04-08
- **Priority:** P1
- **Size:** S
- **Concept:** Add a context action to open a repository's remote page in the default browser. Parse the origin remote URL (SSH or HTTPS) to construct the web URL. Expose in GUI context menu, TUI keybinding, and CLI command.
- **Notes:** Web URL is `Account.URL + "/" + repoKey` (works for all providers). GUI: add `OpenInBrowser(url)` to `app.go` using same `open`/`xdg-open`/`cmd /c start` pattern as `OpenFileInEditor` (line 443). Wire through `bridge.ts`, add button to `App.svelte` kebab menu (line 1854) and compact view. TUI: add `b` keybinding in `screen_repos.go` (line 234). CLI: add `browse` Cobra command following `fetch_cmd.go` pattern. Tests: URL builder unit test in `helpers_test.go`, TUI keybinding test in `screen_repos_test.go`.
