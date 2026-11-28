import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('./events.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { createActiveEvents, createEventRouter, createSessionSubscriptions } = await import(`data:text/javascript,${encodeURIComponent(outputText)}`);

// runtime stands in for Wails: it counts the subscriptions taken per name.
function runtime() {
  const subscribers = new Map();
  let taken = 0;
  const sessions = createSessionSubscriptions((name, handler) => {
    taken++;
    if (!subscribers.has(name)) subscribers.set(name, new Set());
    const listeners = subscribers.get(name);
    listeners.add(handler);
    return () => {
      listeners.delete(handler);
      if (!listeners.size) subscribers.delete(name);
    };
  });
  function deliver(name, sequence, payload, extra = {}) {
    const event = { schema: 1, sequence, configRevision: 1, operationID: '1', payload, ...extra };
    for (const listener of subscribers.get(name) || []) listener(event);
  }
  return { sessions, deliver, taken: () => taken, active: () => [...subscribers.keys()], listeners: name => subscribers.get(name)?.size ?? 0 };
}

test('the default router ignores an event of another session, even above its watermark', () => {
  const { sessions, deliver } = runtime();
  const main = createEventRouter(sessions.forSession(''));
  const other = createEventRouter(sessions.forSession('remote:r1'));
  const seen = [];
  const elsewhere = [];
  main.on('fetch:done', (payload, meta) => seen.push([payload.repo, meta.sequence]));
  other.on('fetch:done', (payload, meta) => elsewhere.push([payload.repo, meta.sequence]));
  main.snapshot(5, () => 1);
  other.snapshot(5, () => 1);

  deliver('fetch:done', 9, { source: 'acct', repo: 'theirs' }, { session: 'remote:r1' });
  assert.deepEqual(seen, []);
  assert.deepEqual(elsewhere, [['theirs', 9]]);

  // The same sequence, untagged, is still new to the default router.
  deliver('fetch:done', 9, { source: 'acct', repo: 'mine' });
  assert.deepEqual(seen, [['mine', 9]]);
  assert.deepEqual(elsewhere, [['theirs', 9]]);
});

test('untagged and empty-tagged events reach only the default router', () => {
  const { sessions, deliver } = runtime();
  const main = createEventRouter(sessions.forSession(''));
  const local = createEventRouter(sessions.forSession('local'));
  const remote = createEventRouter(sessions.forSession('remote:r1'));
  const got = { main: [], local: [], remote: [] };
  main.on('status:one', (_, meta) => got.main.push(meta.sequence));
  local.on('status:one', (_, meta) => got.local.push(meta.sequence));
  remote.on('status:one', (_, meta) => got.remote.push(meta.sequence));
  for (const router of [main, local, remote]) router.snapshot(0, () => 0);

  deliver('status:one', 1, { source: 'acct', repo: 'a' });
  deliver('status:one', 2, { source: 'acct', repo: 'a' }, { session: '' });
  deliver('status:one', 3, { source: 'acct', repo: 'a' }, { session: 'local' });
  // A tag that is not a string belongs to no session.
  deliver('status:one', 4, { source: 'acct', repo: 'a' }, { session: 7 });
  deliver('status:one', 5, { source: 'acct', repo: 'a' }, { session: null });

  assert.deepEqual(got, { main: [1, 2], local: [3], remote: [] });
});

test('sessions share one runtime subscription per event name and release it with the last listener', () => {
  const { sessions, taken, active, listeners } = runtime();
  const main = createEventRouter(sessions.forSession(''));
  const remote = createEventRouter(sessions.forSession('remote:r1'));
  const offA = main.on('fetch:done', () => {});
  const offB = main.on('fetch:done', () => {});
  const offC = remote.on('fetch:done', () => {});
  const offD = remote.on('clone:done', () => {});
  assert.equal(taken(), 2);
  assert.equal(listeners('fetch:done'), 1);
  assert.deepEqual(active().sort(), ['clone:done', 'fetch:done']);

  offA();
  offC();
  assert.deepEqual(active().sort(), ['clone:done', 'fetch:done']);
  offB();
  assert.deepEqual(active(), ['clone:done']);
  offD();
  offD();
  assert.deepEqual(active(), []);

  // A later listener takes the subscription again.
  main.on('fetch:done', () => {});
  assert.equal(taken(), 3);
  assert.deepEqual(active(), ['fetch:done']);
});

test('the active-session proxy binds each subscription to the router active when it subscribes', () => {
  const { sessions, deliver } = runtime();
  const fallback = createEventRouter(sessions.forSession(''));
  const local = createEventRouter(sessions.forSession('local'));
  const remote = createEventRouter(sessions.forSession('remote:r1'));
  for (const router of [fallback, local, remote]) router.snapshot(5, () => 1);
  const active = createActiveEvents(fallback);
  assert.deepEqual(Object.keys(active).sort(), ['follow', 'on'], 'the proxy cannot move a watermark');
  const got = [];
  active.on('fetch:done', (payload, meta) => got.push(['before', payload.repo, meta.sequence]));
  active.follow(local);
  const off = active.on('fetch:done', (payload, meta) => got.push(['local', payload.repo, meta.sequence]));
  active.follow(remote);

  deliver('fetch:done', 6, { source: 'acct', repo: 'a' });
  deliver('fetch:done', 7, { source: 'acct', repo: 'b' }, { session: 'local' });
  deliver('fetch:done', 8, { source: 'acct', repo: 'c' }, { session: 'remote:r1' });
  // At or below the local router's own watermark.
  deliver('fetch:done', 5, { source: 'acct', repo: 'd' }, { session: 'local' });
  assert.deepEqual(got, [['before', 'a', 6], ['local', 'b', 7]]);

  off();
  deliver('fetch:done', 9, { source: 'acct', repo: 'e' }, { session: 'local' });
  assert.equal(got.length, 2);
});
