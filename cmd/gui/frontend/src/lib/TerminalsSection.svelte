<script lang="ts">
  // TerminalsSection — single Gear-panel row in the same visual rhythm as
  // the other settings rows. The full editor lives in TerminalsModal so
  // the Gear panel itself doesn't grow vertically with the user's profile
  // count (issue #69 user feedback — embedding the table inline made the
  // panel unscrollable on smaller monitors).

  import type { TerminalAppInfo, ShellInfo, TerminalProfileInfo } from './types';
  import { tooltip } from './tooltip';

  export let apps: TerminalAppInfo[] = [];
  export let shells: ShellInfo[] = [];
  export let profiles: TerminalProfileInfo[] = [];
  export let onOpen: () => void;

  $: visibleProfiles = (profiles || []).filter(p => !p.hidden).length;
</script>

<div class="settings-row">
  <span class="settings-label" use:tooltip={"Terminal apps detected on this host (Windows Terminal, WezTerm, …), shells available to launch (cmd, pwsh, git-bash, WSL distros, bash, zsh, …), and the Profiles that pair them. Click ‘Manage…’ to set the default profile, mark preferred ones, or add custom entries."}>Terminals &amp; shells</span>
  <span class="settings-value">
    {apps.length} terminal{apps.length === 1 ? '' : 's'} ·
    {shells.length} shell{shells.length === 1 ? '' : 's'} ·
    {visibleProfiles} profile{visibleProfiles === 1 ? '' : 's'}
  </span>
  <button class="settings-action" on:click={onOpen}>Manage…</button>
</div>
