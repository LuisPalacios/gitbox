<script lang="ts">
  // LauncherMenu — the shared ⋯ menu of repo rows and account headers. A flat
  // list in sections, no submenus (issue #138). Handlers come in as props, so
  // the menu stays dumb and has no bridge dependency.
  //
  //   Open               web page (browserLabel) and folder (revealLabel)
  //   Open with          every AI harness, editor and terminal profile (the
  //                      default profile and the Preferred ones), each with
  //                      its logo; the one the row's launch button uses is
  //                      tagged "default"
  //   Maintenance        repo menu, Advanced: sweep, move, container toggle,
  //                      re-scan (shown when their handlers are passed)
  //   Account            account menu: Find, New and extraItems
  //   Delete from this computer…   repo menu, when onDelete is passed
  //
  // Simple passes no profiles and no maintenance handlers.

  import { onMount } from 'svelte';
  import type { EditorInfo, AIHarnessInfo, TerminalProfileInfo } from './types';
  import BrandIcon, { terminalLogo, harnessLogo, editorLogo } from './BrandIcon.svelte';
  import UIIcon from './UIIcon.svelte';
  import { t } from './i18n';

  export let kind: 'repo' | 'account' = 'repo';
  export let editors: EditorInfo[] = [];
  // profiles is the v2.1 Profile list; the menu's terminal section reads
  // from it.
  export let profiles: TerminalProfileInfo[] = [];
  export let aiHarnesses: AIHarnessInfo[] = [];
  // revealLabel is the OS-aware string for the folder action: "Reveal in
  // Explorer" on Windows, "Reveal in Finder" on macOS, "Reveal in files"
  // on Linux. Falls back to a generic phrasing so missing wiring still
  // renders something coherent.
  export let revealLabel: string = '';
  $: revealText = revealLabel || $t('launcher.revealDefault');

  export let onOpenBrowser: () => void;
  export let onOpenFolder: () => void;
  export let onOpenApp: (command: string) => void;
  // onOpenProfile is the v2.1 launch handler — receives the Profile id and
  // routes through bridge.openProfile / bridge.openAccountProfile in the
  // App.svelte glue.
  export let onOpenProfile: (profileID: string) => void = () => {};
  export let onOpenAIHarness: (harness: AIHarnessInfo) => void;
  export let onSweep: (() => void) | null = null;
  // onMove is the "Move repository…" action (issue #64). Shown only on
  // the repo kebab. moveEnabled gates the click; when false the entry is
  // rendered disabled with moveDisabledReason as the tooltip so users
  // learn WHY the action isn't available.
  export let onMove: (() => void) | null = null;
  export let moveEnabled: boolean = true;
  export let moveDisabledReason: string = '';
  // onToggleContainer marks/unmarks a repo as a multi-repo container. Shown
  // only on the repo kebab when provided; isContainer drives the label.
  export let onToggleContainer: (() => void) | null = null;
  export let isContainer: boolean = false;
  // onRescanContainer re-runs the nested-clone scan for a container repo. Shown
  // only when the repo is already a container.
  export let onRescanContainer: (() => void) | null = null;
  // onFind / onCreate are the account card's Find and New actions (issue
  // #123), repeated on the account kebab so they stay reachable while the
  // cards panel is collapsed. *Enabled gates the click; *DisabledReason is
  // the tooltip, like the Move entry.
  export let onFind: (() => void) | null = null;
  export let findEnabled: boolean = true;
  export let findDisabledReason: string = '';
  export let onCreate: (() => void) | null = null;
  export let createEnabled: boolean = true;
  export let createDisabledReason: string = '';
  // Simple mode (issue #124) passes plain labels and moves "delete local
  // clone" from the top bar's delete mode into the row menu.
  export let browserLabelOverride: string = '';
  export let onDelete: (() => void) | null = null;
  export let deleteLabel: string = '';
  // Extra account actions after Find and New (Edit account, Change
  // credential or Reconnect). icon is a short glyph.
  export let extraItems: { label: string; icon: string; onClick: () => void; title?: string }[] = [];

  // Root element of the dropdown, used to measure and place it.
  let rootEl: HTMLDivElement | undefined;

  const VIEWPORT_PADDING = 8;
  const TRIGGER_GAP = 4;

  // The menu is position: fixed and placed in viewport coordinates, so the
  // scrolling repo list (overflow-y: auto) can't clip it. It anchors to the
  // .action-menu-container that holds its ⋯ button.
  function positionMenu(el: HTMLElement | undefined) {
    if (!el) return;
    const trigger = el.parentElement?.closest('.action-menu-container');
    if (!trigger) return;
    // Reset the size cap so measurement reflects the menu's natural height.
    el.style.maxHeight = '';
    el.style.overflowY = '';

    const r = trigger.getBoundingClientRect();
    const vh = window.innerHeight;
    const vw = window.innerWidth;
    const w = el.offsetWidth;
    const h = el.offsetHeight;

    // Horizontal: right edge on the trigger's right edge. Repo rows share a
    // column of ⋯ buttons, so the repo menu slides off that column (right
    // edge on the trigger's left edge) and a neighbouring ⋯ stays clickable.
    // Clamp inside the viewport either way.
    const anchorRight = kind === 'repo' ? r.left : r.right;
    const left = Math.max(VIEWPORT_PADDING, Math.min(anchorRight - w, vw - VIEWPORT_PADDING - w));

    // Vertical: below the trigger when it fits, else above it, else slid up
    // just enough to fit. If even the viewport is too short, cap the height
    // and scroll inside.
    const below = r.bottom + TRIGGER_GAP;
    const above = r.top - TRIGGER_GAP - h;
    let top: number;
    if (below + h <= vh - VIEWPORT_PADDING) {
      top = below;
    } else if (above >= VIEWPORT_PADDING) {
      top = above;
    } else {
      top = Math.max(VIEWPORT_PADDING, vh - VIEWPORT_PADDING - h);
      if (h > vh - 2 * VIEWPORT_PADDING) {
        el.style.maxHeight = `${vh - 2 * VIEWPORT_PADDING}px`;
        el.style.overflowY = 'auto';
      }
    }

    el.style.left = `${Math.round(left)}px`;
    el.style.top = `${Math.round(top)}px`;
  }

  // Re-place the menu every frame while it is open: the list scrolls, the
  // window resizes, and rows re-sort under it when their status changes.
  onMount(() => {
    let frame = 0;
    const follow = () => { positionMenu(rootEl); frame = requestAnimationFrame(follow); };
    follow();
    return () => cancelAnimationFrame(frame);
  });

  // The menu lists the default profile plus the Preferred ones (Settings →
  // Terminals → Manager); hidden profiles stay out.
  $: visibleProfiles = (profiles || []).filter(p => !p.hidden);
  $: defaultProfile = visibleProfiles.find(p => p.default) || visibleProfiles[0] || null;
  $: menuProfiles = visibleProfiles.filter(p => p === defaultProfile || p.preferred);
  $: hasOpenWith = aiHarnesses.length > 0 || editors.length > 0 || menuProfiles.length > 0;
  $: hasMaintenance = kind === 'repo' && (!!onSweep || !!onMove || !!onToggleContainer);
  $: hasAccountActions = kind === 'account' && (!!onFind || !!onCreate || extraItems.length > 0);

  // Browser action label is kind-aware: repo kebab navigates to the repo
  // page on the provider; account kebab navigates to the account profile.
  $: browserLabel = browserLabelOverride || (kind === 'account' ? $t('launcher.navigateAccount') : $t('launcher.navigateRepo'));
</script>

<div class="action-dropdown launcher-menu" role="menu" bind:this={rootEl}>
  <div class="lm-sec">{$t('menu.open')}</div>
  <button class="action-item" role="menuitem" on:click|stopPropagation={onOpenBrowser} title={browserLabel}>
    <span class="lm-icon">&#8599;</span> {browserLabel}
  </button>
  <button class="action-item" role="menuitem" on:click|stopPropagation={onOpenFolder} title={revealText}>
    <span class="lm-icon">&#9636;</span> {revealText}
  </button>

  {#if hasOpenWith}
    <div class="lm-sec">{$t('menu.openWith')}</div>
    {#each aiHarnesses as harness, i}
      <button class="action-item" role="menuitem" on:click|stopPropagation={() => onOpenAIHarness(harness)} title={harness.name}>
        <span class="lm-icon lm-icon-brand"><BrandIcon name={harnessLogo(harness)} /></span> {harness.name}
        {#if i === 0}<span class="lm-tag">{$t('menu.default')}</span>{/if}
      </button>
    {/each}
    {#each editors as editor, i}
      <button class="action-item" role="menuitem" on:click|stopPropagation={() => onOpenApp(editor.command)} title={editor.name}>
        <span class="lm-icon lm-icon-brand"><BrandIcon name={editorLogo(editor)} /></span> {editor.name}
        {#if i === 0}<span class="lm-tag">{$t('menu.default')}</span>{/if}
      </button>
    {/each}
    {#each menuProfiles as profile}
      <button class="action-item" role="menuitem" on:click|stopPropagation={() => onOpenProfile(profile.id)} title={profile.name}>
        <span class="lm-icon lm-icon-brand"><BrandIcon name={terminalLogo(profile.terminal, profile.shell)} /></span> {profile.name}
        {#if profile === defaultProfile}<span class="lm-tag">{$t('menu.default')}</span>{/if}
      </button>
    {/each}
  {/if}

  {#if hasMaintenance}
    <div class="lm-sec">{$t('menu.maintenance')}</div>
    {#if onSweep}
      <button class="action-item" role="menuitem" on:click|stopPropagation={onSweep}>
        <span class="lm-icon">&#8997;</span> {$t('launcher.sweep')}
      </button>
    {/if}
    {#if onMove}
      <button class="action-item" class:lm-disabled={!moveEnabled} role="menuitem" disabled={!moveEnabled}
        on:click|stopPropagation={onMove}
        title={moveEnabled ? $t('launcher.moveTip') : (moveDisabledReason || $t('launcher.moveNeedsClean'))}>
        <span class="lm-icon">&#8644;</span> {$t('launcher.move')}
      </button>
    {/if}
    {#if onToggleContainer}
      <button class="action-item" role="menuitem" on:click|stopPropagation={onToggleContainer}
        title={$t('launcher.containerTip')}>
        <span class="lm-icon">&#9638;</span>
        {isContainer ? $t('launcher.unmarkContainer') : $t('launcher.markContainer')}
      </button>
      {#if isContainer && onRescanContainer}
        <button class="action-item" role="menuitem" on:click|stopPropagation={onRescanContainer}
          title={$t('launcher.rescanTip')}>
          <span class="lm-icon"><UIIcon name="fetch" /></span> {$t('launcher.rescan')}
        </button>
      {/if}
    {/if}
  {/if}

  {#if hasAccountActions}
    <div class="lm-sec">{$t('menu.account')}</div>
    {#if onFind}
      <button class="action-item" class:lm-disabled={!findEnabled} role="menuitem" disabled={!findEnabled}
        on:click|stopPropagation={onFind}
        title={findEnabled ? $t('launcher.findTip') : findDisabledReason}>
        <span class="lm-icon"><UIIcon name="search" /></span> {$t('account.findProjects')}…
      </button>
    {/if}
    {#if onCreate}
      <button class="action-item" class:lm-disabled={!createEnabled} role="menuitem" disabled={!createEnabled}
        on:click|stopPropagation={onCreate}
        title={createEnabled ? $t('launcher.createTip') : createDisabledReason}>
        <span class="lm-icon"><UIIcon name="plus" /></span> {$t('account.createRepo')}…
      </button>
    {/if}
    {#each extraItems as item}
      <button class="action-item" role="menuitem" on:click|stopPropagation={item.onClick} title={item.title || item.label}>
        <span class="lm-icon">{item.icon}</span> {item.label}
      </button>
    {/each}
  {/if}

  {#if kind === 'repo' && onDelete}
    <hr class="lm-sep" />
    <button class="action-item lm-danger" role="menuitem" on:click|stopPropagation={onDelete} title={deleteLabel || $t('menu.delete')}>
      <span class="lm-icon"><UIIcon name="x" /></span> {deleteLabel || $t('menu.delete')}
    </button>
  {/if}
</div>

<style>
  /* The dropdown and item styling lives here: Svelte's scoped styles don't
     cross component boundaries, so the menu owns it (PRPopover keeps its own
     copy for its wider popover). The menu is fixed so scrolling ancestors
     can't clip it; positionMenu() sets top/left in viewport coordinates. */
  .action-dropdown {
    position: fixed;
    top: 0;
    left: 0;
    min-width: 240px;
    max-width: 340px;
    padding: 5px;
    background: var(--bg-card);
    border: 1px solid var(--border-hover);
    border-radius: 9px;
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.25);
    z-index: 100;
    display: flex;
    flex-direction: column;
    /* Safety net: on very small viewports, scroll inside the menu instead
       of clipping below the fold. */
    max-height: calc(100vh - 16px);
    overflow-y: auto;
  }

  .lm-sec {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-dim);
    padding: 7px 9px 3px;
  }

  .action-item {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 6px 9px;
    border: none;
    border-radius: 6px;
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

  .lm-sep {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 4px 2px;
  }

  .lm-icon {
    width: var(--icon);
    height: var(--icon);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--text-dim);
    font-size: 13px;
    flex: 0 0 auto;
  }

  /* Brand logos: the dark-theme single-colour version follows the item's
     text colour, hover included. */
  .lm-icon-brand {
    color: inherit;
  }

  /* The launcher the row's own button uses. */
  .lm-tag {
    margin-left: auto;
    padding-left: 12px;
    font-size: 10px;
    color: var(--text-dim);
  }

  .lm-danger,
  .lm-danger .lm-icon,
  .lm-danger:hover {
    color: var(--alert);
  }

  .lm-disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .lm-disabled:hover {
    background: transparent;
  }
</style>
