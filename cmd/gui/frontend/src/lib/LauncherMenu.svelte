<script lang="ts">
  // LauncherMenu — shared action menu used by the repo-row kebab and the
  // account-header kebab in full view. Renders a top section with the most-
  // used launchers (editors[0], terminals[0], ai_harnesses[0]) and a submenu
  // section for the rest of each category. Handlers are passed in as props so
  // this component stays dumb and has no bridge dependency.
  //
  // Visual layout (both repo and account kebabs):
  //   browser
  //   folder
  //   ─ separator ─ (only if any default is shown)
  //   terminals[0]                        (hidden if terminals empty)
  //   editors[0]                          (hidden if editors empty)
  //   ai_harnesses[0]                     (hidden if harnesses empty)
  //   ─ separator ─ (only if any submenu is shown)
  //   Terminals ▸                         (hidden if <2 terminals)
  //   Editors ▸                           (hidden if <2 editors)
  //   AI Harnesses ▸                      (hidden if <2 harnesses)
  //   ─ separator ─ (repo kebab only, when onSweep is provided)
  //   Sweep branches                      (repo kebab only)

  import type { EditorInfo, TerminalInfo, AIHarnessInfo } from './types';

  export let kind: 'repo' | 'account' = 'repo';
  export let editors: EditorInfo[] = [];
  export let terminals: TerminalInfo[] = [];
  export let aiHarnesses: AIHarnessInfo[] = [];

  export let onOpenBrowser: () => void;
  export let onOpenFolder: () => void;
  export let onOpenApp: (command: string) => void;
  export let onOpenTerminal: (terminal: TerminalInfo) => void;
  export let onOpenAIHarness: (harness: AIHarnessInfo) => void;
  export let onSweep: (() => void) | null = null;

  type Sub = 'terminals' | 'editors' | 'ai' | null;
  let openSubmenu: Sub = null;

  function toggleSub(name: Exclude<Sub, null>) {
    openSubmenu = openSubmenu === name ? null : name;
  }

  $: showTerminalDefault = terminals.length >= 1;
  $: showEditorDefault = editors.length >= 1;
  $: showHarnessDefault = aiHarnesses.length >= 1;
  $: showTerminalsSub = terminals.length >= 2;
  $: showEditorsSub = editors.length >= 2;
  $: showHarnessSub = aiHarnesses.length >= 2;
  $: hasDefaultsSection = showTerminalDefault || showEditorDefault || showHarnessDefault;
  $: hasSubsSection = showTerminalsSub || showEditorsSub || showHarnessSub;
  $: hasSweepSection = kind === 'repo' && !!onSweep;
</script>

<div class="action-dropdown launcher-menu">
  <button class="action-item" on:click|stopPropagation={onOpenBrowser}>
    <span class="lm-icon">&#127760;</span> Open in browser
  </button>
  <button class="action-item" on:click|stopPropagation={onOpenFolder}>
    <span class="lm-icon">&#128193;</span> Open folder
  </button>

  {#if hasDefaultsSection}
    <hr class="lm-sep" />
    {#if showTerminalDefault}
      <button class="action-item" on:click|stopPropagation={() => onOpenTerminal(terminals[0])} title="Open in {terminals[0].name}">
        <span class="lm-icon lm-icon-mono">&gt;_</span> Open in {terminals[0].name}
      </button>
    {/if}
    {#if showEditorDefault}
      <button class="action-item" on:click|stopPropagation={() => onOpenApp(editors[0].command)} title="Open in {editors[0].name}">
        <span class="lm-icon">&#9998;</span> Open in {editors[0].name}
      </button>
    {/if}
    {#if showHarnessDefault}
      <button class="action-item" on:click|stopPropagation={() => onOpenAIHarness(aiHarnesses[0])} title="Open in {aiHarnesses[0].name}">
        <span class="lm-icon">&#129302;</span> Open in {aiHarnesses[0].name}
      </button>
    {/if}
  {/if}

  {#if hasSubsSection}
    <hr class="lm-sep" />
    {#if showTerminalsSub}
      <div class="lm-sub-container">
        <button class="action-item lm-submenu-trigger" class:lm-active={openSubmenu === 'terminals'} on:click|stopPropagation={() => toggleSub('terminals')}>
          <span class="lm-icon lm-icon-mono">&gt;_</span> Terminals
          <span class="lm-arrow">&#9654;</span>
        </button>
        {#if openSubmenu === 'terminals'}
          <div class="action-dropdown launcher-submenu">
            {#each terminals as terminal}
              <button class="action-item" on:click|stopPropagation={() => onOpenTerminal(terminal)}>
                <span class="lm-icon lm-icon-mono">&gt;_</span> {terminal.name}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
    {#if showEditorsSub}
      <div class="lm-sub-container">
        <button class="action-item lm-submenu-trigger" class:lm-active={openSubmenu === 'editors'} on:click|stopPropagation={() => toggleSub('editors')}>
          <span class="lm-icon">&#9998;</span> Editors
          <span class="lm-arrow">&#9654;</span>
        </button>
        {#if openSubmenu === 'editors'}
          <div class="action-dropdown launcher-submenu">
            {#each editors as editor}
              <button class="action-item" on:click|stopPropagation={() => onOpenApp(editor.command)}>
                <span class="lm-icon">&#9998;</span> {editor.name}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
    {#if showHarnessSub}
      <div class="lm-sub-container">
        <button class="action-item lm-submenu-trigger" class:lm-active={openSubmenu === 'ai'} on:click|stopPropagation={() => toggleSub('ai')}>
          <span class="lm-icon">&#129302;</span> AI Harnesses
          <span class="lm-arrow">&#9654;</span>
        </button>
        {#if openSubmenu === 'ai'}
          <div class="action-dropdown launcher-submenu">
            {#each aiHarnesses as harness}
              <button class="action-item" on:click|stopPropagation={() => onOpenAIHarness(harness)}>
                <span class="lm-icon">&#129302;</span> {harness.name}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  {/if}

  {#if hasSweepSection}
    <hr class="lm-sep" />
    <button class="action-item" on:click|stopPropagation={onSweep}>
      <span class="lm-icon">&#129529;</span> Sweep branches
    </button>
  {/if}
</div>

<style>
  /* Mirror the base dropdown/item styling from App.svelte — Svelte's scoped
     styles don't cross component boundaries, so without these duplicates the
     menu renders as flow-layout pill buttons. Keep these values in sync with
     .action-dropdown / .action-item in App.svelte. */
  .action-dropdown {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 4px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
    min-width: 160px;
    z-index: 100;
    overflow: hidden;
  }

  .action-item {
    display: flex;
    align-items: center;
    width: 100%;
    padding: 8px 14px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    text-align: left;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.1s;
    white-space: nowrap;
  }

  .action-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  /* Launcher-specific layout additions. */
  .launcher-menu {
    min-width: 220px;
  }

  .lm-sep {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }

  .lm-icon {
    display: inline-block;
    width: 18px;
    text-align: center;
    margin-right: 10px;
    color: var(--text-dim);
    flex: 0 0 auto;
  }

  .lm-icon-mono {
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 11px;
    font-weight: 600;
  }

  .lm-arrow {
    margin-left: auto;
    padding-left: 10px;
    color: var(--text-dim);
    font-size: 10px;
    flex: 0 0 auto;
  }

  .lm-active {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .lm-sub-container {
    position: relative;
  }

  /* Overflow has to release for submenus to escape the parent dropdown —
     action-dropdown above uses overflow: hidden which otherwise clips them. */
  .launcher-menu {
    overflow: visible;
  }

  /* Default (repo-row kebab): parent menu is pinned right: 0 so it extends
     to the left of the kebab. Open the submenu further left so it stays on
     screen. */
  .launcher-submenu {
    position: absolute;
    top: -4px;
    right: 100%;
    margin-right: 2px;
    min-width: 180px;
    z-index: 101;
  }

  /* Account-header kebab: parent menu is pinned left: 0 (see
     .source-header-kebab .action-dropdown in App.svelte) so it extends to
     the right. Flip the submenu to also open right so it stays on screen. */
  :global(.source-header-kebab) .launcher-submenu {
    right: auto;
    left: 100%;
    margin-right: 0;
    margin-left: 2px;
  }
</style>
