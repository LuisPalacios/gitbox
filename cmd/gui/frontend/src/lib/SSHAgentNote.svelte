<script lang="ts">
  // The SSH agent note of a remote window (#244): when the connection
  // forwards this computer's ssh agent, which key git on the host uses for
  // an SSH account. It asks the host for its report once per key (tag) and
  // says nothing in a local window, without an agent, or when the report
  // fails. Shown in a setup result and in the Change credential status.
  import { bridge } from './bridge';
  import { t } from './i18n';
  import { remoteWindow } from './stores';
  import { agentNote, agentNoteKey, type AgentReport } from './sshAgent';

  export let accountKey = '';
  export let simple = false;
  // tag changes when the key may have changed (a new public key, a re-check),
  // so the report is asked again; '' asks nothing.
  export let tag = '';

  let report: AgentReport | null = null;
  let askedFor = '';
  $: want = $remoteWindow && accountKey && tag ? `${accountKey}\n${tag}` : '';
  $: if (want !== askedFor) load(want);

  async function load(w: string) {
    askedFor = w;
    report = null;
    if (!w) return;
    try {
      const r = await bridge.credentialSSHAgentReport(accountKey);
      if (askedFor === w) report = r;
    } catch {
      // The note is optional: no report, no note.
    }
  }

  $: note = agentNote(report, $remoteWindow);
  $: vars = { alias: `gitbox-${accountKey}`, n: report?.agentFingerprints?.length ?? 0, fp: report?.keyFileFingerprint ?? '' };
</script>

{#if note}
  <p class="ssh-agent-note" class:ssh-agent-warn={note === 'unpinned'}>{$t(agentNoteKey(note, simple), vars)}</p>
{/if}

<style>
  .ssh-agent-note { margin: 8px 0 0; padding: 6px 8px; border-left: 3px solid var(--border, #444); font-size: 12px; color: var(--text-muted); }
  .ssh-agent-warn { border-left-color: #d97706; color: var(--text-primary); }
</style>
