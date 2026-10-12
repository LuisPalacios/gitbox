<script lang="ts">
  // BrandIcon — tool logos for the repo-row launch buttons (issue #124).
  // The art lives in assets/svg/brand/ and reaches the app through the
  // build (vite-plugin-icons.ts). Light theme shows the colour original
  // (logo-<name>.svg) as drawn; dark theme shows the single-colour version
  // (logo-<name>-dark.svg) in the theme's text colour. Each theme falls back
  // to the other file when its own is missing.
  import { brand } from 'virtual:icons';
  import { themeStore } from './stores';

  export let name: string;
  export let size: number = 16;

  $: entry = brand[name] || {};
  $: useColor = entry.color && ($themeStore === 'light' || !entry.mono);
</script>

{#if useColor}
  <img class="brand-icon" src={entry.color} width={size} height={size} alt="" aria-hidden="true" />
{:else if entry.mono}
  <span class="brand-icon" style="width: {size}px; height: {size}px" aria-hidden="true">{@html entry.mono}</span>
{/if}

<style>
  .brand-icon { display: block; flex-shrink: 0; }
  .brand-icon :global(svg) { display: block; width: 100%; height: 100%; }
</style>
