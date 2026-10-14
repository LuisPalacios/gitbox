---
name: preparing-brand-logos
description: Finds, downloads and prepares tool logos (terminals, editors, AI harnesses, shells) for assets/svg/brand/ in gitbox's 150x150 template, and drafts their single-colour -dark versions. Use when the user asks to find or download missing logos, fit a logo into the template, add a new brand logo, or create a logo-<name>-dark.svg.
---

# Preparing brand logos

Logos live in `assets/svg/brand/` and reach the app through `cmd/gui/frontend/vite-plugin-icons.ts`. File contract and naming: [assets/svg/README.md](../../../assets/svg/README.md). The work happens in the repo's `tmp/` folder (gitignored); the user reviews it there before anything moves into `assets/`.

## Workflow

1. **Name.** One `logo-<name>.svg` per tool, lowercase, hyphens allowed. The name must match what the icon lookup in `cmd/gui/frontend/src/` returns (`harnessIcon`, `editorIcon`, `terminalIcon`). `terminal`, `ide` and `ai` are the generic fallbacks, so never reuse them for a specific tool.
2. **Find.** Download raw sources to `tmp/originals/<name>.<ext>` (second candidate: `<name>.alt.<ext>`). Source order: the project's own repo (icons/, res/, assets/, data/icons/hicolor/scalable/apps/), Wikimedia Commons (upload.wikimedia.org original), Papirus icon theme (`Papirus/64x64/apps/`, follow symlinks), Breeze icons, gilbarbara/logos, svgl (`pheralb/svgl` `static/library`), then other free sites. Simple Icons is monochrome only, so it's a good base for `-dark`, not for colour. Prefer the full-colour app icon as SVG; fall back to the largest PNG. Check every download: `head -c 300` shows `<svg`/`<?xml` (not HTML), `file` says PNG. Parallelize by group with subagents when there are many.
3. **Record sources** in `tmp/SOURCES.md`: name, file, source URL, licence, note.
4. **Fit** each source into the colour template:

   ```bash
   uv run .claude/skills/preparing-brand-logos/scripts/fit_logo.py tmp/originals/<name>.svg tmp/logo-<name>.svg --source-url <url>
   ```

   `--bbox content` (default) fits the painted artwork, clamped to the source viewBox, and skips clip-path/mask/pattern shapes. A source with `<text>` falls back to its viewBox, because text extents aren't measured. `--bbox viewbox` keeps the source padding; `--bbox X,Y,W,H` sets the box by hand (use it when a drop shadow, a stray shape or a clip path skews the content bbox). A PNG source gets embedded as `<image>`. That's fine for review, but it must be redrawn as vectors before shipping.
5. **Check** the result by eye. Run `uv run .claude/skills/preparing-brand-logos/scripts/preview_logos.py tmp` and open `tmp/preview.html` (light and dark, 16/24/120 px, guide box outlined). On macOS, `qlmanage -t -s 300 -o <dir> <file.svg>` renders a PNG you can Read.
6. **Stop** and hand over to the user for review. Don't create `-dark` files or touch `assets/` until they say so.
7. **Dark version** (when asked): write `tmp/logo-<name>-dark.svg` by hand from the reviewed colour file, following the dark template below. Simplify to the logo's recognizable silhouette; inner details become cut-outs (`fill-rule="evenodd"`) or strokes. Check it at 16px in `preview.html`.
8. **Install** (when asked): copy into `assets/svg/brand/`, add the name to the icon lookup and to the name list in `assets/svg/README.md`, then `cd cmd/gui/frontend && npm run build` (the build fails on a `-dark` file with gradients, filters, masks, patterns or images).

## Colour template (`logo-<name>.svg`)

Boxy SVG header, a hidden 120×120 guide box at 15,15, and the artwork scaled to touch the guide box and centred. `fit_logo.py` writes exactly this:

```xml
<?xml version="1.0" encoding="utf-8"?>
<svg height="100%" style="fill-rule:evenodd;clip-rule:evenodd;stroke-linecap:round;stroke-linejoin:round;" width="100%" version="1.1" viewBox="0 0 150 150" xmlns="http://www.w3.org/2000/svg" xmlns:bx="https://boxy-svg.com">
  <defs>
    <bx:grid x="0" y="0" width="15" height="15"/>
  </defs>
  <rect width="120" height="120" style="fill: none; stroke-width: 0.2px; stroke: rgb(84, 84, 84); pointer-events: none; visibility: hidden;" x="15" y="15"/>
  <g transform="matrix(s, 0, 0, s, tx, ty)" style="fill-rule:nonzero;clip-rule:nonzero;stroke-linecap:butt;stroke-linejoin:miter">
    <!-- source artwork, verbatim -->
  </g>
</svg>
```

`s = 120 / max(w, h)`, `tx = 15 + (120 - w·s)/2 - x·s`, `ty` likewise, for the artwork box `x, y, w, h`. The group's style resets the root's evenodd/round defaults, which would otherwise punch holes in imported paths.

## Dark template (`logo-<name>-dark.svg`)

Single colour, no editor header. The build repaints every colour with the theme's text colour. Model: `assets/svg/brand/logo-terminal-dark.svg`.

```xml
<svg viewBox="0 0 150 150" xmlns="http://www.w3.org/2000/svg" fill="currentColor" fill-rule="evenodd" clip-rule="evenodd">
  <rect x="15" y="15" width="120" height="120" style="fill: none; stroke-width: 0.2px; stroke: rgb(84, 84, 84); pointer-events: none; visibility: hidden;"/>
  <!-- shapes in currentColor; strokes use stroke="currentColor" fill="none" -->
</svg>
```

No gradients, filters, masks, patterns, `url(#…)` or images. Keep the artwork inside 15..135.

## Requirements

`uv` (zero-footprint PEP 723 scripts). `fit_logo.py` pulls `svgelements` (bounding boxes) and `pillow` (PNG trim) on first run; `preview_logos.py` has no dependencies.
