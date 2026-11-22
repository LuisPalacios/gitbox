import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./remotehost.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const rh = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

// The Go table of editors that open remote folders.
const launchGo = await readFile(new URL('../../../../../pkg/remote/launch.go', import.meta.url), 'utf8');
const goEditors = [...launchGo.matchAll(/\{Command: "([\w-]+)", Name:/g)].map((m) => m[1]);

test('I know nothing of a host outside a remote window', () => {
  assert.equal(rh.hostFromState(null), null);
  assert.equal(rh.editorOff('/usr/bin/zed', null), '');
  assert.equal(rh.unavailableOff(null), '');
});

test('I disable the editors the host cannot open and keep VS Code', () => {
  assert.deepEqual(goEditors, ['code', 'code-insiders']);
  const host = rh.hostFromState({ remoteEditors: goEditors });
  assert.equal(rh.editorOff('/usr/local/bin/code', host), '');
  assert.equal(rh.editorOff('C:\\Users\\me\\AppData\\Local\\Programs\\Microsoft VS Code\\bin\\code.cmd', host), '');
  assert.equal(rh.editorOff('CODE-INSIDERS.EXE', host), '');
  assert.equal(rh.editorOff('/opt/homebrew/bin/zed', host), 'editor');
  assert.equal(rh.editorOff('codium', host), 'editor');
  assert.equal(rh.offTip('editor'), 'remote.editorNoRemoteTip');
});

test('I disable the actions with no remote equivalent', () => {
  const host = rh.hostFromState({});
  assert.deepEqual(host, { editors: [] });
  assert.equal(rh.unavailableOff(host), 'unavailable');
  assert.equal(rh.offTip('unavailable'), 'remote.notAvailableTip');
  assert.equal(rh.offTip(''), '');
  assert.deepEqual(rh.hostFromState({ remoteEditors: ['code', 7, null] }), { editors: ['code'] });
});

test('I name an editor the way the Go side does', () => {
  assert.equal(rh.editorName('/usr/local/bin/Code'), 'code');
  assert.equal(rh.editorName('C:\\x\\code.bat'), 'code');
  assert.equal(rh.editorName(''), '');
});
