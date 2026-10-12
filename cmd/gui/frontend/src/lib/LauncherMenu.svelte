<script lang="ts">
  // LauncherMenu — shared action menu used by the repo-row kebab and the
  // account-header kebab in full view. Renders a top section with the most-
  // used launchers (default Profile, editors[0], ai_harnesses[0]) and a
  // submenu section for the rest of each category. Handlers are passed in
  // as props so this component stays dumb and has no bridge dependency.
  //
  // Profile model (issue #69, v2.1):
  //   - The terminal entry shows "Open in <default profile name>" — the
  //     profile flagged Default in Settings → Terminals → Manager.
  //   - The Profiles submenu lists every Preferred profile (excluding the
  //     default to avoid duplication). Hidden when no preferred profiles
  //     exist.
  //
  // Labels are intentionally terse — every row leads with an icon, so the
  // verb (Open / Open in / Open with) is redundant noise. Issue #69 user
  // feedback: drop the prefix everywhere.
  //
  // Visual layout (both repo and account kebabs):
  //   "Navigate to <repo|account>"        (icon: globe)
  //   "Reveal in <Explorer|Finder|files>" (icon: folder; OS-aware via revealLabel)
  //   ─ separator ─ (only if any default is shown)
  //   "<defaultProfile.name>"             (no prefix; hidden if no profile + no legacy term)
  //   "Open with <editors[0].name>"       (hidden if editors empty)
  //   "Open with <aiHarnesses[0].name>"   (hidden if harnesses empty)
  //   ─ separator ─ (only if any submenu is shown)
  //   Profiles ▸                          (hidden if <2 visible profiles)
  //   Editors ▸                           (hidden if <2 editors)
  //   AI Harnesses ▸                      (hidden if <2 harnesses)
  //   ─ separator ─ (repo kebab only, when onSweep is provided)
  //   Sweep branches                      (repo kebab only)
  //   ─ separator ─ (account kebab only, when onFind/onCreate are provided)
  //   Find projects / Create repo         (account kebab only)

  import { onMount, tick } from 'svelte';
  import type { EditorInfo, AIHarnessInfo, TerminalProfileInfo } from './types';

  export let kind: 'repo' | 'account' = 'repo';
  export let editors: EditorInfo[] = [];
  // profiles is the v2.1 Profile list; the menu's terminal section reads
  // from it.
  export let profiles: TerminalProfileInfo[] = [];
  export let aiHarnesses: AIHarnessInfo[] = [];
  // revealLabel is the OS-aware string for the folder action: "Reveal in
  // Explorer" on Windows, "Reveal in Finder" on macOS, "Reveal in files"
  // on Linux. Defaulted to a generic phrasing so missing wiring still
  // renders something coherent.
  export let revealLabel: string = 'Reveal in file manager';

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
  export let deleteLabel: string = 'Delete from this computer…';

  type Sub = 'terminals' | 'editors' | 'ai' | 'workspaces' | null;
  let openSubmenu: Sub = null;

  // Root element of the dropdown, used to measure and flip/shift into view.
  let rootEl: HTMLDivElement | undefined;
  // Submenu element (only one is open at a time).
  let subEl: HTMLDivElement | undefined;

  const VIEWPORT_PADDING = 8;
  const TRIGGER_GAP = 4;

  // The menu is position: fixed and placed in viewport coordinates, so the
  // scrolling repo list (overflow-y: auto) can't clip it — an absolute menu
  // flipped up near the top of the list was cut off by the list's edge.
  function positionMenu(el: HTMLElement | undefined) {
    if (!el) return;
    const trigger = el.parentElement?.parentElement;
    if (!trigger) return;
    // Reset the size cap so measurement reflects the menu's natural height.
    el.style.maxHeight = '';
    el.style.overflowY = '';

    const t = trigger.getBoundingClientRect();
    const vh = window.innerHeight;
    const vw = window.innerWidth;
    const w = el.offsetWidth;
    const h = el.offsetHeight;

    // Horizontal: right edge on the trigger's right edge. Repo kebab rows
    // share a vertical column of kebabs — one per row in the same group and
    // the next — so the repo menu slides off that column (right edge on the
    // trigger's left edge) and the user can click neighbouring kebabs
    // without dismissing this menu first. Account-header kebabs stand alone
    // and don't need the shift. Clamp inside the viewport either way.
    const anchorRight = kind === 'repo' ? t.left : t.right;
    const left = Math.max(VIEWPORT_PADDING, Math.min(anchorRight - w, vw - VIEWPORT_PADDING - w));

    // Vertical: open below the trigger when it fits, else above it, else
    // slide it up just enough to sit fully inside the viewport. If even the
    // whole viewport is too short, cap the height and scroll inside.
    const below = t.bottom + TRIGGER_GAP;
    const above = t.top - TRIGGER_GAP - h;
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

  // Keep the fixed menu attached to its kebab while the repo list scrolls or
  // the window resizes. Scrolls inside the menu itself don't move the anchor.
  function onViewportChange(e: Event) {
    if (rootEl && e.target instanceof Node && rootEl.contains(e.target)) return;
    positionMenu(rootEl);
  }

  function positionSubmenu(el: HTMLElement | undefined) {
    if (!el) return;
    el.style.top = '';
    el.style.bottom = '';
    el.style.left = '';
    el.style.right = '';
    el.style.marginLeft = '';
    el.style.marginRight = '';
    el.style.transform = '';

    const vh = window.innerHeight;
    const vw = window.innerWidth;

    // The submenu is anchored to its own lm-sub-container (the parent
    // menu item). Use the container's position to decide which side has
    // more room. This is more reliable than "flip only if the default
    // side overflows" because on narrow viewports both sides can overflow
    // and we need to pick the larger one up front.
    const container = el.parentElement;
    const anchorRect = container ? container.getBoundingClientRect() : el.getBoundingClientRect();
    const submenuWidth = el.offsetWidth;
    const spaceLeft = anchorRect.left;
    const spaceRight = vw - anchorRect.right;
    const preferRight = spaceRight >= submenuWidth + VIEWPORT_PADDING
      || (spaceRight > spaceLeft && spaceLeft < submenuWidth + VIEWPORT_PADDING);

    if (preferRight) {
      el.style.right = 'auto';
      el.style.left = '100%';
      el.style.marginRight = '0';
      el.style.marginLeft = '2px';
    } else {
      el.style.left = 'auto';
      el.style.right = '100%';
      el.style.marginLeft = '0';
      el.style.marginRight = '2px';
    }

    // Horizontal clamp: if neither side has full room, shift with a
    // transform so the whole submenu stays on screen. May overlap the
    // parent menu, but that's preferable to being clipped off-screen.
    const rect = el.getBoundingClientRect();
    let dx = 0;
    if (rect.right > vw - VIEWPORT_PADDING) {
      dx = vw - VIEWPORT_PADDING - rect.right;
    } else if (rect.left < VIEWPORT_PADDING) {
      dx = VIEWPORT_PADDING - rect.left;
    }

    // Vertical clamp: keep the submenu inside the viewport top and bottom.
    let dy = 0;
    if (rect.bottom > vh - VIEWPORT_PADDING) {
      dy = vh - VIEWPORT_PADDING - rect.bottom;
    } else if (rect.top < VIEWPORT_PADDING) {
      dy = VIEWPORT_PADDING - rect.top;
    }

    if (dx !== 0 || dy !== 0) {
      el.style.transform = `translate(${Math.round(dx)}px, ${Math.round(dy)}px)`;
    }
  }

  onMount(() => {
    tick().then(() => positionMenu(rootEl));
    window.addEventListener('scroll', onViewportChange, true);
    window.addEventListener('resize', onViewportChange);
    return () => {
      window.removeEventListener('scroll', onViewportChange, true);
      window.removeEventListener('resize', onViewportChange);
    };
  });

  async function toggleSub(name: Exclude<Sub, null>) {
    openSubmenu = openSubmenu === name ? null : name;
    if (openSubmenu) {
      await tick();
      positionSubmenu(subEl);
    }
  }

  // Profile-driven terminal section. visibleProfiles excludes Hidden ones
  // (the Gear panel keeps them in config but they're not surfaced here).
  $: visibleProfiles = (profiles || []).filter(p => !p.hidden);
  $: defaultProfile = visibleProfiles.find(p => p.default) || visibleProfiles[0] || null;
  $: preferredProfiles = visibleProfiles.filter(p => p.preferred && (!defaultProfile || p.id !== defaultProfile.id));

  $: showProfileDefault = !!defaultProfile;
  $: showEditorDefault = editors.length >= 1;
  $: showHarnessDefault = aiHarnesses.length >= 1;

  $: showProfilesSub = preferredProfiles.length >= 1;
  $: showEditorsSub = editors.length >= 2;
  $: showHarnessSub = aiHarnesses.length >= 2;

  $: hasDefaultsSection = showProfileDefault || showEditorDefault || showHarnessDefault;
  $: hasSubsSection = showProfilesSub || showEditorsSub || showHarnessSub;
  $: hasSweepSection = kind === 'repo' && !!onSweep;
  $: hasMoveSection = kind === 'repo' && !!onMove;
  $: hasContainerSection = kind === 'repo' && !!onToggleContainer;
  $: hasAccountActions = kind === 'account' && (!!onFind || !!onCreate);

  // Browser action label is kind-aware: repo kebab navigates to the repo
  // page on the provider; account kebab navigates to the account profile.
  $: browserLabel = browserLabelOverride || (kind === 'account' ? 'Navigate to account' : 'Navigate to repo');
</script>

<div class="action-dropdown launcher-menu" bind:this={rootEl}>
  <button class="action-item" on:click|stopPropagation={onOpenBrowser} title={browserLabel}>
    <span class="lm-icon">&#127760;</span> {browserLabel}
  </button>
  <button class="action-item" on:click|stopPropagation={onOpenFolder} title={revealLabel}>
    <span class="lm-icon">&#128193;</span> {revealLabel}
  </button>

  {#if hasDefaultsSection}
    <hr class="lm-sep" />
    {#if showProfileDefault && defaultProfile}
      <button class="action-item" on:click|stopPropagation={() => onOpenProfile(defaultProfile.id)} title={defaultProfile.name}>
        <span class="lm-icon lm-icon-mono">&gt;_</span> {defaultProfile.name}
      </button>
    {/if}
    {#if showEditorDefault}
      <button class="action-item" on:click|stopPropagation={() => onOpenApp(editors[0].command)} title="Open with {editors[0].name}">
        <span class="lm-icon">&#9998;</span> Open with {editors[0].name}
      </button>
    {/if}
    {#if showHarnessDefault}
      <button class="action-item" on:click|stopPropagation={() => onOpenAIHarness(aiHarnesses[0])} title="Open with {aiHarnesses[0].name}">
        <span class="lm-icon">&#129302;</span> Open with {aiHarnesses[0].name}
      </button>
    {/if}
  {/if}

  {#if hasSubsSection}
    <hr class="lm-sep" />
    {#if showProfilesSub}
      <div class="lm-sub-container">
        <button class="action-item lm-submenu-trigger" class:lm-active={openSubmenu === 'terminals'} on:click|stopPropagation={() => toggleSub('terminals')}>
          <span class="lm-icon lm-icon-mono">&gt;_</span> Profiles
          <span class="lm-arrow">&#9654;</span>
        </button>
        {#if openSubmenu === 'terminals'}
          <div class="action-dropdown launcher-submenu" bind:this={subEl}>
            {#each preferredProfiles as profile}
              <button class="action-item" on:click|stopPropagation={() => onOpenProfile(profile.id)}>
                <span class="lm-icon lm-icon-mono">&gt;_</span> {profile.name}
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
          <div class="action-dropdown launcher-submenu" bind:this={subEl}>
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
          <div class="action-dropdown launcher-submenu" bind:this={subEl}>
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

  {#if hasMoveSection}
    <hr class="lm-sep" />
    {#if moveEnabled}
      <button class="action-item" on:click|stopPropagation={onMove} title="Move this repo to another account or provider">
        <span class="lm-icon">&#8644;</span> Move repository…
      </button>
    {:else}
      <button class="action-item lm-disabled" disabled title={moveDisabledReason || 'Move requires a clean, in-sync clone'}>
        <span class="lm-icon">&#8644;</span> Move repository…
      </button>
    {/if}
  {/if}

  {#if hasContainerSection}
    <hr class="lm-sep" />
    {#if isContainer && onRescanContainer}
      <button class="action-item" on:click|stopPropagation={onRescanContainer}
        title="Re-scan this container for nested clones to onboard">
        <span class="lm-icon">&#8635;</span> Re-scan for nested clones
      </button>
    {/if}
    <button class="action-item" on:click|stopPropagation={onToggleContainer}
      title="A container repo holds nested clones in its working tree; gitbox discovers and onboards them">
      <span class="lm-icon">&#128193;</span>
      {isContainer ? 'Unmark as multi-repo container' : 'Mark as multi-repo container'}
    </button>
  {/if}

  {#if kind === 'repo' && onDelete}
    <hr class="lm-sep" />
    <button class="action-item" on:click|stopPropagation={onDelete} title={deleteLabel}>
      <span class="lm-icon">&#128465;</span> {deleteLabel}
    </button>
  {/if}

  {#if hasAccountActions}
    <hr class="lm-sep" />
    {#if onFind}
      <button class="action-item" class:lm-disabled={!findEnabled} disabled={!findEnabled}
        on:click|stopPropagation={onFind}
        title={findEnabled ? 'Discover repos from provider API' : findDisabledReason}>
        <span class="lm-icon">&#128269;</span> Find projects
      </button>
    {/if}
    {#if onCreate}
      <button class="action-item" class:lm-disabled={!createEnabled} disabled={!createEnabled}
        on:click|stopPropagation={onCreate}
        title={createEnabled ? 'Create a new repo on the provider' : createDisabledReason}>
        <span class="lm-icon">&#65291;</span> Create repo
      </button>
    {/if}
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
    /* Safety net: on very small viewports, scroll inside the menu instead
       of clipping below the fold. The flip/shift logic above handles the
       normal case; this only kicks in when no side has enough space. */
    max-height: calc(100vh - 16px);
    overflow-y: auto;
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

  /* Launcher-specific layout additions. The root menu is fixed so scrolling
     ancestors can't clip it; positionMenu() sets top/left in viewport
     coordinates. Submenus stay absolute inside it. */
  .launcher-menu {
    min-width: 220px;
    position: fixed;
    top: 0;
    left: 0;
    right: auto;
    margin: 0;
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

  .lm-disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .lm-disabled:hover {
    background: transparent;
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
