<script module lang="ts">
  import { brand } from 'virtual:icons';

  // Logo names that differ from the terminal app id (pkg/terminals/catalog.go).
  // Terminal.app's id is "terminal", which is the generic fallback's name.
  const TERMINAL_LOGO: Record<string, string> = { wt: 'windows-terminal', terminal: 'macos-terminal' };
  // Bare-shell profiles have no terminal app, so they show their shell's logo.
  const SHELL_LOGO: Record<string, string> = { pwsh: 'powershell', powershell: 'powershell', cmd: 'cmd', 'git-bash': 'git-bash', wsl: 'wsl' };

  // terminalLogo picks the logo for a terminal profile, or for a terminal app
  // when shell is omitted: the app's own logo when assets/svg/brand/ has one,
  // else the generic terminal.
  export function terminalLogo(terminal: string, shell = ''): string {
    const name = terminal
      ? (TERMINAL_LOGO[terminal] ?? terminal)
      : (SHELL_LOGO[shell.startsWith('wsl-') ? 'wsl' : shell] ?? '');
    return brand[name] ? name : 'terminal';
  }
</script>

<script lang="ts">
  // BrandIcon — tool logos for the repo-row launch buttons (issue #124).
  // The art lives in assets/svg/brand/ and reaches the app through the
  // build (vite-plugin-icons.ts). Light theme shows the colour original
  // (logo-<name>.svg) as drawn; dark theme shows the single-colour version
  // (logo-<name>-dark.svg) in the theme's text colour. Each theme falls back
  // to the other file when its own is missing.
  import { themeStore } from './stores';

  export let name: string;
  export let size: number = 16;

  $: entry = brand[name] || {};
  $: useColor = entry.color && ($themeStore === 'light' || !entry.mono);
</script>

{#if useColor}
  <img class="brand-icon" src={entry.color} width={size} height={size} alt="" aria-hidden="true" />
{:else if entry.mono}
  <span class="brand-icon" style="width: {size}px; height: {size}px" aria-hidden="true">{@html entry.mono}</span>
{/if}

<style>
  .brand-icon { display: block; flex-shrink: 0; }
  .brand-icon :global(svg) { display: block; width: 100%; height: 100%; }
</style>
