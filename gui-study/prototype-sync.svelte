<!--
  gitbox Prototype 1: "Cloud Sync" (iCloud/Dropbox style)
  ========================================================
  Paste this entire file into SvelteLab (https://www.sveltelab.dev/)
  as App.svelte to test interactively.

  Layout: Top bar → Account cards → Repo list grouped by source → Summary footer
  Character: Calm, minimal, "everything is handled"
-->

<script>
  import { writable, derived } from 'svelte/store';
  import { onMount } from 'svelte';
  import { fade, slide } from 'svelte/transition';

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  DATA LAYER — mirrors Go pkg/config structure               ║
  // ║  Replace mock data with Wails bindings in production        ║
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

  // Per-repo reactive state
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

  // Derived summary counts
  const summary = derived(repoStates, ($rs) => {
    const vals = Object.values($rs);
    return {
      synced: vals.filter(r => r.status === 'synced').length,
      behind: vals.filter(r => r.status === 'behind').length,
      dirty: vals.filter(r => r.status === 'dirty').length,
      syncing: vals.filter(r => r.status === 'syncing' || r.status === 'cloning').length,
      not_local: vals.filter(r => r.status === 'not_local').length,
      error: vals.filter(r => r.status === 'error').length,
      total: vals.length
    };
  });

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  SIMULATION FUNCTIONS — replace with Wails calls            ║
  // ╚══════════════════════════════════════════════════════════════╝

  let syncing = false;

  function simulateProgress(key, finalStatus) {
    return new Promise((resolve) => {
      repoStates.update(s => {
        s[key] = { ...s[key], progress: 0, status: finalStatus === 'synced' && s[key].status === 'not_local' ? 'cloning' : 'syncing' };
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
            repoStates.update(s => {
              s[key] = { ...s[key], progress: 0 };
              return { ...s };
            });
            resolve();
          }, 400);
        } else {
          repoStates.update(s => {
            s[key] = { ...s[key], progress: Math.round(progress) };
            return { ...s };
          });
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
    // WAILS: window.go.main.App.SyncAll()
    let currentStates;
    repoStates.subscribe(v => currentStates = v)();
    const needsSync = Object.entries(currentStates)
      .filter(([_, r]) => r.status === 'behind' || r.status === 'not_local');
    for (const [key, repo] of needsSync) {
      if (repo.status === 'not_local') {
        await cloneRepo(key);
      } else {
        await syncRepo(key);
      }
    }
    syncing = false;
  }

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  DISCOVERY SIMULATION                                       ║
  // ╚══════════════════════════════════════════════════════════════╝

  let discoverModal = null;    // null or account key
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
    discoverSelected = {};
    // WAILS: window.go.main.App.Discover(accountKey)
    await new Promise(r => setTimeout(r, 1500));
    discoverRepos = fakeDiscoverResults;
    discoverSelected = Object.fromEntries(
      fakeDiscoverResults.filter(r => !r.archived).map(r => [r.name, true])
    );
    discoverLoading = false;
  }

  function addDiscovered() {
    // WAILS: window.go.main.App.AddDiscoveredRepos(accountKey, selectedRepos)
    const added = Object.entries(discoverSelected).filter(([_, v]) => v).map(([k]) => k);
    console.log('Would add repos:', added);
    discoverModal = null;
  }

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  HELPERS                                                    ║
  // ╚══════════════════════════════════════════════════════════════╝

  function statusLabel(status, behind, modified) {
    switch (status) {
      case 'synced': return 'Synced';
      case 'behind': return `${behind} behind`;
      case 'dirty': return `${modified} local change${modified > 1 ? 's' : ''}`;
      case 'syncing': return 'Refreshing...';
      case 'cloning': return 'Bringing local...';
      case 'not_local': return 'Not local';
      case 'error': return 'Error';
      default: return status;
    }
  }

  function statusColor(status) {
    switch (status) {
      case 'synced': return '#61fd5f';
      case 'behind': return '#D91C9A';
      case 'dirty': return '#F07623';
      case 'syncing': case 'cloning': return '#4B95E9';
      case 'not_local': return '#71717a';
      case 'error': return '#D81E5B';
      default: return '#71717a';
    }
  }

  function credColor(status) {
    return status === 'ok' ? '#61fd5f' : status === 'warning' ? '#F07623' : '#D81E5B';
  }

  function providerIcon(provider) {
    switch (provider) {
      case 'github': return 'GH';
      case 'forgejo': case 'gitea': return 'FJ';
      case 'gitlab': return 'GL';
      default: return '??';
    }
  }

  // Svelte auto-subscribes stores when using $storeName in template/script
</script>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE: Cloud Sync Layout                                -->
<!-- ════════════════════════════════════════════════════════════ -->

<div class="app">
  <!-- TOP BAR -->
  <header class="topbar">
    <div class="brand">
      <span class="logo">&#9673;</span>
      <span class="title">gitbox</span>
    </div>
    <div class="actions">
      <button
        class="btn-sync"
        on:click={syncAll}
        disabled={syncing || $summary.behind === 0 && $summary.not_local === 0}
      >
        <span class="sync-icon" class:spinning={syncing}>&#8635;</span>
        {syncing ? 'Syncing...' : 'Sync All'}
      </button>
      <button class="btn-icon" title="Settings">&#9881;</button>
    </div>
  </header>

  <!-- ACCOUNT CARDS -->
  <section class="accounts-row">
    {#each Object.entries($accounts) as [key, acct]}
      <div class="account-card">
        <div class="account-header">
          <span class="cred-dot" style="background: {credColor(acct.credStatus)}"></span>
          <span class="provider-badge">{providerIcon(acct.provider)}</span>
          <span class="account-name">{key.replace(/^(github|git)-/, '')}</span>
        </div>
        <div class="account-meta">
          {acct.repoCount} project{acct.repoCount !== 1 ? 's' : ''} &middot; {acct.credType.toUpperCase()}
        </div>
        <button class="btn-find" on:click={() => openDiscover(key)}>
          Find Projects
        </button>
      </div>
    {/each}
  </section>

  <!-- REPO LIST -->
  <section class="repo-list">
    {#each Object.entries($sources) as [sourceKey, source]}
      <div class="source-group">
        <div class="source-header">{sourceKey}</div>
        {#each source.repos as repoName}
          {@const repoKey = `${sourceKey}/${repoName}`}
          {@const state = $repoStates[repoKey] || { status: 'error', progress: 0, behind: 0, modified: 0 }}
          <div class="repo-row" transition:fade={{ duration: 150 }}>
            <!-- Status circle -->
            <span class="status-dot" style="color: {statusColor(state.status)}">
              {#if state.status === 'synced'}&#9679;
              {:else if state.status === 'syncing' || state.status === 'cloning'}&#9684;
              {:else if state.status === 'not_local'}&#9675;
              {:else if state.status === 'behind'}&#9687;
              {:else if state.status === 'dirty'}&#9670;
              {:else}&#10005;
              {/if}
            </span>

            <!-- Repo name -->
            <span class="repo-name">{repoName}</span>

            <!-- Progress bar (only during sync/clone) -->
            {#if state.status === 'syncing' || state.status === 'cloning'}
              <div class="progress-bar-container">
                <div class="progress-bar" style="width: {state.progress}%; background: {statusColor(state.status)}"></div>
              </div>
              <span class="progress-text" style="color: {statusColor(state.status)}">{state.progress}%</span>
            {:else}
              <!-- Status label + action -->
              <span class="status-label" style="color: {statusColor(state.status)}">
                {statusLabel(state.status, state.behind, state.modified)}
              </span>
              {#if state.status === 'behind'}
                <button class="btn-action" on:click={() => syncRepo(repoKey)}>Refresh</button>
              {:else if state.status === 'not_local'}
                <button class="btn-action" on:click={() => cloneRepo(repoKey)}>Bring Local</button>
              {/if}
            {/if}
          </div>
        {/each}
      </div>
    {/each}
  </section>

  <!-- SUMMARY FOOTER -->
  <footer class="summary">
    <span class="sum-item" style="color: #61fd5f">{$summary.synced} synced</span>
    {#if $summary.syncing > 0}
      <span class="sum-dot">&middot;</span>
      <span class="sum-item" style="color: #4B95E9">{$summary.syncing} syncing</span>
    {/if}
    {#if $summary.behind > 0}
      <span class="sum-dot">&middot;</span>
      <span class="sum-item" style="color: #D91C9A">{$summary.behind} behind</span>
    {/if}
    {#if $summary.dirty > 0}
      <span class="sum-dot">&middot;</span>
      <span class="sum-item" style="color: #F07623">{$summary.dirty} local changes</span>
    {/if}
    {#if $summary.not_local > 0}
      <span class="sum-dot">&middot;</span>
      <span class="sum-item" style="color: #71717a">{$summary.not_local} not local</span>
    {/if}
  </footer>

  <!-- DISCOVER MODAL -->
  {#if discoverModal}
    <div class="modal-overlay" on:click={() => discoverModal = null} transition:fade={{ duration: 150 }}>
      <div class="modal" on:click|stopPropagation transition:slide={{ duration: 200 }}>
        <div class="modal-header">
          <h3>Find Projects &mdash; {discoverModal}</h3>
          <button class="btn-icon" on:click={() => discoverModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if discoverLoading}
            <div class="discover-loading">
              <div class="spinner"></div>
              <span>Checking your account...</span>
            </div>
          {:else}
            <p class="discover-count">Found {discoverRepos.length} new projects:</p>
            {#each discoverRepos as repo}
              <label class="discover-row">
                <input type="checkbox" bind:checked={discoverSelected[repo.name]} />
                <span class="discover-name">{repo.name}</span>
                {#if repo.archived}<span class="tag-archived">archived</span>{/if}
                <span class="discover-desc">{repo.description}</span>
              </label>
            {/each}
          {/if}
        </div>
        {#if !discoverLoading}
          <div class="modal-footer">
            <button class="btn-secondary" on:click={() => discoverModal = null}>Cancel</button>
            <button class="btn-primary" on:click={addDiscovered}>
              Add Selected ({Object.values(discoverSelected).filter(Boolean).length})
            </button>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  STYLES: Dark theme, Linear/Vercel inspired                 -->
<!-- ════════════════════════════════════════════════════════════ -->

<style>
  :global(body) {
    margin: 0;
    background: #09090b;
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', system-ui, sans-serif;
    color: #fafafa;
    -webkit-font-smoothing: antialiased;
  }

  .app {
    max-width: 820px;
    margin: 0 auto;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  /* ── Top Bar ── */
  .topbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 24px;
    border-bottom: 1px solid #27272a;
  }
  .brand { display: flex; align-items: center; gap: 10px; }
  .logo { font-size: 22px; color: #61fd5f; }
  .title { font-size: 18px; font-weight: 600; letter-spacing: -0.3px; }
  .actions { display: flex; gap: 8px; align-items: center; }

  .btn-sync {
    display: flex; align-items: center; gap: 6px;
    padding: 7px 16px;
    background: #18181b; border: 1px solid #27272a;
    color: #fafafa; border-radius: 8px; cursor: pointer;
    font-size: 13px; font-weight: 500;
    transition: background 0.15s, border-color 0.15s;
  }
  .btn-sync:hover:not(:disabled) { background: #27272a; border-color: #3f3f46; }
  .btn-sync:disabled { opacity: 0.4; cursor: default; }

  .sync-icon { font-size: 16px; display: inline-block; }
  .spinning { animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .btn-icon {
    background: none; border: 1px solid transparent;
    color: #a1a1aa; font-size: 18px; cursor: pointer;
    padding: 6px 8px; border-radius: 6px;
    transition: color 0.15s, background 0.15s;
  }
  .btn-icon:hover { color: #fafafa; background: #27272a; }

  /* ── Account Cards ── */
  .accounts-row {
    display: flex; gap: 12px;
    padding: 20px 24px;
    overflow-x: auto;
  }
  .account-card {
    flex: 1; min-width: 160px;
    background: #18181b; border: 1px solid #27272a;
    border-radius: 10px; padding: 14px;
    transition: border-color 0.15s;
  }
  .account-card:hover { border-color: #3f3f46; }
  .account-header {
    display: flex; align-items: center; gap: 8px;
    margin-bottom: 6px;
  }
  .cred-dot {
    width: 8px; height: 8px; border-radius: 50%;
    flex-shrink: 0;
  }
  .provider-badge {
    font-size: 10px; font-weight: 700; color: #a1a1aa;
    background: #27272a; padding: 2px 5px; border-radius: 4px;
    letter-spacing: 0.5px;
  }
  .account-name {
    font-size: 13px; font-weight: 600; color: #fafafa;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .account-meta {
    font-size: 11px; color: #71717a; margin-bottom: 10px;
  }
  .btn-find {
    width: 100%; padding: 5px 0;
    background: transparent; border: 1px solid #27272a;
    color: #a1a1aa; border-radius: 6px; cursor: pointer;
    font-size: 11px; font-weight: 500;
    transition: all 0.15s;
  }
  .btn-find:hover { background: #27272a; color: #fafafa; border-color: #3f3f46; }

  /* ── Repo List ── */
  .repo-list {
    flex: 1; padding: 0 24px 16px;
  }
  .source-group { margin-bottom: 8px; }
  .source-header {
    font-size: 11px; font-weight: 600; color: #71717a;
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 12px 0 6px;
    border-bottom: 1px solid #1a1a1f;
    margin-bottom: 2px;
  }
  .repo-row {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 8px;
    border-radius: 6px;
    transition: background 0.1s;
  }
  .repo-row:hover { background: #18181b; }

  .status-dot { font-size: 14px; flex-shrink: 0; width: 18px; text-align: center; }
  .repo-name {
    font-size: 13px; color: #e4e4e7;
    flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }

  .progress-bar-container {
    width: 120px; height: 4px;
    background: #27272a; border-radius: 2px; overflow: hidden;
    flex-shrink: 0;
  }
  .progress-bar {
    height: 100%; border-radius: 2px;
    transition: width 0.12s ease-out;
  }
  .progress-text { font-size: 11px; width: 32px; text-align: right; flex-shrink: 0; }

  .status-label { font-size: 12px; white-space: nowrap; }

  .btn-action {
    padding: 3px 10px;
    background: transparent; border: 1px solid #27272a;
    color: #a1a1aa; border-radius: 5px; cursor: pointer;
    font-size: 11px; font-weight: 500;
    transition: all 0.15s; flex-shrink: 0;
  }
  .btn-action:hover { background: #27272a; color: #fafafa; border-color: #3f3f46; }

  /* ── Summary Footer ── */
  .summary {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    padding: 12px 24px;
    border-top: 1px solid #27272a;
    font-size: 12px;
  }
  .sum-item { font-weight: 500; }
  .sum-dot { color: #3f3f46; }

  /* ── Discover Modal ── */
  .modal-overlay {
    position: fixed; inset: 0;
    background: rgba(0,0,0,0.6);
    display: flex; align-items: center; justify-content: center;
    z-index: 100;
  }
  .modal {
    background: #18181b; border: 1px solid #27272a;
    border-radius: 12px; width: 440px; max-height: 70vh;
    display: flex; flex-direction: column;
  }
  .modal-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 16px 20px; border-bottom: 1px solid #27272a;
  }
  .modal-header h3 { margin: 0; font-size: 15px; font-weight: 600; }
  .modal-body {
    padding: 16px 20px; overflow-y: auto; flex: 1;
  }
  .modal-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 20px; border-top: 1px solid #27272a;
  }

  .discover-loading {
    display: flex; align-items: center; gap: 12px;
    padding: 24px 0; color: #a1a1aa; font-size: 13px;
  }
  .spinner {
    width: 20px; height: 20px;
    border: 2px solid #27272a; border-top-color: #4B95E9;
    border-radius: 50%; animation: spin 0.7s linear infinite;
  }
  .discover-count { font-size: 13px; color: #a1a1aa; margin: 0 0 12px; }

  .discover-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 4px; cursor: pointer; border-radius: 4px;
    font-size: 13px;
  }
  .discover-row:hover { background: #27272a; }
  .discover-row input[type="checkbox"] { accent-color: #4B95E9; }
  .discover-name { color: #e4e4e7; font-weight: 500; }
  .discover-desc { color: #71717a; font-size: 11px; margin-left: auto; }
  .tag-archived {
    font-size: 10px; padding: 1px 5px;
    background: #27272a; color: #71717a;
    border-radius: 3px;
  }

  .btn-secondary {
    padding: 7px 14px; background: #27272a; border: 1px solid #3f3f46;
    color: #a1a1aa; border-radius: 6px; cursor: pointer; font-size: 12px;
    transition: all 0.15s;
  }
  .btn-secondary:hover { color: #fafafa; }
  .btn-primary {
    padding: 7px 14px; background: #4B95E9; border: none;
    color: #fff; border-radius: 6px; cursor: pointer; font-size: 12px;
    font-weight: 500; transition: background 0.15s;
  }
  .btn-primary:hover { background: #3b82f6; }
</style>
