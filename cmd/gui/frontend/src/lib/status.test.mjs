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
  assert.deepEqual(STATUSES.map(s.simpleKind), ['saved', 'wip', 'update', 'unpushed', 'help', 'help', 'online', 'unpushed', 'help', 'help', 'busy', 'busy', 'busy', 'checking']);
  assert.equal(s.simpleKind('bogus'), 'help');
  assert.equal(s.simpleColor('saved', 'dark'), '#61fd5f');
  assert.equal(s.simpleColor('wip', 'light'), '#c2410c');
  assert.equal(s.simpleColor('unpushed', 'dark'), '#4B95E9');
  assert.equal(s.simpleLabelKey('wip'), 'simple.wip');
  assert.equal(s.simpleLabelKey('unpushed'), 'simple.unpushed');
  assert.equal(s.simpleColor('busy', 'dark'), '#4B95E9');
  assert.equal(s.simpleColor('checking', 'light'), '#52525b');
  assert.equal(s.simpleLabelKey('busy'), 'simple.checking');
  assert.equal(s.simpleLabelKey('online'), 'simple.online');
  assert.deepEqual(STATUSES.filter(s.simpleUnsaved), ['dirty', 'ahead', 'diverged', 'conflict', 'no upstream', 'upstream gone', 'error']);
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

test('I rank every status by how much it needs me, worst first', () => {
  assert.deepEqual(STATUSES.map((x) => s.attentionRank(x)), [4, 1, 2, 1, 0, 0, 3, 1, 0, 0, 2, 3, 4, 4]);
  assert.equal(s.attentionRank('no upstream', true), 1);
  assert.equal(s.attentionRank('no upstream', false), 4);
  assert.equal(s.attentionRank('bogus'), s.CALM_RANK);
  assert.deepEqual([...s.ATTENTION_GROUPS], ['help', 'local', 'behind', 'notCloned']);
  assert.equal(s.CALM_RANK, 4);
});

test('I label every Advanced status in the glossary words', () => {
  const st = { ahead: 2, behind: 3, modified: 4, untracked: 1, isDefault: true };
  const label = (status, extra = {}) => s.statusLabel({ ...st, status, ...extra }, false);
  assert.deepEqual(label('clean'), { key: 'status.upToDate', color: 'clean', badges: false });
  assert.deepEqual(label('behind'), { key: 'status.behind', vars: { n: 3 }, color: 'behind', badges: false });
  assert.deepEqual(label('ahead'), { key: 'status.ahead', vars: { n: 2 }, color: 'ahead', badges: false });
  assert.deepEqual(label('dirty'), { key: 'status.localChanges', vars: { n: 5 }, note: 'status.protected', color: 'dirty', badges: false });
  assert.equal(label('dirty', { modified: 1, untracked: 0 }).key, 'status.localChange');
  assert.equal(label('dirty', { modified: 1, untracked: 0 }).note, 'status.protectedOne');
  assert.deepEqual(label('diverged'), { key: 'status.diverged', color: 'diverged', badges: true });
  assert.deepEqual(label('conflict'), { key: 'status.conflict', color: 'conflict', badges: true });
  assert.deepEqual(label('upstream gone'), { key: 'status.upstreamGone', color: 'upstream gone', badges: false });
  assert.deepEqual(label('error'), { key: 'status.error', color: 'error', badges: false });
  assert.deepEqual(label('not cloned'), { key: 'status.notCloned', color: 'not cloned', badges: false });
  assert.deepEqual(label('no upstream'), { key: 'repo.noUpstream', color: 'no upstream', badges: false });
  assert.deepEqual(label('no upstream', { isDefault: false }), { key: 'repo.localBranch', color: 'clean', badges: false });
  assert.equal(label('unknown').key, 'simple.checking');
});

test('I label every Simple status by its kind', () => {
  const label = (status) => s.statusLabel({ status, modified: 2, untracked: 1 }, true);
  assert.deepEqual(STATUSES.map((x) => label(x).key), ['simple.saved', 'simple.wip', 'simple.update', 'simple.unpushed', 'simple.help', 'simple.help', 'simple.online', 'simple.unpushed', 'simple.help', 'simple.help', 'simple.checking', 'simple.checking', 'simple.checking', 'simple.checking']);
  assert.deepEqual(label('dirty'), { key: 'simple.wip', note: 'simple.wipSafe', noteVars: { n: 3 }, color: 'dirty', badges: false });
  assert.equal(s.statusLabel({ status: 'dirty', modified: 1 }, true).note, 'simple.wipSafeOne');
  assert.equal(label('ahead').color, 'ahead');
  assert.ok(STATUSES.every((x) => !label(x).badges));
});

test('I describe account health with its words and fix action per mode', () => {
  const pick = (h) => [h.level, h.mark, h.color, h.key, h.fix, h.fixKey, h.attention];
  assert.deepEqual(pick(s.accountHealth('ok', false)), ['ok', '●', 'clean', 'health.ok', null, '', false]);
  assert.deepEqual(pick(s.accountHealth('ok', true)), ['ok', '●', 'clean', 'health.okSimple', null, '', false]);
  assert.deepEqual(pick(s.accountHealth('warning', false)), ['warn', '◆', 'dirty', 'health.warning', 'change', 'health.fix.change', false]);
  assert.deepEqual(pick(s.accountHealth('warning', true)), ['warn', '◆', 'dirty', 'health.warningSimple', null, '', false]);
  assert.deepEqual(pick(s.accountHealth('offline', false)), ['bad', '✕', 'error', 'health.offline', 'retry', 'health.fix.retry', true]);
  assert.deepEqual(pick(s.accountHealth('offline', true)), ['bad', '✕', 'error', 'health.offlineSimple', 'retry', 'health.fix.retrySimple', true]);
  assert.deepEqual(pick(s.accountHealth('error', false)), ['bad', '✕', 'error', 'health.error', 'change', 'health.fix.change', true]);
  assert.deepEqual(pick(s.accountHealth('error', true)), ['bad', '✕', 'error', 'health.errorSimple', 'reconnect', 'health.fix.reconnect', true]);
  assert.deepEqual(pick(s.accountHealth('none', false)), ['none', '○', 'ahead', 'health.none', 'setup', 'health.fix.setup', true]);
  assert.deepEqual(pick(s.accountHealth('none', true)), ['none', '○', 'ahead', 'health.noneSimple', 'reconnect', 'health.fix.reconnect', true]);
  for (const x of ['unknown', 'checking', undefined]) assert.deepEqual(pick(s.accountHealth(x, false)), ['pending', '◔', 'not cloned', 'health.checking', null, '', false]);
});
