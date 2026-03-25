<!--
  gitbox Prototype 3: "Dashboard Cards" (Vercel/Supabase style)
  ==============================================================
  Paste this entire file into SvelteLab (https://www.sveltelab.dev/)
  as App.svelte to test interactively.

  Layout: Top bar with health ring → Account cards with mini-rings → "Needs attention" section
  Character: At-a-glance monitoring, "only show me problems"
-->

<script>
  import { writable, derived } from 'svelte/store';
  import { fade, slide } from 'svelte/transition';

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  DATA LAYER                                                 ║
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

  // Per-account derived stats
  const accountStats = derived([sources, repoStates], ([$src, $rs]) => {
    const stats = {};
    for (const [sourceKey, source] of Object.entries($src)) {
      const acctKey = source.account;
      if (!stats[acctKey]) stats[acctKey] = { total: 0, synced: 0, issues: [] };
      for (const repoName of source.repos) {
        const key = `${sourceKey}/${repoName}`;
        const state = $rs[key];
        if (!state) continue;
        stats[acctKey].total++;
        if (state.status === 'synced') stats[acctKey].synced++;
        if (state.status !== 'synced' && state.status !== 'syncing' && state.status !== 'cloning') {
          stats[acctKey].issues.push({ key, repoName, sourceKey, ...state });
        }
      }
    }
    return stats;
  });

  // Repos needing attention (across all accounts)
  const attentionItems = derived([sources, repoStates], ([$src, $rs]) => {
    const items = [];
    for (const [sourceKey, source] of Object.entries($src)) {
      for (const repoName of source.repos) {
        const key = `${sourceKey}/${repoName}`;
        const state = $rs[key];
        if (state && state.status !== 'synced') {
          items.push({ key, repoName, sourceKey, ...state });
        }
      }
    }
    return items;
  });

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  SIMULATION                                                 ║
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

  async function syncRepo(key) { await simulateProgress(key, 'synced'); }
  async function cloneRepo(key) { await simulateProgress(key, 'synced'); }

  async function syncAll() {
    if (syncing) return;
    syncing = true;
    let currentStates;
    repoStates.subscribe(v => currentStates = v)();
    for (const [key, repo] of Object.entries(currentStates)) {
      if (repo.status === 'behind' || repo.status === 'not_local') {
        await simulateProgress(key, 'synced');
      }
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
  ];
  async function openDiscover(accountKey) {
    discoverModal = accountKey;
    discoverLoading = true;
    await new Promise(r => setTimeout(r, 1500));
    discoverRepos = fakeDiscoverResults;
    discoverSelected = Object.fromEntries(fakeDiscoverResults.map(r => [r.name, true]));
    discoverLoading = false;
  }
  function addDiscovered() { discoverModal = null; }

  // Expanded account cards
  let expandedAccounts = {};
  function toggleExpand(key) {
    expandedAccounts = { ...expandedAccounts, [key]: !expandedAccounts[key] };
  }

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  HELPERS                                                    ║
  // ╚══════════════════════════════════════════════════════════════╝

  function statusLabel(s, behind, modified) {
    const map = { synced: 'Synced', behind: `${behind} behind`, dirty: `${modified} changed`,
      syncing: 'Syncing...', cloning: 'Downloading...', not_local: 'Not local', error: 'Error' };
    return map[s] || s;
  }
  function statusColor(s) {
    const map = { synced: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
      syncing: '#4B95E9', cloning: '#4B95E9', not_local: '#71717a', error: '#D81E5B' };
    return map[s] || '#71717a';
  }
  function credColor(s) { return s === 'ok' ? '#61fd5f' : s === 'warning' ? '#F07623' : '#D81E5B'; }
  function providerLabel(p) {
    return { github: 'GitHub', forgejo: 'Forgejo', gitea: 'Gitea', gitlab: 'GitLab' }[p] || p;
  }

  // Ring SVG helper
  function ringPercent(synced, total) {
    if (total === 0) return 0;
    return Math.round((synced / total) * 100);
  }

  // Svelte auto-subscribes stores when using $storeName in template/script
</script>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE: Dashboard Cards                                  -->
<!-- ════════════════════════════════════════════════════════════ -->

<div class="app">
  <!-- TOP BAR -->
  <header class="topbar">
    <div class="brand">
      <span class="logo">&#9673;</span>
      <span class="title">gitbox</span>
    </div>
    <div class="health">
      <span class="health-ring" style="--pct: {ringPercent($summary.synced, $summary.total)}; --ring-color: #61fd5f">
        <span class="health-num">{$summary.synced}/{$summary.total}</span>
      </span>
      <span class="health-label">synced</span>
    </div>
    <div class="actions">
      <button class="btn-sync" on:click={syncAll}
        disabled={syncing || ($summary.behind === 0 && $summary.not_local === 0)}>
        <span class="sync-icon" class:spinning={syncing}>&#8635;</span>
        {syncing ? 'Syncing...' : 'Sync All'}
      </button>
      <button class="btn-gear" title="Settings">&#9881;</button>
    </div>
  </header>

  <!-- ACCOUNT CARDS GRID -->
  <section class="cards-grid">
    {#each Object.entries($accounts) as [key, acct]}
      {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: [] }}
      <div class="card">
        <div class="card-top">
          <span class="card-dot" style="background: {credColor(acct.credStatus)}"></span>
          <span class="card-provider">{providerLabel(acct.provider)}</span>
        </div>
        <div class="card-name">{key.replace(/^(github|git)-/, '')}</div>

        <!-- Mini ring -->
        <div class="card-ring-row">
          <svg class="mini-ring" viewBox="0 0 36 36">
            <circle cx="18" cy="18" r="15" fill="none" stroke="#27272a" stroke-width="3"/>
            <circle cx="18" cy="18" r="15" fill="none"
              stroke="{stats.synced === stats.total ? '#61fd5f' : '#D91C9A'}"
              stroke-width="3" stroke-linecap="round"
              stroke-dasharray="{(stats.synced / Math.max(stats.total, 1)) * 94.2} 94.2"
              transform="rotate(-90 18 18)"/>
          </svg>
          <span class="card-ring-label">{stats.synced}/{stats.total} synced</span>
        </div>

        <!-- Issue summary -->
        {#if stats.issues.length > 0}
          <div class="card-issues">
            {#each stats.issues.slice(0, 2) as issue}
              <span class="card-issue" style="color: {statusColor(issue.status)}">
                {statusLabel(issue.status, issue.behind, issue.modified)}
              </span>
            {/each}
          </div>
        {:else}
          <div class="card-ok">All good &#10003;</div>
        {/if}

        <!-- Actions -->
        <div class="card-actions">
          <button class="card-btn" on:click={() => openDiscover(key)}>Find</button>
          <button class="card-btn" on:click={() => toggleExpand(key)}>
            {expandedAccounts[key] ? 'Collapse' : 'Expand'}
          </button>
        </div>

        <!-- Expanded repo list -->
        {#if expandedAccounts[key]}
          <div class="card-repos" transition:slide={{ duration: 200 }}>
            {#each ($sources[key]?.repos || []) as repoName}
              {@const repoKey = `${key}/${repoName}`}
              {@const state = $repoStates[repoKey] || { status: 'error', progress: 0, behind: 0, modified: 0 }}
              <div class="card-repo-row">
                <span class="crr-dot" style="color: {statusColor(state.status)}">&#9679;</span>
                <span class="crr-name">{repoName.split('/').pop()}</span>
                {#if state.status === 'syncing' || state.status === 'cloning'}
                  <span class="crr-status" style="color: {statusColor(state.status)}">{state.progress}%</span>
                {:else}
                  <span class="crr-status" style="color: {statusColor(state.status)}">{statusLabel(state.status, state.behind, state.modified)}</span>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
  </section>

  <!-- NEEDS ATTENTION -->
  {#if $attentionItems.length > 0}
    <section class="attention">
      <div class="attention-header">
        Needs attention ({$attentionItems.length})
      </div>
      {#each $attentionItems as item}
        <div class="attention-row" transition:fade={{ duration: 120 }}>
          <span class="ar-dot" style="color: {statusColor(item.status)}">&#9679;</span>
          <span class="ar-name">{item.repoName}</span>
          <span class="ar-source">{item.sourceKey}</span>

          {#if item.status === 'syncing' || item.status === 'cloning'}
            <div class="ar-progress">
              <div class="ar-bar" style="width:{item.progress}%; background:{statusColor(item.status)}"></div>
            </div>
            <span class="ar-pct" style="color: {statusColor(item.status)}">{item.progress}%</span>
          {:else}
            <span class="ar-status" style="color: {statusColor(item.status)}">
              {statusLabel(item.status, item.behind, item.modified)}
            </span>
            {#if item.status === 'behind'}
              <button class="ar-btn" on:click={() => syncRepo(item.key)}>Refresh</button>
            {:else if item.status === 'not_local'}
              <button class="ar-btn" on:click={() => cloneRepo(item.key)}>Bring Local</button>
            {/if}
          {/if}
        </div>
      {/each}
    </section>
  {:else}
    <section class="all-clear">
      <span class="all-clear-icon">&#10003;</span>
      <span>Everything is synced</span>
    </section>
  {/if}

  <!-- DISCOVER MODAL -->
  {#if discoverModal}
    <div class="modal-overlay" on:click={() => discoverModal = null} transition:fade={{ duration: 150 }}>
      <div class="modal" on:click|stopPropagation transition:slide={{ duration: 200 }}>
        <div class="modal-header">
          <h3>Find Projects &mdash; {discoverModal}</h3>
          <button class="modal-close" on:click={() => discoverModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if discoverLoading}
            <div class="loading"><div class="spinner"></div><span>Checking account...</span></div>
          {:else}
            <p class="found-count">Found {discoverRepos.length} new projects:</p>
            {#each discoverRepos as repo}
              <label class="dr-row">
                <input type="checkbox" bind:checked={discoverSelected[repo.name]} />
                <span class="dr-name">{repo.name}</span>
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
  .app {
    max-width: 860px; margin: 0 auto;
    min-height: 100vh; display: flex; flex-direction: column;
  }

  /* ── Top Bar ── */
  .topbar {
    display: flex; align-items: center; gap: 16px;
    padding: 16px 24px; border-bottom: 1px solid #27272a;
  }
  .brand { display: flex; align-items: center; gap: 8px; }
  .logo { font-size: 20px; color: #61fd5f; }
  .title { font-size: 17px; font-weight: 700; letter-spacing: -0.3px; }

  .health { display: flex; align-items: center; gap: 8px; margin-left: auto; }
  .health-ring {
    width: 40px; height: 40px; border-radius: 50%;
    background: conic-gradient(var(--ring-color) calc(var(--pct) * 1%), #27272a 0);
    display: flex; align-items: center; justify-content: center;
    position: relative;
  }
  .health-ring::before {
    content: ''; position: absolute; inset: 4px;
    background: #09090b; border-radius: 50%;
  }
  .health-num {
    position: relative; z-index: 1;
    font-size: 10px; font-weight: 700; color: #fafafa;
  }
  .health-label { font-size: 12px; color: #71717a; }

  .actions { display: flex; gap: 8px; }
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
  .btn-gear {
    background: none; border: 1px solid transparent;
    color: #71717a; font-size: 18px; cursor: pointer;
    padding: 4px 6px; border-radius: 6px; transition: all 0.12s;
  }
  .btn-gear:hover { color: #fafafa; background: #27272a; }

  /* ── Cards Grid ── */
  .cards-grid {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 12px; padding: 20px 24px;
  }
  .card {
    background: #18181b; border: 1px solid #27272a;
    border-radius: 10px; padding: 14px;
    transition: border-color 0.15s;
  }
  .card:hover { border-color: #3f3f46; }
  .card-top { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
  .card-dot { width: 7px; height: 7px; border-radius: 50%; }
  .card-provider { font-size: 10px; color: #71717a; font-weight: 600; letter-spacing: 0.3px; }
  .card-name { font-size: 14px; font-weight: 600; margin-bottom: 10px; }

  .card-ring-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
  .mini-ring { width: 32px; height: 32px; }
  .card-ring-label { font-size: 12px; color: #a1a1aa; }

  .card-issues { display: flex; flex-direction: column; gap: 2px; margin-bottom: 10px; }
  .card-issue { font-size: 11px; }
  .card-ok { font-size: 11px; color: #61fd5f; margin-bottom: 10px; }

  .card-actions { display: flex; gap: 6px; }
  .card-btn {
    flex: 1; padding: 4px 0; background: transparent;
    border: 1px solid #27272a; color: #a1a1aa;
    border-radius: 5px; cursor: pointer; font-size: 11px;
    transition: all 0.15s;
  }
  .card-btn:hover { background: #27272a; color: #fafafa; border-color: #3f3f46; }

  .card-repos {
    margin-top: 10px; padding-top: 8px;
    border-top: 1px solid #27272a;
  }
  .card-repo-row {
    display: flex; align-items: center; gap: 6px;
    padding: 3px 0; font-size: 12px;
  }
  .crr-dot { font-size: 8px; }
  .crr-name { flex: 1; color: #e4e4e7; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .crr-status { font-size: 11px; white-space: nowrap; }

  /* ── Needs Attention ── */
  .attention { padding: 0 24px 24px; }
  .attention-header {
    font-size: 12px; font-weight: 700; color: #71717a;
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 8px 0; border-bottom: 1px solid #1c1c22;
    margin-bottom: 4px;
  }
  .attention-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 6px;
    transition: background 0.1s;
  }
  .attention-row:hover { background: #18181b; }
  .ar-dot { font-size: 12px; }
  .ar-name { font-size: 13px; color: #e4e4e7; font-weight: 500; }
  .ar-source { font-size: 11px; color: #52525b; flex: 1; }
  .ar-status { font-size: 12px; white-space: nowrap; }
  .ar-progress { width: 100px; height: 3px; background: #27272a; border-radius: 2px; overflow: hidden; }
  .ar-bar { height: 100%; border-radius: 2px; transition: width 0.12s; }
  .ar-pct { font-size: 11px; width: 30px; text-align: right; }
  .ar-btn {
    padding: 3px 10px; background: transparent; border: 1px solid #27272a;
    color: #a1a1aa; border-radius: 5px; cursor: pointer; font-size: 11px;
    transition: all 0.15s;
  }
  .ar-btn:hover { background: #27272a; color: #fafafa; }

  .all-clear {
    display: flex; align-items: center; justify-content: center;
    gap: 8px; padding: 40px; color: #61fd5f; font-size: 14px;
  }
  .all-clear-icon { font-size: 20px; }

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
  .modal-close {
    background: none; border: none; color: #71717a; cursor: pointer;
    font-size: 16px; transition: color 0.12s;
  }
  .modal-close:hover { color: #fafafa; }
  .modal-body { padding: 14px 18px; overflow-y: auto; flex: 1; }
  .modal-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 10px 18px; border-top: 1px solid #27272a;
  }
  .loading {
    display: flex; align-items: center; gap: 12px;
    padding: 20px 0; color: #a1a1aa; font-size: 13px;
  }
  .spinner {
    width: 18px; height: 18px; border: 2px solid #27272a;
    border-top-color: #4B95E9; border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  .found-count { font-size: 12px; color: #71717a; margin: 0 0 10px; }
  .dr-row {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 4px; cursor: pointer; border-radius: 4px; font-size: 13px;
  }
  .dr-row:hover { background: #27272a; }
  .dr-row input { accent-color: #4B95E9; }
  .dr-name { color: #e4e4e7; font-weight: 500; }
  .dr-desc { color: #52525b; font-size: 11px; margin-left: auto; }

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
