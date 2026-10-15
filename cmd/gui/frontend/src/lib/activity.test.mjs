import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./activity.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const a = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const accountOf = (key) => key.split('/')[0];

test('I scope a fetch run without duplicates and count accounts', () => {
  const statuses = { 'A/1': 'clean', 'A/2': 'dirty', 'B/1': 'clean' };
  const run = a.startFetch(['A/1', 'A/2', 'B/1', 'A/1'], accountOf, (k) => statuses[k]);
  assert.equal(run.kind, 'fetch');
  assert.deepEqual(run.keys, ['A/1', 'A/2', 'B/1']);
  assert.equal(run.accounts, 2);
  assert.deepEqual(run.before, statuses);
  assert.deepEqual(a.progress(run), { done: 0, total: 3 });
});

test('I count each repo once, ignore strangers and keep the run immutable', () => {
  const run = a.startFetch(['A/1', 'A/2'], accountOf, () => 'clean');
  let next = a.noteDone(run, 'A/1');
  next = a.noteDone(next, 'A/1');
  next = a.noteDone(next, 'Z/9');
  assert.deepEqual(a.progress(next), { done: 1, total: 2 });
  assert.equal(run.done.size, 0);
  next = a.noteDone(next, 'A/2', 'boom');
  assert.deepEqual(next.errors, { 'A/2': 'boom' });
  next = a.noteDone(next, 'A/2');
  assert.deepEqual(next.errors, {});
});

test('I summarize a fetch: now behind, offline skips, failures and local work', () => {
  const statuses = { 'A/1': 'clean', 'A/2': 'behind', 'A/3': 'dirty', 'B/1': 'clean', 'B/2': 'clean', 'C/1': 'clean' };
  let run = a.startFetch(Object.keys(statuses), accountOf, (k) => statuses[k]);
  for (const key of ['A/1', 'A/2', 'A/3']) run = a.noteDone(run, key);
  run = a.noteDone(run, 'B/1', 'dial tcp: no route to host');
  run = a.noteDone(run, 'B/2', 'dial tcp: no route to host');
  run = a.noteDone(run, 'C/1', 'repository maintenance: x');
  statuses['A/1'] = 'behind';
  statuses['A/3'] = 'ahead';
  const sum = a.summarizeFetch(run, (k) => statuses[k], accountOf, ['B']);
  assert.deepEqual(sum, { checked: 3, nowBehind: 1, skipped: 2, skippedAccounts: ['B'], failed: 1, localWork: 1, changed: true });
});

test('I report nothing changed when a fetch finds nothing new', () => {
  const statuses = { 'A/1': 'clean', 'A/2': 'behind' };
  let run = a.startFetch(Object.keys(statuses), accountOf, (k) => statuses[k]);
  run = a.noteDone(a.noteDone(run, 'A/1'), 'A/2');
  const sum = a.summarizeFetch(run, (k) => statuses[k], accountOf, []);
  assert.equal(sum.changed, false);
  assert.equal(sum.nowBehind, 0);
  assert.equal(sum.checked, 2);
});

test('I split a pull run into pulls and clones and summarize it', () => {
  const statuses = { 'A/1': 'behind', 'A/2': 'not cloned', 'B/1': 'behind', 'B/2': 'not cloned' };
  let run = a.startPull(Object.keys(statuses), accountOf, (k) => statuses[k]);
  assert.equal(run.kind, 'pull');
  assert.deepEqual([...run.clones], ['A/2', 'B/2']);
  run = a.noteDone(run, 'A/1');
  run = a.noteDone(run, 'A/2');
  run = a.noteDone(run, 'B/1', 'not possible to fast-forward');
  assert.deepEqual(a.summarizePull(run), { pulled: 1, cloned: 1, failed: 1, failures: [{ key: 'B/1', error: 'not possible to fast-forward' }] });
  assert.deepEqual(a.progress(run), { done: 3, total: 4 });
});

test('I count local work across statuses', () => {
  const statuses = { a: 'dirty', b: 'ahead', c: 'diverged', d: 'conflict', e: 'clean', f: 'behind', g: 'not cloned' };
  assert.equal(a.localWorkCount(Object.keys(statuses), (k) => statuses[k]), 4);
});
