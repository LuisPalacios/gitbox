<script lang="ts">
  // Find projects: lists the account's repos from the provider API and adds
  // the picked ones (then clones them).
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts, sources, repoStates } from '../stores';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { onActivateKey } from '../keyboard';
  import type { DiscoverResult } from '../types';

  export let reloadFromDisk: () => Promise<void>;
  export let cloneRepos: (sourceKey: string, repoKeys: string[]) => Promise<void>;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  // An SSH account has no API token for discovery: App opens the token modal.
  export let onNeedsToken: (accountKey: string) => void;

  let discoverModal: string | null = null;
  let discoverLoading = false;
  let discoverError = ''; // last error from Discover, shown inside the Find-projects modal
  let discoverRepos: DiscoverResult[] = [];
  let discoverSelected: Record<string, boolean> = {};
  let discoverFilter = '';
  let orgVisible: Record<string, boolean> = {};

  export function open(accountKey: string) {
    discoverModal = accountKey;
    discoverLoading = true;
    discoverError = '';
    discoverRepos = [];
    discoverSelected = {};
    discoverFilter = '';
    orgVisible = {};
    bridge.discover(accountKey);
  }

  // discover:done — the provider listing arrived (or failed).
  export function onDone(data: any) {
    discoverLoading = false;
    if (data.error) {
      // If SSH account has no API token, close discover modal and show the token setup modal.
      const acctKey = data.accountKey || discoverModal;
      const acct = acctKey ? $accounts[acctKey] : null;
      if (acct?.default_credential_type === 'ssh' && String(data.error).includes('no API token')) {
        discoverModal = null;
        onNeedsToken(acctKey);
        return;
      }
      // Surface the real error in the modal — silently clearing the list
      // hid auth failures (e.g. Forgejo refusing GCM-cached passwords) and
      // made discovery look broken when it was really credential-broken.
      discoverError = String(data.error);
      discoverRepos = [];
      return;
    }
    discoverError = '';
    discoverRepos = (data.repos || []).sort((a: DiscoverResult, b: DiscoverResult) => a.fullName.localeCompare(b.fullName));
    discoverSelected = {};
  }

  function ownerOf(fullName: string): string { return fullName.split('/', 2)[0]; }

  $: owners = [...new Set(discoverRepos.map((r) => ownerOf(r.fullName)))].sort();
  $: ownerCounts = discoverRepos.reduce<Record<string, number>>((acc, r) => {
    const o = ownerOf(r.fullName);
    acc[o] = (acc[o] || 0) + 1;
    return acc;
  }, {});
  $: showOwnerSection = owners.length > 1;
  $: filteredByOwner = discoverRepos.filter((r) => orgVisible[ownerOf(r.fullName)] !== false);
  $: filteredDiscoverRepos = discoverFilter.trim()
    ? filteredByOwner.filter((r) => r.fullName.toLowerCase().includes(discoverFilter.trim().toLowerCase()))
    : filteredByOwner;
  $: existingRepos = discoverModal ? ($sources[discoverModal]?.repos || {}) : {};
  $: selectableRepos = filteredDiscoverRepos.filter((r) => !existingRepos[r.fullName]);
  $: selectedCount = Object.values(discoverSelected).filter(Boolean).length;
  $: allSelected = selectableRepos.length > 0 && selectableRepos.every((r) => discoverSelected[r.fullName]);

  function toggleAll() {
    if (allSelected) {
      for (const r of selectableRepos) delete discoverSelected[r.fullName];
      discoverSelected = discoverSelected;
    } else {
      for (const r of selectableRepos) discoverSelected[r.fullName] = true;
      discoverSelected = discoverSelected;
    }
  }

  function toggleOwnerVisibility(owner: string) {
    const nextOn = orgVisible[owner] === false;
    orgVisible = { ...orgVisible, [owner]: nextOn };
    if (!nextOn) {
      const next = { ...discoverSelected };
      for (const key of Object.keys(next)) {
        if (ownerOf(key) === owner) delete next[key];
      }
      discoverSelected = next;
    }
  }

  function toggleOwnerAll(owner: string) {
    if (orgVisible[owner] === false) {
      orgVisible = { ...orgVisible, [owner]: true };
    }
    const existing = discoverModal ? ($sources[discoverModal]?.repos || {}) : {};
    const eligible = discoverRepos.filter((r) => ownerOf(r.fullName) === owner && !existing[r.fullName]);
    const allOn = eligible.length > 0 && eligible.every((r) => discoverSelected[r.fullName]);
    const next = { ...discoverSelected };
    for (const r of eligible) next[r.fullName] = !allOn;
    discoverSelected = next;
  }

  function toggleAllOwners() {
    const anyOn = owners.some((o) => orgVisible[o] !== false);
    if (anyOn) {
      const next: Record<string, boolean> = {};
      for (const o of owners) next[o] = false;
      orgVisible = next;
      discoverSelected = {};
    } else {
      orgVisible = {};
    }
  }

  async function addDiscovered() {
    if (!discoverModal) return;
    const sourceKey = discoverModal;
    const selected = Object.entries(discoverSelected)
      .filter(([_, v]) => v)
      .map(([k]) => k);
    if (selected.length > 0) {
      await bridge.addDiscoveredRepos(sourceKey, selected);
      await reloadFromDisk();
      // Clone each newly added repo that isn't already local, asking once
      // which root they go to.
      const missing = selected.filter((repoName) => {
        const state = $repoStates[`${sourceKey}/${repoName}`];
        return !state || state.status === 'not cloned';
      });
      discoverModal = null;
      await cloneRepos(sourceKey, missing);
    }
    discoverModal = null;
  }

  // Return the catalog key of a short, user-facing hint explaining a
  // specific discovery failure mode (auth vs. network vs. permission). The
  // raw Go error is already shown to the user; this just adds a one-line
  // "what to do" above the action buttons.
  function detectDiscoveryHint(msg: string, acctKey: string | null): string {
    if (!msg) return '';
    const m = msg.toLowerCase();
    const acct = acctKey ? $accounts[acctKey] : null;
    const credType = acct?.default_credential_type || '';
    const provider = acct?.provider || '';
    if (m.includes('401') || m.includes('authentication failed') || m.includes('token is invalid')) {
      if (credType === 'gcm' && (provider === 'gitea' || provider === 'forgejo')) {
        return 'discover.hintGcmPassword';
      }
      return 'discover.hintRejected';
    }
    if (m.includes('no route to host') || m.includes('host is unreachable') || m.includes('wsaehostunreach')) {
      return 'discover.hintUnreachable';
    }
    if (m.includes('no such host') || m.includes('dns')) {
      return 'discover.hintDns';
    }
    if (m.includes('x509') || m.includes('certificate')) {
      return 'discover.hintCertificate';
    }
    return '';
  }
</script>

{#if discoverModal}
  <div class="overlay" use:dismiss={() => discoverModal = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-discover" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('discover.title')} &mdash; {discoverModal}</h3>
        <button class="btn-x" on:click={() => discoverModal = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if discoverLoading}
          <div class="loading"><div class="spinner"></div><span>{$t('discover.checking')}</span></div>
        {:else if discoverError}
          {@const hint = detectDiscoveryHint(discoverError, discoverModal)}
          <div class="discover-error">
            <p class="discover-error-title">{$t('discover.failed')}</p>
            <p class="discover-error-detail">{discoverError}</p>
            {#if hint}
              <p class="discover-error-hint">{$t(hint)}</p>
            {/if}
            <div class="discover-error-actions">
              <button class="btn-cancel" on:click={() => { if (discoverModal) { discoverLoading = true; discoverError = ''; bridge.discover(discoverModal); } }}>{$t('discover.retry')}</button>
              <button class="btn-add" on:click={() => { const k = discoverModal; discoverModal = null; if (k) openCredChange(k, $accounts[k]?.default_credential_type || ''); }}>{$t('discover.openCredentials')}</button>
            </div>
          </div>
        {:else if discoverRepos.length === 0}
          <p class="found">{$t('discover.none')}</p>
        {:else}
          <div class="found-row">
            <p class="found">{$t('discover.found', { count: discoverRepos.length })}{filteredDiscoverRepos.length !== discoverRepos.length ? ` (${$t('discover.showing', { count: filteredDiscoverRepos.length })})` : ''}:</p>
            {#if showOwnerSection}
              {@const anyOn = owners.some((o) => orgVisible[o] !== false)}
              <button
                class="org-switch"
                class:is-on={anyOn}
                on:click={toggleAllOwners}
                title={anyOn ? $t('discover.hideOwners') : $t('discover.showOwners')}
              >
                <span class="org-switch-thumb">{anyOn ? '✓' : '✕'}</span>
              </button>
            {/if}
          </div>
          {#if showOwnerSection}
            <div class="discover-orgs">
              {#each owners as owner}
                {@const on = orgVisible[owner] !== false}
                {@const eligible = discoverRepos.filter((r) => ownerOf(r.fullName) === owner && !(discoverModal && $sources[discoverModal]?.repos?.[r.fullName]))}
                {@const allSel = eligible.length > 0 && eligible.every((r) => discoverSelected[r.fullName])}
                {@const someSel = eligible.some((r) => discoverSelected[r.fullName])}
                <span class="org-badge" class:is-on={on}>
                  <input
                    type="checkbox"
                    checked={allSel}
                    indeterminate={someSel && !allSel}
                    on:click|stopPropagation={() => toggleOwnerAll(owner)}
                  />
                  <span class="org-badge-body" role="button" tabindex="0" on:click={() => toggleOwnerVisibility(owner)} on:keydown={onActivateKey(() => toggleOwnerVisibility(owner))}>
                    {owner} <span class="org-badge-count">({ownerCounts[owner]})</span>
                  </span>
                </span>
              {/each}
            </div>
          {/if}
          <input class="form-input discover-filter" type="text" placeholder={$t('discover.filter')} bind:value={discoverFilter} />
          <label class="dr dr-all">
            <input type="checkbox" checked={allSelected} on:change={toggleAll} />
            <span class="dr-name">{$t('discover.selectAll')}{discoverFilter ? ` (${$t('discover.filtered')})` : ''}</span>
          </label>
          {#each filteredDiscoverRepos as repo}
            {@const alreadyAdded = !!($sources[discoverModal]?.repos?.[repo.fullName])}
            <label class="dr" class:dr-disabled={alreadyAdded}>
              <input type="checkbox" bind:checked={discoverSelected[repo.fullName]} disabled={alreadyAdded} />
              <span class="dr-name">{repo.fullName}</span>
              {#if alreadyAdded}<span class="dr-tag">{$t('discover.added')}</span>{/if}
              {#if repo.archived}<span class="dr-tag">{$t('onboard.archived')}</span>{/if}
              {#if repo.fork}<span class="dr-tag">{$t('onboard.fork')}</span>{/if}
              <span class="dr-desc">{repo.description}</span>
            </label>
          {/each}
        {/if}
      </div>
      {#if !discoverLoading && discoverRepos.length > 0}
        <div class="modal-foot">
          <button class="btn-cancel" on:click={() => discoverModal = null}>{$t('common.cancel')}</button>
          <button class="btn-add" on:click={addDiscovered} disabled={selectedCount === 0}>{$t('discover.addPull')} ({selectedCount})</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .found { font-size: 12px; color: var(--text-muted); margin: 0 0 10px; }
  .dr { display: flex; align-items: center; gap: 8px; padding: 6px 4px; cursor: pointer; border-radius: 4px; font-size: 13px; }
  .dr:hover { background: var(--bg-hover); }
  .dr-all { border-bottom: 1px solid var(--border); padding-bottom: 8px; margin-bottom: 4px; }
  .dr input { accent-color: #4B95E9; }
  .dr-name { color: var(--text-repo); font-weight: 500; }
  .dr-desc { color: var(--text-dim); font-size: 11px; margin-left: auto; }
  .dr-tag { font-size: 9px; padding: 1px 5px; background: var(--bg-hover); color: var(--text-dim); border-radius: 3px; }
  .dr-disabled { opacity: 0.4; cursor: default; }
  .dr-disabled:hover { background: transparent; }
  .discover-orgs { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin: 0 0 10px; }
  .org-badge {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 2px 8px; border-radius: 12px; font-size: 11px;
    background: transparent; border: 1px solid var(--border);
    color: var(--text-muted); user-select: none;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }
  .org-badge.is-on { background: #14532d; border-color: #166534; color: #86efac; }
  .org-badge input[type="checkbox"] { margin: 0; cursor: pointer; accent-color: #4B95E9; }
  .org-badge-body { cursor: pointer; }
  .org-badge-count { opacity: 0.75; font-variant-numeric: tabular-nums; }
  :global([data-theme="light"]) .org-badge.is-on { background: #dcfce7; border-color: #86efac; color: #166534; }
  .found-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .found-row .found { margin: 0 0 10px; }
  .org-switch {
    flex-shrink: 0;
    width: 28px; height: 14px; border-radius: 7px;
    background: var(--text-muted); border: none; padding: 0;
    cursor: pointer; position: relative;
    transition: background 0.18s;
    margin-bottom: 10px;
  }
  .org-switch.is-on { background: #6ee09c; }
  .org-switch-thumb {
    position: absolute; top: 2px; left: 2px;
    width: 10px; height: 10px; border-radius: 50%;
    background: #fff;
    display: inline-flex; align-items: center; justify-content: center;
    font-size: 7px; font-weight: 800; color: var(--text-muted);
    transition: transform 0.18s, color 0.18s;
    line-height: 1;
  }
  .org-switch.is-on .org-switch-thumb { transform: translateX(14px); color: #14532d; }
  /* Discovery-modal error block */
  .discover-error {
    margin: 4px 0 12px; padding: 12px 14px; border-radius: 6px;
    background: rgba(216, 30, 91, 0.10); border: 1px solid rgba(216, 30, 91, 0.45);
  }
  :global([data-theme="light"]) .discover-error {
    background: rgba(216, 30, 91, 0.08); border-color: #be123c;
  }
  .discover-error-title { margin: 0 0 6px; font-weight: 600; color: #D81E5B; }
  :global([data-theme="light"]) .discover-error-title { color: #be123c; }
  .discover-error-detail {
    margin: 4px 0; color: var(--text-primary); font-size: 12px; line-height: 1.4;
    word-break: break-word;
    font-family: ui-monospace, Menlo, Consolas, monospace;
  }
  .discover-error-hint { margin: 8px 0 0; color: var(--text-primary); font-size: 12px; line-height: 1.5; }
  .discover-error-actions { display: flex; align-items: center; gap: 8px; margin-top: 12px; justify-content: flex-end; }
</style>
