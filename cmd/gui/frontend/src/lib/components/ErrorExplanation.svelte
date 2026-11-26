<script lang="ts">
  import { t } from '../i18n';
  import { bridge } from '../bridge';
  import { statusClock } from '../stores';
  import { errorAction, explain, unsafeHelp } from '../explain';
  import type { ErrorActions, ErrorInfo } from '../explain';

  export let info: ErrorInfo;
  export let simple: boolean;
  export let actions: ErrorActions = {};
  export let scopes = '';
  export let busy = false;
  export let accountKey: string | undefined = undefined;
  // The clone's folder, for the commands of a clone paused for safety.
  export let path = '';
  let checkingConnection = false;
  let copied = false;
  $: message = explain(info.code);
  $: unsafe = info.code === 'git.unsafe_config' ? unsafeHelp(info.unsafe, path) : undefined;

  async function copyCommands() {
    if (!unsafe) return;
    try {
      await navigator.clipboard.writeText(unsafe.commands.join('\n') + '\n');
      copied = true;
      setTimeout(() => { copied = false; }, 2000);
    } catch { /* The commands stay on screen to select by hand. */ }
  }
  $: action = errorAction(info, actions, $statusClock);

  async function activate() {
    const action = errorAction(info, actions, Date.now());
    if (!action || checkingConnection) return;
    const run = actions[action.kind];
    if (accountKey && action.kind === 'retry' && (info.code === 'git.offline' || info.code === 'provider.offline')) {
      checkingConnection = true;
      try { await bridge.runTask(`retry-connection/${accountKey}`); }
      catch { /* The original operation reports its own failure. */ }
      finally { checkingConnection = false; }
    }
    await run?.();
  }
</script>

<div class="explanation" aria-live="polite">
  <strong>{$t(message.titleKey)}</strong>
  {#if unsafe}
    <p>{$t(unsafe.reasonKey, { where: $t(unsafe.whereKey) })}</p>
    <p>{$t('error.git.unsafe_config.unchanged')}</p>
    <details open={!simple}>
      <summary>{$t('error.git.unsafe_config.fix')}</summary>
      <p>{$t('error.git.unsafe_config.fixHint')}</p>
      <pre class="commands">{unsafe.commands.join('\n')}</pre>
      <button type="button" class="text-btn" on:click={copyCommands}>{$t(copied ? 'error.git.unsafe_config.copied' : 'error.git.unsafe_config.copy')}</button>
    </details>
  {:else}
    <p>{$t(message.bodyKey)}</p>
  {/if}
  {#if info.code === 'provider.scope_denied' && scopes}
    <p>{$t('error.requiredScopes', { scopes })}</p>
  {/if}
  {#if action}
    <button type="button" class="text-btn" disabled={busy || checkingConnection} on:click={activate}>{$t(action.labelKey)}</button>
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
  .commands {
    margin: 4px 0; padding: 6px 8px; border-radius: 4px;
    background: var(--bg-hover); color: var(--text-primary);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 11px;
    white-space: pre-wrap; user-select: text; -webkit-user-select: text;
  }
</style>
