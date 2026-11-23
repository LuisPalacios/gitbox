import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile, readdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

// Every use:ro names the bridge calls its control ends in (lib/ro.ts), so a
// remote window dims it only while one of them is refused there.

const srcDir = fileURLToPath(new URL('..', import.meta.url));
const bridgeSource = await readFile(new URL('./bridge.ts', import.meta.url), 'utf8');
const bridgeCalls = new Set([...bridgeSource.slice(0, bridgeSource.indexOf('\n};\n')).matchAll(/^ {2}(\w+): \(/gm)].map((m) => m[1]));

async function svelteFiles(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...await svelteFiles(full));
    else if (entry.name.endsWith('.svelte')) out.push(full);
  }
  return out;
}

// roSites returns each use:ro of a source: its line and its parameter, or
// null when it has none.
function roSites(source) {
  const sites = [];
  for (const m of source.matchAll(/use:ro(?![\w-])(=\{([^}]*)\})?/g)) {
    sites.push({ line: source.slice(0, m.index).split('\n').length, param: m[2] ?? null });
  }
  return sites;
}

const literals = (text) => [...text.matchAll(/'([^']*)'/g)].map((m) => m[1]);

// The one parameter that is not a literal: LauncherMenu's extraItems, whose
// ro fields the last test checks where they are built.
const ITEM_RO = 'item.ro';

test('I find every form of use:ro', () => {
  const fixture = `<button use:ro>a</button>\n<button use:ro={'x'}>\n<b use:ro={['y', 'z']} use:remoteOff={r}>\n<i use:ro={item.ro}>`;
  assert.deepEqual(roSites(fixture), [
    { line: 1, param: null },
    { line: 2, param: "'x'" },
    { line: 3, param: "['y', 'z']" },
    { line: 4, param: 'item.ro' },
  ]);
});

test('every use:ro names bridge calls', async () => {
  assert.ok(bridgeCalls.size > 100, `only ${bridgeCalls.size} bridge calls found`);
  let count = 0;
  for (const file of await svelteFiles(srcDir)) {
    const rel = path.relative(srcDir, file);
    for (const { line, param } of roSites(await readFile(file, 'utf8'))) {
      count++;
      assert.ok(param !== null && param.trim() !== '', `${rel}:${line}: use:ro without the calls it ends in`);
      if (param.trim() === ITEM_RO) {
        assert.equal(rel, path.join('lib', 'LauncherMenu.svelte'), `${rel}:${line}: only LauncherMenu takes its calls from an item`);
        continue;
      }
      const names = literals(param);
      assert.ok(names.length > 0, `${rel}:${line}: use:ro={${param}} names no call`);
      assert.match(param.trim(), /^(?:'\w+'|\[\s*'\w+'(?:\s*,\s*'\w+')*\s*\])$/, `${rel}:${line}: use:ro takes a name or an array of names`);
      for (const name of names) assert.ok(bridgeCalls.has(name), `${rel}:${line}: ${name} is not a bridge call`);
    }
  }
  assert.ok(count >= 64, `only ${count} use:ro sites found`);
});

test('every extra menu item names the bridge calls it ends in', async () => {
  let builders = 0;
  for (const file of await svelteFiles(srcDir)) {
    const source = await readFile(file, 'utf8');
    if (!/extraItems=\{/.test(source)) continue;
    builders++;
    const rel = path.relative(srcDir, file);
    const items = (source.match(/\bonClick:/g) || []).length;
    const ro = (source.match(/\bro:/g) || []).length;
    assert.ok(items > 0, `${rel} builds extraItems but no item was found`);
    assert.equal(ro, items, `${rel}: every extraItems entry needs a ro field`);
    // The literal arrays of names: ro: [...] and the CallName[] lists.
    const lists = [...source.matchAll(/(?:\bro:\s*|CallName\[\]\s*=\s*)\[([^\]]*)\]/g)].map((m) => m[1]);
    assert.ok(lists.length > 0, `${rel}: no list of call names found`);
    for (const list of lists) {
      const names = literals(list);
      assert.ok(names.length > 0, `${rel}: an empty list of call names`);
      for (const name of names) assert.ok(bridgeCalls.has(name), `${rel}: ${name} is not a bridge call`);
    }
  }
  assert.ok(builders > 0, 'no component builds extraItems');
});
