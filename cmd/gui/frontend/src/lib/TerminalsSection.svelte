<script lang="ts">
  // TerminalsSection — Gear-panel section for the v2.1 Terminal Profile model
  // (issue #69). Renders three stacked tables: detected Terminal Apps,
  // detected Shells, and Profiles (terminal × shell pairs that the per-row
  // launcher offers). Profile rows expose:
  //
  //   ●  default radio (mutex across the table)
  //   ★  preferred toggle (shown in launcher submenu)
  //   👁  hidden toggle  (kept in config but not surfaced)
  //   ✎  inline edit (rename, change terminal/shell binding)
  //   🗑 delete       (only when source = "user")
  //
  // The component owns its working draft of the three arrays. Changes are
  // staged locally and pushed to the parent via `onSave`. The parent calls
  // bridge.saveTerminalProfiles, then reloads the config and re-passes
  // updated props on the next tick.

  import type { TerminalAppInfo, ShellInfo, TerminalProfileInfo } from './types';
  import { bridge } from './bridge';

  export let apps: TerminalAppInfo[] = [];
  export let shells: ShellInfo[] = [];
  export let profiles: TerminalProfileInfo[] = [];
  export let onSave: (apps: TerminalAppInfo[], shells: ShellInfo[], profiles: TerminalProfileInfo[]) => Promise<void>;
  export let onConfigReloaded: () => void = () => {};

  let collapsed = true;
  let busy = false;
  let statusMsg = '';

  // Local working draft. Reactive so prop changes (after Save / Re-detect)
  // refresh the view without forcing a manual sync.
  let draftApps: TerminalAppInfo[] = [];
  let draftShells: ShellInfo[] = [];
  let draftProfiles: TerminalProfileInfo[] = [];
  $: draftApps = apps.map(a => ({ ...a, args_template: [...(a.args_template || [])] }));
  $: draftShells = shells.map(s => ({ ...s, args: [...(s.args || [])] }));
  $: draftProfiles = profiles.map(p => ({ ...p, args: [...(p.args || [])] }));

  // Inline editor state. editingId == null → no row in edit mode.
  let editingId: string | null = null;
  let editName = '';
  let editTerminal = '';
  let editShell = '';

  // Add-form state. When non-null, an empty profile draft is being filled in.
  type Draft = { name: string; terminal: string; shell: string };
  let addDraft: Draft | null = null;

  function appName(id: string): string {
    return draftApps.find(a => a.id === id)?.name ?? id ?? '—';
  }
  function shellName(id: string): string {
    if (!id) return '(default)';
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
    addDraft = { name: '', terminal: draftApps[0]?.id ?? '', shell: '' };
  }

  function cancelAdd() {
    addDraft = null;
  }

  async function saveAdd() {
    if (!addDraft) return;
    const name = addDraft.name.trim();
    if (!name || !addDraft.terminal) return;
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

  // nextProfileID generates a stable id for a freshly-added user profile.
  // Falls back to a numeric suffix when the natural id collides — same
  // pattern the Go migrator follows.
  function nextProfileID(terminal: string, shell: string, name: string): string {
    const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    let base = slug(`${terminal}-${shell || 'default'}-${name}`);
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
    if (p.source !== 'user') return; // detected/migrated rows are hide-only.
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
</script>

<div class="terminals-section">
  <button class="ts-header" type="button" on:click={() => collapsed = !collapsed}>
    <span class="ts-chevron" class:ts-chevron-open={!collapsed}>▸</span>
    <span class="ts-title">Terminals &amp; shells</span>
    <span class="ts-summary">
      {draftApps.length} app{draftApps.length === 1 ? '' : 's'} ·
      {draftShells.length} shell{draftShells.length === 1 ? '' : 's'} ·
      {draftProfiles.length} profile{draftProfiles.length === 1 ? '' : 's'}
    </span>
  </button>

  {#if !collapsed}
    <div class="ts-body">
      <div class="ts-toolbar">
        <button class="ts-btn" type="button" on:click={redetect} disabled={busy}>Re-detect</button>
        <button class="ts-btn" type="button" on:click={startAdd} disabled={busy || !!addDraft}>+ Add profile</button>
        {#if statusMsg}
          <span class="ts-status">{statusMsg}</span>
        {/if}
      </div>

      <h4 class="ts-h4">Detected terminals</h4>
      {#if draftApps.length === 0}
        <p class="ts-empty">No terminal apps detected. Click <em>Re-detect</em> after installing one.</p>
      {:else}
        <table class="ts-table ts-table-rotw">
          <thead><tr><th>Name</th><th>Command</th></tr></thead>
          <tbody>
            {#each draftApps as app (app.id)}
              <tr>
                <td>{app.name}</td>
                <td class="ts-mono">{app.command}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <h4 class="ts-h4">Detected shells</h4>
      {#if draftShells.length === 0}
        <p class="ts-empty">No shells detected.</p>
      {:else}
        <table class="ts-table ts-table-rotw">
          <thead><tr><th>Name</th><th>Command</th><th>Args</th></tr></thead>
          <tbody>
            {#each draftShells as shell (shell.id)}
              <tr>
                <td>{shell.name}</td>
                <td class="ts-mono">{shell.command}</td>
                <td class="ts-mono">{(shell.args || []).join(' ')}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <h4 class="ts-h4">Profiles</h4>
      {#if draftProfiles.length === 0 && !addDraft}
        <p class="ts-empty">No profiles yet. Click <em>+ Add profile</em> or <em>Re-detect</em> to populate.</p>
      {:else}
        <table class="ts-table">
          <thead>
            <tr>
              <th title="Default — primary action of the per-row launcher">●</th>
              <th title="Preferred — shown in the launcher submenu">★</th>
              <th>Name</th>
              <th>Terminal</th>
              <th>Shell</th>
              <th title="Hidden — kept in config but not shown in the launcher">👁</th>
              <th class="ts-th-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each draftProfiles as p (p.id)}
              <tr class:ts-row-hidden={p.hidden}>
                <td>
                  <input type="radio" name="ts-default" checked={p.default} disabled={busy} on:change={() => setDefault(p.id)} />
                </td>
                <td>
                  <button class="ts-icon-btn" class:ts-on={p.preferred} type="button" disabled={busy} on:click={() => togglePreferred(p.id)} title="Toggle preferred">★</button>
                </td>
                {#if editingId === p.id}
                  <td><input class="ts-input" bind:value={editName} placeholder="Name" /></td>
                  <td>
                    <select class="ts-input" bind:value={editTerminal}>
                      {#each draftApps as a (a.id)}
                        <option value={a.id}>{a.name}</option>
                      {/each}
                    </select>
                  </td>
                  <td>
                    <select class="ts-input" bind:value={editShell}>
                      <option value="">(terminal default)</option>
                      {#each draftShells as s (s.id)}
                        <option value={s.id}>{s.name}</option>
                      {/each}
                    </select>
                  </td>
                  <td>
                    <button class="ts-icon-btn" type="button" disabled={busy} on:click={() => toggleHidden(p.id)} title="Toggle hidden">{p.hidden ? '🙈' : '👁'}</button>
                  </td>
                  <td class="ts-actions">
                    <button class="ts-btn ts-btn-small" type="button" disabled={busy} on:click={saveEdit}>Save</button>
                    <button class="ts-btn ts-btn-small ts-btn-ghost" type="button" disabled={busy} on:click={cancelEdit}>Cancel</button>
                  </td>
                {:else}
                  <td>{p.name}</td>
                  <td class="ts-cell-dim">{appName(p.terminal)}</td>
                  <td class="ts-cell-dim">{shellName(p.shell)}</td>
                  <td>
                    <button class="ts-icon-btn" type="button" disabled={busy} on:click={() => toggleHidden(p.id)} title="Toggle hidden">{p.hidden ? '🙈' : '👁'}</button>
                  </td>
                  <td class="ts-actions">
                    <button class="ts-icon-btn" type="button" disabled={busy} on:click={() => startEdit(p)} title="Edit">✎</button>
                    {#if p.source === 'user'}
                      <button class="ts-icon-btn ts-icon-danger" type="button" disabled={busy} on:click={() => deleteProfile(p)} title="Delete">🗑</button>
                    {:else}
                      <span class="ts-source-tag" title="This profile was {p.source === 'wt-profile' ? 'imported from Windows Terminal settings.json' : p.source === 'wezterm-launchmenu' ? 'imported from your wezterm.lua launch_menu' : p.source === 'migrated' ? 'migrated from a v2.0 terminals[] entry' : 'detected automatically'}; toggle Hidden if you want it out of the menu.">{p.source}</span>
                    {/if}
                  </td>
                {/if}
              </tr>
            {/each}
            {#if addDraft}
              <tr class="ts-row-add">
                <td>—</td>
                <td>—</td>
                <td><input class="ts-input" bind:value={addDraft.name} placeholder="Display name" autofocus /></td>
                <td>
                  <select class="ts-input" bind:value={addDraft.terminal}>
                    {#each draftApps as a (a.id)}
                      <option value={a.id}>{a.name}</option>
                    {/each}
                  </select>
                </td>
                <td>
                  <select class="ts-input" bind:value={addDraft.shell}>
                    <option value="">(terminal default)</option>
                    {#each draftShells as s (s.id)}
                      <option value={s.id}>{s.name}</option>
                    {/each}
                  </select>
                </td>
                <td>—</td>
                <td class="ts-actions">
                  <button class="ts-btn ts-btn-small" type="button" disabled={busy || !addDraft.name.trim() || !addDraft.terminal} on:click={saveAdd}>Add</button>
                  <button class="ts-btn ts-btn-small ts-btn-ghost" type="button" disabled={busy} on:click={cancelAdd}>Cancel</button>
                </td>
              </tr>
            {/if}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}
</div>

<style>
  .terminals-section {
    border-top: 1px solid var(--border);
    margin-top: 8px;
    padding-top: 6px;
  }

  .ts-header {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    background: transparent;
    border: 0;
    padding: 6px 4px;
    color: var(--text-secondary);
    cursor: pointer;
    font: inherit;
    text-align: left;
  }
  .ts-header:hover { color: var(--text-primary); }

  .ts-chevron {
    display: inline-block;
    transition: transform 120ms;
    font-size: 10px;
    width: 10px;
    color: var(--text-dim);
  }
  .ts-chevron-open { transform: rotate(90deg); }

  .ts-title { font-weight: 600; color: var(--text-primary); }
  .ts-summary { font-size: 11px; color: var(--text-dim); margin-left: auto; }

  .ts-body { padding: 6px 4px 12px; }

  .ts-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    flex-wrap: wrap;
  }
  .ts-status {
    font-size: 11px;
    color: var(--text-dim);
    margin-left: 4px;
  }

  .ts-h4 {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-dim);
    margin: 12px 0 4px;
    font-weight: 600;
  }

  .ts-empty {
    font-size: 12px;
    color: var(--text-dim);
    margin: 4px 0 8px;
  }

  .ts-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .ts-table thead th {
    text-align: left;
    color: var(--text-dim);
    font-weight: 500;
    border-bottom: 1px solid var(--border);
    padding: 4px 6px;
  }
  .ts-table tbody td {
    padding: 4px 6px;
    border-bottom: 1px solid var(--border);
    color: var(--text-secondary);
    vertical-align: middle;
  }
  .ts-table tbody tr:hover { background: var(--bg-hover); }

  /* Read-only-tables (rotw) get a slight visual weight reduction so the
     editable Profiles table is the focus of the section. */
  .ts-table-rotw { opacity: 0.85; }

  .ts-mono {
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 11px;
  }

  .ts-cell-dim { color: var(--text-dim); }
  .ts-row-hidden { opacity: 0.5; }
  .ts-row-add td { background: var(--bg-card); border-bottom: 1px solid var(--border); }

  .ts-th-actions { width: 1%; white-space: nowrap; }
  .ts-actions {
    white-space: nowrap;
    display: flex;
    gap: 4px;
  }

  .ts-btn {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    padding: 4px 10px;
    font-size: 11px;
    border-radius: 4px;
    cursor: pointer;
    transition: background 0.1s, color 0.1s;
  }
  .ts-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .ts-btn:disabled { opacity: 0.5; cursor: not-allowed; }

  .ts-btn-small { padding: 2px 8px; font-size: 10px; }
  .ts-btn-ghost { background: transparent; }

  .ts-icon-btn {
    background: transparent;
    border: 0;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 14px;
    padding: 2px 4px;
    border-radius: 3px;
    line-height: 1;
  }
  .ts-icon-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
  .ts-icon-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .ts-icon-btn.ts-on { color: #f5b400; }
  .ts-icon-btn.ts-icon-danger:hover:not(:disabled) { color: #e74c3c; }

  .ts-input {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-primary);
    border-radius: 3px;
    padding: 2px 6px;
    font-size: 11px;
    width: 100%;
  }
  .ts-input:focus { outline: 1px solid var(--accent); }

  .ts-source-tag {
    font-size: 10px;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 2px 4px;
    border-radius: 2px;
  }
</style>
