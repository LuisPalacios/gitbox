<script lang="ts">
  // One translated sentence with inline markup (lib/rich.ts): code, strong
  // and emphasis spans stay inside the sentence, so word order is the
  // translation's, not the template's.
  import { tpl, format, type Vars } from './i18n';
  import { richSegments } from './rich';

  export let key: string;
  export let vars: Vars | undefined = undefined;

  $: segments = richSegments($tpl(key));
</script>

{#each segments as s}{#if s.kind === 'code'}<code>{format(s.text, vars)}</code>{:else if s.kind === 'strong'}<strong>{format(s.text, vars)}</strong>{:else if s.kind === 'em'}<em>{format(s.text, vars)}</em>{:else}{format(s.text, vars)}{/if}{/each}
