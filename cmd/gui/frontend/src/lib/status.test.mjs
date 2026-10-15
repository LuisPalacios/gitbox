import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./status.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const s = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const STATUSES = ['clean', 'dirty', 'behind', 'ahead', 'diverged', 'conflict', 'not cloned', 'no upstream', 'upstream gone', 'error', 'syncing', 'cloning', 'fetching', 'unknown'];

test('I map every status to its shape', () => {
  assert.deepEqual(STATUSES.map(s.statusSymbol), ['●', '◆', '◗', '▲', '⚠', '⚡', '○', '~', '⊗', '✕', '◔', '◔', '◔', '●']);
  assert.equal(s.statusSymbol('bogus'), '?');
});

test('I colour every status per theme, with a muted fallback', () => {
  assert.deepEqual(STATUSES.map((x) => s.statusColor(x, 'dark')), ['#61fd5f', '#F07623', '#D91C9A', '#4B95E9', '#D81E5B', '#D81E5B', '#71717a', '#71717a', '#D81E5B', '#D81E5B', '#4B95E9', '#4B95E9', '#4B95E9', '#71717a']);
  assert.deepEqual(STATUSES.map((x) => s.statusColor(x, 'light')), ['#166534', '#c2410c', '#a21caf', '#2563eb', '#be123c', '#be123c', '#52525b', '#52525b', '#be123c', '#be123c', '#2563eb', '#2563eb', '#2563eb', '#52525b']);
});

test('I show a spinning glyph while a row is unscanned or fetching', () => {
  assert.equal(s.rowGlyph('unknown', false), '↻');
  assert.equal(s.rowGlyph('dirty', true), '↻');
  assert.equal(s.rowGlyph('dirty', false), '◆');
  assert.equal(s.rowBusy('clean', false), false);
  assert.equal(s.rowBusy('unknown', false), true);
});

test('I fold every status into one Simple kind with its colour and label', () => {
  assert.deepEqual(STATUSES.map(s.simpleKind), ['saved', 'unsaved', 'update', 'unsaved', 'help', 'help', 'online', 'unsaved', 'help', 'help', 'busy', 'busy', 'busy', 'checking']);
  assert.equal(s.simpleKind('bogus'), 'help');
  assert.equal(s.simpleColor('saved', 'dark'), '#61fd5f');
  assert.equal(s.simpleColor('unsaved', 'light'), '#c2410c');
  assert.equal(s.simpleColor('busy', 'dark'), '#4B95E9');
  assert.equal(s.simpleColor('checking', 'light'), '#52525b');
  assert.equal(s.simpleLabelKey('busy'), 'simple.checking');
  assert.equal(s.simpleLabelKey('online'), 'simple.online');
  assert.deepEqual(STATUSES.filter(s.simpleUnsaved), ['dirty', 'ahead', 'diverged', 'conflict', 'no upstream', 'upstream gone', 'error']);
});

test('I label settled Advanced statuses and leave the rest to count badges', () => {
  assert.deepEqual(s.advancedLabel('clean'), { key: 'repo.synced', color: 'clean' });
  assert.deepEqual(s.advancedLabel('not cloned'), { key: 'repo.notLocal', color: 'not cloned' });
  assert.deepEqual(s.advancedLabel('no upstream', true), { key: 'repo.noUpstream', color: 'no upstream' });
  assert.deepEqual(s.advancedLabel('no upstream', false), { key: 'repo.localBranch', color: 'clean' });
  assert.deepEqual(s.advancedLabel('error'), { key: 'repo.error', color: 'error' });
  assert.deepEqual(s.advancedLabel('upstream gone'), { key: 'repo.upstreamGone', color: 'upstream gone' });
  for (const x of ['dirty', 'ahead', 'behind', 'diverged', 'conflict']) assert.equal(s.advancedLabel(x), null);
});

test('I describe count badges in a fixed order and pick the compact one', () => {
  assert.deepEqual(s.COUNT_BADGES.map((b) => `${b.glyph}${b.field}:${b.color}:${b.word}`), ['↓behind:behind:behind', '↑ahead:ahead:ahead', '✎modified:dirty:changed', '?untracked:not cloned:untracked']);
  assert.equal(s.statusCountBadge('behind').field, 'behind');
  assert.equal(s.statusCountBadge('dirty').field, 'modified');
  assert.equal(s.statusCountBadge('ahead').field, 'ahead');
  assert.equal(s.statusCountBadge('diverged'), null);
});

test('I open the detail panel only for statuses that need attention', () => {
  assert.deepEqual(STATUSES.filter(s.opensDetail), ['dirty', 'ahead', 'diverged', 'conflict', 'no upstream', 'upstream gone', 'error', 'fetching']);
  assert.deepEqual(STATUSES.filter(s.detailLocked), ['clean', 'behind', 'not cloned', 'syncing', 'cloning']);
  assert.equal(s.detailLocked('unknown'), false);
});

test('I apply the credential rules for cards and kebabs', () => {
  const creds = ['ok', 'warning', 'offline', 'error', 'none', 'unknown', 'checking', undefined];
  assert.deepEqual(creds.map(s.credBroken), [false, false, false, true, true, false, false, false]);
  assert.deepEqual(creds.map(s.canDiscoverWith), [true, true, false, false, false, false, true, false]);
  assert.deepEqual(creds.map(s.canCreateWith), [true, false, false, false, false, false, false, false]);
  assert.deepEqual(['ok', 'error', 'warning', 'offline', 'none', 'unknown', 'checking'].map(s.credBadgeClass), ['ok', 'err', 'warn', 'offline', 'none', 'pending', '']);
  assert.deepEqual(['unknown', 'none', 'offline', 'ok', 'error'].map((x) => s.credBadgeLabel(x, 'ssh')), ['···', 'config', 'offline', 'ssh', 'ssh']);
  assert.equal(s.credBadgeLabel('ok', ''), 'gcm');
});
