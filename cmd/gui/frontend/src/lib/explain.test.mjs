import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { test } from 'node:test';
import ts from 'typescript';

const code = await readFile(new URL('./explain.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { accountIssue, caughtIssue, errorAction, explain, repoIssue, unsafeHelp } =
  await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('I explain every declared Go Git and provider code in both languages', async () => {
  const codes = JSON.parse(execFileSync('go', ['run', './src/lib/testdata/error_codes.go'], {
    cwd: new URL('../../', import.meta.url), encoding: 'utf8',
  }));
  assert.ok(codes.length > 0);
  const source = await readFile(new URL('./i18n.ts', import.meta.url), 'utf8');
  const file = ts.createSourceFile('i18n.ts', source, ts.ScriptTarget.ES2022, true);
  const catalogs = file.statements.filter(ts.isVariableStatement)
    .flatMap(node => [...node.declarationList.declarations])
    .find(declaration => declaration.name.getText(file) === 'catalogs').initializer.properties;
  for (const catalog of catalogs) {
    const keys = new Set(catalog.initializer.properties.map(entry => entry.name.text));
    for (const code of codes) {
      const message = explain(code);
      assert.equal(message.titleKey, `error.${code}.title`);
      assert.equal(message.bodyKey, `error.${code}.body`);
      for (const key of [message.titleKey, message.bodyKey, message.action?.labelKey].filter(Boolean)) {
        assert.ok(keys.has(key), `${catalog.name.getText(file)} is missing ${key}`);
      }
    }
  }
});

test('I offer only the action backed by the current context and respect Retry-After', () => {
  const retry = () => {};
  assert.equal(errorAction({ code: 'git.offline' }, { retry }, 10)?.kind, 'retry');
  assert.equal(errorAction({ code: 'git.offline' }, {}, 10), undefined);
  assert.equal(errorAction({ code: 'provider.rate_limited' }, { retry }, 10), undefined);
  assert.equal(errorAction({ code: 'provider.rate_limited', retryAt: 20 }, { retry }, 19), undefined);
  assert.equal(errorAction({ code: 'provider.rate_limited', retryAt: 20 }, { retry }, 20)?.kind, 'retry');
  assert.equal(errorAction({ code: 'repo.root_offline' }, { retry }, 20), undefined);
});

test('I use known facts and leave legacy text unclassified', () => {
  assert.equal(explain('constructor').titleKey, 'error.unknown.title');
  assert.equal(explain('git.unknown').unknown, true);
  assert.equal(explain('provider.forbidden').action, undefined);
  assert.equal(repoIssue({ status: 'error', error: '401 offline' }).code, undefined);
  assert.equal(repoIssue({ status: 'dirty', pullSkipped: 'dirty' }).code, 'repo.dirty');
  assert.equal(repoIssue({ status: 'dirty', behind: 1 }).code, 'repo.dirty');
  assert.equal(repoIssue({ status: 'dirty', behind: 0 }), undefined);
  assert.equal(repoIssue({ status: 'diverged' }).code, 'repo.diverged');
  assert.equal(accountIssue({ status: 'error', problem: { code: 'provider.unauthorized' } }).code, 'provider.unauthorized');
  assert.deepEqual(caughtIssue(new Error('offline')), {});
  assert.equal(caughtIssue({ code: 'git.unknown', detail: 'x'.repeat(900) }).detail.length, 300);
});

test('I render offline, revoked-token and dirty-skip actions in English and Spanish', async (t) => {
  const cacheDir = await mkdtemp(join(tmpdir(), 'gitbox-explain-vite-'));
  t.after(() => rm(cacheDir, { recursive: true, force: true }));
  const server = await createServer({
    root: fileURLToPath(new URL('../../', import.meta.url)),
    configFile: false,
    cacheDir,
    logLevel: 'error',
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, ws: false },
    plugins: [svelte()],
  });
  try {
    const { default: ErrorExplanation } = await server.ssrLoadModule('/src/lib/components/ErrorExplanation.svelte');
    const { default: RepoDetail } = await server.ssrLoadModule('/src/lib/components/RepoDetail.svelte');
    const { render } = await server.ssrLoadModule('svelte/server');
    const { languageStore } = await server.ssrLoadModule('/src/lib/i18n.ts');
    const actions = { retry() {}, credential() {}, editor() {} };

    for (const [lang, offline, retry, auth, fix, dirty, editor] of [
      ['en', 'Can’t reach the server', 'Retry', 'Sign-in needed', 'Fix credential', 'Changes in the clone', 'Open in editor'],
      ['es', 'No se puede conectar con el servidor', 'Reintentar', 'Debes iniciar sesión', 'Arreglar credencial', 'Cambios en el clon', 'Abrir en el editor'],
    ]) {
      languageStore.set(lang);
      for (const [code, title, action] of [['git.offline', offline, retry], ['provider.unauthorized', auth, fix]]) {
        const { body } = render(ErrorExplanation, { props: {
          info: { code, detail: 'diagnostic fixture' }, actions, simple: true,
        } });
        assert.ok(body.includes(title));
        assert.ok(body.includes(`>${action}</button>`));
        assert.doesNotMatch(body, /diagnostic fixture/);
      }
      const { body } = render(RepoDetail, { props: {
        simple: true, state: { status: 'dirty', behind: 1 }, repoDetail: null,
        detailLoading: false, actions, issueActions: actions,
      } });
      assert.ok(body.includes(dirty));
      assert.ok(body.includes(`>${editor}</button>`));
    }

    const details = (simple) => render(ErrorExplanation, { props: {
      info: { detail: 'bounded diagnostic fixture' }, simple,
    } }).body;
    assert.doesNotMatch(details(true), /bounded diagnostic fixture/);
    assert.match(details(false), /<details\b[^>]*>.*bounded diagnostic fixture/s);
    const rateLimited = render(ErrorExplanation, { props: {
      info: { code: 'provider.rate_limited', retryAt: Date.now() + 60_000 }, actions, simple: true,
    } }).body;
    assert.doesNotMatch(rateLimited, /<button/);
  } finally {
    await server.close();
  }
});

test('I explain a clone paused for safety with its own commands (#344)', () => {
  const path = '/work/clone';
  assert.deepEqual(unsafeHelp({ setting: 'core.hooksPath', scope: 'local' }, path), {
    reasonKey: 'error.git.unsafe_config.hooksPath',
    whereKey: 'error.git.unsafe_config.scope.local',
    commands: [
      'git -C "/work/clone" config --show-origin --show-scope --get-all core.hooksPath',
      'git -C "/work/clone" config --local --unset core.hooksPath',
    ],
  });
  assert.deepEqual(unsafeHelp({ setting: 'core.fsmonitor', scope: 'worktree' }, path).commands[1],
    'git -C "/work/clone" config --worktree --unset core.fsmonitor');
  // An included file or an insteadOf rule is edited where the first command points.
  assert.equal(unsafeHelp({ setting: 'core.hooksPath', scope: 'include' }, path).commands.length, 1);
  assert.deepEqual(unsafeHelp({ setting: 'url.*.insteadOf', scope: 'local' }).commands,
    ['git config --show-origin --show-scope --get-regexp "^url\\..*\\.insteadof$"']);
  for (const unknown of [undefined, { setting: 'core.sshCommand', scope: 'local' }, { setting: 'core.hooksPath', scope: 'global' }]) {
    assert.equal(unsafeHelp(unknown, path), undefined);
  }
  assert.equal(explain('git.unsafe_config').action.kind, 'terminal');
});
