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
  assert.deepEqual(sum, { checked: 3, nowBehind: 1, skipped: 2, skippedAccounts: ['B'], unreachable: [], failed: 1, firstFailure: { key: 'C/1', error: 'repository maintenance: x' }, localWork: 1, changed: true });
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
  const statuses = { 'A/1': 'behind', 'A/2': 'not cloned', 'B/1': 'behind', 'B/2': 'behind', 'B/3': 'not cloned' };
  let run = a.startPull(Object.keys(statuses), accountOf, (k) => statuses[k]);
  assert.equal(run.kind, 'pull');
  assert.deepEqual([...run.clones], ['A/2', 'B/3']);
  run = a.noteDone(run, 'A/1');
  run = a.noteDone(run, 'A/2');
  run = a.noteDone(run, 'B/1', 'not possible to fast-forward');
  run = a.noteDone(run, 'B/2', undefined, undefined, 'dirty');
  assert.deepEqual(a.summarizePull(run), { pulled: 1, cloned: 1, skipped: 1, failed: 1, failures: [{ key: 'B/1', error: 'not possible to fast-forward' }] });
  assert.deepEqual(a.progress(run), { done: 4, total: 5 });
});

test('I count local work across statuses', () => {
  const statuses = { a: 'dirty', b: 'ahead', c: 'diverged', d: 'conflict', e: 'clean', f: 'behind', g: 'not cloned' };
  assert.equal(a.localWorkCount(Object.keys(statuses), (k) => statuses[k]), 4);
});

test('I group connection failures by host and spot an instant refusal', () => {
  // The backend reports the class, the host and the instant refusal as
  // data; the text is only shown, never parsed.
  const text = 'git fetch: offline (exit 128)';
  const lan = { code: 'git.offline', host: 'git.example.org', fast: true };
  assert.equal(a.cleanGitError('fatal: repository not found'), 'repository not found');
  assert.equal(a.cleanGitError(text), text);

  let run = a.startFetch(['A/1', 'A/2', 'A/3', 'A/4', 'A/5', 'B/1', 'C/1'], accountOf, () => 'clean');
  run = a.noteDone(run, 'A/1', text, lan);
  run = a.noteDone(run, 'A/2', text, { code: 'git.timeout', host: 'git.example.org' });
  run = a.noteDone(run, 'A/3', 'git fetch: repo_not_found (exit 128)', { code: 'git.repo_not_found' });
  run = a.noteDone(run, 'A/4', text, { code: 'git.offline' });
  // A message that only looks like a connection failure is not one.
  run = a.noteDone(run, 'A/5', "Failed to connect to git.example.org port 443 after 5 ms: Couldn't connect to server");
  run = a.noteDone(run, 'B/1', text, lan);
  run = a.noteDone(run, 'C/1');
  const sum = a.summarizeFetch(run, () => 'clean', accountOf, ['B']);
  assert.deepEqual(sum.unreachable, [{ host: 'git.example.org', count: 2, fast: true }, { host: '', count: 1, fast: false }]);
  assert.equal(sum.skipped, 1);
  assert.equal(sum.failed, 2);
  assert.deepEqual(sum.firstFailure, { key: 'A/3', error: 'git fetch: repo_not_found (exit 128)', code: 'git.repo_not_found' });
  assert.equal(sum.checked, 1);

  // A repo that later succeeds forgets its failure.
  run = a.noteDone(run, 'A/1');
  assert.equal(run.failures['A/1'], undefined);
  assert.deepEqual(a.summarizeFetch(run, () => 'clean', accountOf, ['B']).unreachable, [{ host: '', count: 1, fast: false }, { host: 'git.example.org', count: 1, fast: false }]);
});
