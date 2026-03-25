<!--
  gitbox — Final Hybrid Prototype
  ================================
  Cloud Sync simplicity + Dashboard Cards account boxes + flat repo list
  Paste into SvelteLab +page.svelte to test.
-->

<script>
  import { writable, derived } from 'svelte/store';
  import { fade, slide } from 'svelte/transition';
  import { onMount } from 'svelte';

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  THEME — System / Light / Dark                              ║
  // ╚══════════════════════════════════════════════════════════════╝

  // 'system' | 'light' | 'dark'
  let themeChoice = 'system';
  // Use a store so statusColor/credColor reactively update in the template
  const themeStore = writable('dark');
  let resolvedTheme = 'dark';

  function applyTheme() {
    if (themeChoice === 'system') {
      resolvedTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    } else {
      resolvedTheme = themeChoice;
    }
    themeStore.set(resolvedTheme);
    document.documentElement.setAttribute('data-theme', resolvedTheme);
  }

  function cycleTheme() {
    const order = ['system', 'light', 'dark'];
    themeChoice = order[(order.indexOf(themeChoice) + 1) % 3];
    applyTheme();
  }

  function themeIcon(choice) {
    return { system: '◐', light: '☀', dark: '☾' }[choice] || '◐';
  }

  onMount(() => {
    applyTheme();
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (themeChoice === 'system') applyTheme();
    });
  });

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  DATA LAYER — mirrors Go pkg/config                         ║
  // ╚══════════════════════════════════════════════════════════════╝

  const accounts = writable({
    'git-parchis-luis': {
      provider: 'forgejo', url: 'https://git.parchis.org',
      username: 'luis', name: 'Luis Palacios',
      credType: 'gcm', credStatus: 'ok'
    },
    'github-LuisPalacios': {
      provider: 'github', url: 'https://github.com',
      username: 'LuisPalacios', name: 'Luis Palacios',
      credType: 'gcm', credStatus: 'ok'
    },
    'github-Renueva': {
      provider: 'github', url: 'https://github.com',
      username: 'Renueva', name: 'Renueva Dev',
      credType: 'gcm', credStatus: 'ok'
    },
    'github-Azelerum': {
      provider: 'github', url: 'https://github.com',
      username: 'Azelerum', name: 'Azelerum',
      credType: 'ssh', credStatus: 'warning'
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
    'github-Renueva': {
      account: 'github-Renueva',
      repos: ['Renueva/platform', 'Renueva/docs', 'Renueva/infra']
    },
    'github-Azelerum': {
      account: 'github-Azelerum',
      repos: ['Azelerum/homelab']
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
    'github-Renueva/Renueva/platform':     { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-Renueva/Renueva/docs':         { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-Renueva/Renueva/infra':        { status: 'synced', progress: 0, behind: 0, modified: 0 },
    'github-Azelerum/Azelerum/homelab': { status: 'synced', progress: 0, behind: 0, modified: 0 }
  });

  // Derived: global summary
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

  // Derived: per-account stats (for card rings)
  const accountStats = derived([sources, repoStates], ([$src, $rs]) => {
    const stats = {};
    for (const [sourceKey, source] of Object.entries($src)) {
      const acctKey = source.account;
      if (!stats[acctKey]) stats[acctKey] = { total: 0, synced: 0, issues: 0 };
      for (const repoName of source.repos) {
        const state = $rs[`${sourceKey}/${repoName}`];
        if (!state) continue;
        stats[acctKey].total++;
        if (state.status === 'synced') stats[acctKey].synced++;
        else stats[acctKey].issues++;
      }
    }
    return stats;
  });

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  SIMULATION — replace with Wails calls in production        ║
  // ╚══════════════════════════════════════════════════════════════╝

  let syncing = false;

  function simulateProgress(key, finalStatus) {
    return new Promise((resolve) => {
      repoStates.update(s => {
        const isClone = s[key].status === 'not_local';
        s[key] = { ...s[key], progress: 0, status: isClone ? 'cloning' : 'syncing' };
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
      await simulateProgress(key, 'synced');
    }
    syncing = false;
  }

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  DISCOVERY                                                  ║
  // ╚══════════════════════════════════════════════════════════════╝

  let discoverModal = null;
  let discoverLoading = false;
  let discoverRepos = [];
  let discoverSelected = {};

  const fakeDiscoverResults = [
    { name: 'team/new-project', desc: 'A brand new project' },
    { name: 'team/experiment', desc: 'Experimental work' },
    { name: 'other-org/shared-tools', desc: 'Shared utilities' },
    { name: 'team/archived-thing', desc: 'Old archived repo', archived: true },
  ];

  async function openDiscover(accountKey) {
    discoverModal = accountKey;
    discoverLoading = true;
    discoverRepos = [];
    discoverSelected = {};
    // WAILS: window.go.main.App.Discover(accountKey)
    await new Promise(r => setTimeout(r, 1500));
    discoverRepos = fakeDiscoverResults;
    discoverSelected = {};
    discoverLoading = false;
  }

  $: selectedCount = Object.values(discoverSelected).filter(Boolean).length;
  $: allSelected = discoverRepos.length > 0 && selectedCount === discoverRepos.length;

  function toggleAll() {
    if (allSelected) {
      discoverSelected = {};
    } else {
      discoverSelected = Object.fromEntries(discoverRepos.map(r => [r.name, true]));
    }
  }

  function addDiscovered() {
    // WAILS: window.go.main.App.AddDiscoveredRepos(discoverModal, selectedRepos)
    discoverModal = null;
  }

  // ╔══════════════════════════════════════════════════════════════╗
  // ║  HELPERS                                                    ║
  // ╚══════════════════════════════════════════════════════════════╝

  function statusLabel(s, behind, modified) {
    const map = {
      synced: 'Synced', behind: `${behind} behind`, dirty: `${modified} local change${modified > 1 ? 's' : ''}`,
      syncing: 'Refreshing...', cloning: 'Bringing local...', not_local: 'Not local', error: 'Error'
    };
    return map[s] || s;
  }
  function statusColor(s) {
    // Read $themeStore to create reactive dependency
    const theme = $themeStore;
    const dark = {
      synced: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
      syncing: '#4B95E9', cloning: '#4B95E9', not_local: '#71717a', error: '#D81E5B'
    };
    const light = {
      synced: '#166534', behind: '#a21caf', dirty: '#c2410c',
      syncing: '#2563eb', cloning: '#2563eb', not_local: '#52525b', error: '#be123c'
    };
    const palette = theme === 'light' ? light : dark;
    return palette[s] || (theme === 'light' ? '#52525b' : '#71717a');
  }
  function credColor(s) {
    const theme = $themeStore;
    if (theme === 'light') {
      return s === 'ok' ? '#166534' : s === 'warning' ? '#c2410c' : '#be123c';
    }
    return s === 'ok' ? '#61fd5f' : s === 'warning' ? '#F07623' : '#D81E5B';
  }
  function providerLabel(p) { return { github: 'GitHub', forgejo: 'Forgejo', gitea: 'Gitea', gitlab: 'GitLab' }[p] || p; }

  // Settings panel
  let showSettings = false;
</script>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  TEMPLATE                                                   -->
<!-- ════════════════════════════════════════════════════════════ -->

<div class="app">

  <!-- ── TOP BAR ── -->
  <header class="topbar">
    <div class="brand">
      <svg class="logo-svg" viewBox="0 0 500 500" xmlns="http://www.w3.org/2000/svg">
        <g transform="matrix(0.838711, 0, 0, 0.838711, 52.15668, 40.24861)">
          <path d="M 119.779 265.572 L 146.694 265.582 C 146.694 265.582 162.549 310.895 163.783 313.997 L 145.642 332.841 L 166.338 355.62 L 185.565 336.41 C 184.801 337.405 234.42 356.617 234.42 356.617 L 234.74 387.858 L 265.36 389.26 L 265.013 356.868 L 313.812 336.358 L 339.89 361.913 L 362.266 340.797 L 336.562 314.13 L 354.13 265.424 L 394.197 265.15 L 393.609 234.438 L 353.715 234.488 C 352.82 232.067 335.975 185.534 335.975 185.534 L 360.441 160.899 L 337.258 140.162 L 313.098 163.288 C 313.098 163.288 266.045 143.401 265.958 143.359 L 265.79 114.664 L 234.642 116.098 L 234.194 143.169 L 186.316 163.095 L 168.728 146.231 L 147.462 168.692 L 163.361 185.54 L 146.557 234.451 L 120.386 234.37 M 210.493 187.051 L 256.57 234.488 C 256.57 234.488 320.45 234.364 320.586 234.229 C 320.721 234.094 306.162 193.954 306.162 193.954 C 306.162 193.954 250.088 170.32 249.69 170.347 M 187.967 210.526 C 187.649 210.754 174.103 248.738 174.103 249.471 C 174.103 250.2 187.998 289.362 188.741 289.362 C 189.632 289.362 227.328 250.756 227.328 249.843 C 227.328 248.581 188.7 209.996 187.967 210.526 M 257.27 265.455 C 256.179 265.594 249.754 271.775 232.923 288.884 C 220.348 301.667 210.161 312.294 210.285 312.494 C 210.41 312.695 212.068 313.532 213.966 314.356 C 215.864 315.18 220.73 317.306 224.778 319.081 C 241.811 326.551 248.988 329.565 249.748 329.565 C 250.514 329.565 252.968 328.567 267.247 322.463 C 272.788 320.092 283.196 315.705 300.652 308.373 L 306.313 305.995 L 308.715 299.518 C 310.034 295.957 311.861 291.004 312.774 288.514 C 313.686 286.021 315.955 279.905 317.813 274.924 C 319.673 269.94 321.108 265.771 321 265.655 C 320.761 265.397 259.289 265.202 257.27 265.455" fill="var(--logo-light)" fill-rule="evenodd"/>
          <g>
            <rect x="389.945" y="234.245" width="88.288" height="31.534" fill="var(--logo-dark)"/>
            <rect x="145.508" y="578.72" width="57.346" height="31.534" fill="var(--logo-dark)" transform="matrix(0.707107, -0.707107, 0.707107, 0.707107, -37.625373, -231.099206)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
            <rect x="268.056" y="574.269" width="50.735" height="31.007" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 89.660145, -467.147635)" style="transform-origin: 152.09px 305.696px;"/>
            <g transform="matrix(1.929278, 0, 0, 1.929278, -169.667138, -230.101468)">
              <rect x="167.474" y="297.66" width="31.698" height="16.072" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 97.683973, 6.752047)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(0, 1.929278, -1.929278, 0, -117.597083, 162.12922)" style="transform-origin: 290.242px 321.943px;">
              <path d="M 234.052 304.012 L 295.85 303.793 L 295.85 319.865 L 240.576 320.139 L 236.241 324.134 L 225.125 312.911 L 234.052 304.012 Z" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 4.206065, -14.423297)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(-1.364205, 1.364205, -1.364205, -1.364205, -225.276898, -72.630123)" style="transform-origin: 290.242px 321.943px;">
              <rect x="167.474" y="297.66" width="31.698" height="16.072" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 97.683973, 6.752047)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(-1.929277, 0, 0, -1.929277, -96.489034, -305.33812)" style="transform-origin: 290.242px 321.943px;">
              <path d="M 157.74 296.617 L 199.172 297.66 L 199.172 313.732 L 151.483 313.732 L 133.58 295.866 L 144.962 284.648 L 157.74 296.617 Z" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, 107.248061, -3.32573)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="298.182" cy="330.143" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
            <g transform="matrix(0, -1.929278, 1.929278, 0, 97.57237, -210.81805)" style="transform-origin: 290.242px 321.943px;">
              <rect x="299.224" y="297.66" width="56.634" height="16.072" fill="var(--logo-dark)" transform="matrix(0.707107, 0.707107, -0.707107, 0.707107, -37.717619, 15.568392)" style="transform-box: fill-box; transform-origin: 50% 50%;"/>
              <ellipse cx="319.805" cy="350.736" rx="18.184" ry="18.184" fill="var(--logo-dark)"/>
            </g>
          </g>
        </g>
      </svg>
      <span class="title">gitbox</span>
    </div>
    <div class="health">
      <span class="health-ring" style="--pct: {$summary.total ? ($summary.synced / $summary.total) * 100 : 0}">
        <span class="health-num">{$summary.synced}/{$summary.total}</span>
      </span>
      <span class="health-label">synced</span>
    </div>
    <div class="topbar-actions">
      <button class="btn-sync" on:click={syncAll}
        disabled={syncing || ($summary.behind === 0 && $summary.not_local === 0)}>
        <span class="sync-icon" class:spinning={syncing}>&#8635;</span>
        {syncing ? 'Syncing...' : 'Sync All'}
      </button>
      <button class="btn-gear" on:click={cycleTheme} title="Theme: {themeChoice}">{themeIcon(themeChoice)}</button>
      <button class="btn-gear" on:click={() => showSettings = !showSettings} title="Settings" class:active-gear={showSettings}>&#9881;</button>
    </div>
  </header>

  <!-- ── SETTINGS PANEL ── -->
  {#if showSettings}
    <div class="settings" transition:slide={{ duration: 150 }}>
      <div class="settings-row">
        <span class="settings-label">Config</span>
        <span class="settings-value">~/.config/gitbox/gitbox.json</span>
        <button class="settings-btn" on:click={() => {
          // WAILS: runtime.BrowserOpenURL(ctx, configPath) or shell.OpenFile()
          alert('Would open config in default editor');
        }}>Open in Editor</button>
      </div>
      <div class="settings-row">
        <span class="settings-label">Clone folder</span>
        <span class="settings-value">~/00.git</span>
      </div>
      <div class="settings-row">
        <span class="settings-label">Theme</span>
        <div class="theme-toggle">
          <button class="theme-btn" class:theme-active={themeChoice === 'system'} on:click={() => { themeChoice = 'system'; applyTheme(); }}>System</button>
          <button class="theme-btn" class:theme-active={themeChoice === 'light'} on:click={() => { themeChoice = 'light'; applyTheme(); }}>Light</button>
          <button class="theme-btn" class:theme-active={themeChoice === 'dark'} on:click={() => { themeChoice = 'dark'; applyTheme(); }}>Dark</button>
        </div>
      </div>
      <div class="settings-row">
        <span class="settings-label">Accounts</span>
        <span class="settings-value">{Object.keys($accounts).length} configured</span>
      </div>
    </div>
  {/if}

  <!-- ── ACCOUNT CARDS ── -->
  <section class="cards-row">
    {#each Object.entries($accounts) as [key, acct]}
      {@const stats = $accountStats[key] || { total: 0, synced: 0, issues: 0 }}
      <div class="card">
        <div class="card-top">
          <span class="card-dot" style="background: {credColor(acct.credStatus)}"></span>
          <span class="card-provider">{providerLabel(acct.provider)}</span>
        </div>
        <div class="card-name">{key.replace(/^(github|git)-/, '')}</div>
        <div class="card-ring-row">
          <svg class="mini-ring" viewBox="0 0 36 36">
            <circle cx="18" cy="18" r="15" fill="none" stroke="#27272a" stroke-width="3"/>
            <circle cx="18" cy="18" r="15" fill="none"
              stroke="{stats.issues === 0 ? statusColor('synced') : statusColor('behind')}"
              stroke-width="3" stroke-linecap="round"
              stroke-dasharray="{(stats.synced / Math.max(stats.total, 1)) * 94.2} 94.2"
              transform="rotate(-90 18 18)"/>
          </svg>
          <span class="card-stat">{stats.synced}/{stats.total}</span>
          {#if stats.issues > 0}
            <span class="card-issues" style="color: {statusColor('behind')}">{stats.issues} need{stats.issues > 1 ? '' : 's'} attention</span>
          {:else}
            <span class="card-ok" style="color: {statusColor('synced')}">All good</span>
          {/if}
        </div>
        <button class="card-btn" on:click={() => openDiscover(key)}>Find Projects</button>
      </div>
    {/each}
  </section>

  <!-- ── REPO LIST (flat, grouped by source) ── -->
  <section class="repo-list">
    {#each Object.entries($sources) as [sourceKey, source]}
      <div class="source-group">
        <div class="source-header">{sourceKey}</div>
        {#each source.repos as repoName}
          {@const repoKey = `${sourceKey}/${repoName}`}
          {@const state = $repoStates[repoKey] || { status: 'error', progress: 0, behind: 0, modified: 0 }}
          <div class="repo-row">
            <!-- Status indicator -->
            <span class="dot" style="color: {statusColor(state.status)}">
              {#if state.status === 'synced'}&#9679;
              {:else if state.status === 'syncing' || state.status === 'cloning'}&#9684;
              {:else if state.status === 'not_local'}&#9675;
              {:else if state.status === 'behind'}&#9687;
              {:else if state.status === 'dirty'}&#9670;
              {:else}&#10005;{/if}
            </span>

            <!-- Name -->
            <span class="repo-name">{repoName}</span>

            <!-- Progress or status + action -->
            {#if state.status === 'syncing' || state.status === 'cloning'}
              <div class="progress-track">
                <div class="progress-fill" style="width:{state.progress}%; background:{statusColor(state.status)}"></div>
              </div>
              <span class="progress-pct" style="color:{statusColor(state.status)}">{state.progress}%</span>
            {:else}
              <span class="status-text" style="color:{statusColor(state.status)}">
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

  <!-- ── SUMMARY FOOTER ── -->
  <footer class="summary">
    <span class="sum" style="color:{statusColor('synced')}">{$summary.synced} synced</span>
    {#if $summary.syncing > 0}<span class="sep">&middot;</span><span class="sum" style="color:{statusColor('syncing')}">{$summary.syncing} syncing</span>{/if}
    {#if $summary.behind > 0}<span class="sep">&middot;</span><span class="sum" style="color:{statusColor('behind')}">{$summary.behind} behind</span>{/if}
    {#if $summary.dirty > 0}<span class="sep">&middot;</span><span class="sum" style="color:{statusColor('dirty')}">{$summary.dirty} local changes</span>{/if}
    {#if $summary.not_local > 0}<span class="sep">&middot;</span><span class="sum" style="color:{statusColor('not_local')}">{$summary.not_local} not local</span>{/if}
  </footer>

  <!-- ── DISCOVER MODAL ── -->
  {#if discoverModal}
    <div class="overlay" on:click={() => discoverModal = null} transition:fade={{ duration: 120 }}>
      <div class="modal" on:click|stopPropagation transition:slide={{ duration: 180 }}>
        <div class="modal-head">
          <h3>Find Projects &mdash; {discoverModal}</h3>
          <button class="btn-x" on:click={() => discoverModal = null}>&#10005;</button>
        </div>
        <div class="modal-body">
          {#if discoverLoading}
            <div class="loading"><div class="spinner"></div><span>Checking your account...</span></div>
          {:else}
            <p class="found">Found {discoverRepos.length} new projects:</p>
            <label class="dr dr-all">
              <input type="checkbox" checked={allSelected} on:change={toggleAll} />
              <span class="dr-name">Select all</span>
            </label>
            {#each discoverRepos as repo}
              <label class="dr">
                <input type="checkbox" bind:checked={discoverSelected[repo.name]} />
                <span class="dr-name">{repo.name}</span>
                {#if repo.archived}<span class="dr-tag">archived</span>{/if}
                <span class="dr-desc">{repo.desc}</span>
              </label>
            {/each}
          {/if}
        </div>
        {#if !discoverLoading}
          <div class="modal-foot">
            <button class="btn-cancel" on:click={() => discoverModal = null}>Cancel</button>
            <button class="btn-add" on:click={addDiscovered} disabled={selectedCount === 0}>Add Selected ({selectedCount})</button>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- ════════════════════════════════════════════════════════════ -->
<!--  STYLES                                                     -->
<!-- ════════════════════════════════════════════════════════════ -->

<style>
  /* ── Theme Variables ── */
  :global([data-theme="dark"]) {
    --bg-base: #09090b; --bg-card: #18181b; --bg-hover: #27272a;
    --border: #27272a; --border-hover: #3f3f46;
    --text-primary: #fafafa; --text-secondary: #a1a1aa; --text-muted: #71717a; --text-dim: #52525b;
    --text-repo: #e4e4e7;
    --logo-dark: #4abdd4; --logo-light: #7cd9ec;
    --ring-bg: #27272a; --ring-accent: #61fd5f; --overlay: rgba(0,0,0,0.6);
  }
  :global([data-theme="light"]) {
    --bg-base: #fafafa; --bg-card: #ffffff; --bg-hover: #f4f4f5;
    --border: #e4e4e7; --border-hover: #d4d4d8;
    --text-primary: #18181b; --text-secondary: #52525b; --text-muted: #71717a; --text-dim: #a1a1aa;
    --text-repo: #27272a;
    --logo-dark: #1c5566; --logo-light: #2e9fc0;
    --ring-bg: #e4e4e7; --ring-accent: #166534; --overlay: rgba(0,0,0,0.3);
  }
  :global(html) { color-scheme: dark light; }
  :global(body) {
    margin: 0; background: var(--bg-base);
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', system-ui, sans-serif;
    color: var(--text-primary); -webkit-font-smoothing: antialiased;
    transition: background 0.2s, color 0.2s;
  }

  .app { max-width: 860px; margin: 0 auto; min-height: 100vh; display: flex; flex-direction: column; }

  /* ── Top Bar ── */
  .topbar { display: flex; align-items: center; gap: 16px; padding: 14px 24px; border-bottom: 1px solid var(--border); }
  .brand { display: flex; align-items: center; gap: 8px; }
  .logo-svg { width: 26px; height: 26px; }
  .title { font-size: 17px; font-weight: 700; letter-spacing: -0.3px; }

  .health { display: flex; align-items: center; gap: 8px; margin-left: auto; }
  .health-ring {
    width: 38px; height: 38px; border-radius: 50%;
    background: conic-gradient(var(--ring-accent) calc(var(--pct) * 1%), var(--ring-bg) 0);
    display: flex; align-items: center; justify-content: center; position: relative;
  }
  .health-ring::before { content: ''; position: absolute; inset: 4px; background: var(--bg-base); border-radius: 50%; transition: background 0.2s; }
  .health-num { position: relative; z-index: 1; font-size: 10px; font-weight: 700; }
  .health-label { font-size: 12px; color: var(--text-muted); }

  .topbar-actions { display: flex; gap: 8px; }
  .btn-sync {
    display: flex; align-items: center; gap: 6px;
    padding: 7px 14px; background: var(--bg-card); border: 1px solid var(--border);
    color: var(--text-primary); border-radius: 7px; cursor: pointer;
    font-size: 12px; font-weight: 500; transition: all 0.15s;
  }
  .btn-sync:hover:not(:disabled) { background: var(--bg-hover); border-color: var(--border-hover); }
  .btn-sync:disabled { opacity: 0.4; cursor: default; }
  .sync-icon { font-size: 15px; display: inline-block; }
  .spinning { animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .btn-gear {
    background: none; border: 1px solid transparent; color: var(--text-muted);
    font-size: 18px; cursor: pointer; padding: 4px 6px; border-radius: 6px; transition: all 0.12s;
  }
  .btn-gear:hover { color: var(--text-primary); background: var(--bg-hover); }

  /* ── Account Cards ── */
  .cards-row { display: flex; gap: 10px; padding: 18px 24px; overflow-x: auto; }
  .card {
    flex: 1; min-width: 165px; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 12px 14px; transition: border-color 0.15s;
  }
  .card:hover { border-color: var(--border-hover); }
  .card-top { display: flex; align-items: center; gap: 6px; margin-bottom: 2px; }
  .card-dot { width: 7px; height: 7px; border-radius: 50%; }
  .card-provider { font-size: 10px; color: var(--text-muted); font-weight: 600; letter-spacing: 0.3px; }
  .card-name { font-size: 14px; font-weight: 600; margin-bottom: 8px; }

  .card-ring-row { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
  .mini-ring { width: 28px; height: 28px; flex-shrink: 0; }
  .card-stat { font-size: 12px; font-weight: 600; color: var(--text-repo); }
  .card-issues { font-size: 11px; font-weight: 600; }
  .card-ok { font-size: 11px; font-weight: 600; }

  .card-btn {
    width: 100%; padding: 5px 0; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 11px; font-weight: 500;
    transition: all 0.15s;
  }
  .card-btn:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }

  /* ── Repo List ── */
  .repo-list { flex: 1; padding: 0 24px 12px; }
  .source-group { margin-bottom: 6px; }
  .source-header {
    font-size: 11px; font-weight: 600; color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.8px;
    padding: 10px 0 5px; border-bottom: 1px solid var(--border);
  }
  .repo-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 6px; border-radius: 6px; transition: background 0.1s;
  }
  .repo-row:hover { background: var(--bg-card); }

  .dot { font-size: 14px; flex-shrink: 0; width: 18px; text-align: center; }
  .repo-name { font-size: 13px; color: var(--text-repo); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .progress-track { width: 110px; height: 4px; background: var(--ring-bg); border-radius: 2px; overflow: hidden; flex-shrink: 0; }
  .progress-fill { height: 100%; border-radius: 2px; transition: width 0.12s ease-out; }
  .progress-pct { font-size: 11px; width: 32px; text-align: right; flex-shrink: 0; font-weight: 600; }

  .status-text { font-size: 12px; white-space: nowrap; font-weight: 600; }

  .btn-action {
    padding: 3px 10px; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 5px; cursor: pointer;
    font-size: 11px; font-weight: 500; transition: all 0.15s; flex-shrink: 0;
  }
  .btn-action:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }

  /* ── Summary ── */
  .summary {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    padding: 10px 24px; border-top: 1px solid var(--border); font-size: 12px; font-weight: 500;
  }
  .sum { font-weight: 600; }
  .sep { color: var(--border-hover); }

  /* ── Settings ── */
  .settings {
    padding: 14px 24px; border-bottom: 1px solid var(--border);
    background: var(--bg-card); display: flex; flex-direction: column; gap: 8px;
  }
  .settings-row { display: flex; align-items: center; gap: 12px; }
  .settings-label { font-size: 11px; font-weight: 600; color: var(--text-muted); width: 80px; flex-shrink: 0; }
  .settings-value { font-size: 12px; color: var(--text-secondary); font-family: monospace; flex: 1; }
  .settings-btn {
    padding: 2px 8px; font-size: 10px; font-weight: 500;
    background: transparent; border: 1px solid var(--border); color: var(--text-secondary);
    border-radius: 4px; cursor: pointer; transition: all 0.12s; white-space: nowrap;
  }
  .settings-btn:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .theme-toggle { display: flex; gap: 4px; }
  .theme-btn {
    padding: 3px 10px; font-size: 11px; border: 1px solid var(--border);
    background: transparent; color: var(--text-secondary); border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .theme-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .theme-active { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .active-gear { color: var(--text-primary) !important; }

  /* ── Modal ── */
  .overlay {
    position: fixed; inset: 0; background: var(--overlay);
    display: flex; align-items: center; justify-content: center; z-index: 100;
  }
  .modal {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 12px; width: 440px; max-height: 70vh;
    display: flex; flex-direction: column;
  }
  .modal-head {
    display: flex; justify-content: space-between; align-items: center;
    padding: 14px 18px; border-bottom: 1px solid var(--border);
  }
  .modal-head h3 { margin: 0; font-size: 14px; font-weight: 600; }
  .btn-x { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 16px; transition: color 0.12s; }
  .btn-x:hover { color: var(--text-primary); }
  .modal-body { padding: 14px 18px; overflow-y: auto; flex: 1; }
  .modal-foot {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 10px 18px; border-top: 1px solid var(--border);
  }

  .loading { display: flex; align-items: center; gap: 12px; padding: 20px 0; color: var(--text-secondary); font-size: 13px; }
  .spinner { width: 18px; height: 18px; border: 2px solid var(--border); border-top-color: #4B95E9; border-radius: 50%; animation: spin 0.7s linear infinite; }
  .found { font-size: 12px; color: var(--text-muted); margin: 0 0 10px; }

  .dr { display: flex; align-items: center; gap: 8px; padding: 6px 4px; cursor: pointer; border-radius: 4px; font-size: 13px; }
  .dr:hover { background: var(--bg-hover); }
  .dr-all { border-bottom: 1px solid var(--border); padding-bottom: 8px; margin-bottom: 4px; }
  .dr input { accent-color: #4B95E9; }
  .dr-name { color: var(--text-repo); font-weight: 500; }
  .dr-desc { color: var(--text-dim); font-size: 11px; margin-left: auto; }
  .dr-tag { font-size: 9px; padding: 1px 5px; background: var(--bg-hover); color: var(--text-dim); border-radius: 3px; }

  .btn-cancel { padding: 6px 12px; background: var(--bg-hover); border: 1px solid var(--border-hover); color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 12px; }
  .btn-cancel:hover { color: var(--text-primary); }
  .btn-add { padding: 6px 12px; background: #4B95E9; border: none; color: #fff; border-radius: 6px; cursor: pointer; font-size: 12px; font-weight: 500; }
  .btn-add:hover:not(:disabled) { background: #3b82f6; }
  .btn-add:disabled { opacity: 0.4; cursor: default; }
</style>
