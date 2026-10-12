<script lang="ts">
  // UIIcon — the top-bar action icons, drawn the same way as BrandIcon so
  // they read as one set: a shared 24×24 viewBox whose artwork stays inside
  // the same 18×18 guide box (3,3 → 21,21), one stroke weight, round caps,
  // painted with currentColor so they follow the light / dark theme.
  export let name: string;
  export let size: number = 18;
</script>

<svg class="ui-icon" viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor"
  stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  {#if name === 'pull'}
    <path d="M12 3.5v12M7 10.5l5 5 5-5M4.5 20.5h15"/>
  {:else if name === 'fetch'}
    <path d="M20.5 12a8.5 8.5 0 1 1-2.5-6"/>
    <path d="M20.5 3.5v5h-5"/>
  {:else if name === 'trash'}
    <path d="M3.5 6.5h17M9 6.5V3.5h6v3M5.5 6.5l1 14h11l1-14M10 10.5v6M14 10.5v6"/>
  {:else if name === 'select'}
    <rect x="3.5" y="3.5" width="17" height="17" rx="3.5"/>
  {:else if name === 'select-on'}
    <rect x="3.5" y="3.5" width="17" height="17" rx="3.5"/>
    <path d="M8 12.5l3 3 5.5-6.5"/>
  {:else if name === 'compact'}
    <rect x="3.5" y="4.5" width="17" height="15" rx="3"/>
    <path d="M6.5 4.5h3v15h-3a3 3 0 0 1-3-3v-9a3 3 0 0 1 3-3z" fill="currentColor" stroke="none"/>
  {:else if name === 'theme-light'}
    <circle cx="12" cy="12" r="4.5"/>
    <path d="M12 3v2.5M12 18.5V21M3 12h2.5M18.5 12H21M5.64 5.64l1.77 1.77M16.59 16.59l1.77 1.77M5.64 18.36l1.77-1.77M16.59 7.41l1.77-1.77"/>
  {:else if name === 'theme-dark'}
    <path d="M20 14.5A8.5 8.5 0 1 1 9.5 4a6.75 6.75 0 0 0 10.5 10.5z"/>
  {:else if name === 'theme-system'}
    <circle cx="12" cy="12" r="8.5"/>
    <path d="M12 3.5a8.5 8.5 0 0 0 0 17z" fill="currentColor" stroke="none"/>
  {:else if name === 'gear'}
    <path d="M18.69 9.95 L20.83 10.28 L20.83 13.72 L18.69 14.05 L18.18 15.29 L19.46 17.03 L17.03 19.46 L15.29 18.18 L14.05 18.69 L13.72 20.83 L10.28 20.83 L9.95 18.69 L8.71 18.18 L6.97 19.46 L4.54 17.03 L5.82 15.29 L5.31 14.05 L3.17 13.72 L3.17 10.28 L5.31 9.95 L5.82 8.71 L4.54 6.97 L6.97 4.54 L8.71 5.82 L9.95 5.31 L10.28 3.17 L13.72 3.17 L14.05 5.31 L15.29 5.82 L17.03 4.54 L19.46 6.97 L18.18 8.71 Z"/>
    <circle cx="12" cy="12" r="3"/>
  {/if}
</svg>

<style>
  .ui-icon { display: block; flex-shrink: 0; }
</style>
