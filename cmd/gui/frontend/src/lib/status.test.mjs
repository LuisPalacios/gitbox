import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./status.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const s = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const STATUSES = ['clean', 'dirty', 'behind', 'ahead', 'diverged', 'conflict', 'not cloned', 'no upstream', 'upstream gone', 'root offline', 'error', 'syncing', 'cloning', 'fetching', 'unknown'];

test('I map every status to its shape', () => {
  assert.deepEqual(STATUSES.map(s.statusSymbol), ['●', '◆', '◗', '▲', '⚠', '⚡', '○', '~', '⊗', '◌', '✕', '◔', '◔', '◔', '●']);
  assert.equal(s.statusSymbol('bogus'), '?');
});

test('I colour every status per theme, with a muted fallback', () => {
  assert.deepEqual(STATUSES.map((x) => s.statusColor(x, 'dark')), ['#61fd5f', '#F07623', '#D91C9A', '#4B95E9', '#D81E5B', '#D81E5B', '#71717a', '#71717a', '#D81E5B', '#71717a', '#D81E5B', '#4B95E9', '#4B95E9', '#4B95E9', '#71717a']);
  assert.deepEqual(STATUSES.map((x) => s.statusColor(x, 'light')), ['#166534', '#c2410c', '#a21caf', '#2563eb', '#be123c', '#be123c', '#52525b', '#52525b', '#be123c', '#52525b', '#be123c', '#2563eb', '#2563eb', '#2563eb', '#52525b']);
});

test('I show a spinning glyph while a row is unscanned or fetching', () => {
  assert.equal(s.rowGlyph('unknown', false), '↻');
  assert.equal(s.rowGlyph('dirty', true), '↻');
  assert.equal(s.rowGlyph('dirty', false), '◆');
  assert.equal(s.rowBusy('clean', false), false);
  assert.equal(s.rowBusy('unknown', false), true);
});

test('I describe count badges in a fixed order and pick the compact one', () => {
  assert.deepEqual(s.COUNT_BADGES.map((b) => `${b.glyph}${b.field}:${b.color}:${b.word}`), ['↓behind:behind:count.behind', '↑ahead:ahead:count.ahead', '✎modified:dirty:count.changed', '?untracked:not cloned:count.untracked']);
  assert.equal(s.statusCountBadge('behind').field, 'behind');
  assert.equal(s.statusCountBadge('dirty').field, 'modified');
  assert.equal(s.statusCountBadge('ahead').field, 'ahead');
  assert.equal(s.statusCountBadge('diverged'), null);
});

test('I open the detail panel only for statuses that need attention', () => {
  assert.deepEqual(STATUSES.filter(s.opensDetail), ['dirty', 'behind', 'ahead', 'diverged', 'conflict', 'no upstream', 'upstream gone', 'error', 'fetching']);
  assert.deepEqual(STATUSES.filter(s.detailLocked), ['clean', 'not cloned', 'root offline', 'syncing', 'cloning']);
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

test('I describe account health with its words and fix action per mode', () => {
  const pick = (h) => [h.level, h.mark, h.color, h.key, h.fix, h.fixKey, h.attention];
  assert.deepEqual(pick(s.accountHealth('ok', false)), ['ok', '●', 'clean', 'health.ok', null, '', false]);
  assert.deepEqual(pick(s.accountHealth('ok', true)), ['ok', '●', 'clean', 'health.okSimple', null, '', false]);
  assert.deepEqual(pick(s.accountHealth('warning', false)), ['warn', '◆', 'dirty', 'health.warning', 'change', 'health.fix.change', false]);
  assert.deepEqual(pick(s.accountHealth('warning', true)), ['warn', '◆', 'dirty', 'health.warningSimple', null, '', false]);
  assert.deepEqual(pick(s.accountHealth('offline', false)), ['bad', '✕', 'error', 'health.offline', 'retry', 'health.fix.retry', true]);
  assert.deepEqual(pick(s.accountHealth('offline', true)), ['bad', '✕', 'error', 'simple.connectionCheck', 'retry', 'health.fix.retrySimple', true]);
  assert.deepEqual(pick(s.accountHealth('error', false)), ['bad', '✕', 'error', 'health.error', 'change', 'health.fix.change', true]);
  assert.deepEqual(pick(s.accountHealth('error', true)), ['bad', '✕', 'error', 'health.errorSimple', 'reconnect', 'health.fix.reconnect', true]);
  assert.deepEqual(pick(s.accountHealth('none', false)), ['none', '○', 'ahead', 'health.none', 'setup', 'health.fix.setup', true]);
  assert.deepEqual(pick(s.accountHealth('none', true)), ['none', '○', 'ahead', 'health.noneSimple', 'reconnect', 'health.fix.reconnect', true]);
  for (const x of ['unknown', 'checking', undefined]) assert.deepEqual(pick(s.accountHealth(x, false)), ['pending', '◔', 'not cloned', 'health.checking', null, '', false]);
});

test('I warn about a working token within 14 days of its expiry', () => {
  const now = Date.parse('2026-10-16T12:00:00Z');
  const inDays = (d) => new Date(now + d * 24 * 60 * 60 * 1000 + 60 * 1000).toISOString();
  const pick = (h) => [h.level, h.mark, h.color, h.key, h.vars, h.fix, h.fixKey, h.attention];
  for (const simple of [false, true]) {
    for (const expiresAt of [undefined, '', 'garbage', inDays(15), inDays(400)]) {
      assert.deepEqual(s.accountHealth('ok', simple, expiresAt, now), s.accountHealth('ok', simple), String(expiresAt));
    }
  }
  assert.deepEqual(pick(s.accountHealth('ok', false, inDays(14), now)), ['warn', '◆', 'dirty', 'health.expiresIn', { n: 14 }, 'replace', 'health.fix.replace', false]);
  assert.deepEqual(pick(s.accountHealth('ok', false, inDays(6), now)), ['warn', '◆', 'dirty', 'health.expiresIn', { n: 6 }, 'replace', 'health.fix.replace', false]);
  assert.deepEqual(pick(s.accountHealth('ok', true, inDays(6), now)), ['warn', '◆', 'dirty', 'health.expiresIn', { n: 6 }, 'renew', 'health.fix.renew', false]);
  assert.equal(s.accountHealth('ok', false, inDays(1), now).key, 'health.expiresInOne');
  assert.equal(s.accountHealth('ok', false, inDays(0), now).key, 'health.expiresToday');
  assert.equal(s.accountHealth('ok', false, inDays(-1), now).key, 'health.expiresToday');
  // A failing check keeps its own words: the expiry only refines a working token.
  for (const x of ['warning', 'offline', 'error', 'none', 'unknown']) {
    assert.deepEqual(s.accountHealth(x, false, inDays(3), now), s.accountHealth(x, false), x);
  }
});


test('I format the age of a known network check in the current language', () => {
  const checkedAt = '2026-01-01T00:00:00Z';
  const checked = Date.parse(checkedAt);
  assert.equal(s.checkedAgo(checkedAt, 'en', checked), 'now');
  assert.equal(s.checkedAgo(checkedAt, 'en', checked + 60_000), '1 minute ago');
  assert.equal(s.checkedAgo(checkedAt, 'es', checked + 60_000), 'hace 1 minuto');
  assert.equal(s.checkedAgo(checkedAt, 'en', checked + 3_600_000), '1 hour ago');
  assert.equal(s.checkedAgo(checkedAt, 'en', checked + 86_400_000), 'yesterday');
  assert.equal(s.checkedAgo('unknown', 'en', checked), null);
});

test('a status batch belongs to exactly one snapshot and startup pass', (t) => {
  const batch = { passID: 3, revision: 8, items: [], done: true };
  assert.equal(s.acceptStatusBatch(batch, 3, 8), true);
  for (const [passID, revision] of [[3, 7], [3, 9], [3, undefined], [4, 8], [2, 8]]) {
    assert.equal(s.acceptStatusBatch(batch, passID, revision), false);
  }

  t.mock.timers.enable({ apis: ['setTimeout'] });
  const paints = [];
  const queue = s.statusPaintQueue(rows => paints.push(rows));
  queue.push([1]);
  queue.push([2, 3]);
  queue.push([4]);
  assert.deepEqual(paints, [[1]]);
  t.mock.timers.tick(125);
  assert.deepEqual(paints, [[1], [2, 3, 4]]);
  queue.push([5]);
  queue.flush(); // completion applies the remaining rows immediately
  queue.push([6]);
  queue.discard(); // superseded pass
  t.mock.timers.tick(250);
  assert.deepEqual(paints, [[1], [2, 3, 4], [5]]);
});
