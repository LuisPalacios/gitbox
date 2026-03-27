---
name: screenshot-prototype
description: Generate an anonymized, self-contained Svelte prototype of the gitbox GUI for SvelteLab screenshots. Use when updating the README screenshot or when the user asks to regenerate the screenshot prototype.
---

# /screenshot-prototype — Anonymized GUI Prototype

**IMPORTANT:** Before starting, inform the user: "I'm executing `/screenshot-prototype`"

Generate a self-contained Svelte file at `assets/screenshot.svelte` that visually replicates the gitbox GUI dashboard with anonymized demo data. The output is designed to be pasted into [SvelteLab](https://sveltelab.dev/) for taking README screenshots.

## When to use

- User wants to update `assets/gitbox-screenshot.png`
- User asks to regenerate or refresh the screenshot prototype
- After visual changes to `cmd/gui/frontend/src/App.svelte`

## Workflow

1. **Read** the current `cmd/gui/frontend/src/App.svelte` (the real GUI)
2. **Read** `cmd/gui/frontend/src/lib/theme.ts` (color palettes and helpers)
3. **Read** the current `assets/screenshot.svelte` (if it exists, to preserve any manual tweaks)
4. **Generate** `assets/screenshot.svelte` — a single-file Svelte component that:
   - Is **100% self-contained** — no imports (except `svelte`), no Wails bridge, no stores
   - Inlines all CSS variables, styles, and theme helpers from the real app
   - Uses **hardcoded anonymized demo data** (see Data section below)
   - Renders the **dashboard view**: topbar + settings panel + account cards + repo list + footer
   - Skips modals (discover, add/edit account, delete, credential) and onboarding
   - Matches the real app's visual output as closely as possible
5. **Show** the user the output path and remind them to paste it into SvelteLab

## Demo Data

Use these fake but realistic-looking accounts and repos. The mix of statuses should showcase the app's capabilities:

### Accounts (4 cards)

| Key | Provider | Credential | Synced | Total | Issues |
| --- | -------- | ---------- | ------ | ----- | ------ |
| parchis-luis | Forgejo | gcm | 2 | 4 | 2 |
| LuisPalacios | GitHub | gcm | 1 | 3 | 2 |
| Renueva | GitHub | ssh | 3 | 3 | 0 |
| Azelerum | GitHub | token | 1 | 1 | 0 |

### Repos per source

**git-parchis-luis:**

| Repo | Status | Details |
| --- | --- | --- |
| infra/homelab | clean | Synced |
| infra/migration | clean | Synced |
| familia/fotos | behind | 3 behind |
| parchis/web | not cloned | Not local |

**github-LuisPalacios:**

| Repo | Status | Details |
| --- | --- | --- |
| LuisPalacios/gitbox | clean | Synced |
| LuisPalacios/dotfiles | dirty | 2 local changes |
| LuisPalacios/homelab | behind | 2 behind |

**github-Renueva:**

| Repo | Status | Details |
| --- | --- | --- |
| Renueva/platform | clean | Synced |
| Renueva/docs | clean | Synced |
| Renueva/infra | clean | Synced |

**github-Azelerum:**

| Repo | Status | Details |
| --- | --- | --- |
| Azelerum/homelab | clean | Synced |

### Summary footer

`7 synced · 2 behind · 1 local changes · 1 not local`

## Output

Write to `assets/screenshot.svelte`. This file is **not** part of the build — it's a standalone artifact for SvelteLab.

## Interactive features

The prototype is mostly static data, but these UI elements **must be functional** so the user can screenshot any state:

- **Theme cycling**: the topbar theme button cycles through `dark` → `system` → `light` via `cycleTheme()`. Icon updates to match (`☾` / `◐` / `☀`). Both dark and light CSS palettes must be present. Status colors must be **reactive** to theme changes (use both `darkPalette` and `lightPalette` from `theme.ts`, select via `$:` reactive statement).
- **Settings panel toggle**: the gear button toggles a `showSettings` boolean. When open, show a static settings panel with fake config path (`~/.config/gitbox/gitbox.json`), clone folder (`~/00.git`), working theme buttons (same as cycling), periodic fetch buttons (static, "Off" active), and version. Copy the settings CSS from App.svelte (`.settings`, `.settings-row`, `.settings-label`, `.settings-value`, `.theme-toggle`, `.theme-btn`, `.theme-active`, `.active-gear`).
- **Default to dark theme** on mount (`data-theme="dark"`).

## Key rules

- **Never expose real account names, URLs, emails, or repo names** from the user's config
- Keep the file under 500 lines if possible (it's just a static prototype)
- Match the real app's CSS exactly — copy from App.svelte, don't reinvent
- The SVG logo must be included inline (copy from App.svelte)
- Set `document.documentElement.setAttribute('data-theme', 'dark')` on mount via `applyTheme()`
