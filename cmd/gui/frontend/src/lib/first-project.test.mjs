import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

const src = await readFile(new URL('./first-project.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(src, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { firstCloneOutcome } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const event = { source: 'account', repo: 'org/one', path: '/projects/one' };
const status = { ...event, state: 'clean' };

test('I wait for the selected checkout, even when Git reports a destination', async () => {
  for (const row of [null, { ...status, source: 'other' }, { ...status, repo: 'org/two' },
    { ...status, path: '/elsewhere/one' }, { ...status, state: 'not cloned' }, { ...status, state: 'unknown' }]) {
    assert.equal(await firstCloneOutcome(event, event.path, async () => row ? [row] : []), 'failed');
  }
  assert.equal(await firstCloneOutcome(event, event.path, async () => [status]), 'ready');
  assert.equal(await firstCloneOutcome({ ...event, warnings: ['identity'] }, event.path, async () => [status]), 'attention');
});

test('I keep skipped and failed downloads out of the success screen', async () => {
  for (const change of [{ skipped: true }, { error: 'failed' }, { path: '' }, { path: '/other' }]) {
    assert.equal(await firstCloneOutcome({ ...event, ...change }, event.path, () => assert.fail('did not start a checkout')), 'failed');
  }
});
