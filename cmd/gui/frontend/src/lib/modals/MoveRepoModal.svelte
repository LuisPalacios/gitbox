<script lang="ts">
  // Move a repository to another account or provider (issue #64): the form
  // with the credential readiness panel, the type-to-confirm gate, then the
  // progress and result of the move.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import { accounts, repoStates } from '../stores';
  import { dismiss } from '../dismiss';
  import type { MoveOwnerOption, MovePreflightDTO, MoveProgressEventDTO, MoveResultDTO, MoveReadinessDTO } from '../types';

  export let reloadFromDisk: () => Promise<void>;
  export let openCredChange: (accountKey: string, currentType: string) => void;
  export let openTokenSetup: (accountKey: string) => Promise<void>;

  // ── Move repository (issue #64) ──
  //
  // Two-step modal. Step 1 is the form (destination account+owner, new
  // name, visibility, opt-in delete toggles). Step 2 is the red
  // type-to-confirm gate. moveProgress accumulates phase events so the
  // progress overlay can show each step; moveResult holds the final
  // outcome and is set once "move:done" fires.
  type MoveModalStep = null | 'form' | 'confirm' | 'progress' | 'result';
  let moveModalStep: MoveModalStep = null;
  let moveSourceSourceKey = '';
  let moveSourceRepoKey = '';
  let moveOwnerOptions: MoveOwnerOption[] = [];
  let moveOwnerSelected = ''; // "account::owner"
  let moveDestAccountKey = '';
  let moveDestOwner = '';
  let moveNewName = '';
  let moveIsPrivate = true;
  let moveDeleteSource = false;
  let moveDeleteLocal = false;
  let moveTypeConfirm = '';
  let moveLoadingOwners = false;
  let moveError = '';
  let movePreflight: MovePreflightDTO | null = null;
  let moveProgress: MoveProgressEventDTO[] = [];
  let moveResult: MoveResultDTO | null = null;
  // Readiness panel — per-side credential status + scope hint.
  // Refreshed when the modal opens, when the destination changes,
  // and when the "Delete source" toggle flips (since delete adds
  // a required scope to the source side).
  let moveReadiness: MoveReadinessDTO | null = null;
  let moveReadinessLoading = false;

  async function refreshMoveReadiness() {
    if (!moveSourceSourceKey) return;
    moveReadinessLoading = true;
    try {
      moveReadiness = await bridge.checkMoveReadiness(
        moveSourceSourceKey,
        moveDestAccountKey || '',
        moveDeleteSource,
      );
    } catch {
      moveReadiness = null;
    } finally {
      moveReadinessLoading = false;
    }
  }

  // Jump from the Move modal into the credential setup modal for
  // the given account, so the user can paste a fresh PAT with the
  // right scopes. The Move modal stays mounted so the user can
  // return to it by re-clicking "Move repository…" after closing
  // the credential modal; we preserve their form inputs by NOT
  // resetting moveModalStep until they dismiss Move explicitly.
  function openCredentialsFromMove(accountKey: string) {
    if (!accountKey) return;
    const acct = $accounts[accountKey];
    if (!acct) return;
    // Use openTokenSetup when the account is token-type so the
    // PAT paste field is focused immediately; otherwise fall back
    // to openCredChange which lets the user switch type or refresh
    // GCM creds.
    if (acct.default_credential_type === 'token') {
      void openTokenSetup(accountKey);
    } else {
      openCredChange(accountKey, acct.default_credential_type || 'gcm');
    }
    // Close the move form — the credential modal opens over the
    // same dashboard surface and the user restarts Move after.
    moveModalStep = null;
  }

  export async function openMoveRepo(sourceKey: string, repoKey: string) {
    moveSourceSourceKey = sourceKey;
    moveSourceRepoKey = repoKey;
    moveOwnerOptions = [];
    moveOwnerSelected = '';
    moveDestAccountKey = '';
    moveDestOwner = '';
    // Prefill new name from the source repo name (strip owner/).
    const slash = repoKey.indexOf('/');
    moveNewName = slash >= 0 ? repoKey.slice(slash + 1) : repoKey;
    moveIsPrivate = true;
    moveDeleteSource = false;
    moveDeleteLocal = false;
    moveTypeConfirm = '';
    moveError = '';
    movePreflight = null;
    moveProgress = [];
    moveResult = null;
    moveModalStep = 'form';
    moveLoadingOwners = true;
    try {
      moveOwnerOptions = await bridge.listMoveDestinationOwners(sourceKey);
      if (moveOwnerOptions.length > 0) {
        moveOwnerSelected = `${moveOwnerOptions[0].account}::${moveOwnerOptions[0].owner}`;
        syncMoveSelection();
      }
    } catch (e: any) {
      moveError = e?.message || 'Failed to load destination owners';
    } finally {
      moveLoadingOwners = false;
    }
    // Kick off the readiness probe once we know both sides so the
    // credentials panel populates without the user having to change
    // anything.
    void refreshMoveReadiness();
  }

  function syncMoveSelection() {
    const [acct, owner] = moveOwnerSelected.split('::');
    moveDestAccountKey = acct || '';
    moveDestOwner = owner || '';
    // Destination account changed → re-probe readiness.
    void refreshMoveReadiness();
  }

  function cancelMoveRepo() {
    moveModalStep = null;
  }

  async function advanceMoveToConfirm() {
    moveError = '';
    syncMoveSelection();
    if (!moveDestAccountKey || !moveDestOwner) {
      moveError = 'Pick a destination.';
      return;
    }
    if (!/^[a-zA-Z0-9_][a-zA-Z0-9._-]*$/.test(moveNewName)) {
      moveError = 'Name must be alphanumeric (hyphens/dots/underscores allowed, no leading dot/hyphen).';
      return;
    }
    try {
      const req = buildMoveRequest();
      movePreflight = await bridge.preflightMove(req);
      if (!movePreflight.ok) {
        moveError = movePreflight.error || 'Preflight failed';
        return;
      }
      moveModalStep = 'confirm';
    } catch (e: any) {
      moveError = e?.message || 'Preflight failed';
    }
  }

  function buildMoveRequest() {
    return {
      sourceSourceKey: moveSourceSourceKey,
      sourceRepoKey: moveSourceRepoKey,
      destAccountKey: moveDestAccountKey,
      destOwner: moveDestOwner,
      destRepoName: moveNewName,
      destPrivate: moveIsPrivate,
      deleteSourceRemote: moveDeleteSource,
      deleteLocalClone: moveDeleteLocal,
    };
  }

  async function confirmMoveRepo() {
    if (moveTypeConfirm !== moveSourceRepoKey) {
      moveError = 'Repository name does not match.';
      return;
    }
    moveError = '';
    moveProgress = [];
    moveResult = null;
    moveModalStep = 'progress';
    try {
      await bridge.moveRepo(buildMoveRequest());
    } catch (e: any) {
      moveError = e?.message || 'Move failed';
      moveModalStep = 'result';
    }
  }

  async function closeMoveResult() {
    moveModalStep = null;
    try {
      await reloadFromDisk();
    } catch {
      /* surfaced as reloadError */
    }
  }

  // move:progress — one phase finished (or failed).
  export function onProgress(p: any) {
    moveProgress = [...moveProgress, p as MoveProgressEventDTO];
  }

  // move:done — the move finished; r carries the outcome.
  export function onDone(r: any) {
    moveResult = r as MoveResultDTO;
    moveModalStep = 'result';
    // If the user opted to delete the local source clone, the
    // backend auto-triggers a fresh clone of the destination.
    // Paint the new card as "cloning" up front so the repo
    // doesn't flash through an "error/not cloned" state before
    // clone:progress starts flowing.
    if (moveResult && !moveResult.error && moveResult.localCloneDeleted && moveResult.destSourceKey && moveResult.newRepoKey) {
      repoStates.update((s) => ({
        ...s,
        [`${moveResult!.destSourceKey}/${moveResult!.newRepoKey}`]: {
          status: 'cloning', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0,
        },
      }));
    }
    // Reload config so the dashboard reflects the repo's new
    // home immediately — otherwise the user reads "Move complete"
    // while still seeing the source-account card list with the
    // repo in its OLD slot, which looks like the move didn't work.
    if (moveResult && !moveResult.error) {
      reloadFromDisk().catch(() => { /* surfaced as reloadError */ });
    }
  }
</script>

{#if moveModalStep === 'form'}
  <div class="overlay" use:dismiss={cancelMoveRepo} transition:fade={{ duration: 120 }}>
    <div class="modal modal-account" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>Move repository &mdash; {moveSourceRepoKey}</h3>
        <button class="btn-x" on:click={cancelMoveRepo}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if moveLoadingOwners}
          <p class="detail-loading">Loading destination accounts…</p>
        {:else if moveOwnerOptions.length === 0}
          <p class="form-error">No other accounts are configured. Add a second account first to move a repo across accounts/providers.</p>
        {:else}
          <p style="color: var(--text-secondary); margin-bottom: 8px;">This will push every ref and tag to the destination, rewire <code>origin</code>, and (optionally) delete the source.</p>
          {#if moveError}
            <p class="form-error">{moveError}</p>
          {/if}
          <div class="form-row">
            <label class="form-label" for="move-repo-dest">Destination</label>
            <select id="move-repo-dest" class="form-input" bind:value={moveOwnerSelected} on:change={syncMoveSelection}>
              {#each moveOwnerOptions as opt}
                <option value="{opt.account}::{opt.owner}">{opt.account} &rarr; {opt.owner}{opt.isOrg ? ' (org)' : ''}</option>
              {/each}
            </select>
          </div>
          <div class="form-row">
            <label class="form-label" for="move-repo-name">New name</label>
            <input id="move-repo-name" class="form-input" bind:value={moveNewName} placeholder="{moveNewName}" />
          </div>
          <div class="form-row form-row-check">
            <label><input type="checkbox" bind:checked={moveIsPrivate} /> Private repository</label>
          </div>
          <div class="form-row form-row-check">
            <label><input type="checkbox" bind:checked={moveDeleteSource} on:change={refreshMoveReadiness} /> Delete source repository after successful move</label>
          </div>
          <div class="form-row form-row-check">
            <label><input type="checkbox" bind:checked={moveDeleteLocal} /> Delete local clone after successful move</label>
          </div>

          <!-- Credential readiness panel (issue #64, UX polish) -->
          <div class="move-readiness">
            <div class="move-readiness-title">Credential readiness</div>
            {#if moveReadinessLoading && !moveReadiness}
              <div class="move-readiness-loading">Checking credentials…</div>
            {:else if moveReadiness}
              {#each ['source', 'dest'] as side}
                {@const r = side === 'source' ? moveReadiness.source : moveReadiness.dest}
                {#if r.accountKey}
                  <div class="move-readiness-card move-readiness-{r.status || 'unknown'}">
                    <div class="move-readiness-head">
                      <span class="move-readiness-role">{side === 'source' ? 'Source' : 'Destination'}</span>
                      <span class="move-readiness-acct">{r.accountKey}</span>
                      <span class="move-readiness-type">{r.credentialType || '—'}</span>
                      <span class="move-readiness-dot move-readiness-dot-{r.status || 'unknown'}" title={r.status}></span>
                    </div>
                    {#if r.message}
                      <div class="move-readiness-msg">{r.message}</div>
                    {/if}
                    {#if r.scopesHint}
                      <div class="move-readiness-scopes">{r.scopesHint}</div>
                    {/if}
                    <div class="move-readiness-actions">
                      <button type="button" class="btn-link" on:click={() => openCredentialsFromMove(r.accountKey)}>Fix credentials…</button>
                      <button type="button" class="btn-link-subtle" on:click={refreshMoveReadiness} disabled={moveReadinessLoading}>Re-check</button>
                    </div>
                  </div>
                {/if}
              {/each}
              <p class="move-readiness-hint">Scope hints show what each side needs in total for the options you've selected. gitbox only probes auth reachability here — the actual scope check happens when the move runs, with a warning naming the missing scope if it fails.</p>
            {/if}
          </div>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={cancelMoveRepo}>Cancel</button>
        {#if moveOwnerOptions.length > 0 && !moveLoadingOwners}
          <button class="btn-add" on:click={advanceMoveToConfirm}>Continue…</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

{#if moveModalStep === 'confirm'}
  <div class="overlay" transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm modal-danger" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head modal-head-danger">
        <h3>Confirm move — destructive</h3>
        <button class="btn-x" on:click={cancelMoveRepo}>&#10005;</button>
      </div>
      <div class="modal-body">
        <p class="delete-warning delete-danger">This action is destructive and cannot be undone.</p>
        <ul class="move-summary">
          <li>Destination: <strong>{moveDestAccountKey} &rarr; {moveDestOwner}/{moveNewName}</strong></li>
          {#if moveDeleteSource}
            <li class="move-danger-line">Source repo on the origin provider <strong>WILL</strong> be deleted.</li>
          {:else}
            <li>Source repo on the origin provider will be kept.</li>
          {/if}
          {#if moveDeleteLocal}
            <li class="move-danger-line">Local clone at <code>{movePreflight?.sourceRepoPath || ''}</code> <strong>WILL</strong> be removed.</li>
          {:else}
            <li>Local clone will be kept; its <code>origin</code> will be rewired to the destination.</li>
          {/if}
          {#if movePreflight?.destCloneUrl}
            <li>New origin: <code>{movePreflight.destCloneUrl}</code></li>
          {/if}
        </ul>
        <p class="delete-repo-name">Type <strong>{moveSourceRepoKey}</strong> to unlock:</p>
        <input class="form-input" bind:value={moveTypeConfirm} placeholder="{moveSourceRepoKey}" />
        {#if moveError}
          <p class="form-error">{moveError}</p>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={cancelMoveRepo}>Cancel</button>
        <button class="btn-danger" disabled={moveTypeConfirm !== moveSourceRepoKey} on:click={confirmMoveRepo}>Move</button>
      </div>
    </div>
  </div>
{/if}

{#if moveModalStep === 'progress' || moveModalStep === 'result'}
  <div class="overlay" transition:fade={{ duration: 120 }}>
    <div class="modal modal-confirm" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>
          {#if moveModalStep === 'progress'}Moving {moveSourceRepoKey}…
          {:else if moveResult?.error}Move failed
          {:else}Move complete
          {/if}
        </h3>
        {#if moveModalStep === 'result'}
          <button class="btn-x" on:click={closeMoveResult}>&#10005;</button>
        {/if}
      </div>
      <div class="modal-body">
        {#each moveProgress as p}
          <div class="move-phase" class:move-phase-has-error={!!p.error}>
            <span class="move-phase-dot" class:move-phase-err={!!p.error}></span>
            <span class="move-phase-label">{p.phase.replace(/_/g, ' ')}</span>
            <span class="move-phase-msg">{p.message}</span>
          </div>
        {/each}
        {#if moveModalStep === 'result' && moveResult}
          {#if moveResult.error}
            <p class="form-error">{moveResult.error}</p>
          {:else}
            <p class="move-done-ok">✓ Move complete.</p>
            {#if moveResult.localCloneDeleted && moveResult.destSourceKey && moveResult.newRepoKey}
              <p class="move-info">Cloning the destination into <code>{moveResult.destSourceKey}/{moveResult.newRepoKey}</code> — watch the card on the dashboard.</p>
            {/if}
            {#if moveResult.warnings && moveResult.warnings.length > 0}
              <ul class="move-warnings">
                {#each moveResult.warnings as w}
                  <li>⚠ {w}</li>
                {/each}
              </ul>
            {/if}
          {/if}
        {/if}
      </div>
      <div class="modal-foot">
        {#if moveModalStep === 'result'}
          <button class="btn-add" on:click={closeMoveResult}>Close</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .detail-loading {
    color: var(--text-muted); font-style: italic;
  }
  /* Move-repo modal (issue #64). */
  .modal-danger { border-color: #D81E5B; }
  .modal-head-danger { border-bottom-color: #D81E5B; color: #D81E5B; }
  .modal-head-danger h3 { color: #D81E5B; }
  .form-row-check { display: flex; align-items: center; margin: 6px 0; }
  .form-row-check label { display: flex; align-items: center; gap: 8px; color: var(--text-secondary); font-size: 13px; }
  .move-summary { list-style: none; padding: 0; margin: 8px 0 12px; color: var(--text-secondary); font-size: 13px; }
  .move-summary li { padding: 2px 0; }
  .move-summary code { background: var(--bg-card); padding: 1px 4px; border-radius: 3px; font-size: 12px; }
  .move-danger-line { color: #D81E5B; font-weight: 500; }
  .move-phase { display: flex; align-items: baseline; gap: 8px; padding: 4px 0; font-size: 13px; color: var(--text-secondary); }
  .move-phase-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #22c55e; flex: 0 0 auto; }
  .move-phase-dot.move-phase-err { background: #f59e0b; }
  .move-phase-label { min-width: 150px; color: var(--text-primary); text-transform: capitalize; }
  .move-phase-msg { flex: 1; }
  .move-done-ok { color: #22c55e; font-size: 14px; margin: 8px 0; }
  .move-info { color: var(--text-secondary); font-size: 13px; margin: 4px 0 8px; }
  .move-info code { background: var(--bg-card); padding: 1px 4px; border-radius: 3px; font-size: 12px; }
  .move-warnings { list-style: none; padding: 0; margin: 8px 0; color: #f59e0b; font-size: 13px; }
  .move-warnings li { padding: 2px 0; }
  /* Credential readiness panel in the Move form. */
  .move-readiness { margin-top: 12px; border-top: 1px solid var(--border); padding-top: 10px; }
  .move-readiness-title { font-size: 12px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.04em; margin-bottom: 6px; }
  .move-readiness-loading { font-size: 13px; color: var(--text-secondary); padding: 6px 0; }
  .move-readiness-hint { font-size: 11px; color: var(--text-dim); margin-top: 6px; }
  .move-readiness-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px 10px;
    margin: 6px 0;
    font-size: 13px;
  }
  .move-readiness-head { display: flex; align-items: center; gap: 8px; }
  .move-readiness-role { font-weight: 600; min-width: 84px; color: var(--text-primary); }
  .move-readiness-acct { flex: 1; color: var(--text-secondary); font-family: 'SF Mono', 'Cascadia Code', 'Consolas', monospace; font-size: 12px; }
  .move-readiness-type { text-transform: uppercase; font-size: 10px; font-weight: 700; color: var(--text-dim); border: 1px solid var(--border); border-radius: 3px; padding: 1px 5px; }
  .move-readiness-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; flex: 0 0 auto; background: #6b7280; }
  .move-readiness-dot-ok { background: #22c55e; }
  .move-readiness-dot-warning { background: #f59e0b; }
  .move-readiness-dot-error,
  .move-readiness-dot-none { background: #ef4444; }
  .move-readiness-dot-offline { background: #64748b; }
  .move-readiness-dot-checking,
  .move-readiness-dot-unknown { background: #6b7280; }
  .move-readiness-msg { margin-top: 4px; color: var(--text-secondary); font-size: 12px; }
  .move-readiness-scopes { margin-top: 4px; color: var(--text-dim); font-size: 12px; }
  .move-readiness-actions { margin-top: 6px; display: flex; gap: 12px; }
  .btn-link { background: none; border: none; color: var(--accent); padding: 0; font-size: 12px; cursor: pointer; text-decoration: underline; }
  .btn-link:hover { color: var(--text-primary); }
  .btn-link-subtle { background: none; border: none; color: var(--text-dim); padding: 0; font-size: 12px; cursor: pointer; }
  .btn-link-subtle:hover { color: var(--text-secondary); }
  .btn-link-subtle:disabled { opacity: 0.5; cursor: default; }
</style>
