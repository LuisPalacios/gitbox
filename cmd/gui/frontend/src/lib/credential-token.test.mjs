import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import vm from 'node:vm';
import ts from 'typescript';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

const appSource = await readFile(new URL('../App.svelte', import.meta.url), 'utf8');
const modalSource = await readFile(new URL('./modals/CredentialModal.svelte', import.meta.url), 'utf8');

// I execute the actual handlers with an isolated bridge, without loading App's
// unrelated startup work or putting any credential on disk.
function functionsFrom(source, names) {
  const script = source.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
  const tree = ts.createSourceFile('component.ts', script, ts.ScriptTarget.Latest, true);
  return names.map(name => {
    const fn = tree.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name);
    assert.ok(fn, `handler ${name} exists`);
    return ts.transpileModule(fn.getText(tree).replace(/^export\s+/, ''), {
      compilerOptions: { target: ts.ScriptTarget.ES2022 },
    }).outputText;
  }).join('\n');
}

function credentialFixture(store) {
  const context = vm.createContext({
    credTokenInput: 'fixture-token', credResult: null, credBusy: false,
    credTokenGuide: 'https://example.test/tokens', credTokenScopes: '', credPrecheck: null,
    credChangeModal: 'account', credChangeType: 'token', credForceToken: false,
    credDeleteBusy: false, credSetupStarted: false, sshTokenDialog: null,
    verifyAllCredentials() {}, fetchTokenGuide: async () => {},
    bridge: { credentialStoreToken: store },
    credPostSetup: async () => {},
  });
  vm.runInContext(functionsFrom(appSource, ['storeCredToken', 'runCredSetup', 'resetCredState']) + '\n'
    + functionsFrom(modalSource, ['closeCredChange', 'openCredChange', 'openTokenSetup']), context);
  return context;
}

test('I clear saved tokens in both flows before post-setup runs', async () => {
  for (const handler of ['storeCredToken', 'runCredSetup']) {
    const fixture = credentialFixture(async (key, token) => {
      assert.equal(key, 'account');
      assert.equal(token, 'fixture-token');
      return { ok: true, message: 'Saved' };
    });
    let postSetup = false;
    fixture.credPostSetup = async () => {
      postSetup = true;
      assert.equal(fixture.credTokenInput, '');
    };
    await fixture[handler]('account', 'token');
    assert.ok(postSetup);
    assert.equal(fixture.credTokenInput, '');
    assert.equal(fixture.credBusy, false);
  }
});

test('I retain failed tokens for correction and clear them after a successful retry', async () => {
  for (const handler of ['storeCredToken', 'runCredSetup']) {
    for (const rejects of [false, true]) {
      const fixture = credentialFixture(async () => {
        if (rejects) throw new Error('Storage unavailable');
        return { ok: false, message: 'Storage unavailable' };
      });
      await fixture[handler]('account', 'token');
      assert.equal(fixture.credTokenInput, 'fixture-token');
      assert.equal(fixture.credResult.ok, false);
      assert.equal(fixture.credBusy, false);
      fixture.credTokenInput = 'corrected-fixture-token';
      fixture.bridge.credentialStoreToken = async (_key, token) => {
        assert.equal(token, 'corrected-fixture-token');
        return { ok: true, message: 'Saved' };
      };
      await fixture[handler]('account', 'token');
      assert.equal(fixture.credTokenInput, '');
    }
  }
});

test('I clear tokens when closing and reopening either credential modal mode', async () => {
  const fixture = credentialFixture(async () => ({ ok: false, message: 'Failure' }));
  fixture.closeCredChange();
  assert.equal(fixture.credTokenInput, '');
  assert.equal(fixture.credChangeModal, null);
  fixture.credTokenInput = 'stale-fixture-token';
  fixture.openCredChange('account', 'token');
  assert.equal(fixture.credTokenInput, '');
  fixture.credTokenInput = 'another-stale-fixture-token';
  await fixture.openTokenSetup('account');
  assert.equal(fixture.credTokenInput, '');
});

// I compile the real modal and its children. Only the initial modal selection
// changes for this server-rendered fixture; all rendering conditions stay intact.
test('I render the failed token and enabled retry, then hide the successful input', async () => {
  const server = await createServer({
    root: fileURLToPath(new URL('../../', import.meta.url)),
    configFile: false,
    logLevel: 'error',
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null },
    plugins: [{
      name: 'credential-modal-fixture',
      enforce: 'pre',
      transform(code, id) {
        if (!id.endsWith('/CredentialModal.svelte')) return;
        assert.ok(code.includes("let credChangeModal: string | null = null;"));
        assert.ok(code.includes("let credChangeType = '';"));
        return code.replace('let credChangeModal: string | null = null;', "let credChangeModal: string | null = 'account';")
          .replace("let credChangeType = '';", "let credChangeType = 'token';");
      },
    }, svelte()],
  });
  try {
    const { default: CredentialModal } = await server.ssrLoadModule('/src/lib/modals/CredentialModal.svelte');
    const { render } = await server.ssrLoadModule('svelte/server');
    const props = {
      credResult: { ok: false, message: 'Storage unavailable' },
      credTokenInput: 'fixture-token', credTokenGuide: 'https://example.test/tokens',
      credTokenScopes: '', credBusy: false, credPrecheck: null, credStatuses: {},
      hostOS: 'darwin', sshKeyCopied: false,
    };
    const failed = render(CredentialModal, { props }).body;
    assert.match(failed, /<input\b[^>]*id="cc-token"[^>]*value="fixture-token"/);
    assert.match(failed, /Storage unavailable/);
    const retry = failed.match(/<button\b([^>]*)>\s*Store token\s*<\/button>/);
    assert.ok(retry, 'failed save offers retry');
    assert.doesNotMatch(retry[1], /\bdisabled\b/);
    const saved = render(CredentialModal, { props: { ...props, credTokenInput: '', credResult: { ok: true, message: 'Saved' } } }).body;
    assert.doesNotMatch(saved, /id="cc-token"/);
    assert.doesNotMatch(saved, /fixture-token/);
    assert.match(saved, /Saved/);
  } finally {
    await server.close();
  }
});
