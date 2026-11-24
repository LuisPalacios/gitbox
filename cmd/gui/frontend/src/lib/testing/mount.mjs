import { JSDOM } from 'jsdom'
import { build } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import icons from '../../../vite-plugin-icons.ts'

const root = fileURLToPath(new URL('../../../', import.meta.url))
let assets
async function compile() {
  const bundle = await build({
    configFile: false,
    root,
    logLevel: 'error',
    plugins: [svelte(), icons()],
    build: {
      write: false,
      minify: false,
      lib: { entry: fileURLToPath(new URL('./entry.ts', import.meta.url)), formats: ['es'] },
      rolldownOptions: { output: { codeSplitting: false } },
    },
  })
  const { output } = Array.isArray(bundle) ? bundle[0] : bundle
  return {
    code: output.find(file => file.type === 'chunk').code,
    css: output.filter(file => file.type === 'asset' && file.fileName.endsWith('.css')).map(file => file.source).join('\n'),
  }
}

export async function mount(t, width = 1040) {
  const { code, css } = await (assets ??= compile())
  const scratch = await mkdtemp(join(root, 'node_modules', '.component-test-'))
  t.after(() => rm(scratch, { recursive: true, force: true }))
  const entry = join(scratch, 'components.mjs')
  await writeFile(entry, code)
  const dom = new JSDOM('<!doctype html><html lang="en"><head></head><body></body></html>', {
    url: 'https://gitbox.invalid/',
    pretendToBeVisual: true,
  })
  const { window } = dom
  window.innerWidth = width
  window.matchMedia = query => Object.assign(new window.EventTarget(), { media: query, matches: false })
  // jsdom has no Web Animations API. Complete Svelte's transitions on the
  // next microtask, keeping their lifecycle and DOM removal observable.
  window.Element.prototype.animate = (_frames, { duration = 0 }) => {
    let cancelled = false
    const animation = { currentTime: duration, cancel: () => { cancelled = true } }
    queueMicrotask(() => { if (!cancelled) animation.onfinish?.() })
    return animation
  }
  const style = window.document.createElement('style')
  style.textContent = css
  window.document.head.append(style)

  const globals = [
    'window', 'document', 'navigator', 'MutationObserver', 'Node', 'NodeFilter', 'Comment', 'Text', 'Element',
    'HTMLElement', 'HTMLInputElement', 'HTMLMediaElement', 'Event', 'CustomEvent', 'AbortController',
    'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame',
  ]
  const prior = globals.map(name => [name, Object.getOwnPropertyDescriptor(globalThis, name)])
  for (const name of globals) {
    const value = name === 'window' ? window : window[name]
    Object.defineProperty(globalThis, name, {
      configurable: true,
      value: ['getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame'].includes(name) ? value.bind(window) : value,
    })
  }
  let ui
  t.after(() => {
    try {
      ui?.cleanup()
    } finally {
      window.close()
      for (const [name, descriptor] of prior) {
        if (descriptor) Object.defineProperty(globalThis, name, descriptor)
        else delete globalThis[name]
      }
    }
  })
  // A fresh module graph gives every fixture independent Svelte stores and event routing.
  ui = await import(pathToFileURL(entry).href)
  return { window, ...ui }
}
