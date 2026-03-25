<!--
  gitbox Prototype 2: "Project Hub" (Linear/Notion style)
  ========================================================
  Paste this entire file into SvelteLab (https://www.sveltelab.dev/)
  as App.svelte to test interactively.

  Layout: Sidebar (nav) + main content area with table-style repo rows
  Character: Structured, scannable, "I manage a lot of things"
-->

<script>
  import { writable, derived } from 'svelte/store';
  import { fade, slide } from 'svelte/transition';

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  DATA LAYER — identical to other prototypes                 ║
  // ╚══════════════════════════════════════════════════════════════╝

  const accounts = writable({
    'git-parchis-luis': {
      provider: 'forgejo', url: 'https://git.parchis.org',
      username: 'luis', name: 'Luis Palacios',
      credType: 'gcm', credStatus: 'ok', repoCount: 4
    },
    'github-LuisPalacios': {
      provider: 'github', url: 'https://github.com',
      username: 'LuisPalacios', name: 'Luis Palacios',
      credType: 'gcm', credStatus: 'ok', repoCount: 3
    },
    'github-Sumwall': {
      provider: 'github', url: 'https://github.com',
      username: 'Sumwall', name: 'Sumwall Dev',
      credType: 'gcm', credStatus: 'ok', repoCount: 3
    },
    'github-AgorastisMesaio': {
      provider: 'github', url: 'https://github.com',
      username: 'AgorastisMesaio', name: 'Mesaio',
      credType: 'ssh', credStatus: 'warning', repoCount: 1
    }
  });

  const sources = writable({
    'git-parchis-luis': {
      account: 'git-parchis-luis',
      repos: ['infra/homelab', 'infra/migration', 'familia/fotos', 'parchis/web']
    },
    'github-LuisPalacios': {
      account: 'github-LuisPalacios',
      repos: ['LuisPalacios/gitbox', 'LuisPalacios/dotfiles', 'LuisPalacios/homelab']
    },
    'github-Sumwall': {
      account: 'github-Sumwall',
      repos: ['Sumwall/platform', 'Sumwall/docs', 'Sumwall/infra']
    },
    'github-AgorastisMesaio': {
      account: 'github-AgorastisMesaio',
      repos: ['AgorastisMesaio/homelab']
    }
  });

  const repoStates = writable({
    'git-parchis-luis/infra/homelab':       { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'git-parchis-luis/infra/migration':     { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'git-parchis-luis/familia/fotos':       { status: 'behind', progress: 0, behind: 3, modified: 0 },
    'git-parchis-luis/parchis/web':         { status: 'not_local', progress: 0, behind: 0, modified: 0 },
    'github-LuisPalacios/LuisPalacios/gitbox':    { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-LuisPalacios/LuisPalacios/dotfiles':  { status: 'dirty', progress: 0, behind: 0, modified: 2 },
    'github-LuisPalacios/LuisPalacios/homelab':   { status: 'behind', progress: 0, behind: 2, modified: 0 },
    'github-Sumwall/Sumwall/platform':     { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-Sumwall/Sumwall/docs':         { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-Sumwall/Sumwall/infra':        { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-AgorastisMesaio/AgorastisMesaio/homelab': { status: 'synced', progress: 0, behind: 0, modified: 0 }
  });

  const summary = derived(repoStates, ($rs) => {
    const vals = Object.values($rs);
    return {
      synced: vals.filter(r => r.status === 'synced').length,
      behind: vals.filter(r => r.status === 'behind').length,
      dirty: vals.filter(r => r.status === 'dirty').length,
      syncing: vals.filter(r => r.status === 'syncing' || r.status === 'cloning').length,
      not_local: vals.filter(r => r.status === 'not_local').length,
      total: vals.length
    };
  });

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  SIMULATION FUNCTIONS                                       ║
  // ╚══════════════════════════════════════════════════════════════╝

  let syncing = false;

  function simulateProgress(key, finalStatus) {
    return new Promise((resolve) => {
      repoStates.update(s => {
        s[key] = { ...s[key], progress: 0, status: s[key].status === 'not_local' ? 'cloning' : 'syncing' };
        return { ...s };
      });
      let progress = 0;
      const interval = setInterval(() => {
        progress += Math.random() * 15 + 5;
        if (progress >= 100) {
          progress = 100;
          clearInterval(interval);
          repoStates.update(s => {
            s[key] = { ...s[key], status: finalStatus, progress: 100, behind: 0 };
            return { ...s };
          });
          setTimeout(() => {
            repoStates.update(s => { s[key] = { ...s[key], progress: 0 }; return { ...s }; });
            resolve();
          }, 400);
        } else {
          repoStates.update(s => { s[key] = { ...s[key], progress: Math.round(progress) }; return { ...s }; });
        }
      }, 120);
    });
  }

  async function syncRepo(key) {
    // WAILS: window.go.main.App.PullRepo(sourceKey, repoKey)
    await simulateProgress(key, 'synced');
  }

  async function cloneRepo(key) {
    // WAILS: window.go.main.App.CloneRepo(sourceKey, repoKey)
    await simulateProgress(key, 'synced');
  }

  async function syncAll() {
    if (syncing) return;
    syncing = true;
    let currentStates;
    repoStates.subscribe(v => currentStates = v)();
    const needsSync = Object.entries(currentStates).filter(([_, r]) => r.status === 'behind' || r.status === 'not_local');
    for (const [key, repo] of needsSync) {
      if (repo.status === 'not_local') await cloneRepo(key);
      else await syncRepo(key);
    }
    syncing = false;
  }

  // Discovery
  let discoverModal = null;
  let discoverLoading = false;
  let discoverRepos = [];
  let discoverSelected = {};
  const fakeDiscoverResults = [
    { name: 'team/new-project', description: 'A brand new project', isNew: true },
    { name: 'team/experiment', description: 'Experimental work', isNew: true },
    { name: 'other-org/shared-tools', description: 'Shared utilities', isNew: true },
    { name: 'team/archived-thing', description: 'Old archived repo', isNew: true, archived: true },
  ];
  async function openDiscover(accountKey) {
    discoverModal = accountKey;
    discoverLoading = true;
    discoverRepos = [];
    await new Promise(r => setTimeout(r, 1500));
    discoverRepos = fakeDiscoverResults;
    discoverSelected = Object.fromEntries(fakeDiscoverResults.filter(r => !r.archived).map(r => [r.name, true]));
    discoverLoading = false;
  }
  function addDiscovered() {
    // WAILS: window.go.main.App.AddDiscoveredRepos(accountKey, selectedRepos)
    discoverModal = null;
  }

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  HELPERS                                                    ║
  // ╚══════════════════════════════════════════════════════════════╝

  function statusLabel(s, behind, modified) {
    const map = { synced: 'Synced', behind: `${behind} behind`, dirty: `${modified} changed`,
      syncing: 'Refreshing...', cloning: 'Downloading...', not_local: 'Not local', error: 'Error' };
    return map[s] || s;
  }
  function statusColor(s) {
    const map = { synced: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
      syncing: '#4B95E9', cloning: '#4B95E9', not_local: '#71717a', error: '#D81E5B' };
    return map[s] || '#71717a';
  }
  function credColor(s) { return s === 'ok' ? '#61fd5f' : s === 'warning' ? '#F07623' : '#D81E5B'; }
  function providerIcon(p) {
    return { github: 'GH', forgejo: 'FJ', gitea: 'GT', gitlab: 'GL' }[p] || '??';
  }
  function statusSymbol(s) {
    return { synced: '+', behind: '<', dirty: '!', syncing: '~', cloning: '~', not_local: 'o', error: 'x' }[s] || '?';
  }

  // Navigation
  let activeView = 'dashboard'; // dashboard | accounts | settings
  // Svelte auto-subscribes stores when using $storeName in template/script
</script>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE: Project Hub — Sidebar + Content                  -->
<!-- ════════════════════════════════════════════════════════════ -->

<div class="app">
  <!-- SIDEBAR -->
  <nav class="sidebar">
    <div class="sidebar-brand">
      <span class="logo">&#9673;</span>
      <span class="brand-text">gitbox</span>
    </div>

    <div class="nav-section">
      <button class="nav-item" class:active={activeView === 'dashboard'} on:click={() => activeView = 'dashboard'}>
        <span class="nav-icon">&#9632;</span> Dashboard
      </button>
    </div>

    <div class="nav-label">Accounts</div>
    {#each Object.entries($accounts) as [key, acct]}
      <button class="nav-item nav-account" on:click={() => openDiscover(key)}>
        <span class="nav-dot" style="background: {credColor(acct.credStatus)}"></span>
        <span class="nav-account-name">{key.replace(/^(github|git)-/, '')}</span>
        <span class="nav-badge">{providerIcon(acct.provider)}</span>
      </button>
    {/each}

    <div class="nav-spacer"></div>
    <button class="nav-item" class:active={activeView === 'settings'} on:click={() => activeView = 'settings'}>
      <span class="nav-icon">&#9881;</span> Settings
    </button>
  </nav>

  <!-- MAIN CONTENT -->
  <main class="content">
    <!-- Header -->
    <div class="content-header">
      <div>
        <h1>All Projects</h1>
        <span class="content-subtitle">{$summary.total} projects across {Object.keys($accounts).length} accounts</span>
      </div>
      <div class="content-actions">
        <button
          class="btn-sync"
          on:click={syncAll}
          disabled={syncing || ($summary.behind === 0 && $summary.not_local === 0)}
        >
          <span class="sync-icon" class:spinning={syncing}>&#8635;</span>
          {syncing ? 'Syncing...' : 'Sync All'}
        </button>
      </div>
    </div>

    <!-- Summary chips -->
    <div class="chips">
      <span class="chip" style="--chip-color: #61fd5f">{$summary.synced} synced</span>
      {#if $summary.behind > 0}<span class="chip" style="--chip-color: #D91C9A">{$summary.behind} behind</span>{/if}
      {#if $summary.dirty > 0}<span class="chip" style="--chip-color: #F07623">{$summary.dirty} changed</span>{/if}
      {#if $summary.not_local > 0}<span class="chip" style="--chip-color: #71717a">{$summary.not_local} not local</span>{/if}
      {#if $summary.syncing > 0}<span class="chip" style="--chip-color: #4B95E9">{$summary.syncing} syncing</span>{/if}
    </div>

    <!-- Table -->
    <div class="table-wrap">
      {#each Object.entries($sources) as [sourceKey, source]}
        <div class="table-group">
          <div class="table-group-header">{sourceKey}</div>
          {#each source.repos as repoName}
            {@const repoKey = `${sourceKey}/${repoName}`}
            {@const state = $repoStates[repoKey] || { status: 'error', progress: 0, behind: 0, modified: 0 }}
            <div class="table-row" transition:fade={{ duration: 120 }}>
              <span class="col-status" style="color: {statusColor(state.status)}">{statusSymbol(state.status)}</span>
              <span class="col-name">{repoName}</span>
              <span class="col-state">
                {#if state.status === 'syncing' || state.status === 'cloning'}
                  <div class="inline-progress">
                    <div class="inline-bar" style="width:{state.progress}%; background:{statusColor(state.status)}"></div>
                  </div>
                  <span style="color:{statusColor(state.status)}">{state.progress}%</span>
                {:else}
                  <span style="color: {statusColor(state.status)}">{statusLabel(state.status, state.behind, state.modified)}</span>
                {/if}
              </span>
              <span class="col-action">
                {#if state.status === 'behind'}
                  <button class="btn-sm" on:click={() => syncRepo(repoKey)}>&#8635;</button>
                {:else if state.status === 'not_local'}
                  <button class="btn-sm" on:click={() => cloneRepo(repoKey)}>&#8615;</button>
                {/if}
              </span>
            </div>
          {/each}
        </div>
      {/each}
    </div>
  </main>

  <!-- DISCOVER MODAL (shared) -->
  {#if discoverModal}
    <div class="modal-overlay" on:click={() => discoverModal = null} transition:fade={{ duration: 150 }}>
      <div class="modal" on:click|stopPropagation transition:slide={{ duration: 200 }}>
        <div class="modal-header">
          <h3>Find Projects &mdash; {discoverModal}</h3>
          <button class="btn-close" on:click={() => discoverModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if discoverLoading}
            <div class="discover-loading"><div class="spinner"></div><span>Checking account...</span></div>
          {:else}
            <p class="discover-hint">Found {discoverRepos.length} new projects:</p>
            {#each discoverRepos as repo}
              <label class="discover-row">
                <input type="checkbox" bind:checked={discoverSelected[repo.name]} />
                <span class="dr-name">{repo.name}</span>
                {#if repo.archived}<span class="dr-tag">archived</span>{/if}
                <span class="dr-desc">{repo.description}</span>
              </label>
            {/each}
          {/if}
        </div>
        {#if !discoverLoading}
          <div class="modal-footer">
            <button class="btn-cancel" on:click={() => discoverModal = null}>Cancel</button>
            <button class="btn-add" on:click={addDiscovered}>Add ({Object.values(discoverSelected).filter(Boolean).length})</button>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  :global(body) {
    margin: 0; background: #09090b;
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', system-ui, sans-serif;
    color: #fafafa; -webkit-font-smoothing: antialiased;
  }
  .app { display: flex; min-height: 100vh; }

  /* ── Sidebar ── */
  .sidebar {
    width: 200px; flex-shrink: 0;
    background: #0f0f12; border-right: 1px solid #1c1c22;
    display: flex; flex-direction: column;
    padding: 12px 8px;
  }
  .sidebar-brand { display: flex; align-items: center; gap: 8px; padding: 8px 10px 18px; }
  .logo { font-size: 18px; color: #61fd5f; }
  .brand-text { font-size: 15px; font-weight: 700; letter-spacing: -0.3px; }

  .nav-section { margin-bottom: 6px; }
  .nav-label {
    font-size: 10px; font-weight: 700; color: #52525b;
    text-transform: uppercase; letter-spacing: 1px;
    padding: 14px 10px 6px;
  }
  .nav-item {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 7px 10px; background: none; border: none;
    color: #a1a1aa; font-size: 13px; border-radius: 6px;
    cursor: pointer; text-align: left; transition: all 0.12s;
  }
  .nav-item:hover { background: #18181b; color: #fafafa; }
  .nav-item.active { background: #1e1e24; color: #fafafa; }
  .nav-icon { font-size: 14px; width: 18px; text-align: center; }
  .nav-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
  .nav-account-name {
    flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .nav-badge {
    font-size: 9px; font-weight: 700; color: #52525b;
    background: #1c1c22; padding: 1px 4px; border-radius: 3px;
  }
  .nav-spacer { flex: 1; }

  /* ── Content ── */
  .content { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
  .content-header {
    display: flex; justify-content: space-between; align-items: flex-start;
    padding: 20px 28px 12px;
  }
  .content-header h1 { margin: 0; font-size: 20px; font-weight: 700; letter-spacing: -0.5px; }
  .content-subtitle { font-size: 12px; color: #71717a; margin-top: 2px; }
  .content-actions { display: flex; gap: 8px; }

  .btn-sync {
    display: flex; align-items: center; gap: 6px;
    padding: 7px 14px; background: #18181b; border: 1px solid #27272a;
    color: #fafafa; border-radius: 7px; cursor: pointer;
    font-size: 12px; font-weight: 500; transition: all 0.15s;
  }
  .btn-sync:hover:not(:disabled) { background: #27272a; border-color: #3f3f46; }
  .btn-sync:disabled { opacity: 0.4; cursor: default; }
  .sync-icon { font-size: 15px; display: inline-block; }
  .spinning { animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Chips ── */
  .chips { display: flex; gap: 6px; padding: 0 28px 14px; flex-wrap: wrap; }
  .chip {
    font-size: 11px; font-weight: 600; padding: 3px 10px;
    border-radius: 20px; border: 1px solid var(--chip-color);
    color: var(--chip-color); background: transparent;
  }

  /* ── Table ── */
  .table-wrap { flex: 1; overflow-y: auto; padding: 0 28px 20px; }
  .table-group { margin-bottom: 6px; }
  .table-group-header {
    font-size: 11px; font-weight: 700; color: #52525b;
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 10px 0 4px; border-bottom: 1px solid #1c1c22;
  }
  .table-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 5px; transition: background 0.1s;
  }
  .table-row:hover { background: #14141a; }

  .col-status { width: 18px; text-align: center; font-size: 13px; font-weight: 700; font-family: monospace; }
  .col-name { flex: 1; font-size: 13px; color: #e4e4e7; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .col-state { width: 140px; font-size: 12px; display: flex; align-items: center; gap: 6px; }
  .col-action { width: 30px; text-align: center; }

  .inline-progress { width: 80px; height: 3px; background: #27272a; border-radius: 2px; overflow: hidden; }
  .inline-bar { height: 100%; border-radius: 2px; transition: width 0.12s; }

  .btn-sm {
    background: none; border: 1px solid #27272a; color: #a1a1aa;
    width: 24px; height: 24px; border-radius: 5px; cursor: pointer;
    font-size: 13px; display: flex; align-items: center; justify-content: center;
    transition: all 0.15s;
  }
  .btn-sm:hover { background: #27272a; color: #fafafa; border-color: #3f3f46; }

  /* ── Modal ── */
  .modal-overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,0.6);
    display: flex; align-items: center; justify-content: center; z-index: 100;
  }
  .modal {
    background: #18181b; border: 1px solid #27272a;
    border-radius: 12px; width: 440px; max-height: 70vh;
    display: flex; flex-direction: column;
  }
  .modal-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 14px 18px; border-bottom: 1px solid #27272a;
  }
  .modal-header h3 { margin: 0; font-size: 14px; font-weight: 600; }
  .modal-body { padding: 14px 18px; overflow-y: auto; flex: 1; }
  .modal-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 10px 18px; border-top: 1px solid #27272a;
  }
  .btn-close {
    background: none; border: none; color: #71717a; cursor: pointer;
    font-size: 16px; padding: 2px 4px; transition: color 0.12s;
  }
  .btn-close:hover { color: #fafafa; }

  .discover-loading {
    display: flex; align-items: center; gap: 12px;
    padding: 20px 0; color: #a1a1aa; font-size: 13px;
  }
  .spinner {
    width: 18px; height: 18px; border: 2px solid #27272a;
    border-top-color: #4B95E9; border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  .discover-hint { font-size: 12px; color: #71717a; margin: 0 0 10px; }
  .discover-row {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 4px; cursor: pointer; border-radius: 4px; font-size: 13px;
  }
  .discover-row:hover { background: #27272a; }
  .discover-row input { accent-color: #4B95E9; }
  .dr-name { color: #e4e4e7; font-weight: 500; }
  .dr-desc { color: #52525b; font-size: 11px; margin-left: auto; }
  .dr-tag { font-size: 9px; padding: 1px 5px; background: #27272a; color: #52525b; border-radius: 3px; }

  .btn-cancel {
    padding: 6px 12px; background: #27272a; border: 1px solid #3f3f46;
    color: #a1a1aa; border-radius: 6px; cursor: pointer; font-size: 12px;
  }
  .btn-cancel:hover { color: #fafafa; }
  .btn-add {
    padding: 6px 12px; background: #4B95E9; border: none;
    color: #fff; border-radius: 6px; cursor: pointer;
    font-size: 12px; font-weight: 500;
  }
  .btn-add:hover { background: #3b82f6; }
</style>
