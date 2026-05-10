<script lang="ts">
  // TerminalsModal — modal dialog that owns the v2.1 Terminal Profile editor
  // (issue #69). Replaces the inline expandable Gear-panel section: the
  // Profile table can grow to 20+ rows on a developer host (Windows
  // Terminal × N WT profiles + WezTerm × launch_menu entries + composed
  // terminal × shell pairs) and embedding that vertically inside the
  // settings panel made the panel itself unscrollable. Modal body has its
  // own overflow.

  import type { TerminalAppInfo, ShellInfo, TerminalProfileInfo } from './types';
  import { bridge } from './bridge';
  import { fade, slide } from 'svelte/transition';

  export let open = false;
  // mode='overlay' renders the editor as an in-app modal with a scrim
  // (the original behaviour). mode='window' assumes the host is a
  // dedicated OS window (the gitbox.exe --terminals-window sub-process)
  // and skips the overlay/close-button — the OS window chrome already
  // provides the close affordance.
  export let mode: 'overlay' | 'window' = 'overlay';
  export let apps: TerminalAppInfo[] = [];
  export let shells: ShellInfo[] = [];
  export let profiles: TerminalProfileInfo[] = [];
  export let onSave: (apps: TerminalAppInfo[], shells: ShellInfo[], profiles: TerminalProfileInfo[]) => Promise<void>;
  export let onConfigReloaded: () => void = () => {};
  export let onClose: () => void = () => {};

  let busy = false;
  let statusMsg = '';
  let missingModern = false;
  let isWindows = false;

  // Detect host OS once on mount so the Add form can choose between the
  // OS-aware shapes (Windows = mandatory shell selector, mac/Linux = optional
  // shell, blank means "login shell"). Issue #71.
  import { onMount } from 'svelte';
  onMount(async () => {
    try {
      const goos = await bridge.getOS();
      isWindows = goos === 'windows';
      missingModern = await bridge.missingModernTerminal();
    } catch {
      // Best-effort — banner stays hidden, Add form falls back to mandatory shell.
    }
  });

  // Local working draft, refreshed reactively on prop change so a
  // Re-detect / external save updates the table on the next tick.
  let draftApps: TerminalAppInfo[] = [];
  let draftShells: ShellInfo[] = [];
  let draftProfiles: TerminalProfileInfo[] = [];
  $: draftApps = apps.map(a => ({ ...a, args_template: [...(a.args_template || [])] }));
  $: draftShells = shells.map(s => ({ ...s, args: [...(s.args || [])] }));
  $: draftProfiles = profiles.map(p => ({ ...p, args: [...(p.args || [])] }));

  // Inline editor state.
  let editingId: string | null = null;
  let editName = '';
  let editTerminal = '';
  let editShell = '';

  // Add-form state. On Windows the shell field is required (Profiles pair a
  // Terminal with a Shell). On macOS/Linux the shell defaults to "" — the
  // login shell — so the Add row matches the Terminal-only auto-Profiles;
  // power users can still override with an explicit shell pick.
  type Draft = { name: string; terminal: string; shell: string };
  let addDraft: Draft | null = null;

  function appName(id: string): string {
    return draftApps.find(a => a.id === id)?.name ?? id ?? '—';
  }
  function shellName(id: string): string {
    // Empty shell on Profile rows means "login shell" on macOS/Linux —
    // render that as a hint so the dim cell tells the user what'll launch
    // instead of an opaque em-dash.
    if (!id) return isWindows ? '—' : 'login shell';
    return draftShells.find(s => s.id === id)?.name ?? id;
  }

  async function setDefault(id: string) {
    draftProfiles = draftProfiles.map(p => ({ ...p, default: p.id === id }));
    await persist('Default updated');
  }

  async function togglePreferred(id: string) {
    draftProfiles = draftProfiles.map(p => p.id === id ? { ...p, preferred: !p.preferred } : p);
    await persist('Preferred updated');
  }

  async function toggleHidden(id: string) {
    draftProfiles = draftProfiles.map(p => p.id === id ? { ...p, hidden: !p.hidden } : p);
    await persist('Visibility updated');
  }

  function startEdit(p: TerminalProfileInfo) {
    editingId = p.id;
    editName = p.name;
    editTerminal = p.terminal;
    editShell = p.shell;
  }

  function cancelEdit() {
    editingId = null;
  }

  async function saveEdit() {
    if (!editingId) return;
    draftProfiles = draftProfiles.map(p => p.id === editingId
      ? { ...p, name: editName.trim() || p.name, terminal: editTerminal, shell: editShell }
      : p);
    editingId = null;
    await persist('Profile updated');
  }

  function startAdd() {
    // macOS/Linux: shell defaults to "" (login shell); Windows: first shell.
    const defaultShell = isWindows ? (draftShells[0]?.id ?? '') : '';
    addDraft = { name: '', terminal: draftApps[0]?.id ?? '', shell: defaultShell };
  }

  function cancelAdd() {
    addDraft = null;
  }

  async function saveAdd() {
    if (!addDraft) return;
    const name = addDraft.name.trim();
    // Windows requires both Terminal AND Shell; mac/Linux requires only
    // Terminal (Shell="" means login shell).
    if (!name || !addDraft.terminal) return;
    if (isWindows && !addDraft.shell) return;
    const id = nextProfileID(addDraft.terminal, addDraft.shell, name);
    const fresh: TerminalProfileInfo = {
      id,
      name,
      terminal: addDraft.terminal,
      shell: addDraft.shell,
      args: [],
      default: false,
      preferred: false,
      hidden: false,
      source: 'user',
    };
    draftProfiles = [...draftProfiles, fresh];
    addDraft = null;
    await persist('Profile added');
  }

  function nextProfileID(terminal: string, shell: string, name: string): string {
    const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    let base = slug(`${terminal}-${shell}-${name}`);
    if (!base) base = `user-${Date.now()}`;
    let id = base, n = 1;
    const taken = new Set(draftProfiles.map(p => p.id));
    while (taken.has(id)) {
      n += 1;
      id = `${base}-${n}`;
    }
    return id;
  }

  async function deleteProfile(p: TerminalProfileInfo) {
    if (p.source !== 'user') return;
    draftProfiles = draftProfiles.filter(q => q.id !== p.id);
    await persist('Profile deleted');
  }

  async function persist(label: string) {
    if (busy) return;
    busy = true;
    statusMsg = '';
    try {
      await onSave(draftApps, draftShells, draftProfiles);
      statusMsg = label;
    } catch (e: any) {
      statusMsg = `Save failed: ${e?.message || e}`;
    } finally {
      busy = false;
    }
  }

  async function redetect() {
    if (busy) return;
    busy = true;
    statusMsg = '';
    try {
      await bridge.redetectProfiles();
      onConfigReloaded();
      statusMsg = 'Re-detected';
    } catch (e: any) {
      statusMsg = `Re-detect failed: ${e?.message || e}`;
    } finally {
      busy = false;
    }
  }

  function close() {
    if (addDraft) addDraft = null;
    if (editingId) editingId = null;
    onClose();
  }
</script>

{#if open}
  <div
    class:overlay={mode === 'overlay'}
    class:tp-window-host={mode === 'window'}
    on:click={mode === 'overlay' ? close : undefined}
    transition:fade={{ duration: 120 }}
  >
    <div
      class="modal modal-tprofiles"
      class:tp-modal-window={mode === 'window'}
      on:click|stopPropagation
      transition:slide={{ duration: 180 }}
    >
      {#if mode === 'overlay'}
        <div class="modal-head">
          <h3>Terminals &amp; shells</h3>
          <button class="btn-x" on:click={close} aria-label="Close">&#10005;</button>
        </div>
      {/if}
      <!-- Toolbar lives outside .tp-body so the scrollable area cannot
           render under it (issue #69 user feedback — sticky-inside-scroller
           let rows bleed behind the buttons). -->
      <div class="tp-toolbar">
        <button class="tp-btn" type="button" on:click={redetect} disabled={busy}>Re-detect</button>
        <button class="tp-btn" type="button" on:click={startAdd} disabled={busy || !!addDraft}>+ Add profile</button>
        {#if statusMsg}
          <span class="tp-status">{statusMsg}</span>
        {/if}
      </div>
      <div class="modal-body tp-body">
        {#if missingModern}
          <div class="tp-banner" role="status">
            <strong>Install Windows Terminal for the best experience.</strong>
            gitbox is using bare-shell launches as a fallback. A modern terminal
            (Windows Terminal, WezTerm, Alacritty, …) hosts shells better and
            unlocks the per-Profile launch flow.
          </div>
        {/if}
        <h4 class="tp-h4 tp-h4-first">Detected terminals</h4>
        {#if draftApps.length === 0}
          <p class="tp-empty">No terminal apps detected. Click <em>Re-detect</em> after installing one.</p>
        {:else}
          <table class="tp-table tp-table-rotw">
            <thead><tr><th>Name</th><th>Command</th></tr></thead>
            <tbody>
              {#each draftApps as app (app.id)}
                <tr>
                  <td>{app.name}</td>
                  <td class="tp-mono">{app.command}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}

        <h4 class="tp-h4">Detected shells</h4>
        {#if draftShells.length === 0}
          <p class="tp-empty">No shells detected.</p>
        {:else}
          <table class="tp-table tp-table-rotw">
            <thead><tr><th>Name</th><th>Command</th><th>Args</th></tr></thead>
            <tbody>
              {#each draftShells as shell (shell.id)}
                <tr>
                  <td>{shell.name}</td>
                  <td class="tp-mono">{shell.command}</td>
                  <td class="tp-mono">{(shell.args || []).join(' ')}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}

        <h4 class="tp-h4">Profiles</h4>
        {#if draftProfiles.length === 0 && !addDraft}
          <p class="tp-empty">No profiles yet. Click <em>+ Add profile</em> or <em>Re-detect</em>.</p>
        {:else}
          <table class="tp-table">
            <thead>
              <tr>
                <th class="tp-th-icon" title="Default — primary action of the per-row launcher">●</th>
                <th class="tp-th-icon" title="Preferred — shown in the launcher submenu">★</th>
                <th>Name</th>
                <th>Terminal</th>
                <th>Shell</th>
                <th class="tp-th-icon" title="Hidden — kept in config but not shown in the launcher">👁</th>
                <th class="tp-th-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {#each draftProfiles as p (p.id)}
                <tr class:tp-row-hidden={p.hidden}>
                  <td class="tp-td-icon">
                    <input type="radio" name="tp-default" checked={p.default} disabled={busy} on:change={() => setDefault(p.id)} />
                  </td>
                  <td class="tp-td-icon">
                    <button class="tp-icon-btn" class:tp-on={p.preferred} type="button" disabled={busy} on:click={() => togglePreferred(p.id)} title="Toggle preferred">★</button>
                  </td>
                  {#if editingId === p.id}
                    <td><input class="tp-input" bind:value={editName} placeholder="Name" /></td>
                    <td>
                      <select class="tp-input" bind:value={editTerminal}>
                        {#each draftApps as a (a.id)}
                          <option value={a.id}>{a.name}</option>
                        {/each}
                      </select>
                    </td>
                    <td>
                      <select class="tp-input" bind:value={editShell}>
                        {#if !isWindows}
                          <option value="">(login shell)</option>
                        {/if}
                        {#each draftShells as s (s.id)}
                          <option value={s.id}>{s.name}</option>
                        {/each}
                      </select>
                    </td>
                    <td class="tp-td-icon">
                      <button class="tp-icon-btn" type="button" disabled={busy} on:click={() => toggleHidden(p.id)} title="Toggle hidden">{p.hidden ? '🙈' : '👁'}</button>
                    </td>
                    <td class="tp-actions">
                      <button class="tp-btn tp-btn-small" type="button" disabled={busy} on:click={saveEdit}>Save</button>
                      <button class="tp-btn tp-btn-small tp-btn-ghost" type="button" disabled={busy} on:click={cancelEdit}>Cancel</button>
                    </td>
                  {:else}
                    <td>{p.name}</td>
                    <td class="tp-cell-dim">{appName(p.terminal)}</td>
                    <td class="tp-cell-dim">{shellName(p.shell)}</td>
                    <td class="tp-td-icon">
                      <button class="tp-icon-btn" type="button" disabled={busy} on:click={() => toggleHidden(p.id)} title="Toggle hidden">{p.hidden ? '🙈' : '👁'}</button>
                    </td>
                    <td class="tp-actions">
                      <button class="tp-icon-btn" type="button" disabled={busy} on:click={() => startEdit(p)} title="Edit">✎</button>
                      {#if p.source === 'user'}
                        <button class="tp-icon-btn tp-icon-danger" type="button" disabled={busy} on:click={() => deleteProfile(p)} title="Delete">🗑</button>
                      {/if}
                    </td>
                  {/if}
                </tr>
              {/each}
              {#if addDraft}
                <tr class="tp-row-add">
                  <td class="tp-td-icon">—</td>
                  <td class="tp-td-icon">—</td>
                  <td><input class="tp-input" bind:value={addDraft.name} placeholder="Display name" autofocus /></td>
                  <td>
                    <select class="tp-input" bind:value={addDraft.terminal}>
                      {#each draftApps as a (a.id)}
                        <option value={a.id}>{a.name}</option>
                      {/each}
                    </select>
                  </td>
                  <td>
                    <select class="tp-input" bind:value={addDraft.shell}>
                      {#if !isWindows}
                        <option value="">(login shell)</option>
                      {/if}
                      {#each draftShells as s (s.id)}
                        <option value={s.id}>{s.name}</option>
                      {/each}
                    </select>
                  </td>
                  <td class="tp-td-icon">—</td>
                  <td class="tp-actions">
                    <button class="tp-btn tp-btn-small" type="button" disabled={busy || !addDraft.name.trim() || !addDraft.terminal || (isWindows && !addDraft.shell)} on:click={saveAdd}>Add</button>
                    <button class="tp-btn tp-btn-small tp-btn-ghost" type="button" disabled={busy} on:click={cancelAdd}>Cancel</button>
                  </td>
                </tr>
              {/if}
            </tbody>
          </table>
        {/if}
      </div>
    </div>
  </div>
{/if}


<style>
  /* Modal scaffolding mirrors .overlay / .modal / .modal-head / .modal-body
     conventions in App.svelte so theme tokens stay consistent. */

  .modal-tprofiles {
    width: min(960px, 96vw);
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    min-height: 0;          /* allow inner flex child to shrink + scroll */
  }
  .modal-tprofiles :global(.modal-head) {
    flex: 0 0 auto;
  }

  /* Window mode: the host is an OS-native window already, so let the
     editor fill the whole viewport. Removes the modal "card" framing
     since there's no scrim to contrast against. */
  .tp-window-host {
    position: fixed;
    inset: 0;
    background: var(--bg-base);
    overflow: hidden;
    z-index: 1;
  }
  .tp-modal-window {
    width: 100% !important;
    max-width: none !important;
    height: 100% !important;
    max-height: none !important;
    border: 0;
    border-radius: 0;
    box-shadow: none;
  }

  .tp-body {
    overflow-y: auto;
    padding: 12px 16px 16px;
    flex: 1 1 auto;
    min-height: 0;          /* mandatory for the auto-overflow child to scroll */
  }

  .tp-toolbar {
    flex: 0 0 auto;          /* never shrink — sits above the scroll area */
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    background: var(--bg-card);
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
  }
  .tp-status { font-size: 11px; color: var(--text-dim); margin-left: 4px; }

  .tp-h4 {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-dim);
    margin: 16px 0 4px;
    font-weight: 600;
  }
  /* The first H4 used to live below the in-body toolbar, which gave it
     enough top padding from the toolbar's own margin. Now the toolbar sits
     outside, so trim the leading space so the body doesn't open with a
     ~28px gap. */
  .tp-h4-first { margin-top: 4px; }

  .tp-empty { font-size: 12px; color: var(--text-dim); margin: 4px 0 8px; }

  .tp-table { width: 100%; border-collapse: collapse; font-size: 12px; }
  .tp-table thead th {
    text-align: left;
    color: var(--text-dim);
    font-weight: 500;
    border-bottom: 1px solid var(--border);
    padding: 4px 6px;
  }
  .tp-table tbody td {
    padding: 4px 6px;
    border-bottom: 1px solid var(--border);
    color: var(--text-secondary);
    vertical-align: middle;
  }
  .tp-table tbody tr:hover { background: var(--bg-hover); }
  .tp-table-rotw { opacity: 0.85; }

  .tp-mono {
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 11px;
  }
  .tp-cell-dim { color: var(--text-dim); }
  .tp-row-hidden { opacity: 0.5; }
  .tp-row-add td { background: var(--bg-card); }

  .tp-th-actions { width: 1%; white-space: nowrap; }
  /* Icon-only columns (default radio, preferred star, hidden eye).
     Three rules at play to make the header glyph line up exactly under
     the body control:
       1. text-align:center on both header AND body cells.
       2. width:1% collapses the column to its content's intrinsic
          width so they share the same tight box.
       3. matching font-size (14px) on both — emojis like 👁 have
          asymmetric side bearings that shift the visual midpoint
          differently at different font-sizes, so a 12px header glyph
          centers at a slightly different X than a 14px body glyph.
     The .tp-icon-btn padding is also zeroed inside icon cells so the
     button's content sits at the same offset as the header glyph (cell
     padding only); native radio margin is reset for the same reason. */
  .tp-th-icon, .tp-td-icon {
    text-align: center;
    width: 1%;
    white-space: nowrap;
    padding-left: 4px;
    padding-right: 4px;
    font-size: 14px;
  }
  .tp-td-icon input[type="radio"],
  .tp-td-icon .tp-icon-btn {
    margin: 0;
    vertical-align: middle;
  }
  .tp-td-icon .tp-icon-btn {
    padding-left: 0;
    padding-right: 0;
  }
  .tp-actions { white-space: nowrap; display: flex; gap: 4px; }

  .tp-btn {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    padding: 4px 10px;
    font-size: 11px;
    border-radius: 4px;
    cursor: pointer;
    transition: background 0.1s, color 0.1s;
  }
  .tp-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .tp-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .tp-btn-small { padding: 2px 8px; font-size: 10px; }
  .tp-btn-ghost { background: transparent; }

  .tp-icon-btn {
    background: transparent;
    border: 0;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 14px;
    padding: 2px 4px;
    border-radius: 3px;
    line-height: 1;
  }
  .tp-icon-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .tp-icon-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .tp-icon-btn.tp-on { color: #f5b400; }
  .tp-icon-btn.tp-icon-danger:hover:not(:disabled) { color: #e74c3c; }

  .tp-input {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-primary);
    border-radius: 3px;
    padding: 2px 6px;
    font-size: 11px;
    width: 100%;
  }
  .tp-input:focus { outline: 1px solid var(--accent); }

  .tp-banner {
    background: var(--bg-card);
    border: 1px solid var(--accent, #f5b400);
    border-left-width: 3px;
    border-radius: 4px;
    padding: 8px 12px;
    margin: 0 0 12px;
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.4;
  }
  .tp-banner strong {
    display: block;
    color: var(--text-primary);
    font-weight: 600;
    margin-bottom: 2px;
  }
</style>
