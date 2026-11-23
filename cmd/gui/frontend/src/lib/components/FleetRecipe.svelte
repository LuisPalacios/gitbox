<script lang="ts">
  import { bridge, type RecipeOptions } from '../bridge';
  import type { ConfigDTO } from '../types';
  import { t } from '../i18n';

  export let roots: string[];
  export let onImported: (cfg: ConfigDTO, added: string[]) => void;
  let file: Awaited<ReturnType<typeof bridge.openRecipeImport>> | null = null;
  let preview: Awaited<ReturnType<typeof bridge.previewRecipeImport>> | null = null;
  let options: RecipeOptions = {slots: {}, identities: {}};
  let exported = '';
  let message = '';
  let busy = false;

  function close() {
    file = null;
    exported = '';
    preview = null;
    options = {slots: {}, identities: {}};
  }
  async function run(action: () => Promise<void>) {
    if (busy) return;
    busy = true;
    message = '';
    try { await action(); }
    catch (e) {
      const key = e instanceof Error ? e.message : String(e);
      message = key.startsWith('recipe.') && $t(key) !== key ? key : 'recipe.fileError';
    } finally { busy = false; }
  }
  async function startExport() {
    close();
    exported = await bridge.previewRecipeExport();
  }
  async function save() {
    if (await bridge.exportRecipe(exported)) {
      close();
      message = 'recipe.saved';
    }
  }
  async function startImport() {
    close();
    const opened = await bridge.openRecipeImport();
    if (!opened.document) return;
    options = {
      slots: Object.fromEntries(opened.slots.map((s, i) => [s, roots[i] ?? ''])),
      identities: Object.fromEntries(opened.newAccounts.map(a => [a.server + '/' + a.username, {name: '', email: ''}])),
    };
    file = opened;
  }
  async function review() {
    if (file) preview = await bridge.previewRecipeImport(file.document, options);
  }
  async function apply() {
    if (!file || !preview || preview.collisions) return;
    await bridge.applyRecipeImport(file.document, options, preview.digest);
    const added = preview.rows.filter(row => row.kind === 'account' && row.state === 'add').map(row => row.item);
    close();
    onImported(await bridge.getConfig(), added);
    message = 'recipe.applied';
  }
</script>

<div class="bar">
  <span>{$t('recipe.title')}</span>
  <button disabled={busy} on:click={() => run(startExport)}>{$t('recipe.export')}</button>
  <button disabled={busy} on:click={() => run(startImport)}>{$t('recipe.import')}</button>
</div>
{#if exported || file}
  <section aria-label={$t('recipe.title')}>
    {#if exported}
      <p>{$t('recipe.exportReview')}</p>
      <pre>{exported}</pre>
      <button disabled={busy} on:click={() => run(save)}>{$t('common.save')}</button>
    {:else if file}
      <fieldset disabled={busy || !!preview}>
        <legend>{$t('recipe.mapRoots')}</legend>
        {#each file.slots as slot}
          <label>{slot}
            <select bind:value={options.slots[slot]}>
              <option value="">{$t('recipe.chooseRoot')}</option>
              {#each roots as root}<option value={root}>{root}</option>{/each}
            </select>
          </label>
        {/each}
        {#each file.newAccounts as account}
          {@const ref = account.server + '/' + account.username}
          <p>{ref} · {$t('recipe.needsIdentity')}</p>
          <label>{$t('recipe.authorName')} <input bind:value={options.identities[ref].name} /></label>
          <label>{$t('recipe.authorEmail')} <input type="email" bind:value={options.identities[ref].email} /></label>
        {/each}
      </fieldset>
      {#if preview}
        <p>{$t('recipe.summary', {accounts: preview.accounts, sources: preview.sources, repos: preview.repos})}</p>
        <div class="rows"><table>
          <thead><tr><th>{$t('recipe.type')}</th><th>{$t('recipe.item')}</th><th>{$t('recipe.result')}</th></tr></thead>
          <tbody>{#each preview.rows as row}
            <tr><td>{$t(`recipe.${row.kind}`)}</td><td>{row.item}</td><td>{$t(`recipe.${row.state}`)}</td></tr>
          {/each}</tbody>
        </table></div>
        {#if preview.collisions}<p>{$t('recipe.conflicts')}</p>{/if}
        <button disabled={busy} on:click={() => { preview = null; message = ''; }}>{$t('recipe.change')}</button>
        <button disabled={busy || !!preview.collisions || !preview.rows.some(r => r.state === 'add')} on:click={() => run(apply)}>{$t('recipe.apply')}</button>
      {:else}
        <button disabled={busy} on:click={() => run(review)}>{$t('recipe.preview')}</button>
      {/if}
    {/if}
    <button disabled={busy} on:click={() => {close(); message = '';}}>{$t('common.cancel')}</button>
  </section>
{/if}
{#if message}<p role="status">{$t(message)}</p>{/if}

<style>
  .bar, section { margin: 0.3rem 0; }
  .bar { display: flex; align-items: center; flex-wrap: wrap; gap: 0.5rem; }
  .bar span { min-width: 116px; color: var(--text-secondary); font-size: 12px; }
  section { padding: 0.6rem; border: 1px solid var(--border); border-radius: 0.4rem; }
  fieldset { display: grid; gap: 0.4rem; border: 0; padding: 0; margin: 0 0 0.5rem; }
  label { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  pre, .rows { max-height: 15rem; overflow: auto; }
  pre { white-space: pre; }
  td, th { text-align: start; padding: 0.2rem 0.5rem; }
  button, input, select { color: var(--text-primary); border: 1px solid var(--border); border-radius: 5px; background: var(--bg-base); font: inherit; padding: 3px 8px; }
  button { cursor: pointer; color: var(--text-accent, var(--text-primary)); }
  button:hover:enabled { background: var(--bg-hover); }
  pre, table, label, button, p, legend { font-size: 12px; }
  legend, th { color: var(--text-secondary); }
  button:disabled { cursor: default; }
</style>
