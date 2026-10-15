<script lang="ts">
  // Results of Mirrors → Discover: mirror relationships found across the
  // accounts, added one by one or applied all at once.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { pushNotice, errorText } from '../notices';
  import { t } from '../i18n';
  import { dismiss } from '../dismiss';
  import { configStore, mirrors } from '../stores';

  export let mirrorDiscoverResults: any[] | null;
  export let mirrorDiscoverError: string;
  // Repos added one by one during this discover session (reset per run).
  export let discoverAdded: Record<string, boolean>;
  export let checkAllMirrorStatus: () => Promise<void>;

  // Check if a discovered mirror repo is already configured.
  function isDiscoveredRepoConfigured(accountSrc: string, accountDst: string, repoKey: string): boolean {
    for (const mir of Object.values($mirrors)) {
      if ((mir.account_src === accountSrc && mir.account_dst === accountDst) ||
          (mir.account_src === accountDst && mir.account_dst === accountSrc)) {
        if (mir.repos[repoKey]) return true;
      }
    }
    return false;
  }

  async function addDiscoveredRepo(dr: any, dm: any) {
    const addKey = `${dr.AccountSrc}:${dr.AccountDst}:${dm.RepoKey}`;
    try {
      // Find or create mirror group for this account pair.
      let groupKey = '';
      for (const [key, mir] of Object.entries($mirrors)) {
        const m = mir as any;
        if ((m.account_src === dr.AccountSrc && m.account_dst === dr.AccountDst) ||
            (m.account_src === dr.AccountDst && m.account_dst === dr.AccountSrc)) {
          groupKey = key;
          break;
        }
      }
      if (!groupKey) {
        groupKey = dr.MirrorKey;
        await bridge.addMirrorGroup(groupKey, dr.AccountSrc, dr.AccountDst);
      }
      await bridge.addMirrorRepo(groupKey, dm.RepoKey, dm.Direction, dm.Origin);
      $configStore = await bridge.reloadConfig();
      discoverAdded = { ...discoverAdded, [addKey]: true };
    } catch (e: any) {
      pushNotice('error', $t('notice.mirrorRepoAddFailed'), [errorText(e)]);
    }
  }

  async function applyMirrorDiscovery() {
    try {
      await bridge.applyDiscoveredMirrors();
      $configStore = await bridge.reloadConfig();
      mirrorDiscoverResults = null;
      discoverAdded = {};
      checkAllMirrorStatus();
    } catch (e: any) {
      pushNotice('error', $t('notice.mirrorApplyFailed'), [errorText(e)]);
    }
  }
</script>

{#if mirrorDiscoverResults !== null}
  <div class="overlay" use:dismiss={() => mirrorDiscoverResults = null} transition:fade={{ duration: 120 }}>
    <div class="modal modal-discover" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Mirror Discovery</h3>
        <button class="btn-x" on:click={() => mirrorDiscoverResults = null}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if mirrorDiscoverError}
          <p class="form-error">{mirrorDiscoverError}</p>
        {:else if mirrorDiscoverResults.length === 0}
          <p style="color: var(--text-muted); font-size: 12px;">No mirror relationships found across your accounts.</p>
        {:else}
          {#each mirrorDiscoverResults as dr}
            <div style="margin-bottom: 12px;">
              <div style="font-size: 12px; font-weight: 500; color: var(--text-primary); margin-bottom: 4px;">
                {dr.AccountSrc} ↔ {dr.AccountDst}
              </div>
              {#each dr.Discovered as dm}
                {@const alreadyConfigured = isDiscoveredRepoConfigured(dr.AccountSrc, dr.AccountDst, dm.RepoKey)}
                {@const justAdded = discoverAdded[`${dr.AccountSrc}:${dr.AccountDst}:${dm.RepoKey}`]}
                <div class="mrp-row" style="border-bottom: none; {alreadyConfigured || justAdded ? 'opacity: 0.5;' : ''}">
                  <span class="mrp-name">{dm.RepoKey}</span>
                  <span class="mrp-badge">{dm.Direction}</span>
                  <span class="mrp-badge">{dm.Origin}</span>
                  <span class="mrp-badge" class:mrp-badge-dim={dm.Confidence === 'possible'} style={dm.Confidence === 'confirmed' ? 'background: #166534; color: #dcfce7;' : dm.Confidence === 'likely' ? 'background: #1e40af; color: #dbeafe;' : ''}>{dm.Confidence}</span>
                  {#if alreadyConfigured || justAdded}
                    <span class="mrp-badge" style="background: #166534; color: #dcfce7;">configured</span>
                  {:else}
                    <button class="btn-sm" style="margin-left: auto; padding: 1px 8px; font-size: 10px;" on:click={() => addDiscoveredRepo(dr, dm)}>+ Add</button>
                  {/if}
                </div>
              {/each}
            </div>
          {/each}
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => mirrorDiscoverResults = null}>Close</button>
        {#if mirrorDiscoverResults.length > 0}
          <button class="btn-add" on:click={applyMirrorDiscovery}>Apply to config</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>

</style>
