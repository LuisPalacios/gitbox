<script lang="ts">
  // The Workspaces tab (read-only): the discovered .code-workspace cards
  // and the list of each workspace's member clones.
  import { slide } from 'svelte/transition';
  import { workspaces, workspaceOrder, themeStore } from '../stores';
  import { statusColor } from '../status';
  import { t } from '../i18n';
  import Card from '../Card.svelte';

  export let cardsVisible: boolean;
  export let expandedCard: string | null;
  export let workspaceBusy: boolean;
  export let openWorkspace: (key: string) => Promise<void>;

  $: sc = (s: string) => statusColor(s, $themeStore);
</script>

{#if cardsVisible}
<section class="cards-row" transition:slide={{ duration: 150 }}>
  {#each $workspaceOrder.length > 0 ? $workspaceOrder : Object.keys($workspaces) as wsKey}
    {@const ws = $workspaces[wsKey]}
    {#if ws}
      {@const members = ws.members?.length ?? 0}
      <Card open={expandedCard === `ws:${wsKey}`} title={ws.name || wsKey} name={ws.name || wsKey} dotColor={sc('clean')}
        variant="card-workspace" onOpen={() => expandedCard = `ws:${wsKey}`}
        provider="CODE" count={{ text: `${members}`, title: `${members} member${members === 1 ? '' : 's'}` }}
        actions={[{ label: $t('workspace.open'), onClick: () => openWorkspace(wsKey), disabled: workspaceBusy }]} />
    {/if}
  {/each}
</section>
{/if}

<!-- ── WORKSPACE DETAIL LIST ── -->
{#if Object.keys($workspaces).length > 0}
<section class="repo-list">
  <div class="mirror-list">
    <div class="mirror-section-header">
      <h3>{$t('tab.workspaces')}</h3>
    </div>
    {#each $workspaceOrder.length > 0 ? $workspaceOrder : Object.keys($workspaces) as wsKey}
      {@const ws = $workspaces[wsKey]}
      {#if ws}
        <div class="mirror-group">
          <div class="mirror-group-header">
            <span class="mirror-accounts">{ws.name || wsKey} <span class="workspace-type">· .code-workspace</span></span>
            <div class="mirror-group-actions">
              <button class="btn-tab-action" on:click={() => openWorkspace(wsKey)} disabled={workspaceBusy}>{$t('workspace.open')}</button>
            </div>
          </div>
          {#if ws.file}
            <div class="workspace-file-row">
              <span class="workspace-file-label">{$t('workspace.file')}</span>
              <span class="workspace-file-path" title={ws.file}>{ws.file}</span>
            </div>
          {/if}
          {#if (ws.members?.length ?? 0) === 0}
            <div class="mirror-empty">{$t('workspace.noMembers')}</div>
          {:else}
            {#each ws.members as m}
              <div class="mirror-row">
                <span class="mirror-dot" style="color: {sc('clean')}">●</span>
                <span class="mirror-repo-name">{m.source}/{m.repo}</span>
              </div>
            {/each}
          {/if}
        </div>
      {/if}
    {/each}
  </div>
</section>
{:else}
<section class="repo-list">
  <div class="workspace-empty-hint">
    {$t('workspace.empty')}
  </div>
</section>
{/if}

<style>
  .btn-tab-action {
    padding: 3px 10px; font-size: 11px; font-weight: 500;
    border: 1px solid #1e3a5f; background: rgba(37, 99, 235, 0.1);
    color: #93c5fd; border-radius: 5px;
    cursor: pointer; transition: all 0.12s;
  }
  .btn-tab-action:hover:not(:disabled) { background: rgba(37, 99, 235, 0.2); color: #bfdbfe; }
  .btn-tab-action:disabled { opacity: 0.5; cursor: default; }
  :global([data-theme="light"]) .btn-tab-action { border-color: #93c5fd; background: rgba(37, 99, 235, 0.06); color: #2563eb; }
  :global([data-theme="light"]) .btn-tab-action:hover:not(:disabled) { background: rgba(37, 99, 235, 0.12); }
  /* Chips and the open card are as wide as their content, so names are
     never cut; the row wraps instead of scrolling. */
  .cards-row { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 8px; padding: 10px 24px; flex-shrink: 0; }
  .repo-list { flex: 1; padding: 0 24px 12px; overflow-y: auto; min-height: 0; }
  /* ── Mirror section ── */
  .mirror-list { padding: 12px 0 0; }
  .mirror-section-header {
    display: flex; justify-content: space-between; align-items: center;
    margin-bottom: 8px; padding-bottom: 6px; border-bottom: 1px solid var(--border);
  }
  .mirror-section-header h3 { margin: 0; font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .mirror-group { margin-bottom: 12px; }
  .mirror-group-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 4px 0; font-size: 12px; color: var(--text-muted);
  }
  .mirror-accounts { font-weight: 500; color: var(--text-primary); }
  .mirror-group-actions { display: flex; gap: 4px; align-items: center; }
  .mirror-row {
    display: flex; align-items: center; gap: 8px;
    padding: 3px 0 3px 8px; font-size: 12px;
  }
  .mirror-row:hover { background: var(--hover); border-radius: 4px; }
  .mirror-dot { font-size: 11px; flex-shrink: 0; width: 14px; text-align: center; }
  .mirror-repo-name { font-weight: 500; color: var(--text-primary); min-width: 120px; }
  .mirror-empty { font-size: 11px; color: var(--text-muted); padding: 4px 8px; font-style: italic; }
  /* Workspace type sub-label in the detail list header. */
  .workspace-type { color: var(--text-muted); font-size: 11px; font-weight: 400; margin-left: 8px; }
  /* File path row inside a workspace group card. */
  .workspace-file-row {
    padding: 6px 12px;
    font-size: 11px;
    color: var(--text-muted);
    display: flex; gap: 6px;
    overflow: hidden;
  }
  .workspace-file-label { color: var(--text-dim); flex: 0 0 auto; }
  .workspace-file-path {
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
  }
  .workspace-empty-hint {
    padding: 24px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }
</style>
