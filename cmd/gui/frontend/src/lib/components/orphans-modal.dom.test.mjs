import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

// The Unmatched clones list (#351): every row says what I can do with it,
// and every row, ignored ones included, opens its folder like a clone row.
const row = (name, reason, extra = {}) => ({
  path: `/roots/scratch/${name}`, relPath: `scratch/${name}`, remoteURL: '', repoKey: '', matchedAccount: '',
  reason, nested: false, ...extra,
})
const hostOnly = row('home', 'host-only', { repoKey: 'me/home', remoteURL: 'https://github.com/me/home.git', matchedAccount: 'github-me', candidates: ['github-me'], host: 'github.com' })
const unknownHost = row('lab', 'unknown-host', { repoKey: 'team/lab', remoteURL: 'https://git.example.org/team/lab.git', host: 'git.example.org' })
const worktree = row('home-feature', 'worktree', { mainClone: '/roots/scratch/home' })
const tracked = row('copy', 'already-tracked', { repoKey: 'me/tracked', candidates: ['github-me'], trackedPath: '/roots/github-me/me/tracked' })
const noRemote = row('local', 'no-remote')
const profiles = [
  { id: 'hidden', name: 'Hidden', terminal: 'Terminal', shell: '', hidden: true },
  { id: 'wez', name: 'WezTerm', terminal: 'WezTerm', shell: 'zsh', default: true },
]

const called = (native, name) => native.calls.filter((c) => c.name === name).map((c) => c.args)

async function open(t, props, responses = {}) {
  const ui = await mount(t)
  const native = ui.fakeBridge(ui.window, { OpenInExplorer: async () => {}, OpenProfile: async () => {}, ...responses })
  const added = []
  const view = ui.render(ui.OrphansModal, {
    props: {
      unmatched: [], ignored: [], skipped: [], applyReconcile: async () => {}, revealLabel: 'Reveal in Finder', profiles,
      addAccountFor: (host) => added.push(host), ...props,
    },
  })
  view.component.showOrphanModal()
  await ui.waitFor(() => assert.ok(ui.screen.getByRole('dialog')))
  return { ui, native, added, view }
}

const card = (ui, name) => {
  const el = ui.screen.getByText(name, { selector: '.clone-name' }).closest('.clone')
  assert.ok(el, `no row for ${name}`)
  return ui.within(el)
}

test('each reason says what to do next, and only adoptable rows offer Adopt', async (t) => {
  const { ui, added } = await open(t, { unmatched: [hostOnly, unknownHost, worktree, tracked, noRemote] })
  const { screen, fireEvent } = ui
  assert.match(screen.getByText(/Adopt one into an account/).textContent, /or ignore it/)

  const home = card(ui, 'me/home')
  assert.ok(home.getByRole('button', { name: 'Adopt' }))
  assert.ok(home.getByRole('combobox'))
  assert.equal(home.queryByRole('button', { name: 'Add account' }), null)

  const lab = card(ui, 'team/lab')
  assert.equal(lab.queryByRole('button', { name: 'Adopt' }), null)
  assert.ok(lab.getByText('No account exists for git.example.org. Add one, and this clone can be adopted into it.'))
  await fireEvent.click(lab.getByRole('button', { name: 'Add account' }))
  assert.deepEqual(added, ['git.example.org'])

  const tree = card(ui, 'scratch/home-feature')
  assert.equal(tree.queryByRole('button', { name: 'Adopt' }), null)
  assert.ok(tree.getByText(/isn't adopted on its own\. Adopt its main clone: \/roots\/scratch\/home$/))

  const copy = card(ui, 'me/tracked')
  assert.equal(copy.queryByRole('button', { name: 'Adopt' }), null, 'a tracked key is never adopted twice, even with candidates')
  assert.ok(copy.getByText('gitbox already tracks this repository at /roots/github-me/me/tracked. Ignore this extra copy.'))

  const local = card(ui, 'scratch/local')
  assert.ok(local.getByText(/without a remote can't be adopted\. Add one in a terminal with git remote add origin <url>/))
  assert.equal(local.queryByRole('button', { name: 'Add account' }), null)
})

test('the intro offers adopting only when a visible row can be adopted', async (t) => {
  const { ui, view } = await open(t, { unmatched: [worktree, noRemote], ignored: [hostOnly] })
  const { screen, fireEvent, waitFor } = ui
  assert.ok(screen.getByText(/Each one says what you can do next, or you can ignore it\./))
  assert.equal(screen.queryByText(/Adopt one into an account/), null)

  // The adoptable ignored row counts once the ignored rows show.
  await fireEvent.click(screen.getByRole('button', { name: 'Show ignored (1)' }))
  await waitFor(() => assert.ok(screen.getByText(/Adopt one into an account/)))
  await fireEvent.click(screen.getByRole('button', { name: 'Hide ignored' }))

  // A worktree or tracked row whose details are unknown still says what to do.
  await view.rerender({ unmatched: [row('w2', 'worktree'), row('c2', 'already-tracked'), row('u2', 'unknown-host')], ignored: [] })
  await waitFor(() => assert.ok(screen.getByText(/Adopt its main clone instead\./)))
  assert.ok(screen.getByText(/at another path\. Ignore this extra copy\./))
  assert.ok(screen.getByText(/No account exists for this server\./))
})

test('a row whose account now exists offers Adopt without reopening the list', async (t) => {
  const { ui, view } = await open(t, { unmatched: [unknownHost] })
  const { screen, waitFor } = ui
  assert.equal(screen.queryByRole('button', { name: 'Adopt' }), null)
  await view.rerender({ unmatched: [{ ...unknownHost, reason: 'host-only', matchedAccount: 'gitea-team', candidates: ['gitea-team'] }] })
  await waitFor(() => assert.ok(screen.getByRole('button', { name: 'Adopt' })))
  assert.equal(screen.getByRole('combobox').value, 'gitea-team')
  assert.equal(screen.queryByRole('button', { name: 'Add account' }), null)
  assert.ok(screen.getByRole('dialog'), 'the list stayed open')
})

test('every row, ignored ones included, opens its folder and a terminal there', async (t) => {
  const { ui, native } = await open(t, { unmatched: [noRemote], ignored: [worktree] })
  const { screen, fireEvent, waitFor } = ui

  const local = card(ui, 'scratch/local')
  await fireEvent.click(local.getByRole('button', { name: 'Reveal in Finder' }))
  await fireEvent.click(local.getByRole('button', { name: 'Open with WezTerm' }))
  await waitFor(() => assert.deepEqual(called(native, 'OpenInExplorer'), [[noRemote.path]]))
  await waitFor(() => assert.deepEqual(called(native, 'OpenProfile'), [[noRemote.path, 'wez']]))

  await fireEvent.click(screen.getByRole('button', { name: 'Show ignored (1)' }))
  const tree = card(ui, 'scratch/home-feature')
  assert.ok(tree.getByRole('button', { name: 'Stop ignoring' }))
  await fireEvent.click(tree.getByRole('button', { name: 'Reveal in Finder' }))
  await fireEvent.click(tree.getByRole('button', { name: 'Open with WezTerm' }))
  await waitFor(() => assert.deepEqual(called(native, 'OpenInExplorer').at(-1), [worktree.path]))
  await waitFor(() => assert.deepEqual(called(native, 'OpenProfile').at(-1), [worktree.path, 'wez']))
  assert.deepEqual(native.unexpected, [])
})

test('in a remote window the folder button is off and the terminal opens on the host', async (t) => {
  const { ui, native } = await open(t, { unmatched: [noRemote] })
  const { fireEvent, waitFor, tick } = ui
  ui.remoteHost.set({ editors: [], editor: '' })
  await tick()
  const local = card(ui, 'scratch/local')
  const folder = local.getByRole('button', { name: 'Reveal in Finder' })
  assert.equal(folder.getAttribute('aria-disabled'), 'true')
  await fireEvent.click(folder)
  await fireEvent.click(local.getByRole('button', { name: 'Open with WezTerm' }))
  await waitFor(() => assert.deepEqual(called(native, 'OpenProfile'), [[noRemote.path, 'wez']]))
  assert.deepEqual(called(native, 'OpenInExplorer'), [], 'the folder button stays off on a remote host')
})

test('without a terminal profile a row shows the folder button alone', async (t) => {
  const { ui } = await open(t, { unmatched: [noRemote], profiles: [] })
  const local = card(ui, 'scratch/local')
  assert.ok(local.getByRole('button', { name: 'Reveal in Finder' }))
  assert.equal(local.queryByRole('button', { name: /Open with/ }), null)
})
