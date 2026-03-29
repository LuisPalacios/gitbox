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

  const symbols = { clean: '●', behind: '◗', dirty: '◆', 'not cloned': '○' };
  const sym = (s) => symbols[s] || '?';

  // ── Demo data (anonymized, same as screenshot-full) ──
  const accounts = [
    { key: 'acme-ops', synced: 2, total: 4, issues: 2, repos: [
      { name: 'k8s-prod', status: 'clean' },
      { name: 'terraform', status: 'clean' },
      { name: 'api', status: 'behind', behind: 3 },
      { name: 'web', status: 'not cloned' },
    ]},
    { key: 'stellar-dev', synced: 3, total: 3, issues: 0, repos: [
      { name: 'cosmos', status: 'clean' },
      { name: 'orbit', status: 'clean' },
      { name: 'nova', status: 'clean' },
    ]},
    { key: 'nebula-team', synced: 1, total: 2, issues: 1, repos: [
      { name: 'dashboard', status: 'clean' },
      { name: 'pipeline', status: 'dirty', modified: 2 },
    ]},
  ];

  const totalRepos = 9;
  const syncedRepos = 6;
  $: pct = Math.round((syncedRepos / totalRepos) * 100);
  $: allGood = syncedRepos === totalRepos;

  // ── Expand/collapse ──
  let expanded = {};
  function toggle(key) {
    expanded[key] = !expanded[key];
    expanded = expanded;
  }
</script>

<div class="compact-strip">
  <!-- Global health -->
  <div class="compact-global">
    <svg viewBox="0 0 36 36" class="compact-ring">
      <circle cx="18" cy="18" r="14" fill="none" stroke="var(--ring-bg)" stroke-width="3"/>
      <circle cx="18" cy="18" r="14" fill="none"
        stroke={allGood ? sc('clean') : sc('behind')}
        stroke-width="3" stroke-linecap="round"
        stroke-dasharray="{(syncedRepos / totalRepos) * 87.96} 87.96"
        transform="rotate(-90 18 18)"/>
    </svg>
    <div class="compact-global-text">
      <span class="compact-global-pct" style="color: {allGood ? sc('clean') : sc('behind')}">{pct}%</span>
      <span class="compact-global-label">{syncedRepos}/{totalRepos} synced</span>
    </div>
  </div>

  <div class="compact-sep"></div>

  <!-- Account pills -->
  {#each accounts as acct}
    <button class="compact-acct" class:compact-acct-expanded={expanded[acct.key]} on:click={() => toggle(acct.key)}>
      <svg viewBox="0 0 36 36" class="compact-acct-ring">
        <circle cx="18" cy="18" r="14" fill="none" stroke="var(--ring-bg)" stroke-width="3"/>
        <circle cx="18" cy="18" r="14" fill="none"
          stroke={acct.issues === 0 ? sc('clean') : sc('behind')}
          stroke-width="3" stroke-linecap="round"
          stroke-dasharray="{(acct.synced / Math.max(acct.total, 1)) * 87.96} 87.96"
          transform="rotate(-90 18 18)"/>
      </svg>
      <div class="compact-acct-info">
        <span class="compact-acct-name">{acct.key}</span>
        <span class="compact-acct-stat">
          {#if acct.issues === 0}
            <span style="color:{sc('clean')}">All good</span>
          {:else}
            <span style="color:{sc('behind')}">{acct.issues} need attention</span>
          {/if}
        </span>
      </div>
      <span class="compact-chevron">{expanded[acct.key] ? '▾' : '▸'}</span>
    </button>

    {#if expanded[acct.key]}
      <div class="compact-repo-list" transition:slide={{ duration: 120 }}>
        {#each acct.repos as repo}
          <div class="compact-row" class:compact-row-ok={repo.status === 'clean'}>
            <span class="compact-dot" style="color: {sc(repo.status)}">{sym(repo.status)}</span>
            <span class="compact-repo-name">{repo.name}</span>
            {#if repo.status === 'behind'}
              <span class="compact-badge" style="color: {sc('behind')}">{repo.behind} behind</span>
            {:else if repo.status === 'dirty'}
              <span class="compact-badge" style="color: {sc('dirty')}">{repo.modified} changed</span>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {/each}

  <!-- Bottom actions -->
  <div class="compact-sep"></div>
  <div class="compact-actions">
    <button class="compact-action-btn" on:click={cycleTheme} title="Theme: {themeChoice}">{themeIcon(themeChoice)}</button>
    <button class="compact-action-btn compact-full-btn">◧ Full view</button>
  </div>
</div>

<style>
  :global([data-theme="dark"]) {
    --bg-base: #09090b; --bg-card: #18181b; --bg-hover: #27272a;
    --border: #27272a; --border-hover: #3f3f46;
    --text-primary: #fafafa; --text-secondary: #b4b4bd; --text-muted: #8e8e99; --text-dim: #71717a;
    --text-repo: #e4e4e7;
    --ring-bg: #27272a; --ring-accent: #61fd5f;
  }
  :global([data-theme="light"]) {
    --bg-base: #fafafa; --bg-card: #ffffff; --bg-hover: #f4f4f5;
    --border: #e4e4e7; --border-hover: #d4d4d8;
    --text-primary: #18181b; --text-secondary: #52525b; --text-muted: #71717a; --text-dim: #a1a1aa;
    --text-repo: #27272a;
    --ring-bg: #e4e4e7; --ring-accent: #166534;
  }
  :global(html) { color-scheme: dark light; }
  :global(body) {
    margin: 0; background: var(--bg-base);
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', system-ui, sans-serif;
    color: var(--text-primary); -webkit-font-smoothing: antialiased;
    transition: background 0.2s, color 0.2s;
  }

  /* ── Compact strip ── */
  .compact-strip {
    width: 220px;
    margin: 0 auto;
    background: var(--bg-base);
    color: var(--text-primary);
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    box-sizing: border-box;
    min-height: 100vh;
  }
  .compact-global {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 0;
  }
  .compact-ring { width: 36px; height: 36px; flex-shrink: 0; }
  .compact-global-text { display: flex; flex-direction: column; }
  .compact-global-pct { font-size: 16px; font-weight: 700; line-height: 1.1; }
  .compact-global-label { font-size: 10px; color: var(--text-dim); }

  .compact-sep { border-top: 1px solid var(--border); margin: 2px 0; }

  .compact-acct {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 6px;
    border-radius: 8px;
    border: none;
    background: transparent;
    color: inherit;
    cursor: pointer;
    transition: background 0.1s;
    text-align: left;
    width: 100%;
    font-family: inherit;
  }
  .compact-acct:hover { background: var(--bg-hover); }
  .compact-acct-expanded { background: var(--bg-hover); }
  .compact-acct-ring { width: 24px; height: 24px; flex-shrink: 0; }
  .compact-acct-info { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .compact-acct-name {
    font-size: 11px; font-weight: 600;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .compact-acct-stat { font-size: 9px; }
  .compact-chevron {
    font-size: 10px;
    color: var(--text-dim);
    flex-shrink: 0;
    width: 12px;
    text-align: center;
  }

  .compact-repo-list {
    padding: 0 0 4px 8px;
    border-left: 2px solid var(--border);
    margin-left: 17px;
    margin-bottom: 2px;
  }
  .compact-row {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 2px 6px;
    font-size: 11px;
    border-radius: 4px;
    transition: background 0.1s;
  }
  .compact-row:hover { background: var(--bg-hover); }
  .compact-row-ok { opacity: 0.5; }
  .compact-dot { font-size: 9px; flex-shrink: 0; width: 10px; text-align: center; }
  .compact-repo-name {
    flex: 1;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    font-weight: 500;
  }
  .compact-badge {
    font-size: 8px;
    font-weight: 600;
    white-space: nowrap;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-card);
  }

  .compact-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .compact-action-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 6px;
    font-size: 10px;
    cursor: pointer;
    transition: all 0.12s;
    padding: 4px 8px;
  }
  .compact-action-btn:hover { color: var(--text-primary); border-color: #4B95E9; }
  .compact-full-btn { flex: 1; text-align: center; }
</style>
