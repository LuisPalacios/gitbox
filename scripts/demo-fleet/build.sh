#!/usr/bin/env bash
#
# build.sh — Create a fake gitbox fleet for README screenshots and demos.
#
# Builds, under $DEMO_ROOT (see env.sh):
#   - three accounts: forge-homelab (Forgejo), github-alexdev (personal
#     GitHub) and github-acme (corporate GitHub), all served by mock.py
#   - bare upstreams + real clones in chosen states (synced, behind,
#     ahead, dirty, not cloned) and one .code-workspace file
#   - gitbox.json, token files and an isolated global git config
# Nothing touches ~/.config/gitbox, ~/.gitconfig or the OS credential store.
#
# Usage:
#   ./scripts/demo-fleet/build.sh        # (re)build from scratch

set -euo pipefail
# shellcheck source=env.sh
source "$(dirname "$0")/env.sh"

mkdir -p "$DEMO_ROOT"
cd "$DEMO_ROOT"
rm -rf config fleet upstreams gitconfig gitignore_global
mkdir -p config/gitbox/credentials fleet upstreams

git config --global credential.helper manager
git config --global init.defaultBranch main
git config --global core.autocrlf false

# Pre-install gitbox's managed gitignore block (taken from the source of
# truth in pkg/gitignore) so the "global gitignore" banner stays quiet.
awk '/SentinelBegin *= *"/ { sub(/.*= *"/, ""); sub(/"$/, ""); b = $0 }
     /SentinelEnd *= *"/   { sub(/.*= *"/, ""); sub(/"$/, ""); e = $0 }
     /^const recommendedBody = `/ { f = 1; sub(/^const recommendedBody = `/, "") }
     f { if (sub(/`$/, "")) { body = body $0; f = 0 } else body = body $0 "\n" }
     END { print b; print body; print e }' \
    "$REPO_ROOT/pkg/gitignore/gitignore.go" > gitignore_global
git config --global core.excludesfile "$DEMO_ROOT/gitignore_global"

commit() { # commit <dir> <msg> — one commit with a file change, fixed identity
    echo "$2 $RANDOM" >> "$1/CHANGELOG.md"
    git -C "$1" add -A
    GIT_AUTHOR_NAME="Alex Demo" GIT_AUTHOR_EMAIL="alex@example.com" \
    GIT_COMMITTER_NAME="Alex Demo" GIT_COMMITTER_EMAIL="alex@example.com" \
        git -C "$1" commit -qm "$2"
}

# repo <accountKey> <port> <email> <org/repo> <state>
# state: synced | behind:N | ahead:N | dirty | notcloned
repo() {
    local acct=$1 port=$2 email=$3 full=$4 state=$5
    local up="upstreams/$acct/$full.git" seed="upstreams/_seed/$acct/$full"
    local dst="fleet/$acct/$full" n i
    mkdir -p "$(dirname "$up")" "$seed"
    git init -q "$seed"
    printf '# %s\n\nDemo repository.\n' "${full#*/}" > "$seed/README.md"
    commit "$seed" "Initial commit"
    commit "$seed" "Add docs"
    git clone -q --no-local --bare "$seed" "$up"
    [[ "$state" == notcloned ]] && return 0
    mkdir -p "$(dirname "$dst")"
    git clone -q --no-local "$up" "$dst"
    git -C "$dst" config user.name "Alex Demo"
    git -C "$dst" config user.email "$email"
    case $state in
        behind:*)
            n=${state#*:}
            for i in $(seq "$n"); do commit "$seed" "Upstream change $i"; done
            git -C "$seed" push -q "$DEMO_ROOT/$up" main
            git -C "$dst" fetch -q ;;
        ahead:*)
            n=${state#*:}
            for i in $(seq "$n"); do commit "$dst" "Local change $i"; done ;;
        dirty)
            echo "tweak" >> "$dst/README.md"
            echo "new" > "$dst/notes.txt" ;;
    esac
    # Point origin at the mock provider; mock.py serves fetches from $up.
    git -C "$dst" remote set-url origin "http://127.0.0.1:$port/$full.git"
}

repo forge-homelab  3001 demo@example.com  infra/ansible         behind:3
repo forge-homelab  3001 demo@example.com  infra/k8s-cluster     synced
repo forge-homelab  3001 demo@example.com  tools/dotfiles        synced
repo forge-homelab  3001 demo@example.com  docs/wiki             notcloned
repo github-alexdev 3002 alex@example.com  alexdev/blog          synced
repo github-alexdev 3002 alex@example.com  alexdev/cli-tools     synced
repo github-alexdev 3002 alex@example.com  alexdev/notes         synced
repo github-alexdev 3002 alex@example.com  alexdev/raytracer     ahead:1
repo github-acme    3003 alex@acme.example acme-corp/api-gateway synced
repo github-acme    3003 alex@acme.example acme-corp/web-portal  dirty
repo github-acme    3003 alex@acme.example acme-corp/payments    synced
rm -rf upstreams/_seed

# Dumb-HTTP metadata so mock.py can serve git fetches as static files.
for b in upstreams/*/*/*.git; do git --git-dir="$b" update-server-info; done

# A VS Code workspace for the Workspaces tab.
cat > fleet/github-acme/acme-platform.code-workspace <<'EOF'
{ "folders": [ { "path": "acme-corp/api-gateway" }, { "path": "acme-corp/web-portal" }, { "path": "acme-corp/payments" } ] }
EOF

# Fake tokens in gitbox's file-based token store.
for k in forge-homelab github-alexdev github-acme; do
    echo "demo-token-$k" > "config/gitbox/credentials/$k"
done

cat > config/gitbox/gitbox.json <<EOF
{
  "\$schema": "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/json/gitbox.schema.json",
  "version": 3,
  "global": {
    "folder": "$DEMO_ROOT/fleet",
    "language": "en",
    "view_mode": "${DEMO_VIEW_MODE:-full}",
    "credential_ssh": { "ssh_folder": "~/.ssh" },
    "credential_gcm": { "helper": "manager", "credential_store": "wincredman" },
    "credential_token": {}
  },
  "accounts": {
    "forge-homelab": {
      "provider": "forgejo", "url": "http://127.0.0.1:3001", "username": "demo",
      "name": "Alex Demo", "email": "demo@example.com", "default_credential_type": "token"
    },
    "github-alexdev": {
      "provider": "github", "url": "http://127.0.0.1:3002", "username": "alexdev",
      "name": "Alex Demo", "email": "alex@example.com", "default_credential_type": "token"
    },
    "github-acme": {
      "provider": "github", "url": "http://127.0.0.1:3003", "username": "alex-acme",
      "name": "Alex Demo", "email": "alex@acme.example", "default_credential_type": "token"
    }
  },
  "sources": {
    "forge-homelab": { "account": "forge-homelab", "repos": {
      "infra/ansible": {}, "infra/k8s-cluster": {}, "tools/dotfiles": {}, "docs/wiki": {} } },
    "github-alexdev": { "account": "github-alexdev", "repos": {
      "alexdev/blog": {}, "alexdev/cli-tools": {}, "alexdev/notes": {}, "alexdev/raytracer": {} } },
    "github-acme": { "account": "github-acme", "repos": {
      "acme-corp/api-gateway": {}, "acme-corp/web-portal": {}, "acme-corp/payments": {} } }
  }
}
EOF
echo "demo fleet ready in $DEMO_ROOT"
