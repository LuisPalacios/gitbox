import assert from 'node:assert/strict'
import { mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, join, relative, resolve } from 'node:path'
import { after, test } from 'node:test'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { build } from 'vite'

// Each session keeps its own store bundle (#381). The shared exports of
// lib/stores.ts, the notices and the translator follow activeSession, so a
// component reads the session it shows. The modules are bundled with vite
// like the controller tests; each test imports its own copy.
const root = fileURLToPath(new URL('../../../', import.meta.url))
const lib = fileURLToPath(new URL('../', import.meta.url))
const src = join(lib, '..')
const scratch = await mkdtemp(join(root, 'node_modules', '.bundle-test-'))
after(() => rm(scratch, { recursive: true, force: true }))
const entry = join(scratch, 'entry.ts')
await writeFile(entry, [
  `export * as shared from ${JSON.stringify(join(lib, 'stores.ts'))}`,
  `export { notices, pushNotice, dismissNotice } from ${JSON.stringify(join(lib, 'notices.ts'))}`,
  `export { t } from ${JSON.stringify(join(lib, 'i18n.ts'))}`,
  `export { activeSession, defaultBundle } from ${JSON.stringify(join(lib, 'session/active.ts'))}`,
  `export { createStoreBundle } from ${JSON.stringify(join(lib, 'session/bundle.ts'))}`,
  `export { get } from 'svelte/store'`,
].join('\n'))
const bundle = await build({
  configFile: false, root, logLevel: 'error',
  build: { write: false, minify: false, lib: { entry, formats: ['es'] }, rolldownOptions: { output: { codeSplitting: false } } },
})
const { output } = Array.isArray(bundle) ? bundle[0] : bundle
const bundled = join(scratch, 'bundle.mjs')
await writeFile(bundled, output.find(file => file.type === 'chunk').code)
let cases = 0
const load = () => import(`${pathToFileURL(bundled).href}?case=${++cases}`)

const isStore = value => !!value && typeof value.subscribe === 'function'

// proxies maps each store of a bundle to the shared export that follows it.
function proxies(m) {
  const map = {}
  for (const [name, value] of Object.entries(m.createStoreBundle())) {
    if (!isStore(value)) continue
    map[name] = name === 'notices' ? m.notices : m.shared[name]
  }
  return map
}

function fill(bundle, label) {
  bundle.configStore.set({
    version: 5, revision: label === 'A' ? 1 : 2,
    global: { roots: [`/${label}`] },
    accounts: { [`acct${label}`]: { provider: 'github', url: 'https://example.invalid', username: label } },
    sources: { [`src${label}`]: { account: `acct${label}`, repos: { [`repo${label}`]: {} } } },
    mirrors: { [`mirror${label}`]: { account_src: `acct${label}`, account_dst: `acct${label}`, repos: { [`repo${label}`]: {} } } },
  })
  bundle.repoStates.set({ [`src${label}/repo${label}`]: { status: label === 'A' ? 'clean' : 'dirty', progress: 0, behind: 0, modified: 1, untracked: 0, ahead: 0 } })
  bundle.applyMirrorStatusResults([{ mirrorKey: `mirror${label}`, repoKey: `repo${label}`, error: label === 'A' ? '' : 'down' }])
  bundle.applyPRUpdate({ accountKey: `acct${label}`, byRepo: { [`repo${label}`]: { authored: [], reviewRequested: [] } } })
  bundle.prSettings.set({ enabled: label === 'A', includeDrafts: label === 'B' })
  bundle.workspacePlaceholders.set({ [`src${label}/repo${label}`]: [{ name: label, path: `/${label}` }] })
  bundle.workspaceMembers.set(new Set([`member${label}`]))
  bundle.workspaceUnknown.set(new Set([`unknown${label}`]))
  bundle.workspaceDiagnostics.set({ [label]: { parent: label } })
  bundle.remoteWindow.set(label === 'B')
  bundle.remoteHost.set(label === 'B' ? { editors: ['code'], editor: 'code' } : null)
  bundle.remoteTextHost.set(label === 'B' ? 'host-b' : null)
  bundle.pushNotice('error', `notice ${label}`)
}

test('every store of a bundle has a shared proxy, and no proxy can be written', async () => {
  const m = await load()
  const map = proxies(m)
  assert.ok(Object.keys(map).length >= 20, 'positive control: the bundle holds the session stores')
  for (const [name, proxy] of Object.entries(map)) {
    assert.ok(isStore(proxy), `${name} has a shared proxy`)
    assert.equal(proxy.set, undefined, `${name} has no set`)
    assert.equal(proxy.update, undefined, `${name} has no update`)
  }
  for (const name of ['applyStatusResults', 'applyMirrorStatusResults', 'applyPRUpdate']) {
    assert.equal(m.shared[name], undefined, `${name} writes a bundle, not the shared stores`)
  }
})

test('switching activeSession moves every proxy to the other bundle, with nothing left from the first', async () => {
  const m = await load()
  const a = m.defaultBundle
  const b = m.createStoreBundle()
  fill(a, 'A')
  fill(b, 'B')
  const map = proxies(m)
  // Live subscriptions, as components hold them.
  const seen = {}
  const offs = Object.entries(map).map(([name, proxy]) => proxy.subscribe(value => { seen[name] = value }))
  const say = () => m.get(m.t)('menu.delete')
  const follows = (bundle, label) => {
    for (const name of Object.keys(map)) {
      assert.deepEqual(seen[name], m.get(bundle[name]), `${name} follows ${label}`)
    }
  }

  assert.equal(m.get(m.activeSession), a, 'the default session is active at first')
  follows(a, 'A')
  assert.equal(m.get(m.shared.accounts).acctA.username, 'A', 'positive control: A shows')
  assert.equal(say(), 'Delete clone folder…')

  m.activeSession.set(b)
  follows(b, 'B')
  const shown = JSON.stringify(Object.keys(map).map(name => [name, seen[name] instanceof Set ? [...seen[name]] : seen[name]]))
  assert.doesNotMatch(shown, /acctA|srcA|repoA|mirrorA|memberA|unknownA|notice A/, 'nothing of A shows')
  assert.equal(seen.remoteWindow, true)
  assert.equal(seen.summary.wip + seen.summary.total > 0, true)
  assert.equal(say(), 'Delete clone folder on host-b…', 'the texts name the active host')
  assert.deepEqual(m.get(m.notices).map(n => n.title), ['notice B'])

  // A keeps working in the background without reaching the shell.
  a.repoStates.set({ 'srcA/other': { status: 'clean', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0 } })
  a.pushNotice('info', 'background A')
  follows(b, 'B')
  m.pushNotice('info', 'shown in B')
  assert.deepEqual(m.get(b.notices).map(n => n.title), ['notice B', 'shown in B'], 'the shell pushes to the active session')
  assert.deepEqual(m.get(a.notices).map(n => n.title), ['notice A', 'background A'])

  m.activeSession.set(a)
  follows(a, 'A')
  assert.ok(m.get(m.shared.repoStates)['srcA/other'], 'A shows its background change when it is active again')
  assert.doesNotMatch(JSON.stringify(seen.configStore) + JSON.stringify(seen.notices), /acctB|notice B|shown in B/)
  assert.equal(say(), 'Delete clone folder…')

  for (const off of offs) off()
  for (const bundle of [a, b]) for (const n of m.get(bundle.notices)) bundle.dismissNotice(n.id)
})

// ── Nothing writes through a proxy ──
const STORES = resolve(lib, 'stores.ts')
const NOTICES = resolve(lib, 'notices.ts')
// The exports that follow the active session; the process-wide ones
// (themeStore, statusClock, activeSession) are not proxies.
const PROXIED = new Set(Object.keys(proxies(await load())))

// proxyImports returns the local names a file gives the shared proxies it
// imports from lib/stores.ts and lib/notices.ts.
function proxyImports(file, code) {
  const names = []
  for (const match of code.matchAll(/import\s+(type\s+)?\{([^}]*)\}\s*from\s*['"]([^'"]+)['"]/g)) {
    if (match[1] || !match[3].startsWith('.')) continue
    const target = resolve(dirname(file), match[3])
    const fromStores = target === STORES || target + '.ts' === STORES
    const fromNotices = target === NOTICES || target + '.ts' === NOTICES
    if (!fromStores && !fromNotices) continue
    for (const part of match[2].split(',')) {
      const [imported, local = imported] = part.trim().replace(/^type\s+/, '').split(/\s+as\s+/).map(s => s.trim())
      if (!imported || part.trim().startsWith('type ')) continue
      if (PROXIED.has(imported)) names.push(local)
    }
  }
  return names
}

function proxyWrites(file, code) {
  const found = []
  for (const name of proxyImports(file, code)) {
    const id = name.replace(/\$/g, '\\$')
    const patterns = [
      new RegExp(`(?<![\\w$.])${id}\\s*\\.\\s*(set|update)\\s*\\(`),
      new RegExp(`(?<![\\w$.])\\$${id}\\s*(\\[[^\\]]*\\]|\\.[\\w$]+)*\\s*(=(?![=>])|\\+=|-=|\\+\\+|--)`),
      new RegExp(`bind:[\\w-]+\\s*=\\s*\\{\\s*\\$${id}\\b`),
    ]
    for (const pattern of patterns) if (pattern.test(code)) found.push(`${relative(src, file)}: ${name}`)
  }
  return found
}

async function sources(dir) {
  const files = []
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) files.push(...await sources(path))
    else if (/\.(ts|svelte)$/.test(entry.name) && !entry.name.endsWith('.d.ts')) files.push(path)
  }
  return files
}

test('nothing writes through a shared proxy', async () => {
  const fixture = join(lib, 'modals', 'Fixture.svelte')
  for (const [code, what] of [
    ["import { repoStates } from '../stores';\nrepoStates.update(s => s)", 'an update'],
    ["import { configStore as cfg } from '../stores';\n$cfg = next", 'an aliased $store assignment'],
    ["import { prSettings } from '../stores';\n$prSettings.enabled = false", 'a field assignment'],
    ["import { remoteWindow } from '../stores';\n<Toggle bind:on={$remoteWindow} />", 'a binding'],
    ["import { notices } from '../notices';\nnotices.set([])", 'the notices proxy'],
  ]) {
    assert.equal(proxyWrites(fixture, code).length, 1, `positive control: ${what} is found`)
  }
  assert.deepEqual(proxyWrites(fixture, "import { repoStates, themeStore } from '../stores';\nconst s = get(activeSession);\ns.repoStates.set({});\nif ($repoStates === x) {}\nthemeStore.set('dark')"), [],
    'a write to a captured bundle, a comparison and a process-wide store are fine')

  const files = await sources(src)
  assert.ok(files.some(file => file.endsWith('App.svelte')), 'positive control: the scan reaches App.svelte')
  let importers = 0
  const writes = []
  for (const file of files) {
    const code = await readFile(file, 'utf8')
    if (proxyImports(file, code).length) importers++
    writes.push(...proxyWrites(file, code))
  }
  assert.ok(importers > 30, `positive control: ${importers} files import the shared stores`)
  assert.deepEqual(writes, [])
})
