import type * as Bindings from '../../../wailsjs/go/main/App'
import type * as Runtime from '../../../wailsjs/runtime/runtime'
import type { Envelope, EventPayloads } from '../dto.generated'

type Calls = typeof Bindings
type Call = keyof Calls
type RuntimeCalls = Pick<typeof Runtime, 'WindowGetSize' | 'WindowGetPosition' | 'WindowSetMinSize' | 'WindowSetSize' | 'WindowSetPosition'>
type Listener = (envelope: unknown) => void

/** Keep the real bridge and event router in the module graph. Replace their native transport. */
export function fakeBridge(window: Window, responses: Partial<Calls>, runtime: Partial<RuntimeCalls> = {}) {
  const listeners = new Map<string, Set<Listener>>()
  const calls: { name: Call; args: unknown[] }[] = []
  const unexpected: string[] = []
  function subscribe(name: string, handler: Listener) {
    const group = listeners.get(name) ?? new Set<Listener>()
    listeners.set(name, group)
    group.add(handler)
    return () => {
      group.delete(handler)
      if (!group.size) listeners.delete(name)
    }
  }
  const app = new Proxy({}, {
    get(_target, name: Call) {
      return async (...args: unknown[]) => {
        calls.push({ name, args })
        const respond = responses[name] as ((...args: unknown[]) => unknown) | undefined
        if (respond) return respond(...args)
        unexpected.push(name)
        throw new Error(`Unexpected Wails call ${name}`)
      }
    },
  })
  Object.assign(window, { go: { main: { App: app } }, runtime: { ...runtime, EventsOnMultiple: subscribe } })
  return {
    calls,
    unexpected,
    emit<K extends keyof EventPayloads>(name: K, envelope: Envelope<EventPayloads[K]>) {
      for (const handler of listeners.get(name) ?? []) handler(envelope)
    },
    activeEvents: () => [...listeners.keys()],
  }
}
