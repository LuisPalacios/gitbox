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

test('I neither reconcile nor re-probe harnesses on focus in a read-only remote', () => {
  assert.deepEqual(ro.focusWork(true), { reload: true, reconcile: false, refreshHarnesses: false });
  assert.deepEqual(ro.focusWork(false), { reload: true, reconcile: true, refreshHarnesses: true });
  assert.ok(ro.READ_ONLY_BLOCKED.has('reconcile') && ro.READ_ONLY_BLOCKED.has('refreshAIHarnesses'));
});
