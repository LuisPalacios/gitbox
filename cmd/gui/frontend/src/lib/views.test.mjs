import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

async function loadModule(name) {
  const source = await readFile(new URL(name, import.meta.url), 'utf8');
  const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

const v = await loadModule('./views.ts');
const { derivePresentation, CALM_RANK: CALM } = await loadModule('./presentation.ts');

test('I resolve the saved view, falling back to Needs attention', () => {
  const accounts = ['account-A', 'account-B'];
  assert.equal(v.resolveView('all', accounts, false), 'all');
  assert.equal(v.resolveView('attention', accounts, true), 'attention');
  assert.equal(v.resolveView('account:account-B', accounts, true), 'account:account-B');
  assert.equal(v.resolveView('account:gone', accounts, false), 'attention');
  assert.equal(v.resolveView('account:', accounts, false), 'attention');
  assert.equal(v.resolveView('mirrors', accounts, false), 'mirrors');
  assert.equal(v.resolveView('workspaces', accounts, false), 'attention');
  assert.equal(v.resolveView('mirrors', accounts, true), 'attention');
  assert.equal(v.resolveView('workspaces', accounts, true), 'attention');
  for (const bad of ['', undefined, null, 'bogus', 'account-A']) assert.equal(v.resolveView(bad, accounts, false), 'attention');
});

test('I build and read account views', () => {
  assert.equal(v.accountView('account-A'), 'account:account-A');
  assert.equal(v.viewAccount('account:account-A'), 'account-A');
  assert.equal(v.viewAccount('all'), '');
});

test('I list attention rows worst group first, keeping order inside a group', () => {
  const ranks = { a: 4, b: 2, c: 0, d: 1, e: 2, f: 3 };
  const items = v.attentionItems(['a', 'b', 'c', 'd', 'e', 'f'], (k) => ranks[k], CALM);
  assert.deepEqual(items.map((i) => [i.key, i.rank]), [['c', 0], ['d', 1], ['b', 2], ['e', 2], ['f', 3]]);
});

test('I drop a row as soon as it turns fine and move one whose state changes', () => {
  const ranks = { a: 3, b: 1 };
  const keys = ['a', 'b'];
  assert.deepEqual(v.attentionItems(keys, (k) => ranks[k], CALM).map((i) => [i.key, i.rank]), [['b', 1], ['a', 3]]);
  ranks.a = 4; // cloned: now up to date
  ranks.b = 0; // got worse: moves to the worse group at once
  assert.deepEqual(v.attentionItems(keys, (k) => ranks[k], CALM).map((i) => [i.key, i.rank]), [['b', 0]]);
});

test('I count attention per account and in total, with broken accounts on top', () => {
  const ranks = { 'a/1': 0, 'a/2': 4, 'b/1': 2, 'b/2': 3 };
  const counts = v.attentionCounts({ A: ['a/1', 'a/2'], B: ['b/1', 'b/2', 'a/1'], C: [] }, (k) => ranks[k], CALM, ['C', 'C']);
  assert.deepEqual(counts, { total: 4, byAccount: { A: 1, B: 2, C: 0 } });
  assert.deepEqual(v.attentionCounts({}, () => 0, CALM), { total: 0, byAccount: {} });
});

test('I count only the default branch without an upstream in Needs attention', () => {
  const keys = ['main', 'feature', 'unidentified'];
  const states = { main: true, feature: false, unidentified: undefined };
  for (const mode of ['simple', 'advanced']) {
    const rankOf = key => derivePresentation({ status: 'no upstream', isDefault: states[key] }, mode).attention;
    assert.deepEqual(v.attentionItems(keys, rankOf, CALM), [{ key: 'main', rank: 1 }]);
    assert.deepEqual(v.attentionCounts({ account: keys }, rankOf, CALM), { total: 1, byAccount: { account: 1 } });
  }
});
