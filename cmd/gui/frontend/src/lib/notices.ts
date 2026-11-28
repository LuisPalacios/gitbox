// The notices of the session the shell shows (#138, #381). Each session
// keeps its own list (lib/notice-list.ts, in its store bundle): notices
// follows the active one, and pushNotice and dismissNotice act on it. A
// session controller pushes to its own bundle instead.
import { get } from 'svelte/store';
import { activeSession, follow } from './session/active';
import type { NoticeKind } from './notice-list';

export { AUTO_DISMISS_MS, MAX_NOTICES, errorText } from './notice-list';
export type { Notice, NoticeKind } from './notice-list';

export const notices = follow('notices');

// pushNotice shows a notice in the active session and returns its id.
// Empty lines are dropped.
export function pushNotice(kind: NoticeKind, title: string, lines: Array<string | false | null | undefined> = [], retry?: () => void): number {
  return get(activeSession).pushNotice(kind, title, lines, retry);
}

export function dismissNotice(id: number) {
  get(activeSession).dismissNotice(id);
}
