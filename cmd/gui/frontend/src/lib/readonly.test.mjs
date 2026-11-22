import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./readonly.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const ro = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const bridgeSource = await readFile(new URL('./bridge.ts', import.meta.url), 'utf8');
const bridgeCalls = new Set([...bridgeSource.slice(0, bridgeSource.indexOf('\n};\n')).matchAll(/^ {2}(\w+): \(/gm)].map((m) => m[1]));
const classification = await readFile(new URL('../../../classification.go', import.meta.url), 'utf8');
const mutating = [...classification.matchAll(/^\t"(\w+)":\s+\{class\w+, "\w+", true\},/gm)].map((m) => m[1][0].toLowerCase() + m[1].slice(1));

test('I block a mutation while the remote is read-only and let it through otherwise', async () => {
  const seen = [];
  let readOnly = true;
  const calls = ro.guardCalls({
    deleteRepo: (s, r) => { seen.push(['deleteRepo', s, r]); return Promise.resolve(); },
    getAllStatus: () => { seen.push(['getAllStatus']); return Promise.resolve([]); },
  }, () => readOnly, () => 'read-only');
  await assert.rejects(calls.deleteRepo('a', 'b'), (e) => e instanceof ro.ReadOnlyError && e.message === 'read-only');
  await calls.getAllStatus();
  assert.deepEqual(seen, [['getAllStatus']]);
  readOnly = false;
  await calls.deleteRepo('a', 'b');
  assert.deepEqual(seen, [['getAllStatus'], ['deleteRepo', 'a', 'b']]);
});

test('I block every bridge call whose Go method changes the host', () => {
  assert.ok(bridgeCalls.size > 100, `only ${bridgeCalls.size} bridge calls found`);
  assert.ok(mutating.length > 30, `only ${mutating.length} mutating methods found`);
  for (const name of mutating) {
    if (bridgeCalls.has(name)) assert.ok(ro.READ_ONLY_BLOCKED.has(name), `${name} changes the host but is not blocked`);
  }
  for (const name of ro.READ_ONLY_BLOCKED) assert.ok(bridgeCalls.has(name), `${name} is blocked but is not a bridge call`);
});

test('I do not reconcile on focus in a read-only remote, but I re-probe its harnesses', () => {
  assert.deepEqual(ro.focusWork(true), { reload: true, reconcile: false, refreshHarnesses: true });
  assert.deepEqual(ro.focusWork(false), { reload: true, reconcile: true, refreshHarnesses: true });
  assert.ok(ro.READ_ONLY_BLOCKED.has('reconcile') && !ro.READ_ONLY_BLOCKED.has('refreshAIHarnesses'));
});

test('I let the remote launches through and keep the ones with no remote equivalent blocked', () => {
  for (const name of ['openInApp', 'openProfile', 'openInAIHarness', 'openAccountInApp', 'openAccountProfile', 'openAccountInAIHarness']) {
    assert.ok(!ro.READ_ONLY_BLOCKED.has(name), `${name} opens the host's folder over ssh`);
  }
  for (const name of ['openInExplorer', 'openAccountFolder', 'openFileInEditor']) {
    assert.ok(ro.READ_ONLY_BLOCKED.has(name), `${name} has no remote equivalent`);
  }
});

const tr = (key) => ({ 'error.remote.not_connected': 'No hay conexión con el host.' })[key] ?? key;
const notConnected = new Error(JSON.stringify({ message: 'not connected to the remote', code: 'remote.not_connected', hint: 'error.remote.not_connected' }));

test('I translate a remote window failure and keep its code', () => {
  const e = ro.decodeWindowError(notConnected, tr);
  assert.ok(e instanceof ro.WindowError);
  assert.equal(e.message, 'No hay conexión con el host.');
  assert.equal(e.code, 'remote.not_connected');
  // No catalog entry: the backend's text, never the key.
  const other = ro.decodeWindowError(new Error(JSON.stringify({ message: 'it broke', code: 'remote.x', hint: 'error.remote.x' })), tr);
  assert.equal(other.message, 'it broke');
  // A hostile code or hint is dropped; a local window's plain text passes.
  const hostile = ro.decodeWindowError(new Error(JSON.stringify({ message: 'm', code: '"><script>', hint: 'a b' })), tr);
  assert.equal(hostile.code, '');
  assert.equal(hostile.message, 'm');
  const plain = new Error('open gitbox.json: no such file');
  assert.equal(ro.decodeWindowError(plain, tr), plain);
  assert.equal(ro.decodeWindowError('{not json', tr), '{not json');
});

test('I pass every failure of the bridge through the decoder', async () => {
  const calls = ro.guardCalls({ reloadConfig: () => Promise.reject(notConnected) }, () => false, () => 'read-only', (e) => ro.decodeWindowError(e, tr));
  await assert.rejects(calls.reloadConfig(), (e) => ro.isDisconnectError(e) && e.message === 'No hay conexión con el host.');
});

test('I raise no config error for a disconnection, and a reconnect clears it', () => {
  const failure = ro.decodeWindowError(notConnected, tr);
  // The bug: the remote was killed, a reload failed with not_connected and
  // raised the config banner, which outlived the reconnect.
  assert.equal(ro.backgroundAllowed(true, 'failed'), false);
  assert.equal(ro.backgroundAllowed(true, 'connecting'), false);
  assert.equal(ro.backgroundAllowed(true, 'connected'), true);
  assert.equal(ro.backgroundAllowed(false, undefined), true);
  assert.equal(ro.reloadErrorShown(failure, true, 'connected'), false, 'a not_connected failure that raced the state');
  assert.equal(ro.reloadErrorShown(new Error('dangling account'), true, 'failed'), false, 'any failure while disconnected');
  assert.equal(ro.reloadErrorShown(new Error('dangling account'), true, 'connected'), true);
  assert.equal(ro.reloadErrorShown(new Error('dangling account'), false, undefined), true);
  assert.equal(ro.clearsOnReconnect('failed', 'connected'), true);
  assert.equal(ro.clearsOnReconnect('connecting', 'connected'), true);
  assert.equal(ro.clearsOnReconnect('connected', 'connected'), false);
  assert.equal(ro.clearsOnReconnect('connected', 'failed'), false);
});
