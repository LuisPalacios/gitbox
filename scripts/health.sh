#!/usr/bin/env bash
#
# health.sh — Run every code checker the repo uses and report all findings.
#
# Read-only: no tool is allowed to rewrite files. Each check prints one
# summary line; the details of a failing check follow it. Exits non-zero
# when any check reports a finding or its tool is missing.
#
# Usage:
#   ./scripts/health.sh                 # every check
#   ./scripts/health.sh go svelte       # only the named checks ("go" = all Go checks)
#   HEALTH_FUZZ=1 ./scripts/health.sh fuzz # mutation passes (10s per target)
#   ./scripts/health.sh -v              # print every finding, not just the first 15
#   ./scripts/health.sh -h              # this help
#
# Checks: gofmt vet server staticcheck modernize govulncheck deadcode test
#         svelte frontend-test build funnel npm-install npm-audit shellcheck actionlint markdown
#         docs-parity harness bootstrap selftest fuzz (fuzz requires HEALTH_FUZZ=1)
#
# An unknown check or option, or fuzz without HEALTH_FUZZ=1, exits 2 before
# any check runs. A run in which no check ran fails.
#
# Go analyzers are fetched at pinned versions (go run / go install into the
# run's temp dir), so they need no install and give the same results
# everywhere; vet, staticcheck and modernize analyze linux, darwin and
# windows on every run. shellcheck, actionlint, pnpm and uv must be on PATH
# (see docs/developer-guide.md). Markdown tooling runs in pnpm's cache.

set -uo pipefail

# Resolved before the cd below, so -h finds this file from any directory.
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

cd "$(git rev-parse --show-toplevel)" || exit 1

# Every name the command line accepts: one per check, plus the "go" group.
# Each check has one "want" line in Main; the selftest check keeps both in step.
CHECKS="gofmt vet server staticcheck modernize govulncheck deadcode test fuzz
svelte frontend-test build funnel npm-install npm-audit
shellcheck bootstrap actionlint markdown docs-parity harness selftest"
CHECK_GROUPS="go"

# Pinned tool versions. Bump deliberately; a new release can add findings.
STATICCHECK="honnef.co/go/tools/cmd/staticcheck@v0.8.1"
MODERNIZE="golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@v0.50.0"
GOVULNCHECK="golang.org/x/vuln/cmd/govulncheck@v1.8.0"
DEADCODE="golang.org/x/tools/cmd/deadcode@v0.50.0"

# The blocking status and discovery wrappers remain public for existing callers.
# Functions deadcode may report: kept on purpose, used only from tests or
# kept as part of a small public API. One regex per line.
# I retain standalone sync/cache wrappers and the legacy raw-token writer
# for library callers and their compatibility tests. The config library also
# keeps its domain-only save and repair loader for callers without a file revision.
# AddRoot and PlaceRepo retain their boolean result for library callers.
# Library callers can configure one clone without a saved snapshot and
# reconfigure an account without installing a repair callback.
# SetRunnerForTest and SetDoerForTest are the test seams for git and HTTP:
# only tests call them, by design. The scripted runner and barriers live in
# a test fixture package outside either command's dependency tree, and so
# does the structural secret canary of internal/secretcheck (#242).
# Remote mode (#233): dialRemoteBackend connects a RemoteBackend over any
# pair of streams; only tests do that, the remote window connects through
# its session. Windows update helpers remain portable for fault-injection tests.
DEADCODE_ALLOW='pkg[/\\]credential[/\\]credential[.]go:[0-9]+:[0-9]+: unreachable func: StoreToken$
pkg[/\\]config[/\\](load|cas)[.]go:[0-9]+:[0-9]+: unreachable func: (LoadWithRepair|SaveIfUnchanged)$
pkg[/\\]config[/\\]roots[.]go:[0-9]+:[0-9]+: unreachable func: Config[.](AddRoot|PlaceRepo)$
pkg[/\\]config[/\\]testmode[.]go:[0-9]+:[0-9]+: unreachable func: SetupTestMode$
unreachable func: (PSDecode|resetLookupCachesForTest|Version\.String)$
pkg[/\\]update[/\\]apply_helper[.]go:[0-9]+:[0-9]+: unreachable func: (readUpdateBytes|verifiedWindowsExecutable)$
pkg[/\\]update[/\\]replace_windows_executable[.]go:[0-9]+:[0-9]+: unreachable func: (stageExecutable|removeRecordedWindowsBackup|replaceWindowsExecutableWith)$
pkg[/\\]git[/\\]sweep[.]go:[0-9]+:[0-9]+: unreachable func: DeleteStaleBranches$
pkg[/\\]git[/\\]find[.]go:[0-9]+:[0-9]+: unreachable func: FindNestedRepos$
pkg[/\\]adopt[/\\]adopt[.]go:[0-9]+:[0-9]+: unreachable func: FindOrphans$
pkg[/\\]ops[/\\]account[.]go:[0-9]+:[0-9]+: unreachable func: ReconfigureClones$
pkg[/\\]credential[/\\]repoconfig[.]go:[0-9]+:[0-9]+: unreachable func: ConfigureRepoCredential$
pkg[/\\]status[/\\]status[.]go:[0-9]+:[0-9]+: unreachable func: CheckAllContext$
pkg[/\\]terminals[/\\]sync[.]go:[0-9]+:[0-9]+: unreachable func: Sync$
pkg[/\\]workspace[/\\]workspace[.]go:[0-9]+:[0-9]+: unreachable func: RefreshCache$
pkg[/\\]update[/\\]platform_(unix|windows)[.]go:[0-9]+:[0-9]+: unreachable func: CleanupOldBinary$
unreachable func: (SetRunnerForTest|SetDoerForTest)$
pkg[/\\]git[/\\]testutil[/\\]runner[.]go:[0-9]+:[0-9]+: unreachable func: (RunnerFunc[.]Run|NewBarrier|Barrier[.]Arrive)$
internal[/\\]secretcheck[/\\]secretcheck[.]go:[0-9]+:[0-9]+: unreachable func: Checker[.]Inspect$
cmd[/\\]gui[/\\]backend_remote[.]go:[0-9]+:[0-9]+: unreachable func: dialRemoteBackend$
pkg[/\\]update[/\\]replace_windows_executable[.]go:[0-9]+:[0-9]+: unreachable func: (replaceWindowsExecutable|stageUpdateExecutable|writeUpdateState|cleanupWindowsBackup|regularFileHash)$'

# Advisories npm audit may report: no fixed version exists yet and the package
# never reaches the binary. A listed advisory that stops being reported fails
# the check, so the entry goes away with the fix. Space-separated GHSA ids.
NPM_AUDIT_ALLOW=""

FRONTEND="cmd/gui/frontend"

# The browser funnel (#240): lib/external.ts is the only way the frontend
# opens a link, and it names Wails' BrowserOpenURL only in a comment. A
# {@html} may render compile-time markup only, never a field that a server
# or the config supplies. One "file<TAB>fixed text" pair per allowed use,
# path relative to the frontend's src/.
FUNNEL_FILE="lib/external.ts"
HTML_ALLOW='lib/UIIcon.svelte	{@html ui[name] || '"''"'}
lib/BrandIcon.svelte	{@html entry.mono}
lib/CredentialResult.svelte	{@html sshKeyCopied ? '"'"'<svg'
MD_CONFIG="ai/skills/fixing-markdown/.markdownlint-cli2.jsonc"
MARKDOWNLINT="markdownlint-cli2@0.23.3"
HARNESS="ai/scripts"

if [[ -t 1 ]]; then
    G='\033[0;32m' R='\033[0;31m' D='\033[0;90m' N='\033[0m'
else
    G='' R='' D='' N=''
fi

failed=()
ran=0
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# check NAME COMMAND... — runs COMMAND, treats non-zero exit or any output
# on stdout/stderr as findings, and prints a one-line verdict.
check() {
    local name="$1"; shift
    local out="$tmp/$name.log"
    local rc=0
    ran=$((ran + 1))
    "$@" >"$out.raw" 2>&1 || rc=$?
    # Cold module caches announce downloads, also under per_os's [os] prefix;
    # that is not a finding.
    grep -Ev '^(\[[a-z]+\] )?go: downloading ' "$out.raw" >"$out"
    if [[ $rc -eq 0 && ! -s "$out" ]]; then
        printf '  %bok%b    %s\n' "$G" "$N" "$name"
    else
        local count
        count=$(wc -l <"$out")
        printf '  %bFAIL%b  %s %b(%d)%b\n' "$R" "$N" "$name" "$D" "$count" "$N"
        if [[ $verbose -eq 1 || $count -le $MAX_LINES ]]; then
            sed 's/^/        /' "$out"
        else
            head -n "$MAX_LINES" "$out" | sed 's/^/        /'
            printf '        %b… %d more (run with -v)%b\n' "$D" $((count - MAX_LINES)) "$N"
        fi
        failed+=("$name")
    fi
}

# need TOOL NAME — records NAME as failed when TOOL is not on PATH.
need() {
    if ! command -v "$1" >/dev/null 2>&1; then
        printf '  %bFAIL%b  %s %b(%s not installed)%b\n' "$R" "$N" "$2" "$D" "$1" "$N"
        ran=$((ran + 1))
        failed+=("$2")
        return 1
    fi
}

want() {
    [[ ${#selected[@]} -eq 0 ]] && return 0
    local s
    for s in "${selected[@]}"; do
        [[ "$s" == "$1" ]] && return 0
        [[ "$s" == "go" && "$2" == "go" ]] && return 0
    done
    return 1
}

# usage — prints this file's header comment, from "Usage:" to the blank
# line before the notes on Go analyzers.
usage() {
    sed -n '/^# Usage:/,/^# Go analyzers/p' "$SELF" | sed '$d' | sed -E 's/^# ?//'
}

# known NAME — true when NAME is a check or a group.
known() {
    local c
    for c in $CHECKS $CHECK_GROUPS; do
        [[ "$c" == "$1" ]] && return 0
    done
    return 1
}

verbose=0
MAX_LINES=15
selected=()
bad=()
for arg in "$@"; do
    case "$arg" in
        -h|--help) usage; exit 0 ;;
        -v|--verbose) verbose=1 ;;
        -*) bad+=("unknown option: $arg") ;;
        *) if known "$arg"; then selected+=("$arg"); else bad+=("unknown check: $arg"); fi ;;
    esac
done
for s in "${selected[@]}"; do
    if [[ "$s" == fuzz && ${HEALTH_FUZZ:-0} != 1 ]]; then
        bad+=("fuzz requires HEALTH_FUZZ=1")
    fi
done
if [[ ${#bad[@]} -gt 0 ]]; then
    {
        printf 'health.sh: %s\n' "${bad[@]}"
        printf 'Valid checks: %s\n' "${CHECKS//$'\n'/ }"
        printf 'Groups: %s. Options: -v, --verbose, -h, --help.\n' "$CHECK_GROUPS"
    } >&2
    exit 2
fi

# ── Check bodies (quiet on success) ──────────────────────────────────

run_gofmt() {
    local files
    mapfile -t files < <(git ls-files '*.go')
    gofmt -s -l "${files[@]}"
}

# Platform-specific files (wsl_*, platform_unix.go, …) only show their
# findings when analyzed for that OS, so vet, staticcheck and modernize run
# once per target and tag each finding with it.
TARGET_OSES=(linux darwin windows)

# tool PKG@VERSION — installs a pinned analyzer once per run (for the host,
# so it can run under any GOOS) and prints its path.
tool() {
    local name
    name="$(basename "${1%@*}")"
    if [[ ! -e "$tmp/bin/$name" && ! -e "$tmp/bin/$name.exe" ]]; then
        GOBIN="$tmp/bin" go install "$1" >/dev/null 2>&1 || { echo "cannot install $1"; return 1; }
    fi
    echo "$tmp/bin/$name"
}

# per_os CMD... — runs CMD for every target OS; findings are prefixed [os].
per_os() {
    local os rc=0
    for os in "${TARGET_OSES[@]}"; do
        CGO_ENABLED=0 GOOS="$os" "$@" >"$tmp/per_os.out" 2>&1 || rc=1
        sed "s/^/[$os] /" "$tmp/per_os.out"
    done
    return $rc
}

run_vet()         { per_os go vet ./...; }
# gitbox-server is cmd/gui built with -tags gitboxserver and CGO off (#233).
# The compile fails when Wails or a shell-only method leaks into that build;
# go vet cannot run there, because the tests call the Wails lifecycle hooks.
run_server_build() {
    # One binary per host a client can install it on: every OS on both
    # architectures.
    local arch rc=0
    for arch in amd64 arm64; do
        GOARCH="$arch" per_os go build -tags gitboxserver -o /dev/null ./cmd/gui \
            | sed "s/^/[$arch] /"
        [ "${PIPESTATUS[0]}" -eq 0 ] || rc=1
    done
    [ "$rc" -eq 0 ] || return 1
    if go list -tags gitboxserver -deps ./cmd/gui | grep -q wailsapp; then
        echo "the gitboxserver build of cmd/gui depends on Wails"
        return 1
    fi
}
run_staticcheck() { local bin; bin="$(tool "$STATICCHECK")" || return 1; per_os "$bin" ./...; }
run_modernize()   { local bin; bin="$(tool "$MODERNIZE")" || return 1; per_os "$bin" ./...; }
# failures_first FILE — prints a failed go test run without its ok lines,
# the lines that name a failure and its messages first (#346). A failing
# package prints all of its stderr, and expected noise there would otherwise
# fill the truncated summary and hide which test failed.
failures_first() {
    local signal='^(--- FAIL|FAIL|panic:|fatal error:|WARNING: DATA RACE)|^[[:space:]]+[^[:space:]]+_test\.go:[0-9]+:'
    grep -Ev '^(ok |\?) ' "$1" | grep -E "$signal"
    grep -Ev '^(ok |\?) ' "$1" | grep -Ev "$signal"
    return 0
}
run_test() {
    if ! go test -short ./... >"$tmp/test.full" 2>&1; then
        failures_first "$tmp/test.full"
        return 1
    fi
    # Windows runs the same schedules above without requiring a C toolchain.
    case "$(go env GOOS)" in
        darwin|linux)
            if ! go test -race -short ./cmd/gui/ ./pkg/config/ ./pkg/git/... >"$tmp/test.race" 2>&1; then
                failures_first "$tmp/test.race"
                return 1
            fi
            ;;
    esac
}

# A fuzz command accepts exactly one target. Keep successes quiet while
# retaining the complete reproducer and corpus path on failure.
run_fuzz() {
    local spec package target rc=0
    for spec in config:FuzzDecodeRoundTrip workspace:FuzzStripJSONC status:FuzzRepoPath rpc:FuzzDecodeFrame; do
        package="${spec%%:*}"
        target="${spec#*:}"
        if ! go test "./pkg/$package/" -run='^$' -fuzz="^$target$" -fuzztime=10s -parallel=2 \
            >"$tmp/fuzz-$target.full" 2>&1; then
            cat "$tmp/fuzz-$target.full"
            rc=1
        fi
    done
    return "$rc"
}

run_frontend_test() {
    (cd "$FRONTEND" && npm test) >"$tmp/frontend-test.full" 2>&1 && return 0
    cat "$tmp/frontend-test.full"
    return 1
}

run_funnel() {
    local src="$FRONTEND/src" line file text allowed rc=0
    # Every BrowserOpenURL outside the funnel file is a finding; inside it,
    # only comment lines may name it.
    grep -rn --include='*.ts' --include='*.js' --include='*.svelte' 'BrowserOpenURL' "$src" \
        | grep -v "^$src/$FUNNEL_FILE:" \
        | sed 's/$/  (open links with openExternal from lib\/external.ts)/'
    grep -n 'BrowserOpenURL' "$src/$FUNNEL_FILE" | grep -Ev '^[0-9]+:[[:space:]]*//' \
        | sed "s|^|$src/$FUNNEL_FILE:|; s|\$|  (the funnel must not import or call it)|"
    grep -q 'BrowserOpenURL' "$src/$FUNNEL_FILE" \
        || echo "$src/$FUNNEL_FILE: the comment that bans BrowserOpenURL is missing"
    while IFS= read -r line; do
        file="${line%%:*}"; file="${file#"$src"/}"
        text="${line#*:}"; text="${text#*:}"
        allowed=0
        while IFS=$'\t' read -r afile apattern; do
            [[ -n $afile && $file == "$afile" && $text == *"$apattern"* ]] && allowed=1
        done <<<"$HTML_ALLOW"
        if [[ $allowed -eq 0 ]]; then
            echo "$src/$file: {@html} renders a value that is not compile-time markup; render it as text"
            rc=1
        fi
    done < <(grep -rn --include='*.svelte' '{@html' "$src")
    return "$rc"
}

run_govulncheck() {
    local out
    out="$(go run "$GOVULNCHECK" ./... 2>&1)" || { echo "$out"; return 1; }
    grep -q '^No vulnerabilities found' <<<"$out" || echo "$out"
}

# The repo builds two programs from the same packages: GitboxApp and
# gitbox-server (-tags gitboxserver). A function is dead only when neither
# reaches it, so the check reports what both analyses agree on.
run_deadcode() {
    go run "$DEADCODE" ./... | sort >"$tmp/deadcode.app" || return 1
    go run "$DEADCODE" -tags gitboxserver ./... | sort >"$tmp/deadcode.server" || return 1
    comm -12 "$tmp/deadcode.app" "$tmp/deadcode.server" | grep -Ev "$DEADCODE_ALLOW"
    return 0
}

run_svelte() {
    local out
    # svelte-check 4 has no hint level; unused locals and parameters are
    # reported as errors through noUnusedLocals/noUnusedParameters instead.
    out="$(cd "$FRONTEND" && npx svelte-check --tsconfig ./tsconfig.json \
        --output machine 2>&1)"
    grep -E ' (ERROR|WARNING) ' <<<"$out"
    grep -q ' COMPLETED .* 0 ERRORS 0 WARNINGS' <<<"$out" || return 1
}

run_build() {
    # Rebuilds the gitignored dist/ (the only write); fails on any compiler
    # or bundler warning: any vite-plugin-svelte diagnostic that points at a
    # file:line:col (the Svelte compiler's warnings), rollup "(!)" notices,
    # and npm warnings.
    (cd "$FRONTEND" && npm run build 2>&1) | sed 's/\x1b\[[0-9;]*m//g' \
        | grep -E '\[vite-plugin-svelte\] [^ ]+\.svelte:[0-9]+:[0-9]+ |\(!\)|^npm warn'
    return 0
}

run_npm_install() {
    # Clean install in a scratch copy: surfaces npm's install-time warnings
    # (deprecated packages, install scripts not covered by allowScripts)
    # without touching the repo's node_modules.
    mkdir -p "$tmp/npm-install"
    cp "$FRONTEND/package.json" "$FRONTEND/package-lock.json" "$tmp/npm-install/"
    (cd "$tmp/npm-install" && npm ci --no-audit --no-fund 2>&1) | grep -E '^npm (warn|error)'
    return 0
}

run_npm_audit() {
    # npm audit exits non-zero on any advisory, allowed or not, so the verdict
    # comes from its report. No report (network, registry) is a failure too.
    (cd "$FRONTEND" && npm audit --json >"$tmp/npm-audit.json" 2>/dev/null)
    NPM_AUDIT_ALLOW="$NPM_AUDIT_ALLOW" node -e '
        const report = JSON.parse(require("fs").readFileSync(0, "utf8"));
        if (!report.vulnerabilities) throw new Error("npm audit gave no report");
        const allow = process.env.NPM_AUDIT_ALLOW.split(" ").filter(Boolean);
        const seen = new Set();
        for (const [pkg, vuln] of Object.entries(report.vulnerabilities)) {
            for (const via of vuln.via) {
                if (typeof via === "string" || via.severity === "info") continue;
                const id = via.url.split("/").pop();
                seen.add(id);
                if (!allow.includes(id)) console.log(pkg + "  " + id + "  " + via.severity + "  " + via.title);
            }
        }
        for (const id of allow) {
            if (!seen.has(id)) console.log(id + " is no longer reported: remove it from NPM_AUDIT_ALLOW");
        }
    ' <"$tmp/npm-audit.json"
}

run_shellcheck() {
    local files
    mapfile -t files < <(git ls-files '*.sh' .githooks/pre-push scripts/appimage/AppRun)
    shellcheck -f gcc -x -P SCRIPTDIR "${files[@]}"
}

run_selftest() {
    bash scripts/test-health-args.sh >"$tmp/selftest.full" 2>&1 \
        || { cat "$tmp/selftest.full"; return 1; }
}

run_bootstrap() {
    bash scripts/test-bootstrap.sh >"$tmp/bootstrap.full" 2>&1 \
        || { cat "$tmp/bootstrap.full"; return 1; }
}

run_actionlint() { actionlint; }

run_markdown() {
    # The shared config is read-only; never pass --fix in a health check.
    local out="$tmp/markdown.full" rc=0
    pnpm --silent dlx "$MARKDOWNLINT" --config "$MD_CONFIG" \
        "**/*.md" "#**/node_modules" "#CONTINUITY*.md" >"$out" 2>&1 || rc=$?
    # Keep lint findings concise, but retain diagnostics for tool/config failures.
    if ! grep -E ' error MD[0-9]+' "$out" && [[ $rc -ne 0 ]]; then
        cat "$out"
        printf 'markdownlint-cli2 exited with status %s\n' "$rc"
    fi
    return "$rc"
}

run_docs_parity() { uv run -q scripts/docs_parity.py; }

run_harness() {
    # Test shared maintenance tools, then verify both renders against ai/.
    local suite
    for suite in test_sync.py test_fixing_markdown.py test_coordinate_issues.py test_coordinate_roadmap.py; do
        uv run -q "$HARNESS/$suite" >"$tmp/harness-test.full" 2>&1 \
            || { cat "$tmp/harness-test.full"; return 1; }
    done
    uv run -q "$HARNESS/sync.py" verify --quiet
}

# ── Main ─────────────────────────────────────────────────────────────

printf '\n━━ Health check ━━\n\n'

# cmd/gui embeds frontend/dist; vet, tests and analyzers need it built.
if [[ ! -d "$FRONTEND/dist" ]] && { want vet go || want staticcheck go || want modernize go \
    || want govulncheck go || want deadcode go || want test go; }; then
    printf '  %binfo%b  building the frontend (dist missing)\n' "$D" "$N"
    (cd "$FRONTEND" && npm ci --silent && npm run build --silent) >/dev/null
fi

want gofmt go       && check gofmt       run_gofmt
want vet go         && check vet         run_vet
want server go      && check server      run_server_build
want staticcheck go && check staticcheck run_staticcheck
want modernize go   && check modernize   run_modernize
want govulncheck go && check govulncheck run_govulncheck
want deadcode go    && check deadcode    run_deadcode
want test go        && check test        run_test
[[ ${HEALTH_FUZZ:-0} == 1 ]] && want fuzz go && check fuzz run_fuzz
want svelte fe      && check svelte      run_svelte
want frontend-test fe && check frontend-test run_frontend_test
want build fe       && check build       run_build
want funnel fe      && check funnel      run_funnel
want npm-install fe && check npm-install run_npm_install
want npm-audit fe   && check npm-audit   run_npm_audit
want shellcheck x   && need shellcheck shellcheck        && check shellcheck run_shellcheck
want bootstrap x    && need ssh-keygen bootstrap        && check bootstrap run_bootstrap
want selftest x     && check selftest run_selftest
want actionlint x   && need actionlint actionlint        && check actionlint run_actionlint
want markdown x     && need pnpm markdown               && check markdown   run_markdown
want docs-parity x  && need uv docs-parity && check docs-parity run_docs_parity
want harness x      && need uv harness && need pnpm harness && need node harness \
                    && check harness run_harness

echo ""
if [[ $ran -eq 0 ]]; then
    printf '  %bFAIL%b  no check ran\n' "$R" "$N"
    exit 1
elif [[ ${#failed[@]} -eq 0 ]]; then
    printf '  %bok%b    all checks clean\n' "$G" "$N"
else
    printf '  %bFAIL%b  %d check(s) with findings: %s\n' "$R" "$N" "${#failed[@]}" "${failed[*]}"
    exit 1
fi
