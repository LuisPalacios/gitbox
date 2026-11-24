# Testing

This guide covers the test levels, the `test-gitbox.json` fixture, and the checklists I run before a PR and before a release. The inventory of tests and the harness internals live in [testing-reference.md](testing-reference.md). The health check is described in [developer-guide.md › Health check](developer-guide.md#health-check).

## Test levels

- **Package unit tests** — the Go tests under `pkg/`. They need no setup, no network and no provider, and isolate everything with `t.TempDir()` and `t.Setenv()`. The `pkg/ops` unit tests run against a temp `XDG_CONFIG_HOME`, a temp `GIT_CONFIG_GLOBAL` and a temp SSH folder, so nothing on the host changes.
- **GUI tests** — the Go tests under `cmd/gui/`. They call `App` methods directly, without a Wails window. They need the compiled frontend in `cmd/gui/frontend/dist`, because the package embeds it.
- **Frontend tests** — `npm test` runs pure Node tests and mounts Svelte components in jsdom. Both suites use `node --test` and need neither Wails nor credentials. The catalog test uses Go to read the codes declared in the repository.
- **Remote mode integration tests** — `go test -tags sshd_integration ./pkg/remote/` against a containerized `sshd`, run by `scripts/remote-it.sh` on a Linux host with Docker, and `go test -tags remote_host ./pkg/remote/` against a real host, run by `scripts/remote-host-it.sh`. See [Remote mode tests](#remote-mode-tests).
- **Scenario test** — `TestScenario_FullLifecycle` in `pkg/ops/scenario_test.go` drives the whole account lifecycle through `pkg/ops` against a real provider. It needs the fixture below, and `go test -short` skips it.

`go test ./pkg/...` runs without the frontend build. Everything else under Go needs `cmd/gui/frontend/dist`; build it once:

```bash
(cd cmd/gui/frontend && npm ci && npm run build)
```

`wails build` creates it too. See [developer-guide.md › Building from source](developer-guide.md#building-from-source).

## Work-unit workflow checks

I check changes to the work-unit skills with the harness gate:

```bash
./scripts/health.sh harness markdown shellcheck actionlint
```

The harness gate includes `ai/scripts/test_coordinate_issues.py` and `ai/scripts/test_coordinate_roadmap.py`. They use local fixtures for the shared registry, the worker queue at simulated capacities of 15 and four, and the optional work-unit checklist helper. I describe their coverage in [testing-reference.md](testing-reference.md#agent-harness-maintenance-tests).

I also review the workflow in a fresh Codex and Claude Code session: `wu-container` drafts populated HLR and acceptance criteria, `wu-tasks` maps tasks to those criteria, and `wu-coordinate-tasks` asks for a missing container number. I check that coordination excludes standalone issues and tasks from another parent, holds dependent tasks until integration and preserves the specified work unit on resume. I use read-only scenarios for these checks; testing discovery does not authorize publication. Changes limited to the harness skip Go tests and GUI builds; I still run the full health check before a PR.

## Startup benchmark

I build the native GUI and set `binary` to the absolute path of its executable. I create clean fleets of 50, 200 and 500 local clones in separate empty directories. Their account and remotes use loopback, their default view is **All repos**, and their configuration and Git settings live in the fixture. For the first size I run:

```bash
fleet=$(mktemp -d)
scripts/demo-fleet/benchmark.sh 50 "$fleet"
HOME="$fleet/home" XDG_CONFIG_HOME="$fleet/config" GIT_CONFIG_GLOBAL="$fleet/gitconfig" \
  GIT_CONFIG_NOSYSTEM=1 HTTP_PROXY=http://127.0.0.1:1 HTTPS_PROXY=http://127.0.0.1:1 \
  NO_PROXY=127.0.0.1 "$binary" --print-timing
```

I close the window after the single `startup-timing` JSON line appears. For each size and build I repeat the launch three times on the same machine with fresh copies of the fixture and record the median per stage. I compare the same native build options and cache conditions. For a revision predating the flag I instrument the same observation points before building it.

Values are monotonic milliseconds since the window’s Go application object was created. `decode` records the configuration load, `mount` the renderer mount, `snapshot` the detached snapshot, `firstRow` the first repository row inserted in the current view, `firstStatus` the first applied status, `fullStatus` the completed local sweep and `firstRemote` the first fleet network task scheduled. I retain absent fields as missing observations. The report contains timing only and stays on the machine.

## Fuzz tests

I keep small Go seed corpora under each package's `testdata/fuzz/<target>/`. Ordinary `go test` and `go test -short` execute these inputs without generating mutations. I run the seeds directly with:

```bash
go test ./pkg/config/ ./pkg/workspace/ ./pkg/status/ ./pkg/rpc/
```

I opt into mutation passes with `HEALTH_FUZZ=1`; the default health check keeps running only the seeds. Each target gets 10 seconds and two workers. The Linux PR health job enables this flag. I can run just these passes or include them in the full gate:

```bash
HEALTH_FUZZ=1 ./scripts/health.sh fuzz
HEALTH_FUZZ=1 ./scripts/health.sh
```

For a longer investigation I select one target explicitly:

```bash
go test ./pkg/config/ -run='^$' -fuzz='^FuzzDecodeRoundTrip$' -fuzztime=1m -parallel=2
```

`FuzzDecodeRoundTrip` checks migrated config intent and stable serialization of preserved extension fields, `FuzzStripJSONC` checks stripping properties, `FuzzRepoPath` checks path resolution within its documented safe input domain, and `FuzzDecodeFrame` checks that the frame reader of the server protocol never panics or reserves more than its cap; [testing-reference.md](testing-reference.md#fuzz-properties) describes the assertions. Go saves a failing input under the target's `testdata/fuzz/` directory and prints a reproduction command. I keep the minimized input as a regression seed after fixing its cause; I do not copy the fuzz cache into the repository.

## Remote mode tests

The bootstrap and the read-only window of [remote mode](remote-mode.md) have four layers of tests.

- **Unit tests** in `pkg/remote`, which every `go test -short` runs. They include `TestNoConfigOverrideInProduction` and a fake `ssh` that runs the real remote scripts with the local `sh` in a home folder of the test's own: install, hash checks, `noexec`, banners, uninstall and the refused roots. `TestProbeDarwin` feeds the probe the answer a Mac gave, and `TestHashToolSelection` runs the hash function with only some of `shasum`, `sha256sum` and `openssl` on the `PATH`. Windows skips the fake-ssh tests, which need a POSIX `sh`. `cmd/gui` adds `TestRemoteLifecycle`, which drives a remote window with a dialer that needs no ssh, and the read-only checks: `TestBackendContract` over every served method, `TestHostileServer`, `TestOpenExternal`, `TestRemoteWindowReadOnly`, `TestServerSavesThroughTheGuard` and `TestBackendContractMutations`, the token checks of #242, `TestTokenCanary` and `TestHostRefusesGCM`, and the SSH key check of #244, `TestPrivateKeyNeverOnWire`. The package's `TestMain` gives these tests a home folder of their own, so an SSH setup never writes into the real `~/.ssh`:

  ```bash
  go test -race -short ./cmd/gui/ -run 'TestBackendContract|TestHostileServer|TestOpenExternal|TestRemoteWindowReadOnly|TestServerSavesThroughTheGuard|TestBackendContractMutations|TestTokenCanary|TestHostRefusesGCM|TestPrivateKeyNeverOnWire'
  ```

- **The server build of `cmd/gui`.** `go test -race -short -tags gitboxserver ./cmd/gui/` runs the tests of the in-use marker and the rotating server log. Tests that need the window shell carry `//go:build !gitboxserver`, so this run compiles.
- **sshd integration tests** behind the `sshd_integration` tag, in `pkg/remote/integration_test.go` and the other `*_integration_test.go` files, with the container harness of `itdocker_test.go`:

  ```bash
  scripts/remote-it.sh                           # every scenario
  GITBOX_IT_RUN=TestFishLoginShell scripts/remote-it.sh
  GITBOX_IT_RUN='TestReadsLeaveTheDomainUntouched|TestMutationsOverSSH|TestCutSSHMidSave' scripts/remote-it.sh
  GITBOX_IT_RUN='TestCutSSHMidClone|TestLatency300ms' scripts/remote-it.sh
  GITBOX_IT_RUN=TestTokenCanary scripts/remote-it.sh
  GITBOX_IT_RUN='TestSSHAccountOnRemote|TestPrivateKeyNeverOnWire|TestAgentReport' scripts/remote-it.sh
  ```

  The script needs Docker on Linux. It builds the `gitbox-sshd` image from `test/sshd` (Debian slim with OpenSSH, fish, git, `iproute2` and `procps`, a `test` user whose key lives outside the home, and a 60 MB fixture repository that `git daemon` serves on `127.0.0.1:9418` when the container starts with `GIT_DAEMON=1`, plus a `git` user with `git-shell` that owns `org/small.git` when it starts with `GIT_HOST=1`, the git provider of the SSH account tests), a `linux/amd64` server with the `e2e_sentinel` tag, which appends a line to `$HOME/.gitbox-e2e/ran.log` on every start, and the fake forge of `test/fakeforge`, which `TestTokenCanary` copies into its container: Gitea's API and git over HTTP behind the tokens of a file. Each test starts its own container on an `--internal` Docker network, with no route out, and connects through the real `ssh` with a config file and a `known_hosts` of its own, filled from the container's host key. The fault tests start theirs with `--cap-add NET_ADMIN` and shape its network with `tc netem`. "Never executed" means the line count of `ran.log` did not change. The local gate runs it in its `remote-it` lane.
- **Real-host tests** behind the `remote_host` tag, with the harness of `pkg/remote/ithost_test.go` (#245). They run the tests that need no container of their own against a real host, such as a Mac with Remote Login: the bootstrap battery, `TestTokenCanary` and `TestRemoteTerminalOverSSH` in the host's login shell. The other tests skip.

  ```bash
  scripts/remote-host-it.sh                      # against SSH_REMOTE_MAC_HOST from .env
  GITBOX_IT_RUN=TestTokenCanary scripts/remote-host-it.sh my-mac
  ```

  The script asks the host for its platform with `uname -sm`, builds the sentinel server and the fake forge for it, and runs `go test -race -tags remote_host ./pkg/remote/`. The tests write and delete files in the home of the remote account, so it must be a disposable account of its own. The harness refuses to start without its `~/.gitbox-it-host` marker, or when the alias reaches the account the tests run from. It resolves the alias with `ssh -G`, then reads only its key and a `known_hosts` file (`GITBOX_REMOTE_TEST_KNOWN_HOSTS`, from `SSH_REMOTE_MAC_KNOWN_HOSTS` in `.env`), and writes an ssh config of its own with `IdentityAgent none`. Each test gets a folder of its own under `~/.gitbox-it/` as its `server_dir`, and the cleanup removes it with the gitbox config, the clone root and the rc file the test wrote. A real host keeps its network, so `TestInstallNoInternet` checks the route only in the sshd lane. On a Mac I set up the account once:

  ```bash
  sudo sysadminctl -addUser gitbox-it -fullName "Gitbox IT" -shell /bin/zsh -home /Users/gitbox-it -password -
  sudo createhomedir -c -u gitbox-it
  sudo dscl . -create /Users/gitbox-it IsHidden 1
  sudo dseditgroup -o edit -a gitbox-it -t user com.apple.access_ssh
  mkdir -p ~/.cache/gitbox-it && ssh-keygen -q -t ed25519 -N "" -f ~/.cache/gitbox-it/id_ed25519
  printf 'localhost %s\n' "$(cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)" > ~/.cache/gitbox-it/known_hosts
  sudo -H -u gitbox-it sh -c 'umask 077; mkdir -p ~/.ssh; cat > ~/.ssh/authorized_keys; touch ~/.gitbox-it-host' < ~/.cache/gitbox-it/id_ed25519.pub
  ```

  Then a `Host` alias for it in `~/.ssh/config`, before any `Host *` block, and the alias in `.env`:

  ```text
  Host gitbox-it-mac
    HostName localhost
    User gitbox-it
    IdentityFile ~/.cache/gitbox-it/id_ed25519
    IdentityAgent none
  ```

  The local gate does not run this lane: see [Local gate](local-gate.md#what-each-lane-runs).

A remote smoke drives the client methods against a real host from my machine, with a temporary config: add, connect, log, doctor, disconnect and uninstall. It needs a build that carries its server and a scratch `server_dir`. `GITBOX_SMOKE_OS` names the host's system (`linux` by default), and `./scripts/smoke.sh remote-mac` runs it against `SSH_REMOTE_MAC_HOST`:

```bash
eval "$(scripts/stamp.sh)" && scripts/build-server.sh
pkg=github.com/LuisPalacios/gitbox/cmd/gui
GITBOX_SMOKE_ALIAS=my-server GITBOX_SMOKE_SERVER_DIR='~/.cache/gitbox-smoke' \
  go test -tags serverbin,remote_smoke -run TestRemoteSmoke -v \
  -ldflags "-X $pkg.version=$VERSION -X $pkg.commit=$COMMIT" ./cmd/gui/
```

## Pre-push hook

`.githooks/pre-push` refuses to update `main` unless the pushed commit passed the [local gate](local-gate.md). For other branches it runs a `gofmt -s -l` check, `go vet ./...` and `go test -short ./...` unless the commit already passed the gate, and builds the frontend first when `cmd/gui/frontend/dist` is missing. Tag-only pushes skip it. I activate it once per clone:

```bash
git config core.hooksPath .githooks
```

## The test fixture

The scenario test and `GitboxApp --test-mode` read `test-gitbox.json` at the repo root: a normal `gitbox.json` whose accounts can carry a `_test` key with a token. Unit tests don't need it. Without it, the scenario test fails with instructions to create the file or run `go test -short`.

### Setting it up

I copy the template and fill in real accounts:

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

To test first launch, I use a fixture with no accounts, sources or mirrors and add `"_test": {"firstRun": true}` at its top level. I keep a valid `global.roots` entry and set `global.language` to `en` or `es`. The wizard suggests the temporary test root.

The file is gitignored because it holds real secrets. Never commit it.

Each account that the tests may call gets a `_test` key with a personal access token:

```json
"gb-github-personal": {
  "provider": "github",
  "_test": {
    "token": "ghp_xxxxxxxxxxxxxxxxxxxx"
  }
}
```

Accounts without `_test` are display-only: they show in `--test-mode`, but no API call goes to them. The scenario test uses the first source, in key order, that has repos and whose account has a token. The token needs the same permissions as a gitbox token credential; see [credentials.md › How to create a PAT](credentials.md#how-to-create-a-pat).

### GCM accounts

GCM credentials come from an interactive browser sign-in, so there is no token to store. I leave `_test` off GCM accounts: the scenario test skips them, and they stay available for manual checks in `--test-mode`.

### Mirrors (optional)

To check mirrors by hand in `--test-mode`, I add a `mirrors` section with a real account pair:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "gb-github-personal",
    "account_dst": "gb-home-personal",
    "repos": {}
  }
}
```

Both accounts need a `_test` token, and the destination token needs write access, because setting up a mirror creates the target repo. The scenario test doesn't need this section: its mirror step only creates and deletes a mirror group in the config.

### Verifying the tokens and SSH keys

Before the first scenario run, I check the fixture:

```bash
./scripts/setup-credentials.sh
```

It verifies each `_test` token against its provider, generates per-host SSH key pairs, adds their `Host` entries to the fixture's SSH config and tests the SSH connections. It is idempotent. With no argument it runs on this machine and on every remote configured in `.env`; a target such as `linux` limits it. When every line shows a green `ok`, the fixture is ready. See [multiplatform.md](multiplatform.md) for the remote setup.

### Safety

- The harness and `--test-mode` replace `global.roots` with one temp directory, so every clone and config file lands there and is deleted afterwards.
- `credential_ssh.ssh_folder` comes from the fixture, so the tests find the generated SSH keys. Point it at an isolated folder such as `~/.gitbox-test/ssh`.
- The run stops at once when `ssh_folder` is `~/.ssh` or a root in `global.roots` is the gitbox config directory (`~/.config/gitbox`).

## Running the tests

I run them in this order, from fastest to slowest.

1. Unit tests, no fixture needed:

   ```bash
   go test -short ./...
   ```

2. Frontend type check and frontend tests:

   ```bash
   cd cmd/gui/frontend
   npm run check
   npm test
   ```

3. The scenario test, against the first fixture account with a token:

   ```bash
   go test -v -run TestScenario ./pkg/ops/
   ```

4. Everything, verbose and uncached:

   ```bash
   go test -v -p 1 -count=1 ./...
   ```

5. The app against the fixture instead of my real config:

   ```bash
   GitboxApp --test-mode
   ```

   `--test-mode` finds `test-gitbox.json` by walking up from the current directory, writes a throwaway config to a temp directory, overrides `global.roots`, applies the same safety checks and injects the fixture tokens as environment variables. My real `~/.config/gitbox/` and clones stay untouched, and the temp directory goes away when the app exits. `./scripts/test-commands.sh` prints the launch command for each platform.

### Frontend component tests

I run `npm run test:dom` from `cmd/gui/frontend` to run just the component suite. The harness mounts the real dashboard with Svelte Testing Library and supplies a programmable Wails bridge and event bus. The fixtures use synthetic repositories, accounts and tokens.

The suite checks Needs attention, startup event ordering, late completion after I stop tracking a project, credential token clearing and focus restoration, and view, detail selection and scroll across experience changes. At 600 px of content width, it estimates the untruncated width of Simple row labels in English and Spanish with a monospace fallback, using the row's CSS spacing and controls. I keep the packaged Wails smoke on each OS and use its native webview for actual rendering and focus checks.

### Focused regression runs

- **Startup snapshot** — `go test ./cmd/gui/ -run TestStartupSnapshot` checks nesting before clones exist and detached snapshot data. `go test ./cmd/gui/ -run '^$' -bench BenchmarkStartupSnapshot -benchmem` measures snapshot construction, which is backend work, not visible startup time.
- **Refresh and credential maintenance** — `go test -race ./cmd/gui/ ./pkg/credential/ ./pkg/heal/ ./pkg/git/` covers refresh coordination, cancellation, stale results, upstream probes and pre-fetch maintenance.
- **Failure schedules** — `go test -race -short ./cmd/gui/ ./pkg/config/ ./pkg/git/...` checks overlapping config changes, partial save failures and stalled Git credential helpers in isolated fixtures. The health check runs this race pass on macOS and Linux.
- **Diagnostics export** — `go test -short ./pkg/diag/ ./cmd/gui/ -run "Test(Collect|Activity|Redact|ExportDiagnostics|Diagnostics_)"` checks the allowlist, privacy canaries, retention and saving the reviewed bytes. On the native desktop I use a synthetic fixture, open **Settings → System check → Run → Export diagnostics**, save the file to a temporary folder, and compare it byte for byte with the preview.

For visible startup time I measure on the native desktop: process launch to window, window to the first configured rows with container nesting, and time to complete status. I record the platform, clone count, container layout and number of runs. A browser navigation against a running `wails dev` backend measures a different path, and SSH timings can't show WebView2 row paint, so a Windows startup target stays open until I measure it on an interactive Windows desktop.

### Discovery dialog layout

I check **Find projects** in native WebKit with synthetic accounts and repositories. I cover loading, error, empty, one-result and long-list states, with results arriving both during and after the opening transition. I reopen the dialog, filter the list, resize between short and tall windows, and check keyboard access, selection and **Add & Pull**. I keep provider and clone calls isolated from production credentials and repositories.

With enough viewport space, I expect the count, filter, **Select all** and one repository to fit without scrolling. Long lists must scroll while the header and action buttons remain accessible. When shared modal styles change, I also check account, credential, confirmation and mirror discovery dialogs.

I let the opening transition and result update finish before reading element sizes. Intermediate layout reads can force WebKit to recalculate sizes and hide the regression from #221. I compare the body's `clientHeight` and `scrollHeight`, inspect the rendered result, and check the row and footer bounds; searching CSS text does not test layout.

### Credential storage and permission audit

I run permission checks against a temporary `XDG_CONFIG_HOME` with synthetic accounts and dummy credential files. I never relax production credential permissions or copy real credential files into an exposure fixture.

- I open **Settings → System check → Run** and confirm that credential findings show the affected path, problem and suggested action without token contents. A clean fixture passes; an unreadable or unsupported permission state reports a finding rather than success.
- In the fixture only, I give a dummy file mode `0644` on Unix or a Windows ACL grant outside owner/SYSTEM/Administrators. After restarting, the exposure warning appears in the full and Compact dashboards and opens System check. The audit leaves permissions unchanged. After repairing the fixture permissions, **Re-check** clears the finding and warning.
- I save and rename a dedicated test account, then confirm its credential still works after restarting. The isolated automated checks verify that both actions write git-credential-store format directly, including accounts without clones, and keep reading legacy raw-token files. I never print a real token or credential-file contents in test output.

### Clone identity proof

On macOS or Linux I run `scripts/demo-fleet/identity-proof.sh` from the repository root to generate ten isolated clones. I launch the built binary from the printed fixture directory with its own home and Git settings:

```bash
binary=/absolute/path/to/GitboxApp
fixture=$(scripts/demo-fleet/identity-proof.sh)
(cd "$fixture" && env HOME="$fixture/home" XDG_CONFIG_HOME="$fixture/xdg"   GIT_CONFIG_GLOBAL="$fixture/global.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_COUNT=0   "$binary" --test-mode)
```

I open **Why this account?** for a baseline clone and expect a warning because the fixture endpoints are offline. I check that **wrong-host**, **include-helper** and **scoped-helper** show **Unsafe**, and that **other-host-helper** and **scoped-reset** show a warning without a helper conflict. I compare the clone config bytes before and after opening the panel and check that its synthetic secrets stay hidden. I use **Repair** on **wrong-host** and check that the proof reports the expected fetch URL. In Simple I expect one account sentence.

## Native tests

The [local gate](local-gate.md) runs the native tests on every change: its macOS lane runs the full health check and the coordination suites, its Linux lane runs `go vet` and the short tests, and its Windows lane runs the coordination suites and the short tests. Each lane also builds the GUI and runs the `--version` smoke. The Windows lane sets `core.autocrlf=false` and `core.longpaths=true` for its own processes only, so my global Git config does not change.

The release workflow repeats vet, the short tests and both coordination suites on hosted Linux, macOS and Windows runners before it builds the release. `pr.yml` keeps only the informational Linux `check` job.

Neither the gate nor the release jobs verify desktop rendering or packaged launches. I keep the packaged-launch smoke in `scripts/smoke.sh` and test interactive GUI behavior in a native desktop session.

## Pre-PR checklist

I run these before every push or PR. `/test-plan` in Claude Code or `$test-plan` in Codex automates them.

- [ ] `./scripts/gate.sh` passes on the final commit on macOS, Linux and Windows (see [Local gate](local-gate.md)); it covers the next four items on every platform
- [ ] `./scripts/health.sh` passes: it includes `go vet`, `go test -short`, `svelte-check` and `npm test` (see [Health check](developer-guide.md#health-check))
- [ ] The focused tests for the changed area pass (`go test ./pkg/<area>/ ./cmd/gui/`)
- [ ] `cd cmd/gui && wails build` succeeds after copying the icons (see [Building from source](developer-guide.md#building-from-source))
- [ ] `cmd/gui/build/bin/GitboxApp --version` prints `GitboxApp <version> (<sha>)`
- [ ] `./scripts/ship.sh` and `./scripts/smoke.sh` pass on every remote in `.env` (skip when `.env` has none)

For documentation changes I also run `./scripts/health.sh docs-parity` on its own to check the English and Spanish structure. When I change the checker itself, I run `uv run scripts/test_docs_parity.py` to verify its diagnostics on isolated fixtures.

When the change touches one of these areas, I also check it on the dev machine:

- [ ] Config: GitboxApp loads the config, and the change survives a restart
- [ ] Credentials: the account's health words in the sidebar and on its card match the real credential state
- [ ] Disk reconcile or containers: a clone dropped under a root is adopted or listed in **Unmatched clones** on the next window focus
- [ ] Edit a different repository during a scan. Its edit and the new adoption both survive a restart
- [ ] GUI: the changed screen renders in Simple and in Advanced, in light and dark themes
- [ ] Windows: no console window flashes when the changed action runs

## Full release checklist

I run this before tagging a release. It combines automated and interactive steps across every platform.

### Automated

- [ ] `./scripts/health.sh` passes
- [ ] `go test ./...` passes with `test-gitbox.json` in place, including `TestScenario_FullLifecycle`
- [ ] `./scripts/ship.sh` builds and stages the GUI on every remote
- [ ] `./scripts/smoke.sh all` prints a version on every platform
- [ ] `./scripts/sign-release.sh --check` passes, so the release key is in the SSH agent
- [ ] `create-dmg` is installed on the Mac that runs `./scripts/release-macos.sh`

### GUI verification (interactive, all platforms)

I launch GitboxApp on each platform with the commands `./scripts/run-commands.sh` (real config) or `./scripts/test-commands.sh` (fixture) prints.

- [ ] The app opens; on Windows without a console flash
- [ ] First run: with `XDG_CONFIG_HOME` pointing at an empty folder, the guided setup goes folder → connect (browser sign-in, **Use a token instead**, **Change details** keeps the chosen folder) → pick projects → **Download**; the main screen opens in Simple and the picked repos clone
- [ ] Adding accounts: in Simple, **Connect an account** runs the same connect and pick steps; in Advanced, **Add account** opens the full form
- [ ] Sidebar: Needs attention, All repos, each account and Mirrors open their view; the GitBox logo and ⌘B / Ctrl+B fold it to the icon rail and back; below 780 px the rail shows and the logo opens the sidebar over the content; the view and the rail survive a restart
- [ ] Account groups: in All repos and Needs attention each account sits in a light card with a filled ▾ caret and its name in uppercase in the provider's colour; clicking the header folds it, and its counts stay visible while folded
- [ ] Account card: the account view's card shows the health mark and name, the health words with their fix button, and the counts with **Find** and **+ New** in both modes; a broken credential tints it red and puts its fix at the top of Needs attention
- [ ] Account edit and rename keep the clones, the view and the credential
- [ ] Credential setup works for token, GCM and SSH
- [ ] Discovery: **Find** → select repos → **Add & Pull** clones them into the account's root
- [ ] Clone, **Pull all** and **Fetch all** update the status dots; the activity strip shows the scope while they run, and a result card sums them up
- [ ] In a test repo whose checkout leaves an untracked file, I download the repo in Simple and check that the row goes from **Checking…** to **Changes on this computer**. I repeat in Spanish.
- [ ] The repo detail panel shows branch and ahead/behind in Advanced, and changed files in both modes; a clone with incoming updates opens a scrollable list of every commit with author and message, keeps local-file details, and refreshes after an update
- [ ] Incoming preview: a long list fits a small window; switching clones or closing and reopening while loading never shows another clone's results; opening the panel alone does not fetch or pull
- [ ] In Simple I preview **Update all**, cancel, then reopen the plan, edit a clone listed for update and confirm. The result counts the skipped project and my edits stay intact. In Advanced I turn the plan on and check that it lists the repositories and their actions.
- [ ] Clone roots: in Settings I add a root, make it default and remove it; with two roots, a row's **Clone** asks which root and lands there, and **Pull all** doesn't ask
- [ ] Disk reconcile: a recognised clone dropped under a root is adopted in place on the next window focus; an unrecognised one appears in **Unmatched clones** under **Found on disk**, where **Adopt**, **Ignore**, **Show ignored** and **Stop ignoring** work; a moved clone is re-linked; repos under an unplugged root read **Root offline**
- [ ] In a disposable clone awaiting adoption that needs an identity repair, create an empty `.git/config.lock`. After adoption, verify that **Needs attention** shows the warning and the clone stays in its account. Remove the test lock and click **Retry repair**. Check that the warning clears and your working files keep their contents.
- [ ] Containers: a clone with nested clones shows an outlined ▾ chevron in place of its status dot; the chevron folds everything below, the name opens available details or otherwise folds the children, and folds survive a restart
- [ ] Workspaces: a `.code-workspace` dropped at a clone's root makes it a container on the next window focus, with its nested clones below and missing listed folders as dimmed **not cloned** rows; a second file asks which one to use; the editor button opens the workspace file in VS Code
- [ ] Simple / Advanced: the top-bar switch (grey Simple, red Advanced) changes mode at once; Simple rows show the status dot, name, status sentence, **Update** or **Download** when needed, the AI, editor and terminal icons and ⋯; Advanced rows keep every detail and the per-row fetch button; Simple hides Mirrors, delete mode and the compact view. Check native rendering at narrow and wide content widths in light and dark themes.
- [ ] The top-bar rings show an icon, an attention badge and a hover tooltip, and clicking a pending ring opens Needs attention (the mirror ring opens Mirrors)
- [ ] Mirrors view shows its groups with **Discover** and **Check all** in the header; a mirror chip opens into its card and folds back on an outside click or Escape
- [ ] Settings: System check and the Terminals Manager open; the theme follows the OS, light or dark, and survives a restart
- [ ] Compact view and back to the full view

### Credential flows (interactive, per platform)

- [ ] Windows: token, GCM (browser), SSH (key generation)
- [ ] macOS: token, GCM (browser), SSH
- [ ] Linux: token, GCM (browser on a desktop session), SSH

### Update and upgrade (at least one platform)

- [ ] With a newer signed release published, the update pill appears in the footer; clicking it verifies the signature and checksum, applies the update, and the app restarts on the new version
- [ ] On Windows, I update a test installation under `Program Files` from a signed test release. The User Account Control prompt names `GitboxApp.exe`, the new version opens with my user's permissions, and the update creates no `.cmd` file
- [ ] The Linux AppImage pill opens the release page instead of updating
- [ ] A v1 install upgrades to v2 and keeps its `gitbox.json`

The fail-closed paths (missing or invalid signature, missing checksum) are covered by the `pkg/update` tests, not by hand.

### Platform-specific notes

| Area | Windows | macOS | Linux |
| --- | --- | --- | --- |
| GCM store | Windows Credential Manager | macOS Keychain | `secretservice` (default) or `gpg` |
| SSH agent | OpenSSH agent or Pageant | System ssh-agent | System ssh-agent |
| Git binary | `git.exe` (Git for Windows) | `/usr/bin/git` or Homebrew | System `git` |
| Browser open (GCM) | Always works | Works even via SSH | Needs `DISPLAY` or `WAYLAND_DISPLAY` |
| GUI framework | Wails + WebView2 | Wails + WebKit | Wails + WebKitGTK |
| Config path | `~/.config/gitbox/` | `~/.config/gitbox/` | `~/.config/gitbox/` |

`XDG_CONFIG_HOME` overrides the config path on every platform.

## Adding new checks

When I add a feature, I update the test docs:

1. Add the automated test and list it in the [test inventory](testing-reference.md#test-inventory).
2. Add a manual step to the matching checklist section above.
3. If the feature behaves differently per platform, add a row or note to the platform table.

## Agent harness checks

I verify changes limited to agent guidance and maintenance scripts with `./scripts/health.sh harness markdown shellcheck actionlint`. The harness gate runs the renderer, Markdown formatter, coordination registry and roadmap editing regression suites, then checks both generated trees against `ai/`. These changes do not need application tests or a GUI build. I still run the full health check before a PR and verify discovery in a fresh session of each tool.
