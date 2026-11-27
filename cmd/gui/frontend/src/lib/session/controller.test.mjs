import assert from 'node:assert/strict'
import { readFile, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { after, test } from 'node:test'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { build } from 'vite'

// The controller imports the shared stores, the translator and Svelte, so
// it is bundled with vite like the DOM tests, and each test imports its own
// copy: fresh stores, notices and router per case. The fixtures are fake:
// no host, no account, no token.
const root = fileURLToPath(new URL('../../../', import.meta.url))
const lib = fileURLToPath(new URL('../', import.meta.url))
const scratch = await mkdtemp(join(root, 'node_modules', '.session-test-'))
after(() => rm(scratch, { recursive: true, force: true }))
const entry = join(scratch, 'entry.ts')
await writeFile(entry, [
  `export { createSessionController } from ${JSON.stringify(join(lib, 'session/controller.ts'))}`,
  `export { createEventRouter } from ${JSON.stringify(join(lib, 'events.ts'))}`,
  `export { configStore, repoStates, mirrorStates, workspaceUnknown, prsByAccount } from ${JSON.stringify(join(lib, 'stores.ts'))}`,
  `export { notices, dismissNotice } from ${JSON.stringify(join(lib, 'notices.ts'))}`,
  `export { statusPaintQueue } from ${JSON.stringify(join(lib, 'status.ts'))}`,
  `export { get } from 'svelte/store'`,
].join('\n'))
const bundle = await build({
  configFile: false, root, logLevel: 'error',
  build: { write: false, minify: false, lib: { entry, formats: ['es'] }, rolldownOptions: { output: { codeSplitting: false } } },
})
const { output } = Array.isArray(bundle) ? bundle[0] : bundle
const bundled = join(scratch, 'session.mjs')
await writeFile(bundled, output.find(file => file.type === 'chunk').code)
let cases = 0

const source = 'fixture'
const one = 'example/one'
const two = 'example/two'
const keyOne = `${source}/${one}`
const keyTwo = `${source}/${two}`
const account = { provider: 'github', url: 'https://example.invalid', username: 'demo', default_credential_type: 'token' }

function status(repo, state = 'clean', extra = {}) {
  return { source, repo, state, account: 'demo', path: `/fixture/${repo}`, ahead: 0, behind: 0, modified: 0, untracked: 0, conflicts: 0, ...extra }
}

async function session(t, { bridge = {}, host = {} } = {}) {
  const m = await import(`${pathToFileURL(bundled).href}?case=${++cases}`)
  const listeners = new Map()
  const events = m.createEventRouter((name, handler) => {
    const group = listeners.get(name) ?? new Set()
    listeners.set(name, group)
    group.add(handler)
    return () => {
      group.delete(handler)
      if (!group.size) listeners.delete(name)
    }
  })
  const calls = []
  const fake = new Proxy({}, {
    get(_target, name) {
      return async (...args) => {
        calls.push({ name, args })
        if (!bridge[name]) throw new Error(`unexpected bridge call ${String(name)}`)
        return bridge[name](...args)
      }
    },
  })
  const seen = []
  const record = name => (...args) => { seen.push([name, ...args]) }
  const hostCalls = {
    simple: () => false,
    serverOS: () => 'linux',
    stream: () => null,
    markStartup: record('markStartup'),
    finishTiming: record('finishTiming'),
    restartDashboard: async () => { seen.push(['restartDashboard']) },
    refreshDetail: record('refreshDetail'),
    applyReconcile: async res => { seen.push(['applyReconcile', res]) },
    discoverDone: record('discoverDone'),
    quit: record('quit'),
    ...host,
  }
  const controller = m.createSessionController({ bridge: fake, events, host: hostCalls })
  m.configStore.set({
    version: 5, revision: 1,
    global: { roots: ['/fixture'], editors: [] },
    accounts: { demo: account },
    sources: { [source]: { account: 'demo', repos: { [one]: {}, [two]: {} } } },
    mirrors: { backup: { account_src: 'demo', account_dst: 'demo', repos: { [one]: { direction: 'push', origin: 'src' } } } },
  })
  const row = { status: 'clean', progress: 0, behind: 0, modified: 0, untracked: 0, ahead: 0, path: '/fixture/x' }
  m.repoStates.set({ [keyOne]: { ...row }, [keyTwo]: { ...row } })
  events.snapshot(10, () => m.get(m.configStore)?.revision ?? 0)
  let sequence = 10
  function emit(name, operationID, payload, extra = {}) {
    const envelope = { schema: 1, sequence: ++sequence, operationID: String(operationID), configRevision: 1, payload, ...extra }
    for (const handler of listeners.get(name) ?? []) handler(envelope)
  }
  t.after(() => {
    controller.destroy()
    for (const notice of m.get(m.notices)) m.dismissNotice(notice.id)
  })
  return { ...m, controller, emit, calls, seen, listeners, value: store => m.get(store) }
}

const settle = () => new Promise(resolve => setTimeout(resolve, 0))

test('listen subscribes the session events and destroy leaves none', async t => {
  const s = await session(t)
  assert.equal(s.listeners.size, 0, 'nothing is subscribed before listen')
  s.controller.listen()
  s.controller.watchProfiles(() => {})
  s.controller.watchRemoteState(() => {})
  const names = [...s.listeners.keys()].sort()
  assert.equal(names.length, 33)
  for (const name of ['status:batch', 'fetch:alldone', 'pull:done', 'mirror:setup:done', 'update:quit', 'pr:refreshed', 'profiles:reloaded', 'remote:state']) {
    assert.ok(names.includes(name), name)
  }
  s.controller.destroy()
  assert.deepEqual([...s.listeners.keys()], [])
})

test('App.svelte leaves every event subscription to the session', async () => {
  const app = await readFile(new URL('../../App.svelte', import.meta.url), 'utf8')
  assert.ok(app.includes('session.listen()'), 'positive control: the window starts the session listeners')
  assert.doesNotMatch(app, /\bonEvent\(|\bevents\.on\(/)
})

test('Fetch all tracks its repos, ignores older work and leaves a result card', async t => {
  const s = await session(t, { bridge: { runTask: async () => 'started' } })
  s.controller.listen()
  assert.equal(await s.controller.runFetchAll(true), true)
  assert.equal(s.value(s.controller.fetchingAll), true)
  assert.equal(await s.controller.runFetchAll(true), false, 'a second manual run joins the one going')
  s.emit('fetch:allstart', 8, null)
  assert.deepEqual([...s.value(s.controller.fetchRun).keys].sort(), [keyOne, keyTwo])
  s.emit('fetch:start', 9, { source, repo: one })
  assert.deepEqual(s.value(s.controller.fetchingRepos), { [keyOne]: true })
  s.emit('fetch:done', 7, { source, repo: one })
  assert.deepEqual(s.value(s.controller.fetchingRepos), { [keyOne]: true }, 'an older fetch does not end the newer one')
  s.emit('fetch:done', 9, { source, repo: one })
  s.emit('fetch:done', 12, { source, repo: two, error: 'refused', code: 'git.offline' })
  assert.deepEqual(s.value(s.controller.fetchingRepos), {})
  assert.equal(s.value(s.repoStates)[keyTwo].failedOperation, 'fetch')
  s.emit('fetch:alldone', 99, {})
  assert.equal(s.value(s.controller.fetchingAll), true, 'another run’s end is ignored')
  s.emit('fetch:alldone', 8, {})
  assert.equal(s.value(s.controller.fetchingAll), false)
  assert.equal(s.value(s.controller.fetchRun), null)
  assert.match(s.value(s.controller.lastFetchTime), /\d/)
  const [card] = s.value(s.notices)
  assert.equal(card.kind, 'error')
  assert.equal(card.title, 'Fetch finished')
  assert.deepEqual(card.lines, ['1 clones fetched', "1 couldn't reach the server"])
})

test('a Fetch all that crossed a reconnect ends when its call answers', async t => {
  let answer
  const s = await session(t, { bridge: { runTask: () => new Promise(resolve => { answer = resolve }) } })
  s.controller.listen()
  const run = s.controller.runFetchAll()
  s.controller.noteDisconnect()
  answer('finished')
  assert.equal(await run, true)
  assert.equal(s.value(s.controller.fetchingAll), false)
  assert.notEqual(s.value(s.controller.lastFetchTime), '')
})

test('Pull all follows its own run and sums it up', async t => {
  const s = await session(t, { bridge: { pullAll: async () => {} } })
  s.controller.listen()
  await s.controller.executePullAll('')
  assert.equal(s.value(s.controller.syncing), true)
  const [run] = s.calls.find(call => call.name === 'pullAll').args
  s.emit('pull:allstart', 20, { run: 'other', pulls: [{ source, repo: one }], clones: [] })
  assert.equal(s.value(s.controller.pullRun), null, 'another window’s run is ignored')
  s.emit('pull:allstart', 21, { run, pulls: [{ source, repo: one }], clones: [] })
  assert.deepEqual(s.value(s.controller.pullRun).keys, [keyOne])
  s.emit('pull:start', 22, { source, repo: one })
  assert.equal(s.value(s.repoStates)[keyOne].status, 'syncing')
  s.emit('pull:done', 22, { run, source, repo: one, behindBefore: 1 })
  assert.equal(s.value(s.repoStates)[keyOne].status, 'pending')
  assert.ok(s.value(s.controller.pullRun).done.has(keyOne))
  s.emit('pull:alldone', 21, { run })
  assert.equal(s.value(s.controller.syncing), false)
  assert.equal(s.value(s.controller.pullRun), null)
  const [card] = s.value(s.notices)
  assert.equal(card.title, 'Pull all finished')
  assert.equal(card.kind, 'success')
})

test('an expired pull plan ends the run with its error', async t => {
  const s = await session(t, { bridge: { pullAll: async () => {} }, host: { simple: () => true } })
  s.controller.listen()
  await s.controller.executePullAll('plan-1')
  const [run] = s.calls.find(call => call.name === 'pullAll').args
  s.emit('pull:alldone', 30, { run, planID: 'plan-1', planExpired: true })
  assert.equal(s.value(s.controller.syncing), false)
  const [card] = s.value(s.notices)
  assert.equal(card.kind, 'error')
  assert.equal(card.lines.length, 1)
})

test('a clone the backend did not attempt goes back to its earlier status', async t => {
  const s = await session(t)
  s.controller.listen()
  s.repoStates.update(states => ({ ...states, [keyOne]: { ...states[keyOne], status: 'not cloned' } }))
  s.controller.markCloneStart(keyOne)
  assert.equal(s.value(s.repoStates)[keyOne].status, 'cloning')
  s.emit('clone:progress', 40, { source, repo: one, percent: 50 })
  assert.equal(s.value(s.repoStates)[keyOne].progress, 50)
  s.emit('clone:done', 40, { source, repo: one, skipped: true, code: 'git.offline', error: 'no server' })
  const row = s.value(s.repoStates)[keyOne]
  assert.equal(row.status, 'not cloned')
  assert.equal(row.failedOperation, 'clone')
  assert.deepEqual(row.problem, { code: 'git.offline', detail: 'no server' })
})

test('a completion check waits for its clone and an older one is dropped', async t => {
  const s = await session(t)
  s.controller.listen()
  s.emit('clone:start', 50, { source, repo: one })
  s.emit('status:one', 50, status(one, 'behind', { behind: 3 }))
  assert.equal(s.value(s.repoStates)[keyOne].status, 'cloning', 'the check waits for the clone to end')
  const generation = s.controller.statusGeneration
  s.emit('clone:done', 50, { source, repo: one, path: '/fixture/example/one' })
  assert.equal(s.value(s.repoStates)[keyOne].status, 'behind')
  assert.equal(s.value(s.repoStates)[keyOne].behind, 3)
  assert.equal(s.controller.statusGeneration, generation + 1)
  s.emit('status:one', 49, status(one, 'dirty'))
  assert.equal(s.value(s.repoStates)[keyOne].status, 'behind', 'an older operation cannot replace the newer result')
})

test('status:updated applies configured repos, wakes waiters and refreshes the detail', async t => {
  const s = await session(t)
  s.controller.listen()
  const waited = s.controller.nextStatus(30_000)
  s.emit('status:updated', 60, [status(one, 'dirty', { modified: 2 }), { ...status('example/gone'), repo: 'example/gone' }])
  await waited
  assert.equal(s.value(s.repoStates)[keyOne].status, 'dirty')
  assert.equal(s.value(s.repoStates)[`${source}/example/gone`], undefined)
  assert.deepEqual(s.seen.filter(([name]) => name === 'refreshDetail'), [['refreshDetail']])
})

test('status:batch paints its stream, finishes it and restarts on a newer config', async t => {
  let stream = null
  const s = await session(t, { host: { stream: () => stream } })
  s.controller.listen()
  let firsts = 0
  let finishes = 0
  const painted = []
  stream = {
    id: 3, full: false, painted: false, pending: new Set([keyOne, keyTwo]),
    first: () => { firsts++ }, finish: () => { finishes++ },
    paint: s.statusPaintQueue(rows => {
      for (const row of rows) stream.pending.delete(`${row.source}/${row.repo}`)
      painted.push(...rows.map(row => row.repo))
    }),
  }
  s.emit('status:batch', 1, { passID: 9, revision: 1, items: [status(one)], done: false })
  assert.deepEqual(painted, [], 'another pass is ignored')
  s.emit('status:batch', 1, { passID: 3, revision: 1, items: [status(one), status(two)], done: true })
  await settle()
  assert.deepEqual(painted, [one, two])
  assert.equal(stream.full, true)
  assert.ok(firsts >= 1)
  assert.equal(finishes, 1)
  assert.deepEqual(s.seen.filter(([name]) => name === 'markStartup').map(([, stage]) => stage), ['firstStatus', 'fullStatus'])
  assert.ok(s.seen.some(([name]) => name === 'finishTiming'))
  stream = { ...stream, id: 4, full: false }
  s.emit('status:batch', 1, { passID: 4, revision: 0, items: [], done: true })
  assert.ok(s.seen.some(([name]) => name === 'restartDashboard'), 'a pass of an older config starts over')
})

test('credentials:updated keeps known accounts and outdates a running check', async t => {
  let verified
  const s = await session(t, { bridge: { credentialVerify: () => new Promise(resolve => { verified = resolve }) } })
  s.controller.listen()
  s.controller.verifyAllCredentials()
  s.emit('credentials:updated', 70, { demo: { status: 'offline' }, removed: { status: 'ok' } })
  assert.deepEqual(Object.keys(s.value(s.controller.credStatuses)), ['demo'])
  verified({ status: 'ok' })
  await settle()
  assert.equal(s.value(s.controller.credStatuses).demo.status, 'offline', 'the older check is dropped')
})

test('a repo of an offline account that fetches fine rechecks the account', async t => {
  const s = await session(t, { bridge: { credentialVerify: async () => ({ status: 'ok' }) } })
  s.controller.listen()
  s.emit('credentials:updated', 71, { demo: { status: 'offline' } })
  s.emit('fetch:done', 72, { source, repo: one })
  await settle()
  assert.equal(s.value(s.controller.credStatuses).demo.status, 'ok')
})

test('mirror events clear their spinners, drop stale progress and reload after a setup', async t => {
  const s = await session(t, { bridge: { reloadConfig: async () => s.value(s.configStore), runTask: async () => 'finished' } })
  s.controller.listen()
  s.controller.mirrorChecking.set({ backup: true })
  s.emit('mirror:status', 80, [{ mirrorKey: 'backup', repoKey: one }])
  assert.deepEqual(s.value(s.controller.mirrorChecking), { backup: false })
  s.emit('mirror:discover:done', 82, { results: [] })
  s.emit('mirror:discover:progress', 81, { phase: 'scan', account: 'demo', current: 1, total: 2 })
  assert.equal(s.value(s.controller.mirrorDiscoverProgress), null, 'progress older than the result is ignored')
  assert.deepEqual(s.value(s.controller.mirrorDiscoverResults), [])
  s.controller.mirrorSettingUp.set({ [`backup/${one}`]: true, 'backup/other': true })
  s.emit('mirror:setup:done', 83, { mirrorKey: 'backup', repoKey: one })
  await settle()
  assert.deepEqual(s.value(s.controller.mirrorSettingUp), { 'backup/other': true })
  assert.equal(s.value(s.controller.mirrorSetupResultModal).repoKey, one)
  assert.ok(s.calls.some(call => call.name === 'runTask' && call.args[0] === 'mirror-status'))
})

test('update events respect a dismissed version and quit after the message', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const s = await session(t)
  s.controller.listen()
  s.controller.updateDismissed.set('2.0.0')
  s.emit('update:available', 90, { latest: '2.0.0' })
  assert.equal(s.value(s.controller.updateInfo), null)
  s.emit('update:available', 91, { latest: '2.1.0' })
  assert.equal(s.value(s.controller.updateInfo).latest, '2.1.0')
  s.controller.rollbackVersion.set('2.0.0')
  s.emit('update:recoveryExpired', 92, null)
  assert.equal(s.value(s.controller.rollbackVersion), '')
  s.controller.updateApplying.set(true)
  s.emit('update:done', 93, '')
  assert.equal(s.value(s.controller.updateApplying), false)
  assert.equal(s.value(s.controller.updateDone), true)
  s.emit('update:quit', 94, '2.1.0')
  assert.match(s.value(s.controller.updateProgress), /2\.1\.0/)
  t.mock.timers.tick(1499)
  assert.ok(!s.seen.some(([name]) => name === 'quit'))
  t.mock.timers.tick(1)
  assert.ok(s.seen.some(([name]) => name === 'quit'))
})

test('config, reconcile, discovery, network and PR events reach their stores and the window', async t => {
  const s = await session(t, { bridge: { getConfig: async () => ({ global: { editors: [{ name: 'fixture' }], terminal_apps: [], shells: [], terminal_profiles: [] } }) } })
  s.controller.listen()
  s.emit('harnesses:updated', 100, [{ name: 'fixture-harness' }])
  assert.deepEqual(s.value(s.configStore).global.ai_harnesses, [{ name: 'fixture-harness' }])
  s.emit('tools:updated', 101, null)
  await settle()
  assert.deepEqual(s.value(s.configStore).global.editors, [{ name: 'fixture' }])
  s.emit('reconcile:done', 102, { workspaceUnknown: [keyOne] })
  assert.deepEqual([...s.value(s.workspaceUnknown)], [keyOne])
  s.emit('reconcile:done', 103, { background: true, adopted: 0 })
  assert.equal(s.seen.find(([name]) => name === 'applyReconcile')[1].background, true)
  s.emit('discover:done', 104, { accountKey: 'demo' })
  assert.deepEqual(s.seen.find(([name]) => name === 'discoverDone')[1], { accountKey: 'demo' })
  s.emit('network:state', 105, { offline: true, hosts: ['example.invalid'] })
  assert.deepEqual(s.value(s.controller.networkDown), ['example.invalid'])
  s.emit('pr:refreshed', 106, { accountKey: 'unknown', prs: {} })
  assert.deepEqual(s.value(s.prsByAccount), {}, 'an account outside the config is ignored')
})

test('the window watchers follow the tint, the profiles and the connection', async t => {
  let read
  const s = await session(t, { bridge: { getRemoteTint: () => new Promise(resolve => { read = resolve }) } })
  const loading = s.controller.loadRemoteTint()
  s.emit('remote:tint', 110, '#22c55e')
  read('#8b5cf6')
  await loading
  assert.equal(s.value(s.controller.remoteTint), '#22c55e', 'a change during the read wins over it')
  let reloads = 0
  const states = []
  s.controller.watchProfiles(() => { reloads++ })
  s.controller.watchRemoteState(state => states.push(state.state))
  s.emit('profiles:reloaded', 111, null)
  s.emit('remote:state', 112, { state: 'connected', alias: 'fixture-host' })
  assert.equal(reloads, 1)
  assert.deepEqual(states, ['connected'])
})

test('forgetRuns and forgetOperations drop what a lost connection leaves unknown', async t => {
  const s = await session(t, { bridge: { pullAll: async () => {} } })
  s.controller.listen()
  await s.controller.executePullAll('')
  s.emit('fetch:start', 120, { source, repo: one })
  s.controller.mirrorSettingUp.set({ x: true })
  s.controller.forgetRuns()
  assert.equal(s.value(s.controller.syncing), false)
  assert.deepEqual(s.value(s.controller.fetchingRepos), {})
  assert.deepEqual(s.value(s.controller.mirrorSettingUp), {})
  s.emit('fetch:done', 119, { source, repo: one, error: 'late' })
  assert.equal(s.value(s.repoStates)[keyOne].failedOperation, undefined, 'positive control: the newer fetch still blocks the older one')
  s.controller.forgetOperations()
  s.emit('fetch:done', 119, { source, repo: one, error: 'late' })
  assert.equal(s.value(s.repoStates)[keyOne].failedOperation, 'fetch')
})
