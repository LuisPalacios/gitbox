---
name: screenshot-prototype
description: Generate anonymized, self-contained Svelte prototypes of the gitbox GUI (full and compact views) for SvelteLab screenshots. Use when updating the README screenshot or when the user asks to regenerate the screenshot prototype.
---

# /screenshot-prototype — Anonymized GUI Prototypes

**IMPORTANT:** Before starting, inform the user: "I'm executing `/screenshot-prototype`"

Generate two self-contained Svelte files that visually replicate the gitbox GUI with anonymized demo data. The output is designed to be pasted into [SvelteLab](https://sveltelab.dev/) for taking README screenshots.

## Output files

| File | View | Description |
| --- | --- | --- |
| `assets/screenshot-full.svelte` | Full dashboard | Topbar + account cards + repo list + footer |
| `assets/screenshot-small.svelte` | Compact status strip | Health ring + account pills + expandable repo rows |

## When to use

- User wants to update `assets/gitbox-screenshot.png`
- User asks to regenerate or refresh the screenshot prototype
- After visual changes to `cmd/gui/frontend/src/App.svelte`

## Workflow

1. **Read** `cmd/gui/frontend/src/App.svelte` — capture both the full dashboard layout AND the compact view (`{#if viewMode === 'compact'}` section)
2. **Read** `cmd/gui/frontend/src/lib/theme.ts` — color palettes and helpers
3. **Read** existing `assets/screenshot-full.svelte` and `assets/screenshot-small.svelte` (if they exist, to preserve manual tweaks)
4. **Generate** both files following the rules below
5. **Show** the user the output paths and remind them to paste into SvelteLab

## Demo data (shared by both files)

Use these 3 fictional accounts and repos. The same data appears in both prototypes. The mix of statuses showcases the app's capabilities.

### Accounts (3 cards)

| Key | Provider | Credential | Cred Status | Synced | Total | Issues |
| --- | -------- | ---------- | ----------- | ------ | ----- | ------ |
| acme-ops | Forgejo | gcm | ok | 2 | 4 | 2 |
| stellar-dev | GitHub | ssh | ok | 3 | 3 | 0 |
| nebula-team | GitLab | token | ok | 1 | 2 | 1 |

### Repos per source

**acme-ops:**

| Repo | Status | Details |
| --- | --- | --- |
| infra/k8s-prod | clean | Synced |
| infra/terraform | clean | Synced |
| platform/api | behind | 3 behind |
| platform/web | not cloned | Not local |

**stellar-dev:**

| Repo | Status | Details |
| --- | --- | --- |
| stellar-dev/cosmos | clean | Synced |
| stellar-dev/orbit | clean | Synced |
| stellar-dev/nova | clean | Synced |

**nebula-team:**

| Repo | Status | Details |
| --- | --- | --- |
| nebula/dashboard | clean | Synced |
| nebula/pipeline | dirty | 2 local changes |

### Summary footer

`6 synced · 1 behind · 1 local changes · 1 not local`

## Rules for both files

- **100% self-contained** — no imports (except `svelte`), no Wails bridge, no stores
- Inline all CSS variables, styles, and theme helpers from the real app
- Copy CSS **exactly** from App.svelte — do not reinvent
- Use `darkPalette` and `lightPalette` from `theme.ts` with `$:` reactive statements
- **Theme cycling must work**: button cycles `dark` → `system` → `light`, icon updates (`☾` / `◐` / `☀`), all colors react to theme
- Default to dark theme on mount via `data-theme="dark"`
- SVG logo must be inlined (copy from App.svelte)
- Keep each file under 500 lines
- **Never expose real account names, URLs, emails, or repo names**

## screenshot-full.svelte specifics

Renders the **full dashboard view**:

- Topbar (brand, health ring, action buttons including compact toggle `◧`)
- Settings panel toggle (gear button, static config/folder/theme/version when open)
- Account cards row with credential badges (colored by status: green ok, blue config, etc.)
- Repo list grouped by source with status dots, labels, and badges
- Summary footer
- No modals, no onboarding

## screenshot-small.svelte specifics

Renders the **compact status strip** (~220px wide):

- Global health ring with percentage and synced count
- Per-account pills with mini ring + name + issue count + chevron
- Click account pill to expand/collapse repo list underneath (interactive)
- Repo rows with status symbol + name + badge
- Clean repos dimmed (opacity 0.5)
- Bottom actions: theme toggle + "Full view" button
- All compact CSS classes from App.svelte (`.compact-strip`, `.compact-acct`, etc.)

## Interactive features (both files)

- **Theme cycling**: topbar/bottom theme button cycles through dark/system/light. Status colors are reactive.
- **Settings panel toggle** (full only): gear button toggles settings panel visibility.
- **Account expand/collapse** (small only): click account pills to show/hide repo list.
