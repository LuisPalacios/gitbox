#!/usr/bin/env bash
#
# test-setup-credentials.sh — Set up and verify all test credentials.
#
# Reads test-gitbox.json. Accounts without a "_test" key are silently skipped.
# Accounts WITH "_test" must have a "token" filled in.
# SSH key names are derived from the convention: test-<hostname>-<accountKey>-sshkey
# so each OS gets its own key pair and can be registered on providers simultaneously.
#
# The script:
#   1. Verifies API tokens against each provider
#   2. Generates SSH key pairs (skips existing)
#   3. Adds Host entries to ~/.ssh/config (skips existing)
#   4. Verifies SSH connections
#
# Idempotent — safe to run multiple times.
#
# Requirements: jq, ssh-keygen, curl
#
# Usage:
#   ./scripts/test-setup-credentials.sh
#   ./scripts/test-setup-credentials.sh path/to/test-gitbox.json

set -euo pipefail

G='\033[0;32m' R='\033[0;31m' Y='\033[0;33m' C='\033[0;36m' D='\033[0;90m' N='\033[0m'

# ---------------------------------------------------------------------------
# Locate test-gitbox.json
# ---------------------------------------------------------------------------

FIXTURE="${1:-}"
if [[ -z "$FIXTURE" ]]; then
    dir="$(cd "$(dirname "$0")/.." && pwd)"
    FIXTURE="$dir/test-gitbox.json"
fi

if [[ ! -f "$FIXTURE" ]]; then
    echo -e "${R}error:${N} $FIXTURE not found"
    echo "Copy test-gitbox.json.example to test-gitbox.json and fill in your accounts."
    exit 1
fi

for cmd in jq curl; do
    if ! command -v "$cmd" &>/dev/null; then
        echo -e "${R}error:${N} $cmd is required"
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# Read config — only accounts that have a _test key
# ---------------------------------------------------------------------------

HNAME=$(hostname 2>/dev/null | sed 's/\..*//' | tr '[:upper:]' '[:lower:]' | tr -d '\r')

SSH_FOLDER=$(jq -r '.global.credential_ssh.ssh_folder // "/tmp/gitbox-test/ssh"' "$FIXTURE" | tr -d '\r')
SSH_FOLDER="${SSH_FOLDER/#\~/$HOME}"

# account_key|provider|url|username|token|ssh_host|ssh_hostname|key_type
ACCOUNTS=$(jq -r '
    .accounts | to_entries[]
    | select(.value._test != null)
    | "\(.key)|\(.value.provider // "")|\(.value.url // "")|\(.value.username // "")|\(.value._test.token // "")|\(.value.ssh.host // "")|\(.value.ssh.hostname // "")|\(.value.ssh.key_type // "ed25519")"
' "$FIXTURE" | tr -d '\r')

if [[ -z "$ACCOUNTS" ]]; then
    echo "No accounts with _test key found in $FIXTURE"
    exit 0
fi

ERRORS=0

# ---------------------------------------------------------------------------
# 1. Verify tokens
# ---------------------------------------------------------------------------

echo -e "${C}Tokens${N}"
while IFS='|' read -r ACCT PROVIDER URL USERNAME TOKEN SSH_HOST SSH_HOSTNAME KEY_TYPE; do
    URL="${URL%/}"

    if [[ -z "$TOKEN" || "$TOKEN" == *"xxxx"* ]]; then
        ERRORS=$((ERRORS + 1))
        case "$PROVIDER" in
            github)        TOKEN_URL="$URL/settings/tokens" ;;
            gitlab)        TOKEN_URL="$URL/-/user_settings/personal_access_tokens" ;;
            gitea|forgejo) TOKEN_URL="$URL/user/settings/applications" ;;
            bitbucket)     TOKEN_URL="$URL/account/settings/app-passwords/" ;;
            *)             TOKEN_URL="$URL" ;;
        esac
        echo -e "  ${R}FAIL${N}  $ACCT ${D}($PROVIDER)${N} — token missing or placeholder"
        echo -e "        ${D}Create a PAT at $TOKEN_URL and paste it in _test.token${N}"
        continue
    fi

    case "$PROVIDER" in
        github)
            if [[ "$URL" == "https://github.com" ]]; then
                API_URL="https://api.github.com/user"
            else
                API_URL="$URL/api/v3/user"
            fi
            AUTH_HEADER="Authorization: token $TOKEN"
            ;;
        gitlab)
            API_URL="$URL/api/v4/user"
            AUTH_HEADER="PRIVATE-TOKEN: $TOKEN"
            ;;
        gitea|forgejo)
            API_URL="$URL/api/v1/user"
            AUTH_HEADER="Authorization: token $TOKEN"
            ;;
        bitbucket)
            API_URL="https://api.bitbucket.org/2.0/user"
            AUTH_HEADER="Authorization: Bearer $TOKEN"
            ;;
        *)
            echo -e "  ${R}FAIL${N}  $ACCT — unknown provider: $PROVIDER"
            ERRORS=$((ERRORS + 1))
            continue
            ;;
    esac

    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "$AUTH_HEADER" "$API_URL" 2>/dev/null || echo "000")

    if [[ "$HTTP_CODE" == "200" ]]; then
        echo -e "  ${G}ok${N}    $ACCT ${D}($PROVIDER)${N}"
    else
        ERRORS=$((ERRORS + 1))
        case "$PROVIDER" in
            github)        TOKEN_URL="$URL/settings/tokens" ;;
            gitlab)        TOKEN_URL="$URL/-/user_settings/personal_access_tokens" ;;
            gitea|forgejo) TOKEN_URL="$URL/user/settings/applications" ;;
            bitbucket)     TOKEN_URL="$URL/account/settings/app-passwords/" ;;
            *)             TOKEN_URL="$URL" ;;
        esac
        if [[ "$HTTP_CODE" == "000" ]]; then
            echo -e "  ${R}FAIL${N}  $ACCT ${D}($PROVIDER)${N} — cannot reach $URL"
            echo -e "        ${D}Check your network or the URL in test-gitbox.json${N}"
        else
            echo -e "  ${R}FAIL${N}  $ACCT ${D}($PROVIDER)${N} — HTTP $HTTP_CODE"
            echo -e "        ${D}Create/renew token at $TOKEN_URL and update _test.token${N}"
        fi
    fi
done <<< "$ACCOUNTS"

# ---------------------------------------------------------------------------
# 2. SSH keys (name derived from convention: test-<hostname>-<accountKey>-sshkey)
# ---------------------------------------------------------------------------

echo -e "${C}SSH keys${N} ${D}($SSH_FOLDER)${N}"
mkdir -p "$SSH_FOLDER" && chmod 700 "$SSH_FOLDER"

while IFS='|' read -r ACCT PROVIDER URL USERNAME TOKEN SSH_HOST SSH_HOSTNAME KEY_TYPE; do
    SSH_KEY_NAME="test-${HNAME}-${ACCT}-sshkey"
    KEY_PATH="$SSH_FOLDER/$SSH_KEY_NAME"
    if [[ -f "$KEY_PATH" ]]; then
        echo -e "  ${G}ok${N}    $SSH_KEY_NAME"
    else
        ssh-keygen -t "$KEY_TYPE" -f "$KEY_PATH" -N "" -C "test-${HNAME}-${ACCT}" -q
        chmod 600 "$KEY_PATH"
        echo -e "  ${G}new${N}   $SSH_KEY_NAME"
    fi
done <<< "$ACCOUNTS"

# ---------------------------------------------------------------------------
# 3. SSH config
# ---------------------------------------------------------------------------

SSH_CONFIG="$SSH_FOLDER/config"
touch "$SSH_CONFIG" && chmod 600 "$SSH_CONFIG"

echo -e "${C}SSH config${N} ${D}($SSH_CONFIG)${N}"
while IFS='|' read -r ACCT PROVIDER URL USERNAME TOKEN SSH_HOST SSH_HOSTNAME KEY_TYPE; do
    SSH_KEY_NAME="test-${HNAME}-${ACCT}-sshkey"
    KEY_PATH="$SSH_FOLDER/$SSH_KEY_NAME"
    # Convert MSYS2 path (/c/Users/...) to Windows-native (C:/Users/...) so both
    # SSH (MSYS2) and Go (native) can read it.
    IDENTITY_PATH="$KEY_PATH"
    if [[ "$IDENTITY_PATH" == /[a-zA-Z]/* ]]; then
        DRIVE="${IDENTITY_PATH:1:1}"
        IDENTITY_PATH="${DRIVE^^}:${IDENTITY_PATH:2}"
    fi
    BLOCK="Host $SSH_HOST
    HostName $SSH_HOSTNAME
    User git
    IdentityFile $IDENTITY_PATH
    IdentitiesOnly yes"

    if ! grep -q "^Host $SSH_HOST\$" "$SSH_CONFIG" 2>/dev/null; then
        printf '\n%s\n' "$BLOCK" >> "$SSH_CONFIG"
        echo -e "  ${G}added${N} Host $SSH_HOST"
    elif grep -q "IdentityFile $IDENTITY_PATH" "$SSH_CONFIG" 2>/dev/null; then
        echo -e "  ${G}ok${N}    Host $SSH_HOST"
    else
        # Host exists but IdentityFile changed — rewrite the block
        awk -v host="$SSH_HOST" '
            /^Host /{ if ($2 == host) {skip=1; next} else {skip=0} }
            skip && /^[^ ]/ && !/^Host /{skip=0}
            skip && /^    /{next}
            !skip
        ' "$SSH_CONFIG" > "${SSH_CONFIG}.tmp"
        mv "${SSH_CONFIG}.tmp" "$SSH_CONFIG"
        printf '\n%s\n' "$BLOCK" >> "$SSH_CONFIG"
        echo -e "  ${Y}fixed${N} Host $SSH_HOST ${D}(updated IdentityFile)${N}"
    fi
done <<< "$ACCOUNTS"

# ---------------------------------------------------------------------------
# 4. SSH connections
# ---------------------------------------------------------------------------

echo -e "${C}SSH verify${N}"
while IFS='|' read -r ACCT PROVIDER URL USERNAME TOKEN SSH_HOST SSH_HOSTNAME KEY_TYPE; do
    SSH_KEY_NAME="test-${HNAME}-${ACCT}-sshkey"
    KEY_PATH="$SSH_FOLDER/$SSH_KEY_NAME"
    OUTPUT=$(ssh -T -F "$SSH_CONFIG" -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new "git@$SSH_HOST" </dev/null 2>&1 || true)

    if echo "$OUTPUT" | grep -qi "successfully\|welcome\|logged in\|Hi "; then
        echo -e "  ${G}ok${N}    $SSH_HOST"
    else
        ERRORS=$((ERRORS + 1))
        URL="${URL%/}"
        case "$PROVIDER" in
            github)        KEY_URL="$URL/settings/keys" ;;
            gitlab)        KEY_URL="$URL/-/user_settings/ssh_keys" ;;
            gitea|forgejo) KEY_URL="$URL/user/settings/keys" ;;
            bitbucket)     KEY_URL="$URL/account/settings/ssh-keys/" ;;
            *)             KEY_URL="$URL" ;;
        esac
        echo -e "  ${R}FAIL${N}  $SSH_HOST — public key not registered"
        echo -e "        ${D}Add key at $KEY_URL: $(cat "$KEY_PATH.pub")${N}"
    fi
done <<< "$ACCOUNTS"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo ""
if [[ "$ERRORS" -eq 0 ]]; then
    echo -e "${G}All credentials verified. Ready for integration tests.${N}"
else
    echo -e "${R}$ERRORS issue(s) found.${N} Fix them and run this script again."
fi
