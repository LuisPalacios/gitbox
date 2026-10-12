<script lang="ts">
  // UIIcon — the top-bar action icons. The art lives in assets/svg/ui/
  // (24×24 viewBox, artwork inside the 18×18 guide box 3,3 → 21,21, one
  // stroke weight) and reaches the app through the build
  // (vite-plugin-icons.ts), recoloured to currentColor so it follows the
  // light / dark theme.
  import { ui } from 'virtual:icons';

  export let name: string;
  export let size: number = 18;
</script>

<span class="ui-icon" style="width: {size}px; height: {size}px" aria-hidden="true">{@html ui[name] || ''}</span>

<style>
  .ui-icon { display: block; flex-shrink: 0; }
  .ui-icon :global(svg) { display: block; width: 100%; height: 100%; }
</style>
