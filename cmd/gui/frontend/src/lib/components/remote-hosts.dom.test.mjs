import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

// The host switcher and the Remote hosts window (#330). The hosts are
// fixtures: no ssh, no server.
const hosts = [
  { alias: 'my-server', open: true, state: 'connected', serverOS: 'linux', serverArch: 'amd64', serverVersion: 'v2.9.0', lastConnected: '2026-11-24T17:30:00Z' },
  { alias: 'nas', open: false },
]
const ok = async () => ({})

function bridge(ui, extra = {}) {
  return ui.fakeBridge(ui.window, {
    ListRemotes: async () => ({ remotes: hosts, remoteEditors: ['code'] }),
    GetRemoteTint: async () => '#3b82f6',
    ...extra,
  })
}

const called = (native, name) => native.calls.filter((c) => c.name === name).map((c) => c.args)
const menuItems = (screen) => screen.getAllByRole('menuitem').map((el) => el.textContent.replace(/\s+/g, ' ').trim())

test('the local switcher lists only the added hosts and opens them or the Remote hosts window', async (t) => {
  const ui = await mount(t)
  const { HostSwitcher, render, screen, fireEvent, waitFor } = ui
  const native = bridge(ui, { ConnectRemote: ok, OpenRemotesWindow: ok })
  render(HostSwitcher, { props: { state: null, simple: false } })

  const trigger = screen.getByRole('button', { name: /This computer/ })
  await fireEvent.click(trigger)
  await waitFor(() => assert.ok(screen.getByRole('menu')))
  const items = menuItems(screen)
  assert.equal(items.length, 4, items.join(' | '))
  assert.match(items[0], /This computer.*This window/)
  assert.match(items[1], /my-server.*connected/)
  assert.match(items[2], /nas.*No window open/)
  assert.match(items[3], /Manage remote hosts/)
  assert.ok(!items.some((i) => /add|connect to another|log/i.test(i)), 'the switcher adds no host and has no log here')

  await fireEvent.click(screen.getByRole('menuitem', { name: /nas/ }))
  await waitFor(() => assert.deepEqual(called(native, 'ConnectRemote'), [['nas']]))
  await waitFor(() => assert.equal(screen.queryByRole('menu'), null))

  await fireEvent.click(trigger)
  await waitFor(() => assert.ok(screen.getByRole('menu')))
  await fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
  await waitFor(() => assert.equal(screen.queryByRole('menu'), null))
  assert.equal(ui.window.document.activeElement, trigger, 'Escape gives the focus back to the trigger')

  await fireEvent.click(trigger)
  await waitFor(() => assert.ok(screen.getByRole('menu')))
  await fireEvent.click(screen.getByRole('menuitem', { name: /Manage remote hosts/ }))
  await waitFor(() => assert.equal(called(native, 'OpenRemotesWindow').length, 1))
  assert.deepEqual(native.unexpected, [])
})

test('the switcher of a remote window shows its host, goes back to this computer and opens the log', async (t) => {
  const ui = await mount(t)
  const { HostSwitcher, render, screen, fireEvent, waitFor } = ui
  const native = bridge(ui, {
    ListRemotes: async () => ({ current: 'my-server', remotes: hosts, remoteEditors: [] }),
    ShowLocalWindow: ok,
    RemoteLog: async () => ['connect: my-server'],
  })
  const state = { state: 'connected', alias: 'my-server', serverOS: 'linux', remoteEditors: [] }
  render(HostSwitcher, { props: { state, simple: false } })

  const trigger = screen.getByRole('button', { name: /my-server \(linux\): connected/ })
  assert.match(trigger.textContent, /my-server\s*linux/)
  await fireEvent.click(trigger)
  await waitFor(() => assert.ok(screen.getByRole('menu')))
  const items = menuItems(screen)
  assert.match(items[1], /my-server.*This window/)
  assert.ok(items.some((i) => /Show connection log/.test(i)))

  await fireEvent.click(screen.getByRole('menuitem', { name: /This computer/ }))
  await waitFor(() => assert.equal(called(native, 'ShowLocalWindow').length, 1))

  // The Remote hosts window asks this window for its log.
  native.emit('remote:show-log', { schema: 1, sequence: 1, operationID: '1', configRevision: 1, payload: null, client: true })
  await waitFor(() => assert.equal(called(native, 'RemoteLog').length, 1))
  await waitFor(() => assert.ok(screen.getByText('connect: my-server')))
  assert.deepEqual(native.unexpected, [])
})

test('the Remote hosts window keeps every remote action and suggests ssh aliases', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor, within } = ui
  let uninstalls = 0
  // The ssh lines arrive late: the configured hosts never wait for them.
  const sshRead = Promise.withResolvers()
  const native = bridge(ui, {
    ListSSHHosts: async () => {
      await sshRead.promise
      return [
        { alias: 'build-box', names: ['build-box', 'build-box.example.org', '192.0.2.10'] },
        { alias: 'nas-backup', names: ['nas-backup'] },
      ]
    },
    ConnectRemote: ok, ShowRemoteLog: ok, SetRemoteEditor: ok, RemoveRemote: ok, AddRemote: ok,
    RemoteUninstall: async () => (++uninstalls === 1
      ? { needsConfirm: true, otherClients: 2, done: false, rootKept: false }
      : { needsConfirm: false, otherClients: 0, done: true, rootKept: false }),
  })
  render(RemoteHostsWindow)

  await waitFor(() => assert.ok(screen.getByRole('heading', { name: 'my-server' })))
  assert.ok(screen.getByText('Reading your ssh configuration…'))
  sshRead.resolve()
  await waitFor(() => assert.equal(screen.queryByText('Reading your ssh configuration…'), null))
  assert.ok(screen.getByText('linux/amd64'))
  assert.ok(screen.getByText('v2.9.0'))
  const list = within(screen.getByRole('listbox'))
  assert.equal(list.getAllByRole('option').length, 2)

  await fireEvent.click(screen.getByRole('button', { name: 'Go to its window' }))
  await waitFor(() => assert.deepEqual(called(native, 'ConnectRemote'), [['my-server']]))
  await fireEvent.click(screen.getByRole('button', { name: 'Connection log' }))
  await waitFor(() => assert.deepEqual(called(native, 'ShowRemoteLog'), [['my-server']]))

  const editor = screen.getByRole('combobox')
  editor.value = ''
  await fireEvent.change(editor)
  await waitFor(() => assert.deepEqual(called(native, 'SetRemoteEditor'), [['my-server', '']]))

  // Uninstall and Remove are not on the details: they live in the context
  // menu of the list, each with what it does.
  assert.equal(screen.queryByRole('button', { name: 'Uninstall' }), null)
  assert.equal(screen.queryByRole('button', { name: 'Remove' }), null)
  await fireEvent.contextMenu(list.getByRole('option', { name: /my-server/ }))
  const menu = within(await waitFor(() => screen.getByRole('menu', { name: 'Actions for my-server' })))
  const uninstallItem = menu.getByRole('menuitem', { name: /Uninstall the gitbox server/ })
  assert.match(uninstallItem.textContent, /Deletes the gitbox server installed on my-server\. Its repositories and its gitbox\.json stay/)
  assert.match(menu.getByRole('menuitem', { name: /Remove from this list/ }).textContent, /Nothing changes on the host/)

  // Uninstall asks first, then again when other clients still use the server.
  await fireEvent.click(uninstallItem)
  assert.equal(screen.queryByRole('menu'), null, 'the menu closes')
  assert.deepEqual(called(native, 'RemoteUninstall'), [], 'nothing runs before the answer')
  const confirm = () => screen.getByRole('button', { name: 'Uninstall' })
  await fireEvent.click(confirm())
  await waitFor(() => assert.deepEqual(called(native, 'RemoteUninstall'), [['my-server', false]]))
  await waitFor(() => assert.ok(screen.getByText(/2 other/)))
  await fireEvent.click(confirm())
  await waitFor(() => assert.deepEqual(called(native, 'RemoteUninstall').at(-1), ['my-server', true]))

  // A host without an open window has no log to show.
  await fireEvent.click(list.getByRole('option', { name: /nas/ }))
  await waitFor(() => assert.ok(screen.getByRole('heading', { name: 'nas' })))
  assert.equal(screen.getByRole('button', { name: 'Connection log' }).disabled, true)
  assert.ok(screen.getByRole('button', { name: 'Connect' }))
  await fireEvent.contextMenu(list.getByRole('option', { name: /nas/ }))
  const nasMenu = within(await waitFor(() => screen.getByRole('menu', { name: 'Actions for nas' })))
  assert.equal(nasMenu.getByRole('menuitem', { name: 'Connection log' }).disabled, true)
  await fireEvent.click(nasMenu.getByRole('menuitem', { name: /Remove from this list/ }))
  // Remove asks first too: Cancel forgets nothing.
  assert.ok(screen.getByText('Remove nas from this list? Nothing changes on the host.'))
  await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  assert.equal(screen.queryByText(/Remove nas from this list/), null)
  assert.deepEqual(called(native, 'RemoveRemote'), [])
  await fireEvent.contextMenu(list.getByRole('option', { name: /nas/ }))
  await fireEvent.click(within(await waitFor(() => screen.getByRole('menu'))).getByRole('menuitem', { name: /Remove from this list/ }))
  await fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
  await waitFor(() => assert.deepEqual(called(native, 'RemoveRemote'), [['nas']]))

  // One line per Host entry, with every name on it; choosing it fills the
  // first name. Add runs the usual check.
  const sshList = within(screen.getByRole('list', { name: 'From your ssh configuration' }))
  const line = sshList.getByRole('button', { name: 'build-box build-box.example.org 192.0.2.10' })
  assert.equal(line.title, 'build-box build-box.example.org 192.0.2.10')
  assert.equal(ui.window.getComputedStyle(line).whiteSpace, 'nowrap', 'a line never wraps')
  await fireEvent.click(line)
  const alias = screen.getByRole('textbox', { name: 'ssh alias' })
  assert.equal(alias.value, 'build-box')
  await fireEvent.click(screen.getByRole('button', { name: 'Add' }))
  await waitFor(() => assert.deepEqual(called(native, 'AddRemote'), [['build-box', '']]))

  // Typing completes inline and filters the lines; deleting never completes.
  alias.value = 'na'
  await fireEvent.input(alias, { inputType: 'insertText', data: 'a' })
  assert.equal(alias.value, 'nas-backup')
  assert.equal(alias.selectionStart, 2)
  assert.equal(alias.selectionEnd, 'nas-backup'.length)
  assert.deepEqual(sshList.getAllByRole('button').map((b) => b.textContent.trim()), ['nas-backup'])
  await fireEvent.keyDown(alias, { key: 'Tab' })
  assert.equal(alias.selectionStart, 'nas-backup'.length)
  alias.value = 'nas-backu'
  await fireEvent.input(alias, { inputType: 'deleteContentBackward' })
  assert.equal(alias.value, 'nas-backu')
  alias.value = 'zz'
  await fireEvent.input(alias, { inputType: 'insertText', data: 'z' })
  assert.equal(alias.value, 'zz')
  assert.ok(sshList.getByText('No line of your ssh configuration matches.'))
  assert.deepEqual(native.unexpected, [])
})

// Several hosts selected with Ctrl-click (⌘ on a Mac) or Shift-click share
// one context menu: its Uninstall and Remove act on all of them.
test('the Remote hosts window uninstalls and removes several hosts at once', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor, within } = ui
  const three = [{ alias: 'alpha', open: false }, { alias: 'beta', open: false }, { alias: 'gamma', open: false }]
  const native = bridge(ui, {
    ListRemotes: async () => ({ remoteEditors: [], remotes: three }),
    ListSSHHosts: async () => [],
    RemoteUninstall: async (alias, force) => (alias === 'beta' && !force
      ? { needsConfirm: true, otherClients: 1, done: false, rootKept: false }
      : { needsConfirm: false, otherClients: 0, done: true, rootKept: false }),
    RemoveRemote: ok,
  })
  render(RemoteHostsWindow)
  const list = within(await waitFor(() => screen.getByRole('listbox')))
  await waitFor(() => assert.equal(list.getAllByRole('option').length, 3))
  assert.ok(screen.getByText(/Right-click a host for more/))
  await fireEvent.click(list.getByRole('option', { name: /alpha/ }))
  await fireEvent.click(list.getByRole('option', { name: /gamma/ }), { shiftKey: true })
  await waitFor(() => assert.ok(screen.getByRole('heading', { name: '3 hosts selected' })))
  await fireEvent.click(list.getByRole('option', { name: /beta/ }), { ctrlKey: true })
  await waitFor(() => assert.ok(screen.getByRole('heading', { name: '2 hosts selected' })))
  assert.deepEqual(list.getAllByRole('option').map((o) => o.getAttribute('aria-selected')), ['true', 'false', 'true'])

  await fireEvent.contextMenu(list.getByRole('option', { name: /gamma/ }))
  const menu = within(await waitFor(() => screen.getByRole('menu', { name: 'Actions for 2 hosts' })))
  assert.equal(menu.queryByRole('menuitem', { name: 'Connect' }), null, 'one host actions stay out')
  assert.match(menu.getByRole('menuitem', { name: /Uninstall/ }).textContent, /these 2 hosts/)
  await fireEvent.click(menu.getByRole('menuitem', { name: /Remove from this list/ }))
  assert.ok(screen.getByText('Remove alpha, gamma from this list? Nothing changes on those hosts.'))
  assert.deepEqual(called(native, 'RemoveRemote'), [], 'nothing runs before the answer')
  await fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
  await waitFor(() => assert.deepEqual(called(native, 'RemoveRemote'), [['alpha'], ['gamma']]))
  await waitFor(() => assert.ok(screen.getByText('alpha, gamma removed.')))

  // A right-click outside the selection selects that host alone; Uninstall
  // asks a second time only for the hosts where other windows use it.
  await fireEvent.click(list.getByRole('option', { name: /alpha/ }))
  await fireEvent.click(list.getByRole('option', { name: /beta/ }), { ctrlKey: true })
  await fireEvent.contextMenu(list.getByRole('option', { name: /beta/ }))
  await fireEvent.click(within(await waitFor(() => screen.getByRole('menu'))).getByRole('menuitem', { name: /Uninstall/ }))
  assert.ok(screen.getByText('Remove gitbox from alpha, beta? Your repositories and settings there stay.'))
  await fireEvent.click(screen.getByRole('button', { name: 'Uninstall' }))
  await waitFor(() => assert.deepEqual(called(native, 'RemoteUninstall'), [['alpha', false], ['beta', false]]))
  await waitFor(() => assert.ok(screen.getByText(/beta: 1 other window/)))
  await fireEvent.click(await waitFor(() => screen.getByRole('button', { name: 'Uninstall' })))
  await waitFor(() => assert.deepEqual(called(native, 'RemoteUninstall').at(-1), ['beta', true]))
  await waitFor(() => assert.equal(screen.queryByRole('button', { name: 'Uninstall' }), null))

  // Escape closes the menu without acting.
  await fireEvent.contextMenu(list.getByRole('option', { name: /gamma/ }))
  const open = await waitFor(() => screen.getByRole('menu', { name: 'Actions for gamma' }))
  await fireEvent.keyDown(open, { key: 'Escape' })
  assert.equal(screen.queryByRole('menu'), null)
  assert.deepEqual(native.unexpected, [])
})

// The connection card of a remote window keeps Connect and the log, and
// no Uninstall: that lives in the Remote hosts window's context menu.
test('the connection card of a remote window offers no uninstall', async (t) => {
  const ui = await mount(t)
  const { RemoteConnected, render, screen, waitFor } = ui
  const native = bridge(ui, {
    RemoteInfo: async () => ({ state: 'failed', alias: 'nas', hint: 'error.remote.ssh_timeout', remoteEditors: [] }),
    RemoteLog: async () => [],
  })
  render(RemoteConnected, { props: { simple: true } })
  await waitFor(() => assert.ok(screen.getByRole('heading', { name: /nas/ })))
  assert.ok(screen.getByRole('button', { name: 'Connect' }))
  assert.ok(screen.getByRole('button', { name: 'Show log' }))
  assert.equal(screen.queryByRole('button', { name: /Uninstall/ }), null)
  assert.deepEqual(native.unexpected, [])
})

// The color of remote windows: six presets, a reset to blue and a preview
// that follows the choice (#331).
test('the Remote hosts window chooses the color of remote windows', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor, within } = ui
  let saved = '#3b82f6'
  const native = bridge(ui, {
    ListSSHHosts: async () => [],
    GetRemoteTint: async () => saved,
    SetRemoteTint: async (c) => (c === '#f43f5e' ? { error: 'no', hint: 'error.remote.tint_unsupported' } : ((saved = c), {})),
  })
  render(RemoteHostsWindow)
  const group = within(await waitFor(() => screen.getByRole('radiogroup', { name: 'Remote window color' })))
  const swatches = group.getAllByRole('radio')
  assert.deepEqual(swatches.map((s) => s.getAttribute('aria-label')), ['Blue', 'Teal', 'Violet', 'Amber', 'Rose', 'Green'])
  await waitFor(() => assert.equal(group.getByRole('radio', { name: 'Blue' }).getAttribute('aria-checked'), 'true'))
  assert.equal(screen.getByRole('button', { name: 'Reset to blue' }).disabled, true)
  const preview = screen.getByRole('img', { name: 'Preview of a remote window with this color' })
  const top = () => preview.querySelector('.pv-top').getAttribute('style')
  const blueTop = top()

  await fireEvent.click(group.getByRole('radio', { name: 'Violet' }))
  await waitFor(() => assert.deepEqual(called(native, 'SetRemoteTint'), [['#8b5cf6']]))
  assert.equal(group.getByRole('radio', { name: 'Violet' }).getAttribute('aria-checked'), 'true')
  assert.notEqual(top(), blueTop, 'the preview follows the choice')
  // The preview shows the stripe and the alias badge in the color (#338).
  // The DOM writes the colors back as rgb(): violet, and its badge shade in
  // light (#8558ec) or dark (#8659ed).
  assert.match(preview.querySelector('.pv-stripe').getAttribute('style'), /background: rgb\(139, 92, 246\)/)
  assert.match(preview.querySelector('.pv-chip').getAttribute('style'), /background: rgb\((133, 88, 236|134, 89, 237)\)/)
  assert.equal(screen.getByRole('button', { name: 'Reset to blue' }).disabled, false)

  // A refused color puts the saved one back and says why.
  await fireEvent.click(group.getByRole('radio', { name: 'Rose' }))
  await waitFor(() => assert.ok(screen.getByText('That color is not one of the remote window colors.')))
  assert.equal(group.getByRole('radio', { name: 'Violet' }).getAttribute('aria-checked'), 'true')

  await fireEvent.click(screen.getByRole('button', { name: 'Reset to blue' }))
  await waitFor(() => assert.deepEqual(called(native, 'SetRemoteTint').at(-1), ['#3b82f6']))
  assert.equal(top(), blueTop)
  assert.deepEqual(native.unexpected, [])
})

// After Connect the window reads the states every second, so the host's
// window that opens and a login waiting for 1Password show here too.
test('the Remote hosts window shows a login that waits for approval', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor } = ui
  let opened = false
  const native = bridge(ui, {
    ListRemotes: async () => ({ remoteEditors: [], remotes: [
      opened ? { alias: 'nas', open: true, state: 'connecting', waiting: '1password' } : { alias: 'nas', open: false },
    ] }),
    ListSSHHosts: async () => [],
    // The window comes up after the reload that follows Connect.
    ConnectRemote: async () => { setTimeout(() => { opened = true }, 300); return {} },
  })
  render(RemoteHostsWindow)
  await waitFor(() => assert.ok(screen.getByRole('heading', { name: 'nas' })))
  await fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
  await waitFor(() => assert.ok(screen.getByText(/1Password is waiting for your approval/)), { timeout: 3000 })
  assert.ok(screen.getAllByText('waiting for your approval').length >= 1)
  assert.deepEqual(native.unexpected, [])
})

// An uninstall whose login waits for 1Password says so with the host while
// it waits, and its login limit ends it with the approval hint (#336).
test('the Remote hosts window shows an uninstall that waits for approval and its timeout', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor, within } = ui
  let finish
  const native = bridge(ui, {
    ListRemotes: async () => ({ remoteEditors: [], remotes: [{ alias: 'nas', open: false }] }),
    ListSSHHosts: async () => [],
    RemoteUninstall: () => new Promise((resolve) => { finish = resolve }),
  })
  render(RemoteHostsWindow)
  const list = within(await waitFor(() => screen.getByRole('listbox')))
  await fireEvent.contextMenu(await waitFor(() => list.getByRole('option', { name: /nas/ })))
  await fireEvent.click(within(await waitFor(() => screen.getByRole('menu'))).getByRole('menuitem', { name: /Uninstall/ }))
  await fireEvent.click(screen.getByRole('button', { name: 'Uninstall' }))
  await waitFor(() => assert.deepEqual(called(native, 'RemoteUninstall'), [['nas', false]]))

  native.emit('remote:waiting', { schema: 1, sequence: 1, operationID: '1', configRevision: 1, payload: { alias: 'nas', waiting: '1password' }, client: true })
  await waitFor(() => assert.ok(screen.getByText(/^nas: .*1Password is waiting for your approval/)))

  finish({ error: 'ssh did not log in to nas within 1m15s; 1Password may be waiting for approval', hint: 'error.remote.ssh_login' })
  await waitFor(() => assert.ok(screen.getByText(/^nas: SSH did not log in in time\. .*1Password/)))
  assert.deepEqual(native.unexpected, [])
})

// After an uninstall the host keeps its place in the list, and its card
// says the server is not installed instead of the old version (#336). A
// host that never connected still reads as unknown.
test('the Remote hosts window shows a host without a server as not installed', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor, within } = ui
  let removed = false
  const native = bridge(ui, {
    ListRemotes: async () => ({ remoteEditors: [], remotes: [
      { alias: 'nas', open: false, serverOS: 'linux', serverArch: 'amd64', lastConnected: '2026-11-24T17:30:00Z', ...(removed ? {} : { serverVersion: 'v2.8.1' }) },
      { alias: 'fresh', open: false },
    ] }),
    ListSSHHosts: async () => [],
    RemoteUninstall: async () => { removed = true; return { needsConfirm: false, otherClients: 0, done: true, rootKept: false } },
  })
  render(RemoteHostsWindow)
  const list = within(await waitFor(() => screen.getByRole('listbox')))
  await fireEvent.click(await waitFor(() => list.getByRole('option', { name: /nas/ })))
  await waitFor(() => assert.ok(screen.getByText('v2.8.1')))
  await fireEvent.contextMenu(list.getByRole('option', { name: /nas/ }))
  await fireEvent.click(within(await waitFor(() => screen.getByRole('menu'))).getByRole('menuitem', { name: /Uninstall/ }))
  await fireEvent.click(screen.getByRole('button', { name: 'Uninstall' }))
  await waitFor(() => assert.ok(screen.getByText('gitbox was removed from nas.')))
  await waitFor(() => assert.ok(screen.getByText('Not installed')))
  assert.equal(list.getAllByRole('option').length, 2, 'the host stays in the list')
  await fireEvent.click(list.getByRole('option', { name: /fresh/ }))
  await waitFor(() => assert.ok(screen.getAllByText('Known after it connects').length >= 1))
  assert.equal(screen.queryByText('Not installed'), null)
  assert.deepEqual(native.unexpected, [])
})

// The hosts are managed only from the switcher: Settings has no Remote hosts
// row in either window, and does not even ask whether remote mode is on.
for (const remote of [false, true]) {
  test(`the Settings of a ${remote ? 'remote' : 'local'} window have no Remote hosts row`, async (t) => {
    const ui = await mount(t)
    const { SettingsPanel, render, screen, remoteWindow } = ui
    const native = bridge(ui, { GetCheckGlobalGitignore: async () => false, GetPRSettings: async () => ({ enabled: false, includeDrafts: false }) })
    remoteWindow.set(remote)
    const noop = () => {}
    const later = async () => {}
    render(SettingsPanel, {
      props: {
        showSettings: true, onRecipeImported: noop, simple: true, configPath: '/fixture/gitbox.json', languageChoice: 'en',
        themeChoice: 'system', fetchInterval: 'off', lastFetchTime: '', autostartSupported: false, autostartOn: false,
        checkGitignorePref: false, doctorSummary: '', terminalsManagerOpening: false, appVersion: 'test',
        openRoots: noop, setLanguage: later, setTheme: noop, setFetchInterval: noop, toggleAutostart: later,
        setPRBadgesEnabled: later, setPRIncludeDrafts: later, setCheckGitignorePref: later, openDoctorModal: noop,
        openTerminalsManagerWindow: later,
      },
    })
    assert.ok(screen.getByText('Version'), 'the panel rendered')
    await new Promise((resolve) => setTimeout(resolve, 50))
    assert.equal(screen.queryByText('Remote hosts'), null)
    assert.equal(called(native, 'ListRemotes').length, 0)
    assert.equal(called(native, 'OpenRemotesWindow').length, 0)
  })
}
