import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('./tint.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const tint = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

const themes = ['dark', 'light'];

test('the presets are the ones Go and the schema accept, blue first', async () => {
  const prefs = await readFile(new URL('../../../../../pkg/config/prefs.go', import.meta.url), 'utf8');
  const goList = prefs.match(/RemoteTintPresets = \[\]string\{([^}]*)\}/)[1].match(/#[0-9a-f]{6}/g);
  const schema = JSON.parse(await readFile(new URL('../../../../../json/gitbox.schema.json', import.meta.url), 'utf8'));
  const schemaList = schema.properties.global.properties.remote_tint.enum;
  const ours = tint.TINT_PRESETS.map((p) => p.color);
  assert.deepEqual(ours, goList);
  assert.deepEqual(ours, schemaList);
  assert.equal(tint.DEFAULT_TINT, '#3b82f6');
});

test('the theme tokens the tint starts from match style.css', async () => {
  const css = await readFile(new URL('../style.css', import.meta.url), 'utf8');
  const names = { base: '--bg-base', card: '--bg-card', border: '--border', primary: '--text-primary', secondary: '--text-secondary', muted: '--text-muted', dim: '--text-dim' };
  for (const theme of themes) {
    const block = css.match(new RegExp(`\\[data-theme="${theme}"\\] \\{([^}]*)\\}`))[1];
    for (const [key, varName] of Object.entries(names)) {
      const value = block.match(new RegExp(`${varName}: (#[0-9a-fA-F]{6})`))[1].toLowerCase();
      assert.equal(tint.THEME_TOKENS[theme][key], value, `${theme} ${varName}`);
    }
  }
});

test('every preset tints both surfaces and keeps text readable in both themes', () => {
  for (const theme of themes) {
    const tk = tint.THEME_TOKENS[theme];
    for (const { color } of tint.TINT_PRESETS) {
      const s = tint.tintSurfaces(color, theme);
      const where = `${color} in ${theme}`;
      assert.notEqual(s.top, tk.base, `${where}: the top bar shows the tint`);
      assert.notEqual(s.side, tk.card, `${where}: the sidebar shows the tint`);
      for (const bg of [s.top, s.side]) {
        // Text and icons: primary, secondary (nav labels) and muted (icons,
        // counts and the second lines) keep 4.5:1.
        for (const fg of ['primary', 'secondary', 'muted']) {
          const c = tint.contrast(s[fg], bg);
          assert.ok(c >= 4.5, `${where}: ${fg} ${s[fg]} on ${bg} is ${c.toFixed(2)}:1`);
        }
        // The section labels were under 4.5:1 untinted; they never get worse.
        const before = Math.min(tint.contrast(tk.dim, tk.base), tint.contrast(tk.dim, tk.card));
        assert.ok(tint.contrast(s.dim, bg) >= before - 1e-9, `${where}: dim lost contrast`);
      }
      // The tint stays subtle: the surfaces move little.
      assert.ok(tint.contrast(s.top, tk.base) < 1.25, `${where}: top bar tint too strong`);
      assert.ok(tint.contrast(s.side, tk.card) < 1.25, `${where}: sidebar tint too strong`);
    }
  }
});

test('the mix and the contrast follow their definitions', () => {
  assert.equal(tint.mix('#000000', '#ffffff', 0.5), '#808080');
  assert.equal(tint.mix('#18181b', '#3b82f6', 0), '#18181b');
  assert.equal(tint.contrast('#000000', '#ffffff').toFixed(1), '21.0');
  assert.equal(tint.contrast('#ffffff', '#ffffff'), 1);
});

test('a saved value reads as its preset, anything else as blue', () => {
  assert.equal(tint.tintOf(' #8B5CF6 '), '#8b5cf6');
  for (const v of ['', null, undefined, '#123456', 'red']) assert.equal(tint.tintOf(v), '#3b82f6');
});
