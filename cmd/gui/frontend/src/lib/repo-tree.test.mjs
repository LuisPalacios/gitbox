import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./repo-tree.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { repoTreeRows, compareRepoKeys, groupTreeRows } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const source = (names, account = 'account-A') => ({ account, repos: Object.fromEntries(names.map(name => [name, {}])), repoOrder: names });
test('I group cross-source children with retained action ownership and no duplicate rows', () => {
  const tree = repoTreeRows({ a: source(['parent', 'other']), b: source(['child'], 'account-B') }, { 'b/child': 'a/parent' }, new Set());
  assert.deepEqual(tree.a.map(row => [row.key, row.depth]), [['a/other', 0], ['a/parent', 0], ['b/child', 1]]);
  assert.equal(tree.a[2].sourceKey, 'b');
  assert.equal(tree.a[2].repoName, 'child');
  assert.deepEqual(tree.b, []);
  assert.deepEqual(tree.a[1].descendants, ['b/child']);
});
test('I collapse descendants across sources without resurrecting them as roots', () => {
  const sources = { a: source(['parent']), b: source(['child', 'grandchild', 'other']) };
  const nesting = { 'b/child': 'a/parent', 'b/grandchild': 'b/child' };
  const tree = repoTreeRows(sources, nesting, new Set(['a/parent']));
  assert.deepEqual(tree.a.map(row => row.key), ['a/parent']);
  assert.deepEqual(tree.a[0].descendants, ['b/child', 'b/grandchild']);
  assert.deepEqual(tree.b.map(row => row.key), ['b/other']);
  assert.deepEqual(repoTreeRows(sources, nesting, new Set(['b/child'])).a.map(row => row.key), ['a/parent', 'b/child']);
});
test('I retain missing-parent clones and cycles exactly once despite stale order entries', () => {
  const sources = { a: { ...source(['one', 'two', 'three', 'four']), repoOrder: ['one', 'one', 'missing'] } };
  const tree = repoTreeRows(sources, { 'a/one': 'a/two', 'a/two': 'a/one', 'a/three': 'missing/parent', 'a/four': 'a/four' }, new Set());
  assert.equal(tree.a.length, 4);
  assert.deepEqual(new Set(tree.a.map(row => row.key)), new Set(['a/one', 'a/two', 'a/three', 'a/four']));
});
test('I preserve nested depth and sort siblings alphabetically at every level', () => {
  const tree = repoTreeRows({ a: source(['zeta', 'parent', 'leaf', 'child', 'sibling']) }, { 'a/leaf': 'a/child', 'a/child': 'a/parent', 'a/sibling': 'a/parent' }, new Set());
  assert.deepEqual(tree.a.map(row => [row.repoName, row.depth]), [['parent', 0], ['child', 1], ['leaf', 2], ['sibling', 1], ['zeta', 0]]);
});
test('I ignore the configured order and sort by the shown name, ignoring case and owner', () => {
  const tree = repoTreeRows({ a: source(['org-z/Mvtv', 'org-a/xurxodev', 'org-b/1password', 'org-a/repo10', 'org-a/repo9', 'org-c/captura']) }, {}, new Set());
  assert.deepEqual(tree.a.map(row => row.repoName), ['org-b/1password', 'org-c/captura', 'org-z/Mvtv', 'org-a/repo9', 'org-a/repo10', 'org-a/xurxodev']);
});
test('I order clone keys by shown name, then by the full key', () => {
  const keys = ['b/org/tools', 'a/org/Alpha', 'a/other/tools', 'c/org/beta'];
  assert.deepEqual([...keys].sort(compareRepoKeys), ['a/org/Alpha', 'c/org/beta', 'a/other/tools', 'b/org/tools']);
});
test('I split an attention group by account, nesting only the clones in the group', () => {
  const sources = { b: source(['parent', 'child', 'other', 'calm']), a: source(['solo'], 'account-B'), c: source(['away']) };
  const nesting = { 'b/child': 'b/parent', 'c/away': 'b/calm' };
  const groups = groupTreeRows(['b/child', 'b/parent', 'b/other', 'a/solo', 'c/away', 'gone/repo'], sources, nesting, new Set());
  assert.deepEqual(groups.map(g => [g.sourceKey, g.count]), [['a', 1], ['b', 3], ['c', 1]]);
  assert.deepEqual(groups[1].rows.map(row => [row.key, row.depth]), [['b/other', 0], ['b/parent', 0], ['b/child', 1]]);
  assert.deepEqual(groups[2].rows.map(row => [row.key, row.depth]), [['c/away', 0]]);
  const folded = groupTreeRows(['b/child', 'b/parent'], sources, nesting, new Set(['b/parent']));
  assert.deepEqual(folded.map(g => [g.rows.map(row => row.key), g.count]), [[['b/parent'], 2]]);
});
