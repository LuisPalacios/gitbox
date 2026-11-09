<script lang="ts">
  // A folder a container's .code-workspace lists that is not cloned yet:
  // a dimmed row under the container, with no actions, its name in the
  // workspace colour like the clones the workspace lists. It reads like a
  // nested clone row so the tree keeps its shape.
  import { t } from '../i18n';
  import type { Placeholder } from '../repo-tree';

  export let placeholder: Placeholder;
  export let depth: number;
</script>

<div class="placeholder-row" style="--depth: {depth}" title={$t('row.placeholderTip', { path: placeholder.path })}>
  <span class="dot">○</span>
  <span class="name">{placeholder.name}</span>
  <span class="path" translate="no">{placeholder.path}</span>
  <span class="words">{$t('row.placeholder')}</span>
</div>

<style>
  .placeholder-row {
    position: relative; display: flex; align-items: center; gap: 8px; min-height: 34px;
    --indent: 16px; padding: 3px 6px 3px calc(6px + var(--depth) * var(--indent)); opacity: 0.55; cursor: default;
  }
  /* Same indent and tree guide as RepoRow, so the tree keeps its shape. */
  .placeholder-row::before {
    content: ''; position: absolute; left: 14px; top: 0; bottom: 0;
    border-left: 2px solid var(--border); opacity: 0.6;
  }
  .dot { font-size: 14px; flex-shrink: 0; width: 18px; text-align: center; color: var(--text-muted); }
  .name { font-size: 13px; color: var(--text-workspace); font-style: italic; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .path { flex: 1; font-size: 11px; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .words { font-size: 12px; color: var(--text-muted); white-space: nowrap; }
  @container content (max-width: 620px) {
    .placeholder-row { gap: 4px; --indent: 12px; }
    .path { display: none; }
  }
</style>
