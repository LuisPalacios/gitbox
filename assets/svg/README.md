# Icons

These SVG files are the source of every logo and top-bar icon in GitboxApp. I edit or replace a file here, rebuild, and the app picks it up. Nothing in the code needs to change. Under `wails dev` the window reloads as soon as a file changes.

The build step lives in `cmd/gui/frontend/vite-plugin-icons.ts`. It runs on every `npm run build`, `wails build` and `wails dev`. It cleans each file with svgo: it removes the hidden guide box and editor metadata, drops fixed sizes, and minifies the paths.

## Tool logos (`brand/`)

Each tool has up to two files:

- `logo-<name>.svg` — the colour original, shown exactly as drawn in the light theme. Gradients, filters and any number of colours are fine.
- `logo-<name>-dark.svg` — a single-colour version for the dark theme. The build repaints every colour with the theme's text colour, so the colour I draw it in doesn't matter.

When one of the two is missing, both themes use the other. The generic `ai` sparkle only has a `-dark` file, so it shows in the text colour in both themes.

The code looks up these names: `claude`, `openai`, `gemini`, `cursor`, `vscode`, `zed`, `wezterm` (AI harnesses, editors and terminal), plus the generic `ai`, `ide` and `terminal` for tools without a known logo. A new name needs a matching rule in `harnessIcon`, `editorIcon` or `terminalIcon` in `cmd/gui/frontend/src/App.svelte`.

## Top-bar icons (`ui/`)

`<name>.svg`, single colour, repainted with the theme's text colour like the `-dark` logos. Names in use: `pull`, `fetch`, `trash`, `select`, `select-on`, `compact`, `theme-light`, `theme-dark`, `theme-system`, `gear`.

## Drawing rules

- Logos use a `0 0 150 150` viewBox with the artwork inside the 120×120 guide box from 15,15 to 135,135, so every logo renders at the same maximum size.
- Top-bar icons use a `0 0 24 24` viewBox with the artwork inside the 18×18 guide box from 3,3 to 21,21, drawn as 1.8-wide strokes with round caps and joins (set on the root `<svg>`).
- The guide box is a `<rect>` with `visibility: hidden`. I keep it in the file while editing; the build removes anything hidden.
- Single-colour files (`-dark` logos and `ui/`) can't use gradients, filters, masks, patterns or embedded images. The build stops with the file name when it finds one. Cut-outs work with `fill-rule="evenodd"`.
- File names are lowercase. The build warns about and skips any file that doesn't follow the naming above.
