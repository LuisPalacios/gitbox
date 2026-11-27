import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

// A remote window on a host that has no gitbox.json yet (#349): it runs the
// first-run onboarding on the host. The fixtures are fake: no host, no
// account, no token.
const alias = 'my-server'
const hostRoot = '/home/demo/00.git'

async function firstRunWindow(t) {
  const ui = await mount(t)
  const { App, render, fakeBridge, waitFor, window } = ui
  let root = ''
  const snapshot = () => ({
    eventWatermark: 10,
    config: {
      version: 5, revision: 1,
      global: { roots: [root], language: 'en', collapsed: { view: 'attention' } },
      accounts: {}, sources: {}, mirrors: {},
    },
    nesting: {}, credentialFindings: [], configPath: '/home/demo/.config/gitbox/gitbox.json',
    appVersion: 'test', os: 'darwin', serverOS: 'linux',
  })
  const connected = { state: 'connected', alias, serverOS: 'linux', serverArch: 'amd64', remoteEditors: [] }
  // RemoteInfo answers the last state, as the window's session does.
  let current = connected
  const native = fakeBridge(window, {
    MarkStartup: async () => {},
    GetWindowMode: async () => 'remote',
    RemoteInfo: async () => current,
    RemoteLog: async () => [],
    ListRemotes: async () => ({ current: alias, remotes: [{ alias, open: true, state: 'connected' }], remoteEditors: [] }),
    GetRemoteTint: async () => '#8b5cf6',
    GetConfigLoadError: async () => '',
    // The host has no config until the folder step saves one.
    IsFirstRun: async () => !root,
    GetLanguage: async () => 'en',
    // Paths resolve on the host.
    PreviewRoot: async (path) => ({
      abs: path || hostRoot, exists: false, isDir: false, writable: true, existingClones: 0, sampled: false,
    }),
    CheckGlobalIdentity: async () => ({ name: '', email: '' }),
    InitRoot: async (folder) => { root = folder },
    GetStartupSnapshot: async () => snapshot(),
    BeginStatus: async () => {},
    RunTask: async () => 'finished',
    GetAllStatus: async () => [],
    RefreshStatus: async () => {},
    Reconcile: async () => ({ adopted: 0, relinked: 0 }),
    IsGlobalGCMConfigNeeded: async () => false,
    GetCheckGlobalGitignore: async () => false,
    GetPRSettings: async () => ({ enabled: false, includeDrafts: false }),
    RepoNesting: async () => ({}),
    SetCollapsed: async () => {},
    GetUIMode: async () => 'simple',
    GetOS: async () => 'linux',
  })
  function emit(name, sequence, operationID, payload, client = false) {
    if (name === 'remote:state') current = payload
    native.emit(name, { schema: 1, sequence, operationID, configRevision: 1, payload, ...(client ? { client: true } : {}) })
  }
  render(App)
  await waitFor(() => assert.ok(ui.screen.getByText(`Set up gitbox on ${alias}`)))
  return { ...ui, native, emit, connected, calls: (name) => native.calls.filter((call) => call.name === name) }
}

test('a host without gitbox.json gets the first-run onboarding, saved on the host', async (t) => {
  const ui = await firstRunWindow(t)
  const { screen, fireEvent, waitFor, native, emit, calls } = ui
  assert.ok(screen.getByText(`Where do you keep your projects on ${alias}?`))
  assert.equal(screen.queryByText(/has no gitbox\.json/), null, 'the blocking card is back')
  // A remote window has no folder dialog: the folder is typed for the host.
  assert.equal(screen.queryByRole('button', { name: 'Browse' }), null)
  await waitFor(() => assert.ok(screen.getByText(hostRoot)))

  await waitFor(() => assert.equal(screen.getByRole('button', { name: 'Download a project' }).disabled, false))
  await fireEvent.click(screen.getByRole('button', { name: 'Download a project' }))
  await waitFor(() => assert.deepEqual(calls('InitRoot').map((call) => call.args), [[hostRoot]]))

  // Skipping the account loads the host's data over the same connection.
  await fireEvent.click(await screen.findByRole('button', { name: 'Skip for now' }))
  await waitFor(() => assert.equal(calls('BeginStatus').length, 1))
  const [passID] = calls('BeginStatus')[0].args
  emit('status:batch', 11, '7', { passID, revision: 1, items: [], done: true })
  await waitFor(() => assert.ok(ui.window.document.querySelector('.topbar')))
  assert.equal(screen.queryByText(`Set up gitbox on ${alias}`), null)
  assert.equal(calls('RemoteReconnect').length, 0, 'onboarding reconnected')
  assert.deepEqual(native.unexpected, [])
})

test('a disconnect during onboarding shows the connection card and saves nothing', async (t) => {
  const ui = await firstRunWindow(t)
  const { screen, waitFor, native, emit, connected, calls } = ui
  emit('remote:state', 100, '40', { state: 'connecting', alias, attempt: 1, attempts: 3, remoteEditors: [] }, true)
  await waitFor(() => assert.ok(screen.getByText(`Connecting to ${alias}…`)))
  assert.equal(screen.queryByText(`Set up gitbox on ${alias}`), null)

  // Even a connection that resumes the daemon asks the host again whether
  // it has a config, and the onboarding comes back.
  const asked = calls('IsFirstRun').length
  emit('remote:state', 101, '41', { ...connected, resumed: true }, true)
  await waitFor(() => assert.ok(screen.getByText(`Set up gitbox on ${alias}`)))
  assert.ok(calls('IsFirstRun').length > asked, 'the reconnect did not ask the host again')
  assert.equal(calls('InitRoot').length, 0)
  assert.deepEqual(native.unexpected, [])
})
