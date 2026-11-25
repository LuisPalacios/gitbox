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
    ListRemotes: async () => ({ enabled: true, remotes: hosts, remoteEditors: ['code'] }),
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
    ListRemotes: async () => ({ enabled: true, current: 'my-server', remotes: hosts, remoteEditors: [] }),
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
    ListRemotes: async () => ({ enabled: true, remoteEditors: [], remotes: three }),
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

// After Connect the window reads the states every second, so the host's
// window that opens and a login waiting for 1Password show here too.
test('the Remote hosts window shows a login that waits for approval', async (t) => {
  const ui = await mount(t)
  const { RemoteHostsWindow, render, screen, fireEvent, waitFor } = ui
  let opened = false
  const native = bridge(ui, {
    ListRemotes: async () => ({ enabled: true, remoteEditors: [], remotes: [
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
