import assert from 'node:assert/strict';
import { test, mock } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// Each session keeps its own list (#381): load makes a fresh one, with the
// module's constants and errorText beside it.
const sourceCode = await readFile(new URL('./notice-list.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const list = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const load = async () => ({ ...list, ...list.createNotices() });

const current = (n) => { let v; n.notices.subscribe((x) => { v = x; })(); return v; };

test('I show notices, drop empty lines and dismiss them', async () => {
  const n = await load();
  const id = n.pushNotice('error', 'Open failed', ['boom', false, '', null]);
  assert.deepEqual(current(n), [{ id, kind: 'error', title: 'Open failed', lines: ['boom'] }]);
  n.dismissNotice(id);
  assert.deepEqual(current(n), []);
  let retried = false;
  const conflict = n.pushNotice('error', 'Settings changed', [], () => { retried = true; });
  const [notice] = current(n);
  assert.equal(retried, false);
  n.dismissNotice(conflict);
  notice.retry();
  assert.equal(retried, true);
  n.dismissNotice(id);
  assert.deepEqual(current(n), []);
});

test('I let info and success notices go on their own, and keep errors', async () => {
  mock.timers.enable({ apis: ['setTimeout'] });
  try {
    const n = await load();
    n.pushNotice('success', 'Fetch finished');
    n.pushNotice('info', 'Nothing new');
    const err = n.pushNotice('error', 'Pull failed');
    mock.timers.tick(n.AUTO_DISMISS_MS - 1);
    assert.equal(current(n).length, 3);
    mock.timers.tick(1);
    assert.deepEqual(current(n).map((x) => x.id), [err]);
  } finally {
    mock.timers.reset();
  }
});

test('I keep at most a few notices, oldest out first', async () => {
  const n = await load();
  const ids = [];
  for (let i = 0; i < n.MAX_NOTICES + 2; i++) ids.push(n.pushNotice('error', `e${i}`));
  assert.deepEqual(current(n).map((x) => x.id), ids.slice(2));
});

test('I read the message of anything thrown', async () => {
  const n = await load();
  assert.equal(n.errorText(new Error('boom')), 'boom');
  assert.equal(n.errorText({ message: 'from Go' }), 'from Go');
  assert.equal(n.errorText('plain'), 'plain');
});

test('two sessions keep their notices apart', async () => {
  const a = await load();
  const b = await load();
  const id = a.pushNotice('error', 'Only in A');
  assert.equal(current(a).length, 1);
  assert.deepEqual(current(b), []);
  b.dismissNotice(id);
  assert.equal(current(a).length, 1, 'B cannot dismiss what A shows');
  a.dismissNotice(id);
  assert.deepEqual(current(a), []);
});
