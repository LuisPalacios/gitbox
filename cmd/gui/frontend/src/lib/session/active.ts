// The session the shell shows (#353, #381). activeSession holds its store
// bundle; follow makes a read-only proxy of one bundle store that follows
// whichever session is active. The exports of lib/stores.ts, the notices
// and the translator are such proxies, so components keep their imports.
import { derived, writable } from 'svelte/store';
import type { Readable } from 'svelte/store';
import { createStoreBundle } from './bundle';
import type { StoreBundle } from './bundle';

// defaultBundle belongs to the process's default session: the local one,
// or the remote host the process started with.
export const defaultBundle = createStoreBundle();

export const activeSession = writable<StoreBundle>(defaultBundle);

type StoreKey = { [K in keyof StoreBundle]: StoreBundle[K] extends Readable<unknown> ? K : never }[keyof StoreBundle];
type ValueOf<S> = S extends Readable<infer V> ? V : never;

// follow returns a store with the value of the active session's store key.
// It has no set or update: a session writes to its own bundle.
export function follow<K extends StoreKey>(key: K): Readable<ValueOf<StoreBundle[K]>> {
  const store = derived<Readable<StoreBundle>, ValueOf<StoreBundle[K]>>(activeSession, ($session, set) =>
    ($session[key] as Readable<ValueOf<StoreBundle[K]>>).subscribe(set));
  return { subscribe: store.subscribe };
}
