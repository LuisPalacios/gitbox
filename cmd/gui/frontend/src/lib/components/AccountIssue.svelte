<script lang="ts">
  import { bridge } from '../bridge';
  import { openExternal } from '../external';
  import { accountIssue } from '../explain';
  import type { CredentialStatus } from '../types';
  import type { HealthFix } from '../status';
  import ErrorExplanation from './ErrorExplanation.svelte';

  export let accountKey: string;
  export let credential: CredentialStatus;
  export let simple: boolean;
  export let fixCredential: (accountKey: string, fix: HealthFix) => void;

  let generation = 0;
  let guide: { scopes?: string; creationURL?: string } | undefined;
  $: info = accountIssue(credential);
  $: void loadGuide(accountKey, info.code);

  async function loadGuide(account: string, code?: string) {
    const request = ++generation;
    guide = undefined;
    if (code !== 'provider.scope_denied') return;
    const result = await bridge.getTokenGuide(account).catch(() => undefined);
    if (request === generation) guide = result;
  }
</script>

<ErrorExplanation {info} {simple} {accountKey} scopes={guide?.scopes || ''} actions={{
  retry: () => fixCredential(accountKey, 'retry'),
  credential: () => fixCredential(accountKey, 'change'),
  token: guide?.creationURL ? () => void openExternal(guide?.creationURL || '') : undefined,
}} />
