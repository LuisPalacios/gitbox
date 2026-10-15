<script lang="ts">
  // One entry of the cards panel (accounts, mirrors, workspaces): a chip
  // (dot + name) that opens into the two-line card. App folds the open card
  // back on an outside click or Escape (the .card-open selector).
  import type { Snippet } from 'svelte';
  import { onActivateKey } from './keyboard';

  // Show the full card instead of the chip.
  export let open: boolean;
  export let title: string;
  export let name: string;
  export let dotColor: string;
  // Inline style of the chip and the card (accounts flag a broken credential).
  export let style: string | undefined = undefined;
  // Extra class of the open card: 'card-mirror' or 'card-workspace'.
  export let variant = '';
  // Extra class of the card name; onName makes the name a button.
  export let nameClass = '';
  export let nameTitle: string | undefined = undefined;
  export let onName: (() => void) | null = null;
  // Delete mode swaps the dot for a delete button (undefined: no delete mode).
  export let deleteMode: boolean | undefined = undefined;
  export let deleteTitle = '';
  export let onDelete: () => void = () => {};
  export let onOpen: () => void;
  // Line 2: provider label, an optional badge or count, then the buttons.
  export let provider: string;
  export let badge: Snippet | undefined = undefined;
  export let count: { text: string; title: string; style?: string } | null = null;
  export let actions: { label: string; onClick: () => void; title?: string; disabled?: boolean }[];
</script>

{#if !open}
<div class="card-chip" role="button" tabindex="0" {title}
  on:click|stopPropagation={onOpen}
  on:keydown={onActivateKey(onOpen)}
  {style}>
  {#if deleteMode}
    <button class="btn-delete-x card-delete-btn" on:click|stopPropagation={onDelete} title={deleteTitle}>&#10005;</button>
  {:else}
    <span class="card-dot" style="background: {dotColor}"></span>
  {/if}
  <span class="card-chip-name">{name}</span>
</div>
{:else}
<div class={variant ? `card ${variant} card-open` : 'card card-open'} class:card-delete-mode={deleteMode}
  {style}>
  <div class="card-top">
    {#if deleteMode}
      <button class="btn-delete-x card-delete-btn" on:click={onDelete} title={deleteTitle}>&#10005;</button>
    {:else}
      <span class="card-dot" style="background: {dotColor}"></span>
    {/if}
    {#if onName}
      <div class="card-name {nameClass}" role="button" tabindex="0" on:click={onName} on:keydown={onActivateKey(onName)} title={nameTitle}>{name}</div>
    {:else}
      <div class={nameClass ? `card-name ${nameClass}` : 'card-name'}>{name}</div>
    {/if}
  </div>
  <div class="card-line">
    <span class="card-provider">{provider}</span>
    {#if badge}{@render badge()}{/if}
    {#if count}
      <span class="card-count" style={count.style} title={count.title}>{count.text}</span>
    {/if}
    <span class="card-actions">
      {#each actions as a}
        <button class="card-btn card-btn-sm" on:click={a.onClick} disabled={a.disabled} title={a.title}>{a.label}</button>
      {/each}
    </span>
  </div>
</div>
{/if}

<style>
  .card-chip {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 10px; border: 1px solid var(--border); border-radius: 999px;
    background: var(--bg-card); box-shadow: var(--card-shadow);
    cursor: pointer; transition: border-color 0.15s, background 0.15s;
    white-space: nowrap;
  }
  .card-chip:hover { border-color: var(--border-hover); }
  .card-chip:focus-visible { outline: 1px solid var(--border-hover); outline-offset: 1px; }
  .card-chip-name { font-size: 12px; font-weight: 600; color: var(--text-primary); }
  .card {
    flex: 0 0 auto; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 10px; padding: 8px 12px; transition: border-color 0.15s, box-shadow 0.15s;
    box-shadow: var(--card-shadow);
  }
  .card:hover { border-color: var(--border-hover); }
  .card-top { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
  .card-count { font-size: 10px; font-weight: 700; color: var(--text-muted); }
  .card-dot { width: 7px; height: 7px; border-radius: 50%; }
  .card-provider { font-size: 10px; color: var(--text-muted); font-weight: 600; letter-spacing: 0.3px; }
  .card-delete-btn { flex-shrink: 0; }
  .card-name { font-size: 14px; font-weight: 600; margin-bottom: 8px; }
  .card-mirror-name { font-size: 12px; }
  .card-name-edit { cursor: pointer; }
  .card-name-edit:hover { text-decoration: underline; }
  /* Line 1 holds the status dot and the full name; line 2 the provider,
     badge or count on the left and the one-word buttons on the right. */
  .card-top .card-name { margin-bottom: 0; white-space: nowrap; }
  .card-line { display: flex; align-items: center; gap: 6px; }
  .card-actions { margin-left: auto; display: flex; gap: 4px; padding-left: 8px; }
  .card-btn {
    width: 100%; padding: 5px 0; background: transparent; border: 1px solid var(--border);
    color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 11px; font-weight: 500;
    transition: all 0.15s;
  }
  .card-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); border-color: var(--border-hover); }
  .card-btn:disabled { opacity: 0.35; cursor: default; }
  .card-btn-sm { width: auto; flex: 0 0 auto; padding: 2px 8px; }
  .btn-delete-x {
    background: none; border: 1px solid #D81E5B55; color: #D81E5B;
    cursor: pointer; font-size: 9px; font-weight: 600;
    width: 16px; height: 16px; padding: 0; flex-shrink: 0;
    border-radius: 3px; display: flex; align-items: center; justify-content: center;
    opacity: 0.7; transition: all 0.12s; line-height: 1;
  }
  .btn-delete-x:hover { opacity: 1; background: #D81E5B22; border-color: #D81E5B; }
  /* Workspace card styling: reuse the mirror card structure but with its
     own dot colour derived inline. */
  .card-workspace .card-provider { letter-spacing: 0.5px; }
</style>
