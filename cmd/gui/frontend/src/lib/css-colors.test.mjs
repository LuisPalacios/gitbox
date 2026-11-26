// Older WebKitGTK builds lack CSS color-mix() and drop any declaration that
// uses it, so the styles use colors computed ahead of time (#340). This
// keeps color-mix() out of the frontend and checks every computed color
// against the mix it stands for.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile, readdir } from 'node:fs/promises';
import ts from 'typescript';

const src = new URL('../', import.meta.url);
const tintSource = await readFile(new URL('./tint.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(tintSource, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { mix } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

// stripComments drops CSS, JS and HTML comments, so a comment that names
// color-mix() does not count as a use.
const stripComments = (text) => text
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/<!--[\s\S]*?-->/g, '')
  .replace(/(^|[^:])\/\/.*$/gm, '$1');

async function sources(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const url = new URL(entry.name + (entry.isDirectory() ? '/' : ''), dir);
    if (entry.isDirectory()) out.push(...(await sources(url)));
    else if (/\.(css|svelte|ts|js|mjs|html)$/.test(entry.name) && !entry.name.endsWith('.test.mjs')) out.push(url);
  }
  return out;
}

test('no frontend source uses color-mix()', async () => {
  const files = await sources(src);
  assert.ok(files.some((f) => f.pathname.endsWith('/style.css')), 'the walk reaches style.css');
  assert.ok(stripComments('a { b: color-mix(in srgb, red 5%, blue); }').includes('color-mix('), 'a use is found');
  assert.ok(!stripComments('/* color-mix() */ // color-mix(\n<!-- color-mix( -->').includes('color-mix('), 'a comment is not');
  const uses = [];
  for (const f of files) {
    if (stripComments(await readFile(f, 'utf8')).includes('color-mix(')) uses.push(f.pathname.slice(src.pathname.length));
  }
  assert.deepEqual(uses, [], 'replace color-mix() with a per-theme token in style.css');
});

// Each computed color is base mixed with weight w of color, as
// color-mix(in srgb, color w, base) would give.
const RECIPES = {
  '--acct-bad-bg': ['--bg-card', '--alert', 0.09],
  '--acct-warn-bg': ['--bg-card', '#f07623', 0.08],
  '--activity-bg': ['--bg-base', '#4b95e9', 0.08],
  '--notice-error-border': ['--border-hover', '--alert', 0.55],
  '--pr-review-bg': ['--bg-hover', '#d81e5b', 0.12],
  '--pr-review-bg-hover': ['--bg-hover', '#d81e5b', 0.22],
};

const channels = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));

test('the computed colors match the mixes they replace in both themes', async () => {
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8');
  for (const theme of ['dark', 'light']) {
    const block = stripComments(css.match(new RegExp(`\\[data-theme="${theme}"\\] \\{([^}]*)\\}`))[1]);
    const tokens = Object.fromEntries([...block.matchAll(/(--[a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim().toLowerCase()]));
    const value = (v) => (v.startsWith('--') ? tokens[v] : v);
    for (const [name, [base, color, w]] of Object.entries(RECIPES)) {
      const want = channels(mix(value(base), value(color), w));
      const got = channels(tokens[name] || '#zzzzzz');
      for (let i = 0; i < 3; i++) {
        assert.ok(Math.abs(got[i] - want[i]) <= 1, `${theme} ${name}: ${tokens[name]} is not ${mix(value(base), value(color), w)}`);
      }
    }
    assert.equal(tokens['--alert-rgb'], channels(tokens['--alert']).join(', '), `${theme} --alert-rgb`);
  }
});
