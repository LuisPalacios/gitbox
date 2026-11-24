<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { bridge, events } from './bridge';
  import { t } from './i18n';
  import { caughtIssue, errorInfo } from './explain';
  import type { ErrorInfo } from './explain';
  import ErrorExplanation from './components/ErrorExplanation.svelte';
  import { ro } from './ro';
  import { firstCloneOutcome, type FirstCloneResult } from './first-project';
  import { providerLabel } from './theme';
  import type { DiscoverResult, OrphanRepoDTO } from './types';

  export let simple: boolean;
  export let reopenCredential: () => void;
  export let openTokenPage: (() => void) | undefined;
  export let tokenScopes: string;
  export let accountKey: string;
  export let provider: string;
  export let url: string;
  export let username: string;
  export let folder: string;
  export let initialRoute: 'adopt' | 'download';
  export let onDone: () => void;

  type Route = 'adopt' | 'download';
  type Choice = { name: string; path: string };
  let route: Route = initialRoute;
  let choices: Choice[] = [];
  let selected: Choice | null = null;
  let destination = '';
  let filter = '';
  let loading = false;
  let saving = false;
  let outcome: 'ready' | 'attention' | '' = '';
  let error = '';
  let listProblem: ErrorInfo | undefined;
  let revision = 0;
  let requested = new Set<string>(); // retries of rows this wizard registered
  let stopListing: (() => void) | undefined;
  let stopClone: (() => void) | undefined;
  $: visible = choices.filter(c => c.name.toLowerCase().includes(filter.trim().toLowerCase()));

  onMount(() => { list(route); });
  onDestroy(() => { ++revision; stopListing?.(); stopClone?.(); });

  async function list(next: Route) {
    const rev = ++revision;
    stopListing?.(); stopListing = undefined;
    route = next;
    choices = []; selected = null; destination = ''; filter = ''; error = ''; listProblem = undefined;
    loading = true;
    try {
      const config = await bridge.getConfig();
      if (rev !== revision) return;
      const tracked = config.sources[accountKey]?.repos || {};
      if (next === 'adopt') {
        const found = await bridge.scanFolderForClones(folder);
        if (rev !== revision) return;
        choices = (found || []).filter((r: OrphanRepoDTO) => r.repoKey && !tracked[r.repoKey] && r.candidates?.includes(accountKey) && r.reason !== 'worktree')
          .map(r => ({ name: r.repoKey, path: r.path }));
        loading = false;
      } else {
        stopListing = events.on('discover:done', (data: { accountKey: string; repos?: DiscoverResult[]; error?: string; problem?: ErrorInfo }) => {
          if (rev !== revision || data?.accountKey !== accountKey) return;
          stopListing?.(); stopListing = undefined;
          loading = false;
          if (data.problem || data.error) { listProblem = errorInfo(data.problem, data.error); return; }
          choices = (data.repos || []).filter(r => !tracked[r.fullName] || requested.has(r.fullName))
            .map(r => ({ name: r.fullName, path: '' })).sort((a,b) => a.name.localeCompare(b.name));
        });
        await bridge.discover(accountKey);
      }
    } catch (failure) {
      if (rev !== revision) return;
      stopListing?.(); stopListing = undefined;
      loading = false; listProblem = caughtIssue(failure);
    }
  }

  async function choose(choice: Choice) {
    selected = choice;
    destination = choice.path;
    error = '';
    if (route === 'adopt') return;
    try {
      const path = await bridge.previewClonePath(accountKey, choice.name);
      if (selected === choice) destination = path;
    } catch {
      if (selected === choice) error = $t('onboard.previewFailed');
    }
  }

  async function prepare() {
    if (!selected || !destination || saving) return;
    const project = selected;
    const target = destination;
    const rev = revision;
    saving = true; error = '';
    try {
      if (route === 'adopt') {
        const ready = await bridge.adoptFirstProject(accountKey, project.name, project.path);
        if (rev === revision) { outcome = ready ? 'ready' : 'attention'; saving = false; }
      } else {
        // Register only this project, after the person has seen its destination.
        const cfg = await bridge.getConfig();
        if (cfg.sources[accountKey]?.repos?.[project.name] && !requested.has(project.name)) throw new Error();
        if (await bridge.previewClonePath(accountKey, project.name) !== target) throw new Error();
        await bridge.addDiscoveredRepos(accountKey, [project.name]);
        requested.add(project.name);
        if (rev !== revision) return;
        stopClone = events.on('clone:done', async (event: FirstCloneResult) => {
          if (event?.source !== accountKey || event.repo !== project.name || rev !== revision) return;
          stopClone?.(); stopClone = undefined;
          try {
            const result = await firstCloneOutcome(event, target, bridge.getAllStatus);
            if (rev !== revision) return;
            if (result === 'failed') error = $t('onboard.prepareFailed');
            else outcome = result;
          } catch { if (rev === revision) error = $t('onboard.prepareFailed'); }
          finally { if (rev === revision) saving = false; }
        });
        await bridge.cloneRepo(accountKey, project.name);
      }
    } catch {
      if (rev !== revision) return;
      stopClone?.(); stopClone = undefined;
      error = $t('onboard.prepareFailed'); saving = false;
    }
  }
</script>

{#if outcome}
  <h2>{outcome === 'ready' ? $t('onboard.projectReady') : $t('onboard.projectAttention')}</h2>
  <p><code>{destination}</code></p>
  <button class="primary" on:click={onDone}>{$t('onboard.done')}</button>
{:else}
  <h2>{$t('onboard.firstProject')}</h2>
  <p>{$t('onboard.connectedIdentity', { provider: providerLabel(provider), server: url, user: username })}</p>
  <div class="routes">
    <button aria-pressed={route === 'download'} disabled={saving} on:click={() => list('download')}>{$t('onboard.downloadProject')}</button>
    <button aria-pressed={route === 'adopt'} disabled={saving} on:click={() => list('adopt')}>{$t('onboard.adoptProject')}</button>
  </div>
  {#if loading}
    <p aria-live="polite">{$t('onboard.projectsChecking')}</p>
  {:else if choices.length}
    {#if choices.length > 10}<input aria-label={$t('discover.filter')} placeholder={$t('discover.filter')} bind:value={filter} disabled={saving} />{/if}
    <div class="projects">
      {#each visible as choice (choice.path || choice.name)}
        <label><input type="radio" name="first-project" checked={selected === choice} disabled={saving} on:change={() => choose(choice)} />
          <span>{choice.name}{#if choice.path}<br/><code>{choice.path}</code>{/if}</span></label>
      {/each}
    </div>
    {#if destination}<p>{$t('onboard.projectFolder')}<br/><code>{destination}</code></p>{/if}
    {#if selected}
      {#if route === 'adopt'}
        <button class="primary" disabled={saving || !destination} use:ro={'adoptFirstProject'} on:click={prepare}>{saving ? $t('onboard.preparing') : $t('onboard.useSelected', { project: selected.name })}</button>
      {:else}
        <button class="primary" disabled={saving || !destination} use:ro={['addDiscoveredRepos', 'cloneRepo']} on:click={prepare}>{saving ? $t('onboard.preparing') : $t('onboard.downloadSelected', { project: selected.name })}</button>
      {/if}
    {/if}
  {:else if listProblem}
    <ErrorExplanation info={listProblem} {simple} scopes={tokenScopes} actions={{ retry: () => list(route), credential: reopenCredential, token: openTokenPage }} />
  {:else if !error}
    <p>{route === 'adopt' ? $t('onboard.noLocalProjects') : $t('onboard.projectsNone')}</p>
  {/if}
  {#if error}
    <p role="alert">{error}</p>
    {#if !choices.length}<button on:click={() => list(route)}>{$t('onboard.tryAgain')}</button>{/if}
  {/if}
  <button class="skip" disabled={saving} on:click={onDone}>{$t('onboard.skip')}</button>
{/if}

<style>
  h2 { font-size: 15px; margin: 0 0 6px; }
  p { font-size: 13px; color: var(--text-secondary); margin: 4px 0 12px; }
  code { overflow-wrap: anywhere; }
  button, input { font: inherit; font-size: 13px; }
  button { background: var(--bg-hover); color: var(--text-secondary); border: 1px solid var(--border); padding: 7px 10px; border-radius: 6px; cursor: pointer; }
  button:disabled { opacity: .5; cursor: default; }
  .routes { display: flex; gap: 6px; margin: 8px 0; }
  button[aria-pressed=true] { border-color: #4b95e9; color: var(--text-primary); }
  .projects { overflow: auto; max-height: 200px; }
  label { display: flex; align-items: center; gap: 6px; padding: 4px; overflow-wrap: anywhere; }
  .primary { background: #346fba; color: white; margin: 8px 0; }
  .skip { border: 0; background: transparent; text-decoration: underline; }
  [role=alert] { color: var(--alert); }
</style>
