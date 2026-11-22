<script lang="ts">
  // The connection log of a remote window (#239): every step with its
  // timing, install decisions, hashes, and what ssh and the server print.
  // The Go side scrubs each line before it gets here.
  import { afterUpdate } from 'svelte';
  import { bridge } from '../bridge';
  import type { RemoteDoctorReport } from '../bridge';
  import { t } from '../i18n';

  export let lines: string[] = [];
  export let onClose: () => void;

  let box: HTMLDivElement;
  let follow = true;
  let copied = false;
  let doctorBusy = false;
  let doctorError = '';
  let report: RemoteDoctorReport | null = null;

  afterUpdate(() => {
    if (follow && box) box.scrollTop = box.scrollHeight;
  });

  function onScroll() {
    follow = box.scrollTop + box.clientHeight >= box.scrollHeight - 8;
  }

  async function copyLog() {
    try {
      await navigator.clipboard.writeText(lines.join('\n') + '\n');
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch (e) {
      console.error(e);
    }
  }

  async function runDoctor() {
    doctorBusy = true;
    doctorError = '';
    report = null;
    try {
      const res = await bridge.remoteRunDoctor();
      if (res.error) doctorError = res.hint ? $t(res.hint) : res.error;
      else report = res.report ?? null;
    } catch (e) {
      doctorError = String(e);
    } finally {
      doctorBusy = false;
    }
  }
</script>

<section class="log-panel" aria-label={$t('remote.log.title')}>
  <header class="log-head">
    <h3>{$t('remote.log.title')}</h3>
    <span class="log-count">{$t('remote.log.lines', { n: lines.length })}</span>
    <div class="log-actions">
      <button class="rb" on:click={copyLog}>{copied ? $t('remote.log.copied') : $t('remote.log.copy')}</button>
      <button class="rb" on:click={runDoctor} disabled={doctorBusy}>{doctorBusy ? $t('remote.log.doctorRunning') : $t('remote.log.doctor')}</button>
      <button class="rb rb-quiet" on:click={onClose}>{$t('common.close')}</button>
    </div>
  </header>

  {#if doctorError}
    <p class="doctor doctor-bad">{doctorError}</p>
  {:else if report}
    <dl class="doctor">
      <dt>{$t('remote.doctor.server')}</dt><dd translate="no">{report.version} · {report.os}/{report.arch}</dd>
      <dt>{$t('remote.doctor.git')}</dt><dd translate="no">{report.gitVersion || $t('remote.doctor.gitMissing')}</dd>
      <dt>{$t('remote.doctor.config')}</dt><dd translate="no">{report.configPath}</dd>
      <dt>{$t('remote.doctor.folder')}</dt>
      <dd class:doctor-bad={!report.configDirWritable || !report.configDirExec}>
        {report.configDirWritable ? $t('remote.doctor.writable') : $t('remote.doctor.notWritable')},
        {report.configDirExec ? $t('remote.doctor.exec') : $t('remote.doctor.noExec')}
      </dd>
      <dt>{$t('remote.doctor.credentials')}</dt>
      <dd>{$t('remote.doctor.findings', { n: report.credentialFindings?.length ?? 0 })}</dd>
    </dl>
  {/if}

  <div class="log-lines" bind:this={box} on:scroll={onScroll} translate="no">
    {#each lines as line}
      <div class="log-line">{line}</div>
    {:else}
      <div class="log-empty">{$t('remote.log.empty')}</div>
    {/each}
  </div>
</section>

<style>
  .log-panel {
    display: flex; flex-direction: column; min-height: 0; flex: 1;
    border: 1px solid var(--border); border-radius: 10px; background: var(--bg-card);
    overflow: hidden;
  }
  .log-head {
    display: flex; align-items: center; gap: 10px; padding: 10px 14px;
    border-bottom: 1px solid var(--border);
  }
  .log-head h3 { margin: 0; font-size: 12px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-secondary); }
  .log-count { font-size: 11px; color: var(--text-muted); }
  .log-actions { margin-left: auto; display: flex; gap: 6px; }
  .log-lines {
    flex: 1; min-height: 120px; overflow: auto; padding: 8px 14px;
    font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    font-size: 11px; line-height: 1.55; color: var(--text-secondary);
    background: var(--bg-base);
  }
  .log-line { white-space: pre-wrap; word-break: break-all; }
  .log-empty { color: var(--text-muted); font-style: italic; }
  .doctor {
    margin: 0; padding: 10px 14px; border-bottom: 1px solid var(--border);
    display: grid; grid-template-columns: max-content 1fr; gap: 4px 14px; font-size: 12px;
  }
  .doctor dt { color: var(--text-muted); }
  .doctor dd { margin: 0; color: var(--text-primary); word-break: break-all; }
  .doctor-bad, dd.doctor-bad { color: var(--alert); }
  p.doctor { display: block; }
  .rb {
    padding: 4px 12px; font-size: 11px; font-weight: 500; border-radius: 6px; cursor: pointer;
    background: rgba(59, 130, 246, 0.10); border: 1px solid rgba(59, 130, 246, 0.28); color: var(--text-primary);
  }
  .rb:hover:not(:disabled) { background: rgba(59, 130, 246, 0.18); }
  .rb:disabled { opacity: 0.6; cursor: progress; }
  .rb-quiet { background: transparent; border-color: var(--border); color: var(--text-secondary); }
</style>
