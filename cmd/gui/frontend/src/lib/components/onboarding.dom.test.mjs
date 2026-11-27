import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

// Step 1 of the first-run onboarding in this computer's window (#355): the
// guided setup and the Skip that opens gitbox. The fixtures are fake: no
// account, no token.
const root = '/fixture/00.git'

async function firstRun(t, { preview = {}, initRoot } = {}) {
  const ui = await mount(t)
  const { App, render, fakeBridge, waitFor, window } = ui
  let saved = ''
  const snapshot = () => ({
    eventWatermark: 10,
    config: {
      version: 5, revision: 1,
      global: { roots: [saved], language: 'en', ui_mode: 'simple', collapsed: { view: 'attention' } },
      accounts: {}, sources: {}, mirrors: {},
    },
    nesting: {}, credentialFindings: [], configPath: '/fixture/config.json',
    appVersion: 'test', os: 'darwin', serverOS: 'darwin',
  })
  const native = fakeBridge(window, {
    MarkStartup: async () => {},
    GetWindowMode: async () => 'main',
    CheckUpdateRecovery: async () => '',
    GetConfigLoadError: async () => '',
    IsFirstRun: async () => !saved,
    GetLanguage: async () => 'en',
    DoctorPrecheck: async () => ({ ok: true }),
    PreviewRoot: async (path) => ({
      abs: path || root, exists: false, isDir: false, writable: true, existingClones: 0, sampled: false, ...preview,
    }),
    CheckGlobalIdentity: async () => ({ name: '', email: '' }),
    InitRoot: initRoot ?? (async (folder) => { saved = folder }),
    GetStartupSnapshot: async () => snapshot(),
    BeginStatus: async () => {},
    RunTask: async () => 'finished',
    IsGlobalGCMConfigNeeded: async () => false,
    GetCheckGlobalGitignore: async () => false,
    GetPRSettings: async () => ({ enabled: false, includeDrafts: false }),
    RepoNesting: async () => ({}),
    SetCollapsed: async () => {},
    GetUIMode: async () => 'simple',
    GetOS: async () => 'darwin',
  })
  render(App)
  await waitFor(() => assert.ok(ui.screen.getByText('Welcome to gitbox')))
  const calls = (name) => native.calls.filter((call) => call.name === name)
  return { ...ui, native, calls }
}

const guided = (screen) => screen.getByRole('button', { name: 'Guided setup' })
const skip = (screen) => screen.getByRole('button', { name: 'Skip and open gitbox' })

test('step 1 offers the guided setup, says what it covers, and goes on to connect an account', async (t) => {
  const { screen, fireEvent, waitFor, native, calls } = await firstRun(t)
  assert.ok(screen.getByText('Connect an account and get your first project.'))
  assert.equal(screen.queryByRole('button', { name: 'Download a project' }), null)
  assert.equal(screen.queryByRole('button', { name: 'Use an existing project' }), null)
  await waitFor(() => assert.equal(guided(screen).disabled, false))
  await fireEvent.click(guided(screen))
  await waitFor(() => assert.ok(screen.getByText('Connect where your projects are stored')))
  assert.deepEqual(calls('InitRoot').map((call) => call.args), [[root]])
  assert.equal(calls('GetStartupSnapshot').length, 0, 'the guide opened the dashboard')
  assert.deepEqual(native.unexpected, [])
})

test('a root that holds clones still shows a single guided button', async (t) => {
  const { screen, waitFor, native } = await firstRun(t, { preview: { exists: true, isDir: true, existingClones: 3 } })
  await waitFor(() => assert.ok(screen.getByText('Existing projects found: 3.')))
  assert.equal(screen.getAllByRole('button', { name: 'Guided setup' }).length, 1)
  assert.equal(screen.queryByRole('button', { name: 'Use an existing project' }), null)
  assert.deepEqual(native.unexpected, [])
})

test('Skip saves the root and opens gitbox, with Connect as the way back to the guide', async (t) => {
  const { screen, fireEvent, waitFor, within, native, calls } = await firstRun(t)
  await waitFor(() => assert.equal(skip(screen).disabled, false))
  await fireEvent.click(skip(screen))
  await waitFor(() => assert.ok(screen.getByText('No accounts yet.')))
  assert.deepEqual(calls('InitRoot').map((call) => call.args), [[root]])
  assert.equal(screen.queryByText('Connect where your projects are stored'), null, 'Skip went through the guide')
  assert.equal(screen.queryByText('Welcome to gitbox'), null)
  // Connect an account (Simple) or Add account (Advanced) opens the guided connection.
  const empty = screen.getByText('No accounts yet.').closest('.empty')
  assert.ok(within(empty).getByRole('button', { name: /^(Connect an account|Add account)$/ }), 'no way back to the guided connection')
  assert.deepEqual(native.unexpected, [])
})

test('an unwritable folder disables both choices', async (t) => {
  const { screen, waitFor, native } = await firstRun(t, { preview: { writable: false } })
  await waitFor(() => assert.ok(screen.getByText('Choose a writable folder on this computer.')))
  assert.equal(guided(screen).disabled, true)
  assert.equal(skip(screen).disabled, true)
  assert.deepEqual(native.unexpected, [])
})

test('a failed save stays on step 1 and explains the failure', async (t) => {
  const failing = await firstRun(t, { initRoot: async () => { throw new Error('config.save_failed') } })
  const { screen, fireEvent, waitFor, native, calls } = failing
  await waitFor(() => assert.equal(skip(screen).disabled, false))
  await fireEvent.click(skip(screen))
  await waitFor(() => assert.equal(calls('InitRoot').length, 1))
  await waitFor(() => assert.equal(skip(screen).disabled, false))
  assert.ok(screen.getByText('Welcome to gitbox'), 'a failed save left step 1')
  assert.equal(calls('GetStartupSnapshot').length, 0, 'a failed save opened the dashboard')
  assert.ok(failing.window.document.querySelector('.ob-card .explanation'), 'the failure is not explained')
  assert.deepEqual(native.unexpected, [])
})
