// The browser funnel on the frontend (#240). Every link this app opens goes
// through openExternal, openAccountExternal or openRepoExternal, which call
// the Go funnel (cmd/gui/external.go): https opens at once, http only after
// the user accepts the exact URL in ExternalConfirm, anything else is
// refused there. Never import Wails' BrowserOpenURL: it checks links with a
// denylist, and health.sh fails on any use of it outside this comment.

import { writable } from 'svelte/store';
import { OpenInBrowser, OpenAccountInBrowser, OpenRepoInBrowser } from '../../wailsjs/go/main/App';
import { pushNotice, errorText } from './notices';

interface OpenResult {
  needsConfirm: boolean;
  url?: string;
}

// The http link waiting for the user's answer, shown by ExternalConfirm.
export const pendingLink = writable<{ url: string; answer: (ok: boolean) => void } | null>(null);

// noticeTitle is set by the app so that refusals read in the user's language.
let noticeTitle = 'The link was not opened';
export function setLinkNoticeTitle(title: string) { noticeTitle = title; }

async function run(open: (confirmed: boolean) => Promise<OpenResult>): Promise<void> {
  try {
    const res = await open(false);
    if (!res?.needsConfirm || !res.url) return;
    const url = res.url;
    const ok = await new Promise<boolean>((answer) => pendingLink.set({ url, answer }));
    pendingLink.set(null);
    if (ok) await open(true);
  } catch (e) {
    pushNotice('error', noticeTitle, [errorText(e)]);
  }
}

export const openExternal = (url: string): Promise<void> =>
  run((confirmed) => OpenInBrowser(url, confirmed) as Promise<OpenResult>);

export const openAccountExternal = (accountKey: string): Promise<void> =>
  run((confirmed) => OpenAccountInBrowser(accountKey, confirmed) as Promise<OpenResult>);

export const openRepoExternal = (sourceKey: string, repoKey: string): Promise<void> =>
  run((confirmed) => OpenRepoInBrowser(sourceKey, repoKey, confirmed) as Promise<OpenResult>);
