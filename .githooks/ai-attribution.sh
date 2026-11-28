#!/usr/bin/env bash
# Finds AI attribution in commit messages, tag messages or PR text.
#
# Usage: ai-attribution.sh [file]   (reads stdin without a file)
#
# Prints each offending line and exits 1; exits 0 when the text is clean.
# It flags co-author trailers that name an AI tool, session trailers, and
# "generated with/by <tool>" footers. Plain mentions of a tool (this
# repository manages AI harnesses) and human co-authors stay allowed.
#
# Callers: .githooks/commit-msg, .githooks/pre-push and the merge-pr skill.

set -euo pipefail

tools='claude|anthropic|openai|codex|chatgpt|gpt-?[0-9]|copilot|cursor|aider|gemini|devin|windsurf|codeium|tabnine|amazon q'

patterns=(
    # Co-author trailer naming an AI tool or its noreply address.
    "^[[:space:]]*co-authored-by:.*($tools)"
    # Harness session trailers.
    '^[[:space:]]*(claude|codex)-session:'
    # "Generated with [Claude Code](…)" and similar footers.
    "(generated|created|written|made|produced|authored|assisted)[[:space:]]+(with|by|using)[[:space:]]+(the[[:space:]]+|an?[[:space:]]+)?\\[?($tools|ai([[:space:]]|$|[.,;:!)]))"
    # Tool landing pages and noreply addresses used by attribution footers.
    'noreply@(anthropic|openai)\.com|claude\.(com|ai)/(claude-)?code|chatgpt\.com/codex'
)

text="$(cat "${1:-/dev/stdin}")"

found=false
for p in "${patterns[@]}"; do
    if matches="$(printf '%s\n' "$text" | grep -inE -- "$p")"; then
        printf '%s\n' "$matches"
        found=true
    fi
done

[ "$found" = false ]
