<script lang="ts">
  // The folder field of a remote window (#241): the person types a folder
  // of the host, and the host says what it resolves to, 300 ms after the
  // typing stops. usable is true when the folder can be used as a root.
  import { bridge } from './bridge';
  import { t } from './i18n';
  import { pathHint, pathUsable, debounce, type RemotePath } from './remotepath';

  export let value = '';
  export let placeholder = '~/00.git';
  export let usable = false;

  let result: RemotePath | null = null;
  let checking = false;
  let asked = '';

  const check = debounce(async (p: string) => {
    asked = p;
    if (!p.trim()) { result = null; checking = false; return; }
    try {
      const got = (await bridge.validateRemotePath(p)) as RemotePath;
      if (asked === p) result = got;
    } catch (err: any) {
      if (asked === p) result = { abs: '', exists: false, isDir: false, writable: false, error: err?.message || String(err) };
    }
    if (asked === p) checking = false;
  }, 300);

  $: { checking = true; result = null; check(value); }
  $: usable = pathUsable(result);
  $: hint = pathHint(result);
</script>

<input class="ob-input remote-path" bind:value {placeholder} translate="no" spellcheck="false" autocapitalize="off" aria-label={$t('remotePath.label')} />
<p class="remote-path-hint" class:remote-path-bad={!!hint && !usable} aria-live="polite">
  {#if checking && value.trim()}{$t('remotePath.checking')}{:else if hint}<span translate="no">{$t(hint.key, hint.params)}</span>{/if}
</p>

<style>
  .remote-path { font-family: monospace; }
  .remote-path-hint { font-size: 11px; color: var(--text-muted); margin: 4px 0 0; min-height: 1.3em; }
  .remote-path-bad { color: var(--alert); }
</style>
