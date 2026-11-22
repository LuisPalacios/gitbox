import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./remotepath.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const rp = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('I explain what the host resolved a typed folder to', () => {
  assert.equal(rp.pathHint(null), null);
  assert.deepEqual(rp.pathHint({ abs: '', exists: false, isDir: false, writable: false, error: 'not absolute' }), { key: 'remotePath.invalid', params: { reason: 'not absolute' } });
  assert.deepEqual(rp.pathHint({ abs: '/srv/f', exists: true, isDir: false, writable: false }), { key: 'remotePath.notDir', params: { abs: '/srv/f' } });
  assert.deepEqual(rp.pathHint({ abs: '/ro', exists: true, isDir: true, writable: false }), { key: 'remotePath.notWritable', params: { abs: '/ro' } });
  assert.deepEqual(rp.pathHint({ abs: '/new', exists: false, isDir: false, writable: true }), { key: 'remotePath.willCreate', params: { abs: '/new' } });
  assert.deepEqual(rp.pathHint({ abs: '/srv/git', exists: true, isDir: true, writable: true }), { key: 'remotePath.ok', params: { abs: '/srv/git' } });
});

test('I accept only a writable folder that exists or can be created', () => {
  assert.equal(rp.pathUsable(null), false);
  assert.equal(rp.pathUsable({ abs: '/srv/git', exists: true, isDir: true, writable: true }), true);
  assert.equal(rp.pathUsable({ abs: '/new', exists: false, isDir: false, writable: true }), true);
  assert.equal(rp.pathUsable({ abs: '/srv/f', exists: true, isDir: false, writable: true }), false);
  assert.equal(rp.pathUsable({ abs: '/ro', exists: true, isDir: true, writable: false }), false);
  assert.equal(rp.pathUsable({ abs: '', exists: false, isDir: false, writable: false, error: 'x' }), false);
});

test('I ask the host once the typing stops', async () => {
  const calls = [];
  const check = rp.debounce((v) => calls.push(v), 30);
  check('/a'); check('/ab'); check('/abc');
  await new Promise((r) => setTimeout(r, 80));
  assert.deepEqual(calls, ['/abc']);
});
