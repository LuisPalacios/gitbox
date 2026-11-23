import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./sshAgent.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const sa = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const i18n = await readFile(new URL('./i18n.ts', import.meta.url), 'utf8');

const report = (over) => ({
  keyFile: '/home/me/.ssh/gitbox-acct-sshkey', keyFileFingerprint: 'SHA256:account',
  agentFingerprints: ['SHA256:laptop'], agentForwarded: true, match: false, identitiesOnly: true, ...over,
});

test('I say nothing in a local window or without a forwarded agent', () => {
  assert.equal(sa.agentNote(report({}), false), '');
  assert.equal(sa.agentNote(null, true), '');
  assert.equal(sa.agentNote(report({ agentForwarded: false }), true), '');
  assert.equal(sa.agentNote(report({ agentFingerprints: [] }), true), '');
  assert.equal(sa.agentNote(report({ agentFingerprints: null }), true), '');
  assert.equal(sa.agentNoteKey('', true), '');
});

test('I tell a pinned entry from one a forwarded key can take over', () => {
  assert.equal(sa.agentNote(report({}), true), 'pinned');
  assert.equal(sa.agentNote(report({ identitiesOnly: false }), true), 'unpinned');
  assert.equal(sa.agentNote(report({ match: true, agentFingerprints: ['SHA256:account'] }), true), 'same');
});

test('every note has a Simple and an Advanced text in both catalogs', () => {
  for (const note of ['pinned', 'unpinned', 'same']) {
    for (const simple of [true, false]) {
      const key = sa.agentNoteKey(note, simple);
      const found = [...i18n.matchAll(new RegExp(`'${key.replace('.', '\\.')}': `, 'g'))].length;
      assert.equal(found, 2, `${key} is in ${found} catalogs`);
    }
  }
  // Simple mode names no ssh options.
  const simpleText = [...i18n.matchAll(/'cred\.agent(?:Pinned|Unpinned|Same)(?:\.remote)?': ['"](.*)['"],/g)].map((m) => m[1]);
  assert.ok(simpleText.length >= 6, `found ${simpleText.length} Simple texts`);
  for (const text of simpleText) assert.doesNotMatch(text, /IdentitiesOnly|ssh -A|agent/i);
});
