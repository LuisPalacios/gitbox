# Local gate

`scripts/gate.sh` verifies one exact commit on macOS, Linux and Windows in parallel, plus the sshd integration tests of remote mode, on my own machines, and replaces the GitHub PR checks as the merge gate. This page explains how it works and how to reproduce it in another repository.

## Why I gate locally

The PR checks waited on GitHub-hosted runners. The macOS job ran for about a minute but often sat 5–15 minutes in the queue, and a required-checks ruleset blocked every change on it. Self-hosted runners would remove the queue, but on a public repository a pull request from a fork could run code on them.

The local gate keeps the verification on hardware I control and never runs code I did not review. GitHub keeps an informational Linux check on pull requests, and the release workflow still builds and tests every platform before a release.

## How a run works

1. The gate refuses to run when tracked files have uncommitted changes, because it tests the committed `HEAD`, not the working tree.
2. It bundles `HEAD` and its tags with `git bundle`. The tags let `git describe` work, as the CI checkout's `fetch-depth: 0` does.
3. For each lane it copies the bundle and `scripts/gate-lane.sh` to the lane's host, under `~/.cache/gitbox-gate/in/`. The lane deletes both when it ends, whether it passes or fails, and removes files that an interrupted run left there more than a day ago.
4. All lanes run at once. Each lane fetches its own bundle into its own cache repository, so lanes that share a host never race on the same files, clones the commit into a clean directory under `~/.cache/gitbox-gate/`, and runs its checks.
5. The gate prints one summary line per lane and keeps each lane's full log under `${TMPDIR:-/tmp}/gitbox-gate/<sha>/<lane>.log`.
6. When every lane passes, the gate records a marker for the SHA.

A lane deletes its clone after a pass (`GATE_KEEP=1` keeps it) and keeps it after a failure for inspection. Clones older than a day are removed on the next run.

Each lane also sends every temporary file (Go build and test folders, npm, Node's compile cache) to its own folder, `~/.cache/gitbox-gate/t-<lane>-<sha>`, instead of the host's `/tmp`, and deletes that folder when it ends, whether it passes or fails. A small `/tmp`, such as a tmpfs, no longer fills up across runs. A folder that an interrupted run left behind is removed after a day.

## What each lane runs

Every lane but remote-it builds the embedded frontend with `npm ci && npm run build`, builds the GUI with the Wails CLI version that CI pins (`go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build`), checks that the bindings that build regenerated match the committed ones, and checks the built executable.

The build is the one a release makes. Each of those lanes compiles the three `gitbox-server` binaries with `scripts/build-server.sh`, runs the test that reads each one back against its pinned SHA-256, and builds the GUI with `-tags serverbin` so that it carries them. `scripts/check-gui-artifact.sh` then fails the lane unless `GitboxApp --version` and `GitboxApp --list-server-targets` report exactly the version and commit of the lane's stamp with all three targets, and unless the main executable is within its ceiling in `scripts/size-ceilings.txt`.

- **mac:** `HEALTH_FUZZ=1 ./scripts/health.sh` (every checker, including vet and staticcheck for linux, darwin and windows, the unit tests, bootstrap fixtures, Markdown lint and the harness mirror check), plus both coordination suites. Each fuzz target runs a fixed number of executions, not a time, so a pass that found nothing never fails at the end of its time (#409; see [Testing reference › Fuzz properties](testing-reference.md#fuzz-properties)). The build targets macOS 13 like the release.
- **linux:** `go vet ./...`, `go test -short ./...` and `bash scripts/test-bootstrap.sh`, then the build with `-tags webkit2_41`.
- **win:** both coordination suites, `go test -short ./...` and `bash scripts/test-bootstrap.sh`, then the build. The lane sets `core.autocrlf=false` and `core.longpaths=true` through `GIT_CONFIG_COUNT` for its own processes, so the host user's global Git config never changes.
- **remote-it:** `bash scripts/remote-it.sh` on the linux lane's host, at the same time as the linux lane. It builds the `gitbox-sshd` image from `test/sshd`, builds a `linux/amd64` server with the `e2e_sentinel` tag, and runs the `go test -race -tags sshd_integration ./pkg/remote/` battery in `GITBOX_SHARDS` test processes at once (default 3): the bootstrap scenarios of remote mode, each against a fresh container on an internal Docker network. The script lists the tests, deals them round-robin to the shards, fails when the shards do not cover the whole list, and prints each shard's log in order. It builds no GUI. See [Testing › Remote mode tests](testing.md#remote-mode-tests).

The real-host tests of macOS hosts (`scripts/remote-host-it.sh`, #245) are not a lane. They need a disposable account on a Mac with Remote Login, which a clean clone on a lane host cannot assume, and the remote-it lane already proves the protocol on every commit. I run them by hand when a change touches the host side of remote mode (the probe, the install and start scripts, the server or its doctor), and the pull request says so.

## Hosts and prerequisites

The gate reads the hosts from `.env`, the same file as `ship.sh` and `smoke.sh` (see [Multiplatform](multiplatform.md)). A lane runs locally when this machine matches its platform, and over SSH otherwise:

```bash
SSH_WIN_HOST="user@windows-host"     # win lane (or SSH_WIN_INTEL_HOST)
SSH_LINUX_HOST="user@linux-host"     # linux and remote-it lanes
SSH_MAC_ARM_HOST=""                  # mac lane; empty when the gate runs on that Mac
```

A linked worktree has no `.env` of its own, so the scripts fall back to the main clone's `.env`.

Each host needs key-based SSH without prompts (`BatchMode`), `git`, Go, Node with npm, `ssh-keygen` from OpenSSH 8.2 or later for the bootstrap fixtures, and `bash` (Git Bash on Windows). The mac and win lanes also need `uv`. The mac lane runs the health check, so it needs the health tools from the [developer guide](developer-guide.md#health-check) (`shellcheck`, `actionlint`, `pnpm`). The Linux host needs the GTK 3 and WebKitGTK 4.1 development packages (`libgtk-3-dev libwebkit2gtk-4.1-dev` on Debian or Ubuntu). Debian works as well as Ubuntu; only the release AppImage depends on an Ubuntu base, and the release still builds on GitHub. The remote-it lane needs Docker on the Linux host, with the SSH user in the `docker` group; Docker Desktop on macOS cannot reach container IPs from the host, so that lane runs on Linux only. Its first run pulls `debian:bookworm-slim` and the OpenSSH, fish and git packages.

The first run on a host downloads Go modules, npm packages and the Wails CLI. Later runs reuse those caches, which is where most of the speed comes from.

## Running the gate

```bash
./scripts/gate.sh                  # every lane
./scripts/gate.sh --only linux     # one lane (repeatable)
./scripts/gate.sh --skip win       # every lane but one (repeatable)
./scripts/gate.sh --only remote-it # the sshd integration tests only
./scripts/gate.sh --auto           # skip remote-it when the diff cannot affect it
./scripts/gate.sh --report         # publish the HEAD marker as a commit status
```

`--only` and `--skip` record a **partial** marker. I use them when a host is down, and say so in the PR.

`--auto` asks `scripts/gate-scope.sh` whether the diff of `HEAD` against `origin/main` can change the remote-it lane. When it cannot, the gate skips that lane, which leaves `win` as the slowest, and records a partial marker with `skipped=remote-it` and `auto_skip=remote-it`. `/merge-pr` accepts that marker without asking, after it re-runs `gate-scope.sh` itself. Any other partial marker still needs my explicit acceptance.

The lane is **needed** when the diff touches one of these, and **unaffected** otherwise:

- Anything under `pkg/remote/`, `test/sshd/` or `test/fakeforge/`.
- `go.mod`, `go.sum`, `scripts/remote-it.sh`, `scripts/gate.sh`, `scripts/gate-lane.sh`, `scripts/gate-scope.sh`, `scripts/stamp.sh` or `scripts/_common.sh`.
- A non-test Go source or embedded file of any package the lane compiles: the `gitboxserver,e2e_sentinel` build of `cmd/gui`, the fake forge and the `sshd_integration` build of `pkg/remote`, with their dependencies. `go list -deps` derives that set for linux/amd64, so a change in a shared package such as `pkg/git` runs the lane and a change in a Windows-only file does not.

Any doubt, such as a failed `git` or `go` call, counts as needed. `scripts/test-gate-scope.sh` covers the rule and runs in the health check's `selftest`.

`--report` posts the marker as a `local-gate` commit status on GitHub: `success` for a full pass, `pending` for a partial one. The commit must already be pushed. It selects the `gh` account from the owner in `remote.origin.url`.

## Markers, the pre-push hook and merges

The marker is a small text file at `$(git rev-parse --git-common-dir)/gitbox-gate/<sha>` with the status, the lanes and their durations. The git common dir is shared by every worktree of the clone, so a gate run in one worktree counts in the others. A failed lane removes the commit's marker, because the latest evidence wins; a passing `--only` or `--skip` re-run keeps an earlier full pass.

`.githooks/pre-push` enforces the gate:

- A push that updates `main` needs a `pass` marker for the pushed commit. A `partial` marker passes only with `GATE_ALLOW_PARTIAL=1`, and no marker blocks the push.
- A push to another branch skips the hook's light checks (`gofmt`, `go vet`, `go test -short`) when the commit already has a marker, and runs them otherwise.
- Tag pushes and branch deletions skip every check.

### Merging several PRs on one gate

`/merge-pr 14 12 13` takes the PRs in any order and merges them on one gate. It infers the order: a PR based on another PR's branch goes after that PR, and the rest go by ascending number, or with a conflicting PR moved first when that stacks cleanly. A PR based on neither `main` nor another PR of the request is left out, with the reason. The skill reports the order, cherry-picks each PR's own commits in a scratch worktree from `origin/main`, and runs one `gate.sh --auto` on the tip. It then retargets each stacked PR to `main`, force-pushes each PR branch at its stack position with a lease against its recorded head, and squash-merges them in order. Because PR k's branch then holds PRs 1 to k, each squash lands exactly the tree the gate tested.

Naming several PRs is the go-ahead for all of that: the order, the retargets, the force pushes, the gate status and the merges. The single gate stops counting when a PR is unusable, when no order avoids a conflict, when a lane fails on the tip, or when a PR head or `main` moved under it; the skill then falls back to one gate per PR for the rest. The final report gives the order, the gate, each merge commit and anything left out.

`/merge-pr` merges only when the PR's head commit has a `pass` marker or a successful `local-gate` status, and when the informational GitHub check has not failed. A rebase gives the head a new SHA, so the gate runs again after it. A squash merge lands the head commit's tree, and `/merge-pr` first makes sure the head contains the latest `main`.

Nothing on GitHub requires a check anymore. The ruleset that protects `main` from deletion and force pushes stays, and the gate depends on the hook being active (`git config core.hooksPath .githooks`). `git push --no-verify` bypasses it, so I never use it on `main`.

## What stays on GitHub

- `.github/workflows/pr.yml` runs the Linux `check` job on pull requests and on pushes to `main`: the health check, a Linux GUI build and a `--version` smoke on a clean `ubuntu-22.04` runner. It is informational, so it neither blocks nor delays anything.
- `.github/workflows/ci.yml` runs on version tags. Its `test` job runs vet, the short tests and both coordination suites on Linux and Windows before it builds and packages their release assets. `scripts/release-macos.sh` builds the macOS assets on my Mac.

## Replicating the gate in another repository

1. Copy `scripts/gate.sh`, `scripts/gate-lane.sh` and the helpers they use from `scripts/_common.sh` (`load_env`, `ssh_host_for`, `run_on`, `platform_label` and the output functions).
2. Edit the lane steps in `gate-lane.sh` for the project: its build, its test commands and its smoke check. Keep the clean clone, the per-SHA paths and the subshell in `step()`, because bash ignores `set -e` inside a function called from an `if`.
3. Create a `.env` with one SSH host per remote platform, and check `ssh -o BatchMode=yes <host> true` against each one.
4. Install the toolchain on each host, then run `./scripts/gate.sh` once to warm the caches.
5. Add the marker check to the repository's `pre-push` hook and activate it with `git config core.hooksPath .githooks`.
6. Make the merge procedure require the marker or the `local-gate` status, as `/merge-pr` does.
7. Remove GitHub's required status checks (rulesets or branch protection), keep the deletion and force-push protection, and keep the slow native jobs only where a release needs them.

## Troubleshooting

- **A lane fails right away with "host unreachable":** the gate checks SSH with a 10-second timeout and `BatchMode`. Wake the host or fix its key, then rerun, or use `--skip <lane>` and accept a partial marker.
- **"uncommitted changes to tracked files":** commit or stash them; the gate never tests the working tree.
- **A lane fails:** the summary shows the last lines of its log, and the full log path. The failed clone stays at `~/.cache/gitbox-gate/work-<lane>-<sha>` on that host.
- **Windows tests fail on locked files:** a handle left open (for example an SQLite connection that only commits through its context manager) keeps the file locked, and temporary-directory cleanup fails. Close the handle explicitly.
