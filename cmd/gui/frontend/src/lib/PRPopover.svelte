<script lang="ts">
  // PRPopover — list of open PRs for one clone, shown when the user clicks a
  // PR badge on the clone row. Reuses the smart placement logic from
  // LauncherMenu (flip-up + horizontal clamp) so it never clips against the
  // viewport. Dumb component — all handlers are passed in as props.

  import { onMount, tick } from 'svelte';
  import type { PullRequestDTO } from './types';

  export let kind: 'authored' | 'review' = 'authored';
  export let prs: PullRequestDTO[] = [];
  export let providerAllURL: string = ''; // "open all PRs" footer link
  export let providerName: string = '';

  export let onOpenPR: (url: string) => void;
  export let onOpenAll: () => void;

  let rootEl: HTMLDivElement | undefined;
  const VIEWPORT_PADDING = 8;
  const MAX_ITEMS = 10;

  $: visible = prs.slice(0, MAX_ITEMS);
  $: remaining = Math.max(0, prs.length - MAX_ITEMS);
  $: title = kind === 'authored' ? 'My open PRs' : 'Review requested';

  function positionMenu(el: HTMLElement | undefined) {
    if (!el) return;
    el.style.top = '';
    el.style.bottom = '';
    el.style.left = '';
    el.style.right = '';
    el.style.marginTop = '';
    el.style.marginBottom = '';
    el.style.transform = '';

    const trigger = el.parentElement;
    if (!trigger) return;
    const triggerRect = trigger.getBoundingClientRect();
    const vh = window.innerHeight;
    const vw = window.innerWidth;

    const rect = el.getBoundingClientRect();
    const overflowsBelow = rect.bottom > vh - VIEWPORT_PADDING;
    const spaceAbove = triggerRect.top;
    const spaceBelow = vh - triggerRect.bottom;
    const flipUp = overflowsBelow && spaceAbove > spaceBelow;
    if (flipUp) {
      el.style.top = 'auto';
      el.style.bottom = '100%';
      el.style.marginTop = '0';
      el.style.marginBottom = '4px';
    }

    const post = el.getBoundingClientRect();
    let dx = 0;
    if (post.right > vw - VIEWPORT_PADDING) {
      dx = vw - VIEWPORT_PADDING - post.right;
    } else if (post.left < VIEWPORT_PADDING) {
      dx = VIEWPORT_PADDING - post.left;
    }
    if (dx !== 0) {
      el.style.transform = `translateX(${Math.round(dx)}px)`;
    }
  }

  onMount(async () => {
    await tick();
    positionMenu(rootEl);
  });

  function formatRelative(iso: string): string {
    if (!iso) return '';
    const t = Date.parse(iso);
    if (Number.isNaN(t)) return '';
    const diffMs = Date.now() - t;
    const mins = Math.floor(diffMs / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 30) return `${days}d ago`;
    const months = Math.floor(days / 30);
    if (months < 12) return `${months}mo ago`;
    return `${Math.floor(months / 12)}y ago`;
  }
</script>

<div class="pr-popover action-dropdown" bind:this={rootEl}>
  <div class="pr-popover-header">{title}</div>
  {#each visible as pr (pr.number + '/' + pr.repoFull)}
    <button class="action-item pr-row" on:click|stopPropagation={() => onOpenPR(pr.url)} title={pr.title}>
      <span class="pr-num">#{pr.number}</span>
      <span class="pr-title">{pr.title}</span>
      {#if pr.isDraft}
        <span class="pr-draft" title="Draft">draft</span>
      {/if}
      <span class="pr-updated">{formatRelative(pr.updated)}</span>
    </button>
  {/each}
  {#if remaining > 0}
    <div class="pr-more">+{remaining} more</div>
  {/if}
  {#if providerAllURL}
    <hr class="pr-sep" />
    <button class="action-item pr-footer" on:click|stopPropagation={onOpenAll}>
      Open all PRs on {providerName || 'provider'} <span class="pr-ext">&#8599;</span>
    </button>
  {/if}
</div>

<style>
  .action-dropdown {
    position: absolute;
    right: 0;
    top: 100%;
    margin-top: 4px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
    min-width: 260px;
    max-width: 360px;
    z-index: 100;
    overflow: hidden;
    max-height: calc(100vh - 16px);
    overflow-y: auto;
  }

  .action-item {
    display: flex;
    align-items: center;
    width: 100%;
    padding: 6px 12px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    text-align: left;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.1s;
    white-space: nowrap;
  }

  .action-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .pr-popover-header {
    padding: 8px 12px 4px 12px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-dim);
    border-bottom: 1px solid var(--border);
    margin-bottom: 2px;
  }

  .pr-row {
    gap: 8px;
    overflow: hidden;
  }

  .pr-num {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace;
    font-size: 11px;
  }

  .pr-title {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .pr-draft {
    flex: 0 0 auto;
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    background: var(--bg-hover);
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .pr-updated {
    flex: 0 0 auto;
    font-size: 11px;
    color: var(--text-dim);
  }

  .pr-more {
    padding: 4px 12px;
    font-size: 11px;
    color: var(--text-dim);
    font-style: italic;
  }

  .pr-sep {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }

  .pr-footer {
    font-size: 12px;
    color: var(--text-secondary);
  }

  .pr-ext {
    margin-left: auto;
    color: var(--text-dim);
  }
</style>
