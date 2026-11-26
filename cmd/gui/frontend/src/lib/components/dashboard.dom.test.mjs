import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

const source = 'fixture'
const account = 'demo'
const target = 'example/actionable'
const saved = 'example/saved'
const row = { source, repo: target }
const fixtureToken = 'test-only-token'

function status(repo, state) {
  return {
    source, repo, state, account, path: `/fixture/${repo}`, ahead: 0,
    behind: state === 'behind' ? 2 : 0, modified: 0, untracked: 0, conflicts: 0,
  }
}

async function dashboard(t, global = {}) {
  const ui = await mount(t)
  const { App, render, fakeBridge, waitFor, window } = ui
  let rows = [status(target, 'behind'), status(saved, 'clean')]
  let revision = 1
  const snapshot = () => ({
    eventWatermark: revision === 1 ? 10 : 19,
    config: {
      version: 5, revision,
      global: { roots: ['/fixture'], language: 'en', collapsed: { view: 'attention' }, ...global },
      accounts: { [account]: {
        provider: 'github', url: 'https://example.invalid', username: 'demo',
        name: 'Demo User', email: 'demo@example.invalid', default_credential_type: 'token',
      } },
      sources: { [source]: {
        account, repos: Object.fromEntries(rows.map(({ repo }) => [repo, {}])),
        repoOrder: rows.map(({ repo }) => repo),
      } },
      mirrors: {},
    },
    nesting: {}, credentialFindings: [], configPath: '/fixture/config.json',
    appVersion: 'test', os: 'darwin', serverOS: 'darwin',
  })
  const initial = Promise.withResolvers()
  const reloadStatuses = Promise.withResolvers()
  let stored = Promise.withResolvers()
  let tokenStored = false
  const completed = started => native.calls.find(call => call.name === started)
  const native = fakeBridge(window, {
    MarkStartup: async () => {},
    GetWindowMode: async () => 'main',
    CheckUpdateRecovery: async () => '',
    GetConfigLoadError: async () => '',
    IsFirstRun: async () => false,
    GetStartupSnapshot: async () => revision === 1 ? initial.promise : snapshot(),
    BeginStatus: async () => {},
    RunTask: async task => {
      assert.equal(task, 'fetch-all')
      emit('fetch:allstart', 12, '8', null)
      emit('fetch:alldone', 13, '8', {})
      return 'started'
    },
    CheckGlobalIdentity: async () => ({ hasName: false, hasEmail: false }),
    IsGlobalGCMConfigNeeded: async () => false,
    GetCheckGlobalGitignore: async () => false,
    GetAutostart: async () => false,
    GetPRSettings: async () => ({ enabled: false, includeDrafts: false }),
    Reconcile: async () => ({ adopted: 0, relinked: 0 }),
    RepoNesting: async () => ({}),
    SetCollapsed: async () => {},
    SetUIMode: async () => {},
    SetShowPullPlan: async () => {},
    ListRemotes: async () => ({ remotes: [], remoteEditors: [] }),
    PreviewPullAll: async () => ({
      planID: 'fixture-plan', configRevision: revision, stale: false,
      willClone: [], willUpdate: [{ ...row, behind: 1 }],
      skipped: [{ source, repo: saved, reason: 'dirty' }], offline: [], repairsPending: [],
    }),
    PullAll: async () => {},
    StopTrackingRepo: async (givenSource, repo) => {
      assert.equal(givenSource, source)
      rows = rows.filter(item => item.repo !== repo)
      revision++
    },
    ReloadConfig: async () => snapshot().config,
    GetAllStatus: async () => revision === 1 ? rows : reloadStatuses.promise,
    GetRepoDetail: async () => ({ branch: 'fixture-branch', changed: [], untracked: [], incoming: [] }),
    GetIdentityProof: async () => ({ git: {} }),
    CredentialVerify: async () => tokenStored
      ? { primary: 'ok', pat: 'none', status: 'ok' }
      : { primary: 'none', pat: 'none', status: 'none' },
    DoctorPrecheck: async () => ({ ok: true }),
    ChangeCredentialType: async () => {},
    GetTokenGuide: async () => ({ creationURL: 'https://example.invalid/token' }),
    CredentialStoreToken: async () => {
      const result = await stored.promise
      tokenStored = result.ok
      return result
    },
  })
  function emit(name, sequence, operationID, payload, configRevision = revision) {
    native.emit(name, { schema: 1, sequence, operationID, configRevision, payload })
  }
  const app = render(App)
  await waitFor(() => assert.ok(completed('GetStartupSnapshot')))
  const releaseSnapshot = () => initial.resolve(snapshot())
  const releaseReloadStatuses = () => reloadStatuses.resolve(rows)
  function replyToStore(result) {
    stored.resolve(result)
    stored = Promise.withResolvers()
  }
  async function hydrate(beforeStatuses = () => {}) {
    releaseSnapshot()
    await waitFor(() => assert.ok(completed('BeginStatus')))
    beforeStatuses()
    const [passID] = completed('BeginStatus').args
    emit('status:batch', 11, '7', { passID, revision, items: rows, done: true })
    await waitFor(() => {
      assert.ok(completed('Reconcile'))
      assert.ok(native.calls.some(call => call.name === 'CheckUpdateRecovery' && call.args[0]))
    })
  }
  const finish = async () => {
    await app.unmount()
    assert.deepEqual(native.activeEvents(), [])
    const count = native.calls.length
    window.dispatchEvent(new window.Event('blur'))
    window.dispatchEvent(new window.Event('focus'))
    await ui.tick()
    assert.equal(native.calls.length, count, 'unmounted dashboard should not respond to window focus')
    assert.deepEqual(native.unexpected, [])
  }
  return { ...ui, native, emit, hydrate, finish, completed, replyToStore, releaseSnapshot, releaseReloadStatuses }
}

test('Needs attention hydrates without replaying an older clone completion', async t => {
  const { emit, hydrate, screen, within, waitFor, get, repoStates, finish } = await dashboard(t)
  // Give the completion a valid matching start. Without the snapshot watermark,
  // the queued pair would set the seeded row to error before the first scan.
  emit('clone:done', 9, '4', { ...row, error: 'stale clone failed' })
  emit('clone:start', 8, '4', row)
  await hydrate(() => assert.equal(get(repoStates)[`${source}/${target}`].status, 'unknown'))
  const main = within(screen.getByRole('main'))
  assert.ok(main.getByRole('heading', { name: 'Needs attention' }))
  assert.ok(main.getByText(target))
  assert.equal(main.queryByText(saved), null)
  assert.match(main.getByText(/Behind · ready to pull/).textContent, /· 1$/)

  // The same route accepts a fresh completion, then removes the row when the
  // host reports it healthy.
  emit('clone:start', 14, '9', row)
  emit('clone:done', 15, '9', { ...row, error: 'fresh clone failed' })
  await waitFor(() => assert.equal(get(repoStates)[`${source}/${target}`].error, 'fresh clone failed'))
  assert.equal(main.queryByText(/Behind · ready to pull/), null)
  assert.match(main.getByText('Needs attention', { selector: '.group-h' }).textContent, /· 1$/)
  emit('status:updated', 16, '10', [status(target, 'clean'), status(saved, 'clean')])
  assert.ok(await main.findByText('Nothing needs you'))
  assert.equal(main.queryByText(target), null)
  await finish()
})

test('a clone paused for safety explains the setting, where it lives and how to fix it (#344)', async t => {
  const { emit, hydrate, screen, within, waitFor, finish } = await dashboard(t)
  await hydrate()
  const main = within(screen.getByRole('main'))
  emit('fetch:allstart', 30, '20', null)
  emit('fetch:done', 31, '21', {
    ...row, error: 'git config: unsafe_config (core.hooksPath)', code: 'git.unsafe_config',
    unsafeConfig: { setting: 'core.hooksPath', scope: 'local' },
  })
  emit('fetch:alldone', 32, '20', { error: '1 repository fetches failed: git config: unsafe_config (core.hooksPath)' })
  assert.ok(await main.findByText('Gitbox paused this clone for safety'))
  assert.ok(main.getByText(/^The clone’s own config \(\.git\/config\) sets core\.hooksPath to a folder outside the clone/))
  assert.ok(main.getByText(/^Gitbox changed nothing\./))
  assert.equal(main.getByText(/--show-origin/).textContent, [
    'git -C "/fixture/example/actionable" config --show-origin --show-scope --get-all core.hooksPath',
    'git -C "/fixture/example/actionable" config --local --unset core.hooksPath',
  ].join('\n'))
  assert.ok(main.getByRole('button', { name: 'Copy commands' }))
  await waitFor(() => assert.ok(screen.getByText(/^1 skipped for safety: core\.hooksPath in their own Git config needs review/)))
  assert.equal(screen.queryByText(/unsafe_config/), null)
  await finish()
})

test('a pull completion cannot restore a row after Stop showing this project', async t => {
  const { emit, hydrate, screen, within, fireEvent, waitFor, get, repoStates, finish, completed, releaseReloadStatuses, tick } = await dashboard(t)
  await hydrate()
  await fireEvent.click(screen.getByTitle('Delete mode'))
  const main = within(screen.getByRole('main'))
  await fireEvent.click(main.getByRole('button', { name: 'Stop showing example/actionable' }))
  const dialog = within(screen.getByRole('dialog'))
  // Mark the operation in flight while the project still exists.
  emit('pull:start', 14, '9', row)
  await waitFor(() => assert.equal(get(repoStates)[`${source}/${target}`].status, 'syncing'))
  await fireEvent.click(dialog.getByRole('button', { name: 'Stop showing this project' }))
  await waitFor(() => {
    assert.deepEqual(completed('StopTrackingRepo')?.args, [source, target])
    assert.equal(main.queryByText(target), null)
    assert.ok(completed('GetAllStatus'))
  })

  // The new config is authoritative while the previous row is still cached.
  // Give the completion a sequence and revision admissible to the router so
  // the configured-repo check, rather than the event watermark, must reject it.
  assert.equal(get(repoStates)[`${source}/${target}`].status, 'syncing')
  emit('pull:done', 20, '9', { ...row, behindBefore: 2 })
  emit('status:one', 21, '9', status(target, 'dirty'))
  await tick()
  assert.equal(get(repoStates)[`${source}/${target}`].status, 'syncing')
  releaseReloadStatuses()
  await waitFor(() => {
    assert.ok(main.getByText('Nothing needs you'))
    assert.equal(get(repoStates)[`${source}/${target}`], undefined)
  })
  assert.equal(main.queryByText(target), null)
  await finish()
})

test('CredentialModal erases the token before closing and restores focus', async t => {
  const { hydrate, screen, within, fireEvent, waitFor, window, replyToStore, completed, native, finish } = await dashboard(t)
  await hydrate()
  await fireEvent.click(screen.getByRole('button', { name: /All repos/ }))
  const invoker = screen.getByRole('button', { name: 'Account actions' })
  invoker.focus()
  await fireEvent.click(invoker)
  const change = screen.getByText('Change credential…')
  change.focus()
  await fireEvent.click(change)
  const modal = screen.getByRole('dialog', { name: 'Change credential — demo' })
  const dialog = within(modal)
  await waitFor(() => assert.equal(window.document.activeElement, modal))
  await fireEvent.click(dialog.getByRole('button', { name: 'Setup' }))
  const input = await dialog.findByLabelText('Token', { selector: 'input' })
  await fireEvent.input(input, { target: { value: fixtureToken } })
  await fireEvent.click(dialog.getByRole('button', { name: 'Store token' }))
  assert.deepEqual(completed('CredentialStoreToken')?.args, [account, fixtureToken])
  assert.equal(input.value, fixtureToken, 'wait for a successful store before clearing')
  replyToStore({ ok: false, message: 'Storage unavailable' })
  assert.ok(await dialog.findByText('Storage unavailable'))
  assert.equal(input.value, fixtureToken)
  assert.equal(input.closest('[hidden]'), null)
  const retry = dialog.getByRole('button', { name: 'Store token' })
  assert.equal(retry.disabled, false)
  await fireEvent.input(input, { target: { value: 'corrected-test-token' } })
  await fireEvent.click(retry)
  assert.deepEqual(native.calls.filter(call => call.name === 'CredentialStoreToken').at(-1)?.args, [account, 'corrected-test-token'])
  replyToStore({ ok: true, message: 'Fixture credential accepted' })
  const done = await dialog.findByRole('button', { name: 'Done' })
  assert.equal(input.value, '')
  assert.equal(dialog.queryByLabelText('Token', { selector: 'input' }), null)
  done.focus()
  await fireEvent.click(done)
  await waitFor(() => assert.equal(screen.queryByRole('dialog'), null))
  assert.equal(window.document.activeElement, invoker)
  await finish()
})

test('the top bar changes experience without losing the selected view or scroll', async t => {
  const { hydrate, screen, within, fireEvent, waitFor, native, finish } = await dashboard(t)
  await hydrate()
  await fireEvent.click(screen.getByRole('button', { name: /All repos/ }))
  const main = screen.getByRole('main')
  // The local window is never tinted, and never asks for the tint (#331).
  const root = main.ownerDocument.documentElement
  assert.equal(root.dataset.remoteTint, undefined)
  assert.equal(root.style.getPropertyValue('--tint-top'), '')
  assert.equal(root.style.getPropertyValue('--tint-stripe'), '')
  assert.equal(root.style.getPropertyValue('--tint-chip-bg'), '')
  assert.equal(native.calls.filter(call => call.name === 'GetRemoteTint').length, 0)
  const page = within(main)
  await fireEvent.click(page.getByText(target))
  const details = await page.findByRole('region', { name: 'Project details' })
  await waitFor(() => assert.equal(details.getAttribute('aria-busy'), 'false'))
  const scroller = main.querySelector('.view-scroll')
  scroller.scrollTop = 127
  const mode = screen.getByRole('switch', { name: 'Experience' })
  for (const [checked, title] of [['false', /All projects/], ['true', /All repos/]]) {
    await fireEvent.click(mode)
    await waitFor(() => assert.equal(mode.getAttribute('aria-checked'), checked))
    assert.equal(screen.getByRole('button', { name: title }).getAttribute('aria-current'), 'true')
    assert.equal(main.querySelector('.view-scroll'), scroller)
    assert.equal(scroller.scrollTop, 127)
    assert.equal(page.getByRole('region', { name: 'Project details' }), details)
    assert.ok(page.getByText(checked === 'true' ? saved : 'saved'))
  }
  assert.deepEqual(native.calls.filter(call => call.name === 'SetUIMode').map(call => call.args[0]), ['simple', 'advanced'])
  await finish()
})

test('unmount cancels hydration while the startup snapshot is pending', async t => {
  const { releaseSnapshot, native, finish, tick } = await dashboard(t)
  assert.ok(native.activeEvents().includes('clone:done'))
  await finish()
  releaseSnapshot()
  await tick()
  assert.deepEqual(native.activeEvents(), [])
  assert.equal(native.calls.some(call => call.name === 'BeginStatus'), false)
})

test('Simple previews before updating, cancels without pulling, and reports a changed plan', async t => {
  const { hydrate, screen, within, fireEvent, completed, native, emit, finish } = await dashboard(t, { ui_mode: 'simple' })
  await hydrate()
  const launch = () => fireEvent.click(screen.getByTitle('Update all'))
  await launch()
  const dialog = await screen.findByRole('dialog', { name: 'Update plan' })
  assert.ok(within(dialog).getByText('I’ll update 1 project.'))
  assert.ok(within(dialog).getByText('I’ll leave 1 project with changes on this computer alone.'))
  assert.equal(within(dialog).queryByRole('table'), null)
  await fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  assert.equal(completed('PullAll'), undefined)

  await launch()
  assert.equal(native.calls.filter(call => call.name === 'PreviewPullAll').length, 2)
  await fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Go' }))
  const [run, planID] = completed('PullAll').args
  assert.equal(planID, 'fixture-plan')
  emit('pull:allstart', 15, '9', { run, planID, pulls: [row], clones: [] })
  emit('pull:done', 16, '10', { ...row, run, behindBefore: 2, skipped: 'dirty', state: 'dirty' })
  emit('pull:alldone', 17, '9', { run, planID, skippedSincePreview: 1 })
  assert.ok(await screen.findByText('1 skipped since preview'))
  await finish()
})

test('Advanced uses direct Pull all by default and can opt into the detailed plan', async t => {
  const { hydrate, screen, within, fireEvent, completed, native, emit, finish } = await dashboard(t)
  await hydrate()
  await fireEvent.click(screen.getByTitle('Pull all'))
  assert.equal(completed('PreviewPullAll'), undefined)
  const [run, planID] = completed('PullAll').args
  assert.equal(planID, '')
  emit('pull:allstart', 15, '9', { run, pulls: [row], clones: [] })
  emit('pull:alldone', 16, '9', { run })
  await fireEvent.click(screen.getByTitle('Settings'))
  await fireEvent.click(screen.getByText('Show plan before Pull all').parentElement.querySelectorAll('button')[1])
  assert.deepEqual(completed('SetShowPullPlan')?.args, ['on'])
  await fireEvent.click(screen.getByTitle('Pull all'))
  const dialog = await screen.findByRole('dialog', { name: 'Pull all plan' })
  assert.ok(within(dialog).getByRole('table'))
  assert.ok(within(dialog).getByText('Fast-forward (behind by 1)'))
  assert.ok(within(dialog).getByText('Skip · Local changes'))
  await fireEvent.keyDown(dialog, { key: 'Escape' })
  assert.equal(screen.queryByRole('dialog'), null)
  assert.equal(native.calls.filter(call => call.name === 'PullAll').length, 1)
  await finish()
})

test('Spanish Advanced previews one pending commit and reports one omitted project', async t => {
  const { hydrate, screen, within, fireEvent, completed, emit, finish } = await dashboard(t, { language: 'es', show_pull_plan: true })
  await hydrate()
  await fireEvent.click(screen.getByTitle('Pull de todo'))
  const dialog = within(await screen.findByRole('dialog', { name: 'Plan de Pull de todo' }))
  assert.ok(dialog.getByText('Avance rápido (commits pendientes: 1)'))
  await fireEvent.click(dialog.getByRole('button', { name: 'Continuar' }))
  const [run, planID] = completed('PullAll').args
  emit('pull:allstart', 15, '9', { run, planID, pulls: [row], clones: [] })
  emit('pull:done', 16, '10', { ...row, run, behindBefore: 1, skipped: 'dirty', state: 'dirty' })
  emit('pull:alldone', 17, '9', { run, planID, skippedSincePreview: 1 })
  assert.ok(await screen.findByText('Proyectos omitidos desde la vista previa: 1'))
  await finish()
})
