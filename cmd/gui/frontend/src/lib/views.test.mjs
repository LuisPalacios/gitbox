import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./views.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const v = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const CALM = 4;

test('I resolve the saved view, falling back to Needs attention', () => {
  const accounts = ['account-A', 'account-B'];
  assert.equal(v.resolveView('all', accounts, false), 'all');
  assert.equal(v.resolveView('attention', accounts, true), 'attention');
  assert.equal(v.resolveView('account:account-B', accounts, true), 'account:account-B');
  assert.equal(v.resolveView('account:gone', accounts, false), 'attention');
  assert.equal(v.resolveView('account:', accounts, false), 'attention');
  assert.equal(v.resolveView('mirrors', accounts, false), 'mirrors');
  assert.equal(v.resolveView('workspaces', accounts, false), 'workspaces');
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
  assert.deepEqual(items.map((i) => [i.key, i.rank, i.settled]), [['c', 0, false], ['d', 1, false], ['b', 2, false], ['e', 2, false], ['f', 3, false]]);
});

test('I keep rows in place while the view is open, faded once they settle', () => {
  const ranks = { a: 2, b: 1 };
  const keys = ['a', 'b'];
  const first = v.attentionItems(keys, (k) => ranks[k], CALM);
  const pinned = v.pinAttention(first, {});
  assert.deepEqual(pinned, { a: 2, b: 1 });
  ranks.a = 4; // pulled: now up to date
  ranks.b = 0; // got worse, but stays where it was until the view closes
  const later = v.attentionItems(keys, (k) => ranks[k], CALM, pinned);
  assert.deepEqual(later.map((i) => [i.key, i.rank, i.settled]), [['b', 1, false], ['a', 2, true]]);
  assert.deepEqual(v.attentionItems(keys, (k) => ranks[k], CALM).map((i) => i.key), ['b']);
  assert.deepEqual(v.pinAttention(later, pinned), pinned);
});

test('I count attention per account and in total, with broken accounts on top', () => {
  const ranks = { 'a/1': 0, 'a/2': 4, 'b/1': 2, 'b/2': 3 };
  const counts = v.attentionCounts({ A: ['a/1', 'a/2'], B: ['b/1', 'b/2', 'a/1'], C: [] }, (k) => ranks[k], CALM, ['C', 'C']);
  assert.deepEqual(counts, { total: 4, byAccount: { A: 1, B: 2, C: 0 } });
  assert.deepEqual(v.attentionCounts({}, () => 0, CALM), { total: 0, byAccount: {} });
});
