<script lang="ts">
  import { t } from '../i18n';
  import { statusClock } from '../stores';
  import { errorAction, explain } from '../explain';
  import type { ErrorActions, ErrorInfo } from '../explain';

  export let info: ErrorInfo;
  export let simple: boolean;
  export let actions: ErrorActions = {};
  export let scopes = '';
  export let busy = false;
  $: message = explain(info.code);
  $: action = errorAction(info, actions, $statusClock);
</script>

<div class="explanation" aria-live="polite">
  <strong>{$t(message.titleKey)}</strong>
  <p>{$t(message.bodyKey)}</p>
  {#if info.code === 'provider.scope_denied' && scopes}
    <p>{$t('error.requiredScopes', { scopes })}</p>
  {/if}
  {#if action}
    <button type="button" class="text-btn" disabled={busy} on:click={() => { if (errorAction(info, actions, Date.now())) actions[action.kind]?.(); }}>{$t(action.labelKey)}</button>
  {/if}
  {#if !simple && message.unknown && info.detail}
    <details>
      <summary>{$t('error.unknown.action')}</summary>
      <p>{info.detail}</p>
    </details>
  {/if}
</div>

<style>
  .explanation { font-size: 12px; color: var(--text-secondary); overflow-wrap: anywhere; }
  strong { color: var(--text-primary); }
  p { margin: 2px 0 4px; }
  summary { cursor: pointer; }
</style>
