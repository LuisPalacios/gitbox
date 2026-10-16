// The UI text rules (issue #140): the English and Spanish catalogs match,
// every literal key the code asks for exists, and no .svelte template
// shows hard-coded English.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile, readdir } from 'node:fs/promises';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import { parse } from 'svelte/compiler';

const libDir = fileURLToPath(new URL('.', import.meta.url));
const srcDir = join(libDir, '..');

// ── Catalogs, read from the i18n.ts syntax tree ──
async function loadCatalogs() {
  const code = await readFile(join(libDir, 'i18n.ts'), 'utf8');
  const sf = ts.createSourceFile('i18n.ts', code, ts.ScriptTarget.ES2022, true);
  const catalogs = {};
  sf.forEachChild(function visit(node) {
    if (ts.isVariableDeclaration(node) && node.name.getText() === 'catalogs') {
      for (const lang of node.initializer.properties) {
        const entries = new Map();
        for (const p of lang.initializer.properties) entries.set(p.name.text, p.initializer.text);
        catalogs[lang.name.getText()] = entries;
      }
    }
    node.forEachChild(visit);
  });
  return catalogs;
}

const placeholders = (value) => [...value.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(',');

test('I keep the same keys and placeholders in English and Spanish', async () => {
  const { en, es } = await loadCatalogs();
  assert.ok(en.size > 0 && es.size > 0);
  assert.deepEqual([...en.keys()].filter((k) => !es.has(k)), [], 'keys missing in es');
  assert.deepEqual([...es.keys()].filter((k) => !en.has(k)), [], 'keys missing in en');
  const mismatched = [...en.keys()].filter((k) => placeholders(en.get(k)) !== placeholders(es.get(k)));
  assert.deepEqual(mismatched, [], 'placeholders differ between en and es');
  const empty = [...en.keys()].filter((k) => !en.get(k).trim() || !es.get(k).trim());
  assert.deepEqual(empty, [], 'empty translations');
});

// ── Source files ──
async function sourceFiles(dir, exts) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...await sourceFiles(path, exts));
    else if (exts.some((e) => entry.name.endsWith(e)) && !entry.name.endsWith('.test.mjs')) out.push(path);
  }
  return out;
}

test('I only ask for keys that exist', async () => {
  const { en } = await loadCatalogs();
  const missing = [];
  for (const file of await sourceFiles(srcDir, ['.svelte', '.ts'])) {
    const code = await readFile(file, 'utf8');
    for (const m of code.matchAll(/\$t\(\s*'([^']+)'|<Rich\s+key="([^"]+)"/g)) {
      const key = m[1] ?? m[2];
      if (!en.has(key)) missing.push(`${relative(srcDir, file)}: ${key}`);
    }
  }
  assert.deepEqual(missing, []);
});

// ── Hard-coded template text ──
// Attributes whose static value a person reads.
const TEXT_ATTRS = new Set(['title', 'aria-label', 'placeholder', 'alt', 'label']);
// Elements whose content is never translated: code, keys, and anything
// marked translate="no" (names, URLs).
const VERBATIM = new Set(['code', 'kbd', 'pre', 'svg', 'style', 'script']);
// Text that reads the same in every language.
const ALLOWED = new Set(['gitbox', 'GitHub', 'GitLab', 'Gitea', 'Forgejo', 'Bitbucket', 'GCM', 'SSH', 'PAT', 'API', 'URL', 'HTTPS', 'Git', 'git', 'OK']);
// Calls whose string arguments are keys or identifiers, not prose.
const NON_PROSE_CALLS = new Set(['$t', '$tpl', 'format', 'includes', 'startsWith', 'endsWith', 'replace', 'replaceAll', 'split', 'join', 'indexOf', 'querySelector', 'closest', 'matches', 'getElementById', 'addEventListener', 'dispatch', 'emit', 'get', 'has', 'set', 'sc', 'statusColor', 'countBadge', 'openModal']);

const hasWords = (s) => /[A-Za-z]{2,}/.test(s);
const looksLikeProse = (s) => hasWords(s) && (/\s/.test(s.trim()) || /^[A-Z][a-z]/.test(s));

function scanTemplate(file, code) {
  const hits = [];
  const lineOf = (pos) => code.slice(0, pos).split('\n').length;
  const hit = (pos, kind, text) => {
    const clean = text.trim().replace(/\s+/g, ' ');
    if (!ALLOWED.has(clean)) hits.push(`${file}:${lineOf(pos)} ${kind}: ${clean}`);
  };

  function expr(node, inert) {
    if (!node || typeof node !== 'object') return;
    if (Array.isArray(node)) return node.forEach((n) => expr(n, inert));
    switch (node.type) {
      case 'Literal':
        if (!inert && typeof node.value === 'string' && looksLikeProse(node.value)) hit(node.start, 'expr', node.value);
        return;
      case 'TemplateLiteral':
        if (!inert) for (const q of node.quasis) if (looksLikeProse(q.value.cooked)) hit(q.start, 'expr', q.value.cooked);
        return expr(node.expressions, inert);
      case 'BinaryExpression':
        if (['===', '!==', '==', '!=', 'in'].includes(node.operator)) return;
        break;
      case 'CallExpression': {
        const callee = node.callee.type === 'Identifier' ? node.callee.name : node.callee.property?.name;
        expr(node.callee, inert);
        return expr(node.arguments, inert || NON_PROSE_CALLS.has(callee));
      }
      case 'MemberExpression':
        expr(node.object, inert);
        return node.computed ? expr(node.property, true) : undefined;
      case 'Property':
        return expr(node.value, inert);
    }
    for (const [k, v] of Object.entries(node)) if (k !== 'metadata' && v && typeof v === 'object') expr(v, inert);
  }

  // A static prop on a component reads as text when it looks like prose.
  let inComponent = false;

  function walk(node) {
    if (!node || typeof node !== 'object') return;
    if (Array.isArray(node)) return node.forEach(walk);
    switch (node.type) {
      case 'Text':
        if (hasWords(node.data)) hit(node.start, 'text', node.data);
        return;
      case 'Comment':
        return;
      case 'Attribute':
        if (node.value === true) return;
        for (const v of Array.isArray(node.value) ? node.value : [node.value]) {
          if (v.type === 'Text' && (TEXT_ATTRS.has(node.name) ? hasWords(v.data) : inComponent && looksLikeProse(v.data))) hit(v.start, `@${node.name}`, v.data);
          if (v.type === 'ExpressionTag' && !['class', 'style', 'id', 'href', 'src', 'type', 'name', 'role'].includes(node.name)) expr(v.expression, false);
        }
        return;
      case 'UseDirective':
      case 'OnDirective':
        return expr(node.expression, false);
      case 'ClassDirective':
      case 'StyleDirective':
      case 'BindDirective':
      case 'TransitionDirective':
      case 'AnimateDirective':
        return;
      case 'ExpressionTag':
        return expr(node.expression, false);
      case 'ConstTag':
        return expr(node.declaration, false);
      case 'Component': {
        const outer = inComponent;
        inComponent = true;
        walk(node.attributes);
        inComponent = outer;
        return walk(node.fragment);
      }
      case 'RegularElement':
      case 'SvelteElement':
        if (VERBATIM.has(node.name)) return;
        if (node.attributes.some((a) => a.type === 'Attribute' && a.name === 'translate' && Array.isArray(a.value) && a.value[0]?.data === 'no')) return;
        break;
    }
    for (const [k, v] of Object.entries(node)) if (k !== 'metadata' && v && typeof v === 'object') walk(v);
  }

  walk(parse(code, { modern: true }).fragment);
  return hits;
}

test('I show no hard-coded text in templates', async () => {
  const hits = [];
  for (const file of await sourceFiles(srcDir, ['.svelte'])) {
    hits.push(...scanTemplate(relative(srcDir, file), await readFile(file, 'utf8')));
  }
  assert.deepEqual(hits, [], `${hits.length} hard-coded strings; move them to lib/i18n.ts`);
});

// ── Inline markup ──
const richCode = await readFile(join(libDir, 'rich.ts'), 'utf8');
const { outputText } = ts.transpileModule(richCode, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const rich = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('I split a sentence into text, code, strong and emphasis', () => {
  assert.deepEqual(rich.richSegments('Set `core.excludesfile` to **{path}**, then click *Re-check*.'), [
    { kind: 'text', text: 'Set ' },
    { kind: 'code', text: 'core.excludesfile' },
    { kind: 'text', text: ' to ' },
    { kind: 'strong', text: '{path}' },
    { kind: 'text', text: ', then click ' },
    { kind: 'em', text: 'Re-check' },
    { kind: 'text', text: '.' },
  ]);
  assert.deepEqual(rich.richSegments('Plain'), [{ kind: 'text', text: 'Plain' }]);
  assert.deepEqual(rich.richSegments('patterns (*~, Thumbs.db)'), [{ kind: 'text', text: 'patterns (*~, Thumbs.db)' }]);
});
