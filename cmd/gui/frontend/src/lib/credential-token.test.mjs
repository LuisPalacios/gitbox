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
const onboardingSource = await readFile(new URL('./Onboarding.svelte', import.meta.url), 'utf8');

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
    credDeleteBusy: false, credSetupStarted: false, sshTokenDialog: null, credentialFindings: [{ path: 'stale' }],
    document: { activeElement: null }, HTMLElement: class {}, invoker: null, dialog: null,
    tick: async () => {}, verifyAllCredentials() {}, fetchTokenGuide: async () => {},
    bridge: { credentialStoreToken: store },
    credPostSetup: async () => {},
  });
  vm.runInContext(functionsFrom(appSource, ['storeCredToken', 'runCredSetup', 'resetCredState', 'tokenStored']) + '\n'
    + functionsFrom(modalSource, ['closeCredChange', 'dismissCredChange', 'focusDialog', 'openCredChange', 'openTokenSetup']), context);
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

test('I show what the audit found right after a store, and clear an old warning when it finds nothing', async () => {
  for (const handler of ['storeCredToken', 'runCredSetup']) {
    const finding = { path: '/host/credentials/account', problem: 'readable by others', hint: 'chmod 600', exposed: true };
    const fixture = credentialFixture(async () => ({ ok: true, message: 'Saved', findings: [finding] }));
    await fixture[handler]('account', 'token');
    assert.deepEqual(JSON.parse(JSON.stringify(fixture.credentialFindings)), [finding]);
    fixture.credTokenInput = 'another-fixture-token';
    fixture.bridge.credentialStoreToken = async () => ({ ok: true, message: 'Saved' });
    await fixture[handler]('account', 'token');
    assert.equal(fixture.credentialFindings.length, 0);
  }
});

// Onboarding keeps its own token field (#163, #242).
function onboardingFixture(store) {
  const context = vm.createContext({
    token: 'fixture-token', accountKey: 'account', busy: false, connectError: '', connectProblem: undefined,
    errorInfo: (_, detail) => ({ detail }),
    caughtIssue: () => ({}),
    started: false, $t: (key) => key,
    bridge: { credentialStoreToken: store, credentialVerify: async () => ({ status: 'ok' }) },
  });
  context.startProjects = () => { context.started = true; };
  vm.runInContext(functionsFrom(onboardingSource, ['saveToken', 'verifyToken']), context);
  return context;
}

test('I clear the onboarding token once it is stored, and keep a refused one for correction', async () => {
  const saved = onboardingFixture(async (key, token) => {
    assert.equal(key, 'account');
    assert.equal(token, 'fixture-token');
    return { ok: true, message: 'Saved' };
  });
  await saved.saveToken();
  assert.equal(saved.token, '');
  assert.ok(saved.started);
  const refused = onboardingFixture(async () => ({ ok: false, message: 'Storage unavailable' }));
  await refused.saveToken();
  assert.equal(refused.token, 'fixture-token');
  assert.equal(refused.connectProblem.detail, 'Storage unavailable');
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

// A remote host refuses GCM (#242): the real modal, rendered in a remote
// window for a GCM account, offers no GCM and a way to a token instead.
test('I show GCM as unavailable in a remote window and offer a switch to token', async () => {
  const server = await createServer({
    root: fileURLToPath(new URL('../../', import.meta.url)),
    configFile: false,
    logLevel: 'error',
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null },
    plugins: [{
      name: 'credential-modal-gcm-fixture',
      enforce: 'pre',
      transform(code, id) {
        if (!id.endsWith('/CredentialModal.svelte')) return;
        return code.replace('let credChangeModal: string | null = null;', "let credChangeModal: string | null = 'account';")
          .replace("let credChangeType = '';", "let credChangeType = 'gcm';");
      },
    }, svelte()],
  });
  try {
    const stores = await server.ssrLoadModule('/src/lib/stores.ts');
    const { gcmRemoteReasonKey, defaultCredentialType } = await server.ssrLoadModule('/src/lib/gcmRemote.ts');
    const { default: CredentialModal } = await server.ssrLoadModule('/src/lib/modals/CredentialModal.svelte');
    const { render } = await server.ssrLoadModule('svelte/server');
    stores.configStore.set({ accounts: { account: { provider: 'github', url: 'https://github.com', username: 'alice', name: 'A', email: 'a@e', default_credential_type: 'gcm' } }, sources: {} });
    const props = {
      credResult: null, credTokenInput: '', credTokenGuide: '', credTokenScopes: '', credBusy: false,
      credPrecheck: null, credStatuses: {}, serverOS: 'linux', sshKeyCopied: false,
    };
    const option = (body) => body.match(/<option\b[^>]*value="gcm"[^>]*>[^<]*<\/option>/)?.[0] || '';

    stores.remoteWindow.set(true);
    const remote = render(CredentialModal, { props }).body;
    assert.match(remote, /Not supported on remote hosts/);
    assert.match(remote, /Secret Service keyring/);
    assert.match(remote, /<button\b[^>]*>\s*Switch to token\s*<\/button>/);
    assert.match(option(remote), /\bdisabled\b/);
    assert.match(option(remote), /not on remote hosts/);

    stores.remoteWindow.set(false);
    const local = render(CredentialModal, { props }).body;
    assert.doesNotMatch(local, /Not supported on remote hosts|Switch to token/);
    assert.doesNotMatch(option(local), /\bdisabled\b/);

    assert.equal(gcmRemoteReasonKey('darwin'), 'error.gcm_unsupported_remote.darwin');
    assert.equal(gcmRemoteReasonKey('plan9'), 'error.gcm_unsupported_remote');
    assert.equal(defaultCredentialType(true), 'token');
    assert.equal(defaultCredentialType(false), 'gcm');
  } finally {
    await server.close();
  }
});
