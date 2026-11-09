import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const sourceCode = await readFile(new URL('./theme.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(sourceCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const th = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('I colour every known provider in both themes, each one apart', () => {
  for (const theme of ['light', 'dark']) {
    const colours = ['github', 'gitlab', 'gitea', 'forgejo', 'bitbucket'].map((p) => th.providerColor(p, theme));
    for (const c of colours) assert.match(c, /^#[0-9a-f]{6}$/);
    assert.equal(new Set(colours).size, colours.length);
  }
  assert.notEqual(th.providerColor('github', 'light'), th.providerColor('github', 'dark'));
});

test('I leave an unknown or missing provider in the text colour', () => {
  assert.equal(th.providerColor('sourcehut', 'dark'), '');
  assert.equal(th.providerColor(undefined, 'light'), '');
});
