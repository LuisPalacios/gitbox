# Icons

These SVG files are the source of every logo and UI icon in GitboxApp. I edit or replace a file here, rebuild, and the app picks it up. Nothing in the code needs to change. Under `wails dev` the window reloads as soon as a file changes.

The build step lives in `cmd/gui/frontend/vite-plugin-icons.ts`. It runs on every `npm run build`, `wails build` and `wails dev`. It cleans each file with svgo: it removes the UI icons' hidden guide box and editor metadata, drops fixed sizes, and minifies the paths. It never moves or rescales the artwork.

## Tool logos (`brand/`)

Each tool has up to two files:

- `logo-<name>.svg` — the colour original, shown exactly as drawn in the light theme. Gradients, filters and any number of colours are fine.
- `logo-<name>-dark.svg` — a single-colour version for the dark theme. The build repaints every colour with the theme's text colour, so the colour I draw it in doesn't matter.

When one of the two is missing, both themes use the other. The generic `ai` sparkle only has a `-dark` file, so it shows in the text colour in both themes.

Many terminal logos are a dark screen, and at menu size they read as a black square in the light theme. I list those in `invert-light.txt`, one name per line, and the light theme shows them with light and dark swapped and the hues kept (CSS `invert` plus `hue-rotate`). The original file stays as drawn. Logos with a light or colourful body stay off the list, because inverting them makes them dark.

The code looks up these names: `claude`, `openai`, `gemini`, `cursor` (AI harnesses), `vscode`, `zed` (editors), plus the generic `ai`, `ide` and `terminal` for tools without a known logo. A new AI harness or editor name needs a matching rule in `harnessLogo` or `editorLogo` in `cmd/gui/frontend/src/lib/BrandIcon.svelte`.

Terminal logos are named after the terminal app's id in `pkg/terminals/catalog.go`, so a new one needs no code: `alacritty`, `conemu`, `foot`, `ghostty`, `gnome-terminal`, `guake`, `hyper`, `iterm`, `kitty`, `konsole`, `mintty`, `tabby`, `terminator`, `tilda`, `warp`, `wezterm` and `xterm`. Two ids map to other names in `terminalLogo` (`cmd/gui/frontend/src/lib/BrandIcon.svelte`): `wt` uses `windows-terminal`, and Terminal.app's `terminal` uses `macos-terminal`, because `terminal` is the generic fallback. Windows bare-shell profiles show their shell's logo: `powershell`, `cmd`, `git-bash` or `wsl`.

To find, fit and preview new logos, I use the `preparing-brand-logos` skill (`.claude/skills/preparing-brand-logos/`).

## UI icons (`ui/`)

`<name>.svg`, single colour, repainted with the theme's text colour like the `-dark` logos. The app shows every one at 18px (`--icon`), inside 30px buttons (`--icon-btn`). Names in use: `pull`, `fetch`, `trash`, `compact`, `theme-light`, `theme-dark`, `theme-system`, `gear`, `menu`, `dots`, `x`, `chevron`, `plus`, `search`, `mirror`, `grid` and `list`.

## Drawing rules

- Logos use a `0 0 150 150` viewBox, and the app shows the whole canvas edge to edge, so what I see in the editor is what the app draws. There is no margin and no guide box: the artwork's longer side spans the full 150, centred. Buttons and menus add their own spacing around the logo. Anything past the canvas edge is cut off, including drop shadows and glows.
- UI icons use a `0 0 24 24` viewBox with the artwork inside the 18×18 guide box from 3,3 to 21,21, drawn as 1.8-wide strokes with round caps and joins (set on the root `<svg>`).
- The UI icon guide box is a `<rect>` with `visibility: hidden`. I keep it in the file while editing; the build removes anything hidden.
- Single-colour files (`-dark` logos and `ui/`) can't use gradients, filters, masks, patterns or embedded images. The build stops with the file name when it finds one. Cut-outs work with `fill-rule="evenodd"`.
- File names are lowercase. The build warns about and skips any file that doesn't follow the naming above.
