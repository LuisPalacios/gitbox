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
#   ./scripts/health.sh -v              # print every finding, not just the first 15
#
# Checks: gofmt vet staticcheck modernize govulncheck deadcode test
#         svelte build npm-install npm-audit shellcheck actionlint markdown
#
# Go analyzers run through `go run` at pinned versions, so they need no
# install and give the same results everywhere. shellcheck, actionlint and
# markdownlint-cli2 must be on PATH (see docs/developer-guide.md).

set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 1

# Pinned tool versions. Bump deliberately; a new release can add findings.
STATICCHECK="honnef.co/go/tools/cmd/staticcheck@v0.8.1"
MODERNIZE="golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@v0.50.0"
GOVULNCHECK="golang.org/x/vuln/cmd/govulncheck@v1.8.0"
DEADCODE="golang.org/x/tools/cmd/deadcode@v0.50.0"

# Functions deadcode may report: kept on purpose, used only from tests or
# kept as part of a small public API. One regex per line.
DEADCODE_ALLOW='unreachable func: (PSDecode|resetLookupCachesForTest|Version\.String)$'

FRONTEND="cmd/gui/frontend"
MD_CONFIG=".claude/skills/fixing-markdown/.markdownlint-cli2.jsonc"

if [[ -t 1 ]]; then
    G='\033[0;32m' R='\033[0;31m' D='\033[0;90m' N='\033[0m'
else
    G='' R='' D='' N=''
fi

failed=()
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# check NAME COMMAND... — runs COMMAND, treats non-zero exit or any output
# on stdout/stderr as findings, and prints a one-line verdict.
check() {
    local name="$1"; shift
    local out="$tmp/$name.log"
    local rc=0
    "$@" >"$out.raw" 2>&1 || rc=$?
    # First runs of `go run` announce module downloads; that is not a finding.
    grep -v '^go: downloading ' "$out.raw" >"$out"
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

verbose=0
MAX_LINES=15
selected=()
for arg in "$@"; do
    case "$arg" in
        -v|--verbose) verbose=1 ;;
        *) selected+=("$arg") ;;
    esac
done

# ── Check bodies (quiet on success) ──────────────────────────────────

run_gofmt() {
    local files
    mapfile -t files < <(git ls-files '*.go')
    gofmt -s -l "${files[@]}"
}

run_vet()         { go vet ./...; }
run_staticcheck() { go run "$STATICCHECK" ./...; }
run_modernize()   { go run "$MODERNIZE" ./...; }
run_test() {
    go test -short ./... >"$tmp/test.full" 2>&1 && return 0
    grep -Ev '^(ok |\?) ' "$tmp/test.full"
    return 1
}

run_govulncheck() {
    local out
    out="$(go run "$GOVULNCHECK" ./... 2>&1)" || { echo "$out"; return 1; }
    grep -q '^No vulnerabilities found' <<<"$out" || echo "$out"
}

run_deadcode() {
    go run "$DEADCODE" ./... | grep -Ev "$DEADCODE_ALLOW"
    return 0
}

run_svelte() {
    local out
    out="$(cd "$FRONTEND" && npx svelte-check --tsconfig ./tsconfig.json \
        --output machine --threshold hint 2>&1)"
    grep -E ' (ERROR|WARNING|HINT) ' <<<"$out"
    grep -q ' COMPLETED .* 0 ERRORS 0 WARNINGS 0 HINTS' <<<"$out" || return 1
}

run_build() {
    # Rebuilds the gitignored dist/ (the only write); fails on any compiler
    # or bundler warning, e.g. the A11y diagnostics vite-plugin-svelte prints.
    (cd "$FRONTEND" && npm run build 2>&1) | sed 's/\x1b\[[0-9;]*m//g' \
        | grep -E '\[vite-plugin-svelte\] .*(A11y|Unused|[Ww]arn)|\(!\)|^npm warn'
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
    (cd "$FRONTEND" && npm audit --audit-level=low >"$tmp/npm-audit.full" 2>&1) && return 0
    grep -E '^[a-z@].*  |^Severity|vulnerabilit' "$tmp/npm-audit.full"
    return 1
}

run_shellcheck() {
    local files
    mapfile -t files < <(git ls-files '*.sh' .githooks/pre-push scripts/appimage/AppRun)
    shellcheck -f gcc -x "${files[@]}"
}

run_actionlint() { actionlint; }

run_markdown() {
    # The skill config auto-fixes; force read-only for a health check.
    sed 's/"fix": true/"fix": false/' "$MD_CONFIG" >"$tmp/.markdownlint-cli2.jsonc"
    markdownlint-cli2 --config "$tmp/.markdownlint-cli2.jsonc" \
        "**/*.md" "#**/node_modules" "#CONTINUITY*.md" 2>&1 \
        | grep -E ' error MD[0-9]+'
    return 0
}

# ── Main ─────────────────────────────────────────────────────────────

printf '\n━━ Health check ━━\n\n'

# cmd/gui embeds frontend/dist; vet, tests and analyzers need it built.
if [[ ! -d "$FRONTEND/dist" ]]; then
    printf '  %binfo%b  building the frontend (dist missing)\n' "$D" "$N"
    (cd "$FRONTEND" && npm ci --silent && npm run build --silent) >/dev/null
fi

want gofmt go       && check gofmt       run_gofmt
want vet go         && check vet         run_vet
want staticcheck go && check staticcheck run_staticcheck
want modernize go   && check modernize   run_modernize
want govulncheck go && check govulncheck run_govulncheck
want deadcode go    && check deadcode    run_deadcode
want test go        && check test        run_test
want svelte fe      && check svelte      run_svelte
want build fe       && check build       run_build
want npm-install fe && check npm-install run_npm_install
want npm-audit fe   && check npm-audit   run_npm_audit
want shellcheck x   && need shellcheck shellcheck        && check shellcheck run_shellcheck
want actionlint x   && need actionlint actionlint        && check actionlint run_actionlint
want markdown x     && need markdownlint-cli2 markdown   && check markdown   run_markdown

echo ""
if [[ ${#failed[@]} -eq 0 ]]; then
    printf '  %bok%b    all checks clean\n' "$G" "$N"
else
    printf '  %bFAIL%b  %d check(s) with findings: %s\n' "$R" "$N" "${#failed[@]}" "${failed[*]}"
    exit 1
fi
