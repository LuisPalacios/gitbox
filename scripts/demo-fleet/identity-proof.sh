#!/usr/bin/env bash
# I prepare ten disposable clones for a native GitboxApp --test-mode check.
set -euo pipefail

fixture=$(mktemp -d "${TMPDIR:-/tmp}/gitbox-identity-proof.XXXXXX")
trap 'rm -rf "$fixture"' EXIT
fixture=$(cd "$fixture" && pwd -P)
mkdir -p "$fixture/home" "$fixture/xdg"
export GIT_CONFIG_GLOBAL="$fixture/global.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_COUNT=0
git config --file "$GIT_CONFIG_GLOBAL" credential.helper manager

personal=(baseline project-author wrong-host wrong-email rewritten-origin)
team=(token-baseline include-helper other-host-helper scoped-helper scoped-reset)
seed_clone() {
  local account=$1 name=$2 user=$3 author=$4 host=$5
  local path="$fixture/clones/$account/$name"
  local origin="https://$user@$host/team/$name.git"
  mkdir -p "$path"
  git -C "$path" init -q -b main
  git -C "$path" remote add origin "$origin"
  git -C "$path" config user.name "$author"
  git -C "$path" config user.email "$user@example.invalid"
  git -C "$path" config --add credential.helper ''
  if [[ $account == team ]]; then
    # Git's store helper takes a shell-quoted, forward-slashed path.
    local store="$fixture/xdg/gitbox/credentials/proof-team"
    git -C "$path" config --add credential.helper "store --file '${store//\'/\'\\\'\'}'"
  else
    git -C "$path" config --add credential.helper manager
  fi
  git -C "$path" commit -q --allow-empty -m 'Synthetic starter commit'
  git -C "$path" update-ref refs/remotes/origin/main HEAD
  git -C "$path" branch --set-upstream-to=origin/main main >/dev/null
  case "$name" in
    project-author) git -C "$path" config user.name "Project author" ;;
    wrong-host) git -C "$path" remote set-url origin "https://user:proof-hidden-value@wrong.example.invalid/team/$name.git?hidden=proof-hidden-value" ;;
    wrong-email) git -C "$path" config user.email "proof-hidden-value" ;;
    rewritten-origin) git -C "$path" config "url.https://wrong.example.invalid/.insteadOf" "https://$user@$host/" ;;
    include-helper)
      printf '[credential]\n  helper = !proof-hidden-value\n' > "$path/.git/override.gitconfig"
      git -C "$path" config --add include.path override.gitconfig
      ;;
    other-host-helper) git -C "$path" config --add credential.https://other.example.invalid.helper '!proof-hidden-value' ;;
    scoped-helper) git -C "$path" config --add "credential.https://$user@$host/team.helper" '!proof-hidden-value' ;;
    scoped-reset)
      git -C "$path" config --add "credential.https://$user@$host.helper" '!proof-hidden-value'
      git -C "$path" config --add "credential.https://$user@$host.helper" ''
      git -C "$path" config --add "credential.https://$user@$host.helper" "store --file '${store//\'/\'\\\'\'}'"
      ;;
  esac
}

for name in "${personal[@]}"; do seed_clone personal "$name" alice "Alice Example" 127.0.0.1:1; done
for name in "${team[@]}"; do seed_clone team "$name" bob "Bob Example" 127.0.0.2:1; done

jq -n --arg dir "$fixture" --arg personal "${personal[*]}" --arg team "${team[*]}" '
  def repos($names; $account):
    $names | split(" ") | map({key: ("team/" + .), value: {
      clone_folder: ($dir + "/clones/" + $account + "/" + .)
    } + (if . == "project-author" then {name: "Project author"} else {} end)}) | from_entries;
  {
    version: 5,
    global: {roots: [$dir + "/clones"], ui_mode: "advanced", check_global_gitignore: false},
    accounts: {
      "proof-personal": {provider: "github", url: "https://127.0.0.1:1", username: "alice",
        name: "Alice Example", email: "alice@example.invalid", default_credential_type: "gcm"},
      "proof-team": {provider: "gitlab", url: "https://127.0.0.2:1", username: "bob",
        name: "Bob Example", email: "bob@example.invalid", default_credential_type: "token",
        _test: {token: "synthetic-proof-token"}}
    },
    sources: {
      personal: {account: "proof-personal", repos: repos($personal; "personal")},
      team: {account: "proof-team", repos: repos($team; "team")}
    }
  }' > "$fixture/test-gitbox.json"

trap - EXIT
printf '%s\n' "$fixture"
