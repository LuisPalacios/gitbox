import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('./events.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { completionStatuses, createEventRouter, hasConfiguredRepo } = await import(`data:text/javascript,${encodeURIComponent(outputText)}`);

function transport() {
  const subscribers = new Map();
  const events = createEventRouter((name, handler) => {
    if (!subscribers.has(name)) subscribers.set(name, new Set());
    const listeners = subscribers.get(name);
    listeners.add(handler);
    return () => listeners.delete(handler);
  });
  function deliver(name, sequence, configRevision, payload, extra = {}) {
    const event = { schema: 1, sequence, configRevision, operationID: '1', payload, ...extra };
    for (const listener of subscribers.get(name) || []) listener(event);
  }
  return { events, deliver };
}

test('I buffer startup events and drop older revisions, duplicates and snapshot-covered sequences', () => {
  const { events, deliver } = transport();
  const seen = [];
  const replay = [];
  let revision = 4;
  events.on('fetch:done', (payload, meta) => seen.push([payload.repo, meta.sequence]));
  events.on('fetch:done', (_, meta) => replay.push(meta.sequence));
  deliver('fetch:done', 7, 4, { source: 'acct', repo: 'second' });
  deliver('fetch:done', 6, 4, { source: 'acct', repo: 'first' });
  deliver('fetch:done', 5, 4, { source: 'acct', repo: 'snapshot-covered' });
  deliver('fetch:done', 8, 3, { source: 'acct', repo: 'old-config' });
  deliver('fetch:done', 9, 4, { source: 'acct', repo: 'bad-schema' }, { schema: 9 });
  assert.deepEqual(seen, []);
  events.snapshot(5, () => revision);
  revision = 5;
  deliver('fetch:done', 12, 5, { source: 'acct', repo: 'second' });
  deliver('fetch:done', 11, 5, { source: 'acct', repo: 'first' });
  deliver('fetch:done', 11, 5, { source: 'acct', repo: 'second' });
  deliver('fetch:done', 13, 4, { source: 'acct', repo: 'first' });
  assert.deepEqual(seen, [['first', 6], ['second', 7], ['second', 12], ['first', 11]]);
  assert.deepEqual(replay, [6, 7, 12, 11]);
});

test('I reset the host sequence on reconnect and keep client events independent', () => {
  const { events, deliver } = transport();
  const seen = [];
  events.on('remote:state', (_, meta) => seen.push(meta.sequence));
  events.on('status:updated', (_, meta) => seen.push(meta.sequence));
  events.snapshot(100, () => 4);
  deliver('status:updated', 102, 4, []);
  events.reset();
  deliver('remote:state', 2, 0, {}, { client: true });
  deliver('status:updated', 2, 1, []);
  events.snapshot(1, () => 1);
  assert.deepEqual(seen, [102, 2, 2]);
});

test('I ignore an operation addressed to a renamed or deleted repository', () => {
  const { events, deliver } = transport();
  const seen = [];
  const config = { sources: { renamed: { repos: { 'team/live': {} } } } };
  events.on('clone:done', ({ source, repo }) => {
    if (hasConfiguredRepo(config, source, repo)) seen.push(repo);
  });
  events.snapshot(4, () => 2);
  deliver('clone:done', 5, 2, { source: 'old-name', repo: 'team/live' });
  deliver('clone:done', 6, 2, { source: 'renamed', repo: 'team/deleted' });
  deliver('clone:done', 7, 2, { source: 'renamed', repo: 'team/live' });
  assert.deepEqual(seen, ['team/live']);
});

test('I correlate post-operation status and ignore a superseded probe', () => {
  const completion = completionStatuses();
  const dirty = { state: 'dirty', untracked: 1 };
  const clean = { state: 'clean', untracked: 0 };
  assert.ok(completion.start('first', '10'));
  assert.deepEqual(completion.done('first', '10', true), { accept: true, row: undefined });
  assert.equal(completion.status('first', '10', dirty), dirty);
  assert.equal(completion.start('first', '10'), false);

  assert.equal(completion.status('second', '11', dirty), undefined);
  assert.deepEqual(completion.done('second', '11', true), { accept: true, row: dirty });
  assert.ok(completion.start('second', '12'));
  assert.equal(completion.status('second', '11', dirty), undefined);
  assert.deepEqual(completion.done('second', '12', false), { accept: true, row: undefined });
  assert.equal(completion.status('second', '12', clean), undefined);
});

test('I keep delayed bulk terminal events independent and preserve stale correlation', () => {
  const { events, deliver } = transport();
  const completed = [];
  const stale = [];
  events.on('pull:alldone', (row, meta) => completed.push([row.run, meta.operationID]),
    (row, meta) => stale.push([row.run, meta.operationID]));
  events.snapshot(4, () => 3);
  deliver('pull:alldone', 9, 3, { run: 'new' }, { operationID: '14' });
  deliver('pull:alldone', 8, 3, { run: 'old' }, { operationID: '12' });
  deliver('pull:alldone', 8, 3, { run: 'old' }, { operationID: '12' });
  deliver('pull:alldone', 10, 2, { run: 'config-changed' }, { operationID: '13' });
  assert.deepEqual(completed, [['new', '14'], ['old', '12']]);
  assert.deepEqual(stale, [['config-changed', '13']]);
});

test('I use typed handlers in the dashboard and its event consumers', async () => {
  for (const file of ['../App.svelte', './Onboarding.svelte', './FirstProject.svelte', './components/RemoteConnected.svelte', './components/HostSwitcher.svelte', './components/RemoteHostsWindow.svelte']) {
    const component = await readFile(new URL(file, import.meta.url), 'utf8');
    assert.doesNotMatch(component, /events\.on\([^\n]+:\s*any\b/, file);
    assert.doesNotMatch(component, /\bEventsOn\(/, file);
  }
});
