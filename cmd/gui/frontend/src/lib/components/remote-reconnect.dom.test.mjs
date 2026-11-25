import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

// A remote window whose link to the host's daemon drops in the middle of
// Fetch all (#247). The fixtures are fake: no host, no account, no token.
const source = 'fixture'
const account = 'demo'
const repos = ['example/one', 'example/two']
const alias = 'my-server'

function status(repo) {
  return {
    source, repo, state: 'clean', account, path: `/fixture/${repo}`, ahead: 0,
    behind: 0, modified: 0, untracked: 0, conflicts: 0,
  }
}

async function remoteWindow(t) {
  const ui = await mount(t)
  const { App, render, fakeBridge, waitFor, window } = ui
  let watermark = 10
  const snapshot = () => ({
    eventWatermark: watermark,
    config: {
      version: 5, revision: 1,
      global: { roots: ['/fixture'], language: 'en', collapsed: { view: 'attention' } },
      accounts: { [account]: {
        provider: 'github', url: 'https://example.invalid', username: 'demo',
        name: 'Demo User', email: 'demo@example.invalid', default_credential_type: 'token',
      } },
      sources: { [source]: {
        account, repos: Object.fromEntries(repos.map((repo) => [repo, {}])), repoOrder: repos,
      } },
      mirrors: {},
    },
    nesting: {}, credentialFindings: [], configPath: '/fixture/config.json',
    appVersion: 'test', os: 'darwin', serverOS: 'linux',
  })
  const connected = { state: 'connected', alias, serverOS: 'linux', serverArch: 'amd64', remoteEditors: [] }
  let fetchAll = Promise.withResolvers()
  const native = fakeBridge(window, {
    MarkStartup: async () => {},
    GetWindowMode: async () => 'remote',
    RemoteInfo: async () => connected,
    // The top bar asks whether remote mode is on before the window knows it is a remote one (#330).
    ListRemotes: async () => ({ enabled: true, current: alias, remotes: [{ alias, open: true, state: 'connected' }], remoteEditors: [] }),
    GetConfigLoadError: async () => '',
    IsFirstRun: async () => false,
    GetStartupSnapshot: async () => snapshot(),
    ReloadConfig: async () => snapshot().config,
    BeginStatus: async () => {},
    GetAllStatus: async () => repos.map(status),
    RefreshStatus: async () => { emit('status:updated', ++watermark, '30', repos.map(status)) },
    RunTask: async (task) => task === 'fetch-all' ? fetchAll.promise : 'finished',
    CheckGlobalIdentity: async () => ({ hasName: false, hasEmail: false }),
    IsGlobalGCMConfigNeeded: async () => false,
    GetCheckGlobalGitignore: async () => false,
    GetPRSettings: async () => ({ enabled: false, includeDrafts: false }),
    Reconcile: async () => ({ adopted: 0, relinked: 0 }),
    RepoNesting: async () => ({}),
    SetCollapsed: async () => {},
    CredentialVerify: async () => ({ primary: 'ok', pat: 'none', status: 'ok' }),
    GetLanguage: async () => 'en',
    GetUIMode: async () => 'advanced',
    GetOS: async () => 'linux',
    RemoteLog: async () => [],
    GetRemoteTint: async () => '#8b5cf6',
  })
  function emit(name, sequence, operationID, payload, client = false) {
    native.emit(name, { schema: 1, sequence, operationID, configRevision: 1, payload, ...(client ? { client: true } : {}) })
  }
  render(App)
  await waitFor(() => assert.ok(native.calls.some((call) => call.name === 'BeginStatus')))
  const [passID] = native.calls.find((call) => call.name === 'BeginStatus').args
  emit('status:batch', 11, '7', { passID, revision: 1, items: repos.map(status), done: true })
  await waitFor(() => assert.ok(native.calls.some((call) => call.name === 'RefreshStatus')))
  // The first connection's load is over once the actions come back.
  await waitFor(() => assert.equal(window.document.querySelector('.remote-offline-banner'), null))
  return {
    ...ui, native, emit, connected,
    fetchAllCalls: () => native.calls.filter((call) => call.name === 'RunTask' && call.args[0] === 'fetch-all').length,
    finishFetchAll: (result) => fetchAll.resolve(result),
    newFetchAll: () => { fetchAll = Promise.withResolvers() },
    setWatermark: (n) => { watermark = n },
  }
}

test('a resumed reconnect during Fetch all keeps the view and shows the completed run', async (t) => {
  const ui = await remoteWindow(t)
  const { screen, fireEvent, waitFor, emit, connected, fetchAllCalls, native, window } = ui
  // The startup fetch of the first connection ends.
  await waitFor(() => assert.equal(fetchAllCalls(), 1))
  emit('fetch:allstart', 12, '8', null)
  emit('fetch:alldone', 13, '8', {})
  ui.finishFetchAll('finished')
  await waitFor(() => assert.equal(screen.getByTitle('Fetch all').disabled, false))
  assert.equal(screen.queryByText('Fetch finished'), null)

  await fireEvent.click(screen.getByRole('button', { name: /All repos/ }))
  assert.equal(screen.getByRole('button', { name: /All repos/ }).getAttribute('aria-current'), 'true')
  ui.newFetchAll()
  await fireEvent.click(screen.getByTitle('Fetch all'))
  await waitFor(() => assert.equal(fetchAllCalls(), 2))
  emit('fetch:allstart', 14, '9', null)

  // The link drops: the window connects again, and the daemon replays
  // what the window missed before it says connected.
  emit('remote:state', 100, '40', { state: 'connecting', alias, attempt: 1, attempts: 3, remoteEditors: [] }, true)
  await waitFor(() => assert.ok(screen.getByText(/attempt 1 of 3/)))
  for (const [i, repo] of repos.entries()) emit('fetch:done', 15 + i, String(10 + i), { source, repo })
  emit('fetch:alldone', 17, '9', {})
  ui.setWatermark(20)
  const reconnected = native.calls.length
  emit('remote:state', 101, '41', { ...connected, resumed: true }, true)
  ui.finishFetchAll('finished')

  // The replayed run ends with its result card.
  await waitFor(() => assert.ok(screen.getByText('Fetch finished')))
  assert.ok(screen.getByText('2 clones fetched'))
  assert.equal(screen.queryByText(/Not connected to the host/), null)
  // Status is read again; nothing else is reloaded, and the view stays.
  const sinceReconnect = () => native.calls.slice(reconnected).map((call) => call.name)
  await waitFor(() => assert.ok(sinceReconnect().includes('RefreshStatus')))
  const reload = sinceReconnect().slice(0, sinceReconnect().indexOf('RefreshStatus'))
  assert.ok(!['IsFirstRun', 'BeginStatus'].some((name) => reload.includes(name)), `the resumed reconnect reloaded everything: ${reload}`)
  await waitFor(() => assert.equal(window.document.querySelector('.remote-offline-banner'), null))
  assert.equal(screen.getByRole('button', { name: /All repos/ }).getAttribute('aria-current'), 'true', 'the view changed')
  assert.equal(fetchAllCalls(), 2, 'the reconnect started another fetch-all')
  assert.deepEqual(native.unexpected, [])
})

test('a reconnect that cannot resume reloads everything but keeps the view', async (t) => {
  const ui = await remoteWindow(t)
  const { screen, fireEvent, waitFor, emit, connected, native, window } = ui
  await fireEvent.click(screen.getByRole('button', { name: /All repos/ }))
  const snapshots = () => native.calls.filter((call) => call.name === 'GetStartupSnapshot').length
  const before = snapshots()

  emit('remote:state', 100, '40', { state: 'connecting', alias, attempt: 1, attempts: 3, remoteEditors: [] }, true)
  await waitFor(() => assert.ok(screen.getByText(/attempt 1 of 3/)))
  emit('remote:state', 101, '41', connected, true)

  await waitFor(() => assert.equal(snapshots(), before + 1))
  await waitFor(() => assert.ok(native.calls.filter((call) => call.name === 'BeginStatus').length >= 2))
  const [passID] = native.calls.findLast((call) => call.name === 'BeginStatus').args
  emit('status:batch', 30, '50', { passID, revision: 1, items: repos.map(status), done: true })
  await waitFor(() => assert.equal(window.document.querySelector('.remote-offline-banner'), null))
  assert.equal(screen.getByRole('button', { name: /All repos/ }).getAttribute('aria-current'), 'true', 'the view changed')
  assert.deepEqual(native.unexpected, [])
})

// A remote window tints its top bar and sidebar with this computer's color,
// and follows a new one without reconnecting (#331).
test('a remote window paints its tint and follows a new color', async (t) => {
  const ui = await remoteWindow(t)
  const { waitFor, emit, native, window } = ui
  const root = window.document.documentElement
  await waitFor(() => assert.equal(root.dataset.remoteTint, '#8b5cf6'))
  assert.match(root.style.getPropertyValue('--tint-top'), /^#[0-9a-f]{6}$/)
  assert.match(root.style.getPropertyValue('--tint-side'), /^#[0-9a-f]{6}$/)
  assert.ok(window.document.querySelector('.topbar'), 'the top bar is there to be tinted')
  assert.ok(window.document.querySelector('.side'), 'the sidebar is there to be tinted')
  // The title still names the host.
  assert.ok(ui.screen.getAllByText(alias).length >= 1)
  const before = root.style.getPropertyValue('--tint-top')
  emit('remote:tint', 40, '41', '#22c55e', true)
  await waitFor(() => assert.equal(root.dataset.remoteTint, '#22c55e'))
  assert.notEqual(root.style.getPropertyValue('--tint-top'), before)
  assert.equal(native.calls.filter((c) => c.name === 'RemoteReconnect').length, 0, 'no reconnect')
})
