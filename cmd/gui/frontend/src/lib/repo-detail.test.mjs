import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./repo-detail.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { createDetailLoader } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const detail = (subject) => ({
  branch: 'main', ahead: 0, behind: 1, changed: [], untracked: [],
  incoming: [{ hash: subject, author: 'Author', subject }],
});

function pendingLoader() {
  const requests = [];
  const loader = createDetailLoader((source, repo) => new Promise((resolve, reject) => {
    requests.push({ source, repo, resolve, reject });
  }));
  return { loader, requests };
}

test('I normalize nil slices while keeping incoming commits and local changes', async () => {
  const raw = { ...detail('New version'), changed: [{ kind: 'modified', path: 'file.txt' }], untracked: null };
  const loader = createDetailLoader(async () => raw);
  assert.deepEqual(await loader.load('account', 'team/repo'), { ...raw, untracked: [] });
  const empty = createDetailLoader(async () => ({ ...detail(''), changed: null, untracked: null, incoming: null }));
  assert.deepEqual(await empty.load('account', 'repo'), { ...detail(''), changed: [], untracked: [], incoming: [] });
});

test('I discard an earlier clone response after switching repositories', async () => {
  const { loader, requests } = pendingLoader();
  const first = loader.load('account-a', 'team/first');
  const second = loader.load('account-b', 'team/second');
  requests[1].resolve(detail('Second clone'));
  assert.deepEqual(await second, detail('Second clone'));
  requests[0].resolve(detail('First clone'));
  assert.equal(await first, null);
  assert.equal(requests[1].source, 'account-b');
  assert.equal(requests[1].repo, 'team/second');
});

test('I cancel an in-flight read when the panel closes and safely reopen the same clone', async () => {
  const { loader, requests } = pendingLoader();
  const first = loader.load('account', 'repo');
  loader.cancel();
  const reopened = loader.load('account', 'repo');
  requests[0].resolve(detail('Before close'));
  assert.equal(await first, null);
  requests[1].resolve(detail('After reopen'));
  assert.deepEqual(await reopened, detail('After reopen'));
});

test('I keep the latest refresh when earlier reads finish after an update', async () => {
  const { loader, requests } = pendingLoader();
  const first = loader.load('account', 'repo');
  const refreshed = loader.load('account', 'repo');
  const updated = { ...detail(''), behind: 0, incoming: [] };
  requests[1].resolve(updated);
  assert.deepEqual(await refreshed, updated);
  requests[0].resolve(detail('Already pulled'));
  assert.equal(await first, null);
});

test('I display current errors and ignore failures from superseded reads', async () => {
  const { loader, requests } = pendingLoader();
  const first = loader.load('account', 'first');
  const second = loader.load('account', 'second');
  requests[0].reject(new Error('Old failure'));
  assert.equal(await first, null);
  requests[1].reject(new Error('Cannot read clone'));
  const result = await second;
  assert.equal(result.error, 'Error: Cannot read clone');
  assert.deepEqual(result.incoming, []);
});

test('I preserve every incoming entry and keep history errors separate from local files', async () => {
  const raw = { ...detail(''), incoming: Array.from({ length: 150 }, (_, n) => ({ hash: String(n), author: 'Zoë García', subject: `Change ${n}` })) };
  const loader = createDetailLoader(async () => raw);
  assert.deepEqual((await loader.load('account', 'repo')).incoming, raw.incoming);
  const failedHistory = { ...detail(''), incoming: null, incomingError: 'Unavailable upstream', changed: [{ kind: 'modified', path: 'file.txt' }] };
  const partial = createDetailLoader(async () => failedHistory);
  const result = await partial.load('account', 'repo');
  assert.deepEqual(result.changed, failedHistory.changed);
  assert.equal(result.incomingError, failedHistory.incomingError);
  assert.equal(result.error, undefined);
});
