<script lang="ts">
  // TerminalsSection — single Gear-panel row matching the visual rhythm of
  // the other settings rows (label + action button right next to it). The
  // editor itself runs in a separate OS window spawned by App.svelte's
  // openTerminalsManagerWindow handler — embedding it inline produced an
  // unscrollable wall of profile rows on developer hosts (issue #69 user
  // feedback).

  import { tooltip } from './tooltip';

  export let onOpen: () => void;
</script>

<div class="settings-row">
  <span class="settings-label" use:tooltip={"Manage terminal apps, shells, and the launch profiles that pair them. Click ‘Manager’ to open the editor in its own window."}>Terminals</span>
  <button class="settings-action-btn" on:click={onOpen}>Manager</button>
</div>
