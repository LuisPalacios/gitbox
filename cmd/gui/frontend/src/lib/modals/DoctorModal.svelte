<script lang="ts">
  // System check: probes the external tools gitbox uses and lists what is
  // missing. Settings → System check → Run opens it.
  import { fade, slide } from 'svelte/transition';
  import { bridge } from '../bridge';
  import type { DoctorReport } from '../bridge';
  import { dismiss } from '../dismiss';
  import { t } from '../i18n';

  // One-line result shown next to the Run button in Settings.
  export let doctorSummary = '';

  let showDoctorModal = false;
  let doctorReport: DoctorReport | null = null;
  let doctorLoading = false;

  export async function openDoctorModal() {
    showDoctorModal = true;
    doctorLoading = true;
    doctorReport = null;
    try {
      doctorReport = await bridge.doctorRun();
    } finally {
      doctorLoading = false;
    }
  }

  // Follows the language, so the Settings line re-reads after a switch.
  $: if (doctorReport) {
    doctorSummary = doctorReport.allOk
      ? $t('doctor.allPresent')
      : $t('doctor.requiredMissing', { n: doctorReport.missingReq });
  }
</script>

{#if showDoctorModal}
  <div class="overlay" use:dismiss={() => showDoctorModal = false} transition:fade={{ duration: 120 }}>
    <div class="modal modal-doctor" role="dialog" aria-modal="true" transition:slide={{ duration: 180 }}>
      <div class="modal-head">
        <h3>{$t('settings.systemCheck')}</h3>
        <button class="btn-x" on:click={() => showDoctorModal = false}>&#10005;</button>
      </div>
      <div class="modal-body">
        {#if doctorLoading}
          <div class="loading"><div class="spinner"></div><span>{$t('doctor.probing')}</span></div>
        {:else if doctorReport}
          <p class="doctor-summary">
            {#if doctorReport.allOk}
              <span class="doctor-ok">✓ {$t('doctor.allOk')}</span>
            {:else}
              <span class="doctor-err">✕ {$t(doctorReport.missingReq === 1 ? 'doctor.missingReqOne' : 'doctor.missingReqN', { n: doctorReport.missingReq })}</span>
            {/if}
            {#if doctorReport.missingOpt > 0}
              <span class="doctor-opt"> {$t(doctorReport.missingOpt === 1 ? 'doctor.missingOptOne' : 'doctor.missingOptN', { n: doctorReport.missingOpt })}</span>
            {/if}
          </p>
          <div class="doctor-list">
            {#each doctorReport.tools as tool}
              <div class="doctor-row" class:doctor-row-missing-req={!tool.found && tool.required}>
                <span class="doctor-state"
                      class:doctor-state-ok={tool.found}
                      class:doctor-state-err={!tool.found && tool.required}
                      class:doctor-state-opt={!tool.found && !tool.required}>
                  {tool.found ? '✓' : (tool.required ? '✕' : '·')}
                </span>
                <div class="doctor-main">
                  <div class="doctor-head">
                    <span class="doctor-name">{tool.displayName}</span>
                    {#if tool.required}<span class="doctor-tag">{$t('doctor.required')}</span>{/if}
                    {#if tool.found && tool.version}<span class="doctor-version">{tool.version}</span>{/if}
                  </div>
                  {#if tool.found}
                    <div class="doctor-path">{tool.path}</div>
                  {:else}
                    <div class="doctor-purpose">{tool.purpose}</div>
                    {#if tool.required && tool.requiredFor}
                      <div class="doctor-reason">{$t('doctor.needed', { what: tool.requiredFor })}</div>
                    {/if}
                    {#if tool.installHint}
                      <div class="doctor-install"><span class="doctor-install-label">{$t('doctor.install')}</span> <code>{tool.installHint}</code></div>
                    {/if}
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn-cancel" on:click={() => showDoctorModal = false}>{$t('common.close')}</button>
        <button class="btn-add" on:click={openDoctorModal} disabled={doctorLoading}>{$t('common.recheck')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-doctor { width: 680px; max-height: 80vh; display: flex; flex-direction: column; }
  .modal-doctor .modal-body { overflow-y: auto; }
  .doctor-summary { margin: 0 0 12px; font-size: 12px; }
  .doctor-ok { color: #86efac; font-weight: 600; }
  .doctor-err { color: #fca5a5; font-weight: 600; }
  .doctor-opt { color: var(--text-muted); }
  :global([data-theme="light"]) .doctor-ok { color: #166534; }
  :global([data-theme="light"]) .doctor-err { color: #b91c1c; }
  .doctor-list { display: flex; flex-direction: column; gap: 8px; }
  .doctor-row { display: flex; gap: 10px; align-items: flex-start; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-card); }
  .doctor-row-missing-req { border-color: #be123c; background: rgba(190, 18, 60, 0.06); }
  :global([data-theme="light"]) .doctor-row-missing-req { background: rgba(190, 18, 60, 0.04); }
  .doctor-state { font-size: 14px; font-weight: 800; flex-shrink: 0; width: 14px; line-height: 1.3; }
  .doctor-state-ok { color: #86efac; }
  :global([data-theme="light"]) .doctor-state-ok { color: #166534; }
  .doctor-state-err { color: #fca5a5; }
  :global([data-theme="light"]) .doctor-state-err { color: #b91c1c; }
  .doctor-state-opt { color: var(--text-muted); }
  .doctor-main { flex: 1; min-width: 0; }
  .doctor-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .doctor-name { font-weight: 600; color: var(--text-primary); font-size: 13px; }
  .doctor-tag { font-size: 9px; padding: 1px 5px; background: var(--bg-hover); color: var(--text-muted); border-radius: 3px; text-transform: uppercase; letter-spacing: 0.3px; }
  .doctor-version { color: var(--text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
  .doctor-path { color: var(--text-dim); font-size: 11px; font-family: ui-monospace, Menlo, Consolas, monospace; margin-top: 2px; word-break: break-all; }
  .doctor-purpose { color: var(--text-muted); font-size: 11px; margin-top: 2px; }
  .doctor-reason { color: var(--text-dim); font-size: 11px; margin-top: 4px; }
  .doctor-install { font-size: 11px; margin-top: 4px; color: var(--text-primary); }
  .doctor-install code { background: var(--bg-hover); padding: 1px 5px; border-radius: 3px; font-family: ui-monospace, Menlo, Consolas, monospace; }
  .doctor-install-label { color: var(--text-muted); margin-right: 4px; }
</style>
