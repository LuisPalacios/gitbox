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
const lower = (name) => name[0].toLowerCase() + name.slice(1);
const mutating = [...classification.matchAll(/^\t"(\w+)":\s+\{class\w+, "\w+", true\},/gm)].map((m) => lower(m[1]));
const server = await readFile(new URL('../../../server.go', import.meta.url), 'utf8');
const servedBlock = server.slice(server.indexOf('var servedMethods = []string{'));
const served = new Set([...servedBlock.slice(0, servedBlock.indexOf('\n}\n')).matchAll(/^\t"(\w+)",$/gm)].map((m) => lower(m[1])));
const UNAVAILABLE = ['openInExplorer', 'openAccountFolder', 'openFileInEditor', 'openRepoConfigInEditor'];
const REMOTE_OFF = ['setAutostart', 'setViewMode'];

test('I block a blocked call in a remote window and let it through otherwise', async () => {
  const seen = [];
  let remote = true;
  const calls = ro.guardCalls({
    setViewMode: (a) => { seen.push(['setViewMode', a]); return Promise.resolve(); },
    getAllStatus: () => { seen.push(['getAllStatus']); return Promise.resolve([]); },
  }, () => remote, () => 'read-only');
  await assert.rejects(calls.setViewMode('compact'), (e) => e instanceof ro.ReadOnlyError && e.message === 'read-only');
  await calls.getAllStatus();
  assert.deepEqual(seen, [['getAllStatus']]);
  remote = false;
  await calls.setViewMode('compact');
  assert.deepEqual(seen, [['getAllStatus'], ['setViewMode', 'compact']]);
});

test('I block exactly the mutations the server does not serve', () => {
  assert.ok(bridgeCalls.size > 100, `only ${bridgeCalls.size} bridge calls found`);
  assert.ok(mutating.length > 30, `only ${mutating.length} mutating methods found`);
  assert.ok(served.size > 70 && served.has('addRoot') && served.has('getConfig'), `servedMethods not parsed: ${served.size}`);
  let servedMutations = 0;
  for (const name of mutating) {
    if (!bridgeCalls.has(name)) continue;
    if (served.has(name)) {
      servedMutations++;
      assert.ok(!ro.READ_ONLY_BLOCKED.has(name), `${name} is served but still blocked`);
    } else {
      assert.ok(ro.READ_ONLY_BLOCKED.has(name), `${name} changes the host and is not served, but is not blocked`);
    }
  }
  assert.ok(servedMutations >= 43, `only ${servedMutations} served mutations found`);
  const expected = new Set([...mutating.filter((name) => bridgeCalls.has(name) && !served.has(name)), ...UNAVAILABLE, ...REMOTE_OFF]);
  assert.deepEqual([...ro.READ_ONLY_BLOCKED].sort(), [...expected].sort());
  for (const name of ro.READ_ONLY_BLOCKED) assert.ok(bridgeCalls.has(name), `${name} is blocked but is not a bridge call`);
});

test('I block a call only in a remote window', () => {
  for (const name of ['setViewMode', 'openInExplorer']) {
    assert.equal(ro.isBlocked(name, true), true, name);
    assert.equal(ro.isBlocked(name, false), false, name);
  }
  for (const name of ['addRoot', 'setNestedScanDepth', 'updateAccount', 'getConfig', 'setTheme', 'cloneRepo', 'pullAll', 'pullRepo', 'reconcile', 'moveRepo', 'setupMirrorRepo', 'credentialStoreToken', 'changeCredentialType', 'credentialDelete', 'credentialSetupGCM', 'credentialSetupSSH', 'credentialRegenerateSSH']) {
    assert.equal(ro.isBlocked(name, true), false, name);
  }
  assert.equal(ro.anyBlocked(['addAccount', 'openInExplorer'], true), true);
  assert.equal(ro.anyBlocked(['addAccount', 'openInExplorer'], false), false);
  assert.equal(ro.anyBlocked(['changeCredentialType', 'credentialSetupSSH'], true), false);
  assert.equal(ro.anyBlocked(['changeCredentialType', 'credentialSetupGCM'], true), false);
  assert.equal(ro.anyBlocked(['updateAccount', 'renameAccount'], true), false);
  assert.equal(ro.anyBlocked('pullRepo', true), false);
  assert.equal(ro.anyBlocked([], true), false);
});

test('I let the remote launches through and keep the ones with no remote equivalent blocked', () => {
  for (const name of ['openInApp', 'openProfile', 'openInAIHarness', 'openAccountInApp', 'openAccountProfile', 'openAccountInAIHarness']) {
    assert.ok(!ro.READ_ONLY_BLOCKED.has(name), `${name} opens the host's folder over ssh`);
  }
  for (const name of UNAVAILABLE) assert.ok(ro.READ_ONLY_BLOCKED.has(name), `${name} has no remote equivalent`);
  for (const name of REMOTE_OFF) assert.ok(ro.READ_ONLY_BLOCKED.has(name), `${name} stays off in a remote window`);
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
