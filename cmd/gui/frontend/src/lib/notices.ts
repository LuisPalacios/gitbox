// Result cards and messages (issue #138): what a finished operation did,
// or why an action failed. They replace window.alert() and the native error
// dialog. Info and success notices dismiss themselves after a few seconds;
// errors stay until I dismiss them.
//
// Kept free of runtime imports so lib/notices.test.mjs can load it on its
// own: `notices` implements the Svelte store contract (subscribe) itself.

export type NoticeKind = 'info' | 'success' | 'error';

export interface Notice {
  id: number;
  kind: NoticeKind;
  title: string;
  lines: string[];
}

// How long info and success notices stay, and how many show at once (the
// oldest goes first).
export const AUTO_DISMISS_MS = 8000;
export const MAX_NOTICES = 4;

type Subscriber = (value: Notice[]) => void;

let list: Notice[] = [];
let nextId = 1;
const subscribers = new Set<Subscriber>();
const timers = new Map<number, ReturnType<typeof setTimeout>>();

function publish(next: Notice[]) {
  list = next;
  for (const fn of subscribers) fn(list);
}

export const notices = {
  subscribe(fn: Subscriber): () => void {
    subscribers.add(fn);
    fn(list);
    return () => { subscribers.delete(fn); };
  },
};

// pushNotice shows a notice and returns its id. Empty lines are dropped.
export function pushNotice(kind: NoticeKind, title: string, lines: Array<string | false | null | undefined> = []): number {
  const id = nextId++;
  const notice: Notice = { id, kind, title, lines: lines.filter((l): l is string => !!l) };
  const kept = [...list, notice];
  while (kept.length > MAX_NOTICES) dismissTimer(kept.shift()!.id);
  publish(kept);
  if (kind !== 'error') timers.set(id, setTimeout(() => dismissNotice(id), AUTO_DISMISS_MS));
  return id;
}

export function dismissNotice(id: number) {
  dismissTimer(id);
  if (list.some((n) => n.id === id)) publish(list.filter((n) => n.id !== id));
}

function dismissTimer(id: number) {
  const timer = timers.get(id);
  if (timer !== undefined) {
    clearTimeout(timer);
    timers.delete(id);
  }
}

// errorText turns a thrown value into its message.
export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (e && typeof e === 'object' && 'message' in e) return String((e as { message: unknown }).message);
  return String(e);
}
