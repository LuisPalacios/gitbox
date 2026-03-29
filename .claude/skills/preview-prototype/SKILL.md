---
name: preview-prototype
description: Preview screenshot Svelte prototypes locally with a temporary Vite dev server. Use when the user wants to see assets/screenshot-full.svelte or assets/screenshot-small.svelte in a browser instead of SvelteLab.
---

# /preview-prototype -- Local Svelte Preview

**IMPORTANT:** Before starting, inform the user: "I'm executing `/preview-prototype`"

Preview `assets/screenshot-full.svelte` or `assets/screenshot-small.svelte` in a local browser using a temporary Vite + Svelte dev server under `preview/` (gitignored).

## Usage

```text
/preview-prototype          # previews screenshot-full.svelte (default)
/preview-prototype full     # previews screenshot-full.svelte
/preview-prototype small    # previews screenshot-small.svelte
/preview-prototype stop     # stops the running dev server
```

## Workflow

### 1. Parse argument

- No argument or `full` -> use `assets/screenshot-full.svelte`
- `small` -> use `assets/screenshot-small.svelte`
- `stop` -> go to step 5 (stop the server) and exit

### 2. Scaffold (only if `preview/package.json` does not exist)

Create the `preview/` directory with these minimal files:

**`preview/package.json`:**

```json
{
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite --open"
  },
  "devDependencies": {
    "vite": "^6",
    "@sveltejs/vite-plugin-svelte": "^5",
    "svelte": "^4"
  }
}
```

**`preview/vite.config.js`:**

```javascript
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
});
```

**`preview/index.html`:**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>gitbox preview</title>
</head>
<body>
  <script type="module" src="/src/main.js"></script>
</body>
</html>
```

**`preview/src/main.js`:**

```javascript
import App from './App.svelte';
const app = new App({ target: document.body });
export default app;
```

Then run `npm install` inside `preview/`.

### 3. Detect server state

Check if a process is already listening on port 5173:

```bash
lsof -ti :5173
```

Record the result: **server running** (exit code 0, PID found) or **server stopped** (exit code 1, no PID).

### 4. Copy the Svelte file and serve

Copy the chosen asset file to `preview/src/App.svelte` (overwriting any previous copy).

- **If server is already running:** inform the user that Vite HMR will hot-reload the new file automatically. Do NOT start a new server. Do NOT use `--open`.
- **If server is stopped:** run `npm run dev` inside `preview/` as a background process. Report the local URL (typically `http://localhost:5173`) to the user.

### 5. Stop (only when `stop` is passed)

Check if a process is listening on port 5173 (`lsof -ti :5173`).

- **If running:** kill it (`kill $(lsof -ti :5173)`), confirm to the user.
- **If not running:** inform the user that no server is running.

## Rules

- **All files go under `preview/`** which is gitignored
- **No npx** -- use `npm install` + `npm run dev` only
- **No interactive prompts** -- the scaffold is written directly, not via `npm create`
- **Idempotent** -- if `preview/node_modules` exists, skip `npm install`
- **Hot reload works** -- copying a new App.svelte while the server runs triggers Vite HMR automatically
