<script>
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';

  // ── Theme ──
  let themeChoice = 'dark';
  let resolvedTheme = 'dark';

  function applyTheme() {
    if (themeChoice === 'system') {
      resolvedTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    } else {
      resolvedTheme = themeChoice;
    }
    document.documentElement.setAttribute('data-theme', resolvedTheme);
  }

  function cycleTheme() {
    const order = ['dark', 'system', 'light'];
    themeChoice = order[(order.indexOf(themeChoice) + 1) % 3];
    applyTheme();
  }

  function themeIcon(choice) {
    return { system: '◐', light: '☀', dark: '☾' }[choice] || '◐';
  }

  let showSettings = false;

  onMount(() => {
    applyTheme();
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (themeChoice === 'system') applyTheme();
    });
  });

  // ── Color helpers (inlined from theme.ts) ──
  const darkPalette = {
    clean: '#61fd5f', behind: '#D91C9A', dirty: '#F07623',
    ahead: '#4B95E9', 'not cloned': '#71717a', 'no upstream': '#71717a',
    error: '#D81E5B',
  };
  const lightPalette = {
    clean: '#166534', behind: '#a21caf', dirty: '#c2410c',
    ahead: '#2563eb', 'not cloned': '#52525b', 'no upstream': '#52525b',
    error: '#be123c',
  };
  $: palette = resolvedTheme === 'light' ? lightPalette : darkPalette;
  $: sc = (s) => palette[s] || (resolvedTheme === 'light' ? '#52525b' : '#71717a');

  function credBadgeClass(cred) {
    return cred === 'ok' ? 'cred-badge-ok' : cred === 'error' ? 'cred-badge-err' : '';
  }

  const symbols = { clean: '●', behind: '◗', dirty: '◆', 'not cloned': '○' };
  const sym = (s) => symbols[s] || '?';

  // ── Demo data (anonymized) ──
  const accounts = [
    { key: 'acme-ops', provider: 'Forgejo', cred: 'gcm', credStatus: 'ok', synced: 2, total: 4, issues: 2 },
    { key: 'stellar-dev', provider: 'GitHub', cred: 'ssh', credStatus: 'ok', synced: 3, total: 3, issues: 0 },
    { key: 'nebula-team', provider: 'GitLab', cred: 'token', credStatus: 'ok', synced: 1, total: 2, issues: 1 },
  ];

  const sources = [
    { header: 'acme-ops', repos: [
      { name: 'infra/k8s-prod', status: 'clean' },
      { name: 'infra/terraform', status: 'clean' },
      { name: 'platform/api', status: 'behind', behind: 3 },
      { name: 'platform/web', status: 'not cloned' },
    ]},
    { header: 'stellar-dev', repos: [
      { name: 'stellar-dev/cosmos', status: 'clean' },
      { name: 'stellar-dev/orbit', status: 'clean' },
      { name: 'stellar-dev/nova', status: 'clean' },
    ]},
    { header: 'nebula-team', repos: [
      { name: 'nebula/dashboard', status: 'clean' },
      { name: 'nebula/pipeline', status: 'dirty', modified: 2 },
    ]},
  ];

  const summary = { clean: 6, behind: 1, dirty: 1, notCloned: 1, total: 9 };

  // ── Repo detail expand/collapse ──
  let expandedRepo = null;
  const demoDetail = {
    branch: 'main', ahead: 0, behind: 0,
    changed: [
      { kind: 'modified', path: 'src/ingest/parser.go' },
      { kind: 'added', path: 'src/ingest/parser_test.go' },
    ],
    untracked: [],
  };

  function toggleDetail(sourceHeader, repoName, status) {
    if (status !== 'dirty') return;
    const key = `${sourceHeader}/${repoName}`;
    expandedRepo = expandedRepo === key ? null : key;
  }

  function kindIcon(kind) {
    return kind === 'deleted' ? '−' : kind === 'added' ? '+' : kind === 'renamed' ? '→' : '~';
  }
  function kindLabel(kind) {
    return kind === 'deleted' ? 'Deleted' : kind === 'added' ? 'New file' : kind === 'renamed' ? 'Renamed' : 'Changed';
  }
</script>

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
      <span class="tagline">accounts & clones</span>
    </div>
    <div class="health">
      <span class="health-ring" style="--pct: {(summary.clean / summary.total) * 100}">
        <span class="health-num">{summary.clean}/{summary.total}</span>
      </span>
      <span class="health-label">synced</span>
    </div>
    <div class="topbar-actions">
      <button class="btn-gear" title="Pull All">
        <svg class="topbar-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
          <line x1="8" y1="2" x2="8" y2="12"/><polyline points="4.5,8.5 8,12 11.5,8.5"/><line x1="4" y1="14" x2="12" y2="14"/>
        </svg>
      </button>
      <button class="btn-gear" title="Fetch All"><span class="sync-icon">&#8635;</span></button>
      <button class="btn-gear btn-trash" title="Delete mode">&#128465;</button>
      <button class="btn-gear" title="Compact view">◧</button>
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
      </div>
      <div class="settings-row">
        <span class="settings-label">Clone folder</span>
        <span class="settings-value">~/repos</span>
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
        <span class="settings-label">Periodic fetch</span>
        <div class="theme-toggle">
          <button class="theme-btn theme-active">Off</button>
          <button class="theme-btn">5m</button>
          <button class="theme-btn">15m</button>
          <button class="theme-btn">30m</button>
        </div>
      </div>
      <div class="settings-row">
        <span class="settings-label">Version</span>
        <span class="settings-value">1.2.1</span>
      </div>
    </div>
  {/if}

  <!-- ── ACCOUNT CARDS ── -->
  <section class="cards-row">
    {#each accounts as acct}
      {@const pct = (acct.synced / Math.max(acct.total, 1)) * 94.2}
      <div class="card">
        <div class="card-top">
          <span class="card-dot" style="background: {sc('clean')}"></span>
          <span class="card-provider">{acct.provider}</span>
          <span class="cred-badge {credBadgeClass(acct.credStatus)}">{acct.cred}</span>
        </div>
        <div class="card-name">{acct.key}</div>
        <div class="card-ring-row">
          <svg class="mini-ring" viewBox="0 0 36 36">
            <circle cx="18" cy="18" r="15" fill="none" stroke="#27272a" stroke-width="3"/>
            <circle cx="18" cy="18" r="15" fill="none"
              stroke="{acct.issues === 0 ? sc('clean') : sc('behind')}"
              stroke-width="3" stroke-linecap="round"
              stroke-dasharray="{pct} 94.2"
              transform="rotate(-90 18 18)"/>
          </svg>
          <span class="card-stat">{acct.synced}/{acct.total}</span>
          {#if acct.issues > 0}
            <span class="card-issues" style="color: {sc('behind')}">{acct.issues} need attention</span>
          {:else}
            <span class="card-ok" style="color: {sc('clean')}">All good</span>
          {/if}
        </div>
        <button class="card-btn">Find projects</button>
      </div>
    {/each}
    <button class="card card-add" title="Add account">
      <span class="card-add-icon">+</span>
    </button>
  </section>

  <!-- ── REPO LIST ── -->
  <section class="repo-list">
    {#each sources as source}
      <div class="source-group">
        <div class="source-header">{source.header}</div>
        {#each source.repos as repo}
          {@const repoKey = `${source.header}/${repo.name}`}
          <div class="repo-row" class:repo-row-clickable={repo.status === 'dirty'}
            on:click={() => toggleDetail(source.header, repo.name, repo.status)}>
            <span class="dot" style="color: {sc(repo.status)}">{sym(repo.status)}</span>
            <span class="repo-name">{repo.name}</span>
            <span class="status-badges">
              {#if repo.status === 'clean'}
                <span class="status-text" style="color:{sc('clean')}">Synced</span>
              {:else if repo.status === 'not cloned'}
                <span class="status-text" style="color:{sc('not cloned')}">Not local</span>
              {:else if repo.status === 'behind'}
                <span class="status-text" style="color:{sc('behind')}">{repo.behind} behind</span>
              {:else if repo.status === 'dirty'}
                <span class="status-text" style="color:{sc('dirty')}">{repo.modified} local change{repo.modified > 1 ? 's' : ''}</span>
              {/if}
            </span>
            {#if repo.status === 'behind'}
              <button class="btn-action">Pull</button>
            {:else if repo.status === 'not cloned'}
              <button class="btn-action">Bring Local</button>
            {/if}
            {#if repo.status !== 'not cloned'}
              <button class="btn-fetch" title="Fetch origin">&#8635;</button>
            {/if}
          </div>
          {#if expandedRepo === repoKey}
            <div class="repo-detail" transition:slide={{ duration: 150 }}>
              <div class="detail-header">
                <span class="detail-branch">Branch: <strong>{demoDetail.branch}</strong></span>
                <span class="detail-badge" style="color:{sc('dirty')}">&#9998; {repo.modified} changed</span>
              </div>
              <div class="detail-section-title">Changed files</div>
              {#each demoDetail.changed as file}
                <div class="detail-file">
                  <span class="detail-kind" class:kind-added={file.kind === 'added'}>{kindIcon(file.kind)}</span>
                  <span class="detail-path">{file.path}</span>
                </div>
              {/each}
            </div>
          {/if}
        {/each}
      </div>
    {/each}
  </section>

  <!-- ── SUMMARY FOOTER ── -->
  <footer class="summary">
    <span class="sum" style="color:{sc('clean')}">{summary.clean} synced</span>
    <span class="sep">&middot;</span>
    <span class="sum" style="color:{sc('behind')}">{summary.behind} behind</span>
    <span class="sep">&middot;</span>
    <span class="sum" style="color:{sc('dirty')}">{summary.dirty} local changes</span>
    <span class="sep">&middot;</span>
    <span class="sum" style="color:{sc('not cloned')}">{summary.notCloned} not local</span>
  </footer>
</div>

<style>
  :global([data-theme="dark"]) {
    --bg-base: #09090b; --bg-card: #18181b; --bg-hover: #27272a;
    --border: #27272a; --border-hover: #3f3f46;
    --text-primary: #fafafa; --text-secondary: #b4b4bd; --text-muted: #8e8e99; --text-dim: #71717a;
    --text-repo: #e4e4e7;
    --logo-dark: #4abdd4; --logo-light: #7cd9ec;
    --ring-bg: #27272a; --ring-accent: #61fd5f;
    --card-shadow: 0 2px 8px rgba(0, 0, 0, 0.25);
  }
  :global([data-theme="light"]) {
    --bg-base: #fafafa; --bg-card: #ffffff; --bg-hover: #f4f4f5;
    --border: #e4e4e7; --border-hover: #d4d4d8;
    --text-primary: #18181b; --text-secondary: #52525b; --text-muted: #71717a; --text-dim: #a1a1aa;
    --text-repo: #27272a;
    --logo-dark: #1c5566; --logo-light: #2e9fc0;
    --ring-bg: #e4e4e7; --ring-accent: #166534;
    --card-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  }
  :global(html) { color-scheme: dark light; }
  :global(body) {
    margin: 0; background: var(--bg-base);
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', system-ui, sans-serif;
    color: var(--text-primary); -webkit-font-smoothing: antialiased;
    transition: background 0.2s, color 0.2s;
  }

  .app { max-width: 860px; margin: 0 auto; height: 100vh; display: flex; flex-direction: column; overflow: hidden; }

  /* ── Topbar ── */
  .topbar { display: flex; align-items: center; gap: 16px; padding: 14px 24px; border-bottom: 1px solid var(--border); }
  .brand { display: flex; align-items: center; gap: 8px; }
  .logo-svg { width: 26px; height: 26px; }
  .title { font-size: 17px; font-weight: 700; letter-spacing: -0.3px; }
  .tagline { font-size: 11px; font-weight: 400; color: var(--text-muted); letter-spacing: 0.3px; margin-left: 6px; white-space: nowrap; }

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
  .topbar-icon { width: 16px; height: 16px; display: inline-block; }
  .sync-icon { font-size: 18px; display: inline-block; }
  .btn-gear {
    background: none; border: 1px solid transparent; color: var(--text-muted);
    font-size: 18px; cursor: pointer; padding: 4px 6px; border-radius: 6px; transition: all 0.12s;
    width: 32px; height: 32px; display: inline-flex; align-items: center; justify-content: center;
    line-height: 1;
  }
  .btn-gear:hover { color: var(--text-primary); background: var(--bg-hover); }
  .btn-trash { font-size: 15px; }
  .active-gear { color: var(--text-primary) !important; }

  /* ── Settings ── */
  .settings {
    padding: 14px 24px; border-bottom: 1px solid var(--border);
    background: var(--bg-card); display: flex; flex-direction: column; gap: 8px;
  }
  .settings-row { display: flex; align-items: center; gap: 12px; }
  .settings-label { font-size: 11px; font-weight: 600; color: var(--text-muted); width: 80px; flex-shrink: 0; }
  .settings-value { font-size: 12px; color: var(--text-secondary); font-family: monospace; flex: 1; }
  .theme-toggle { display: flex; gap: 4px; }
  .theme-btn {
    padding: 3px 10px; font-size: 11px; border: 1px solid var(--border);
    background: transparent; color: var(--text-secondary); border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .theme-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
  .theme-active { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }

  /* ── Cards ── */
  .cards-row { display: flex; gap: 10px; padding: 18px 24px; overflow-x: auto; flex-shrink: 0; }
  .card {
    flex: 1; min-width: 165px; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 12px 14px; transition: border-color 0.15s, box-shadow 0.15s;
    box-shadow: var(--card-shadow);
  }
  .card:hover { border-color: var(--border-hover); }
  .card-top { display: flex; align-items: center; gap: 6px; margin-bottom: 2px; }
  .card-dot { width: 7px; height: 7px; border-radius: 50%; }
  .card-provider { font-size: 10px; color: var(--text-muted); font-weight: 600; letter-spacing: 0.3px; flex: 1; }
  .cred-badge {
    font-size: 8px; font-weight: 700; letter-spacing: 0.3px; text-transform: uppercase;
    padding: 1px 4px; border-radius: 3px;
    background: var(--bg-hover); border: 1px solid var(--border); color: var(--text-muted);
    line-height: 1.3;
  }
  .cred-badge-ok { background: #14532d; border-color: #166534; color: #86efac; }
  :global([data-theme="light"]) .cred-badge-ok { background: #dcfce7; border-color: #166534; color: #166534; }
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

  .card-add {
    min-width: 60px; max-width: 80px; display: flex; align-items: center; justify-content: center;
    cursor: pointer; border-style: dashed; background: transparent;
    color: var(--text-muted); transition: all 0.15s;
  }
  .card-add:hover { border-color: var(--border-hover); color: var(--text-primary); background: var(--bg-hover); }
  .card-add-icon { font-size: 24px; font-weight: 300; line-height: 1; }

  /* ── Repo list ── */
  .repo-list { flex: 1; padding: 0 24px 12px; overflow-y: auto; min-height: 0; }
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
  .status-badges { display: flex; align-items: center; gap: 6px; }
  .status-text { font-size: 12px; white-space: nowrap; font-weight: 600; }
  .btn-action {
    padding: 3px 10px; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 5px; cursor: pointer;
    font-size: 11px; font-weight: 500; transition: all 0.15s; flex-shrink: 0;
  }
  .btn-action:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .btn-fetch {
    background: transparent; border: none; color: var(--text-dim); cursor: pointer;
    font-size: 14px; padding: 2px 4px; border-radius: 4px; transition: color 0.15s;
    flex-shrink: 0; line-height: 1;
  }
  .btn-fetch:hover { color: var(--text-primary); }

  /* ── Repo detail panel ── */
  .repo-row-clickable { cursor: pointer; }
  .repo-row-clickable:hover { background: var(--bg-card); }
  .repo-detail {
    margin: 0 0 2px 0; padding: 8px 16px 10px 30px;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
    max-height: 180px; overflow-y: auto; font-size: 12px;
  }
  .detail-header {
    display: flex; align-items: center; gap: 10px; margin-bottom: 6px;
    font-size: 11px; color: var(--text-secondary);
  }
  .detail-branch { color: var(--text-muted); }
  .detail-branch strong { color: var(--text-primary); font-weight: 600; }
  .detail-badge { font-weight: 600; font-size: 11px; }
  .detail-section-title {
    font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--text-dim); margin: 6px 0 3px 0;
  }
  .detail-file {
    display: flex; align-items: baseline; gap: 6px; padding: 1px 0;
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 10px;
  }
  .detail-kind {
    width: 14px; text-align: center; font-weight: 700; flex-shrink: 0;
    color: var(--text-secondary);
  }
  .kind-added { color: #61fd5f; }
  :global([data-theme="light"]) .kind-added { color: #166534; }
  .detail-path { color: var(--text-repo); word-break: break-all; }

  /* ── Footer ── */
  .summary {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    padding: 10px 24px; border-top: 1px solid var(--border); font-size: 12px; font-weight: 500;
    flex-shrink: 0;
  }
  .sum { font-weight: 600; }
  .sep { color: var(--border-hover); }
</style>
