import type { ConfigDTO } from './types';
import type { Envelope, EventMetadata, EventPayloads, StatusResult } from './dto.generated';

type Name = keyof EventPayloads;
type Subscribe = (name: string, handler: (value: unknown) => void) => () => void;

function isEnvelope(value: unknown): value is Envelope<unknown> {
  if (!value || typeof value !== 'object') return false;
  const event = value as Partial<Envelope<unknown>>;
  return event.schema === 1 &&
    typeof event.configRevision === 'number' && Number.isSafeInteger(event.configRevision) && event.configRevision >= 0 &&
    typeof event.sequence === 'number' && Number.isSafeInteger(event.sequence) && event.sequence > 0 &&
    typeof event.operationID === 'string' && /^[1-9]\d*$/.test(event.operationID) &&
    'payload' in event;
}

// Progress can be delayed by RPC coalescing. Track order within each
// entity and event family so that a late event for A doesn't hide work on B.
function channel(name: Name, value: unknown): string {
  if (!value || typeof value !== 'object') return name;
  const payload = value as Record<string, unknown>;
  if ('source' in payload && 'repo' in payload) return `${name}:${payload.source}/${payload.repo}`;
  if ('mirrorKey' in payload) return `${name}:${payload.mirrorKey}/${payload.repoKey ?? ''}`;
  if ('accountKey' in payload) return `${name}:${payload.accountKey}`;
  return name;
}

export function hasConfiguredRepo(config: Pick<ConfigDTO, 'sources'> | null, source: string, repo: string): boolean {
  const repos = config?.sources[source]?.repos;
  return !!repos && Object.hasOwn(repos, repo);
}

export function createEventRouter(subscribe: Subscribe) {
  type Delivery = { event: Envelope<unknown>; dispatch: () => void };
  let startup: Delivery[] | null = [];
  let watermark = 0;
  let revision = () => 0;
  const hostListeners = new Set<Map<string, number>>();

  function on<K extends Name>(
    name: K,
    handler: (payload: EventPayloads[K], meta: EventMetadata) => void,
    onStaleRevision?: (payload: EventPayloads[K]) => void,
  ) {
    let active = true;
    const hostLatest = new Map<string, number>();
    const clientLatest = new Map<string, number>();
    hostListeners.add(hostLatest);
    const off = subscribe(name, value => {
      if (!isEnvelope(value)) return;
      const deliver = () => {
        if (!active) return;
        if (!value.client && value.sequence <= watermark) return;
        if (!value.client && value.configRevision < revision()) {
          onStaleRevision?.(value.payload as EventPayloads[K]);
          return;
        }
        const latest = value.client ? clientLatest : hostLatest;
        const key = channel(name, value.payload);
        if (value.sequence <= (latest.get(key) ?? 0)) return;
        latest.set(key, value.sequence);
        handler(value.payload as EventPayloads[K], value);
      };
      if (startup && !value.client) {
        // Multiple listeners for one name can be active (e.g. onboarding).
        startup.push({ event: value, dispatch: deliver });
      } else deliver();
    });
    return () => { active = false; hostListeners.delete(hostLatest); off(); };
  }

  return {
    on,
    reset() {
      startup = [];
      watermark = 0;
      for (const latest of hostListeners) latest.clear();
    },
    snapshot(sequence: number, currentRevision: () => number) {
      watermark = Math.max(watermark, sequence);
      revision = currentRevision;
      const queued = startup;
      startup = null;
      queued?.sort((a, b) => a.event.sequence - b.event.sequence);
      queued?.forEach(({ dispatch }) => dispatch());
    },
  };
}

// A post-operation check belongs to one clone or pull. A slow probe from
// an older operation cannot replace the newer result for the same row.
export function completionStatuses() {
  const latest = new Map<string, { id: bigint; done?: boolean; row?: StatusResult }>();
  function forOperation(key: string, operationID: string) {
    const id = BigInt(operationID);
    const prior = latest.get(key);
    if (prior && prior.id > id) return undefined;
    if (prior?.id === id) return prior;
    const next: { id: bigint; done?: boolean; row?: StatusResult } = { id };
    latest.set(key, next);
    return next;
  }
  return {
    start(key: string, operationID: string) {
      const op = forOperation(key, operationID);
      return !!op && op.done === undefined;
    },
    done(key: string, operationID: string, checking: boolean) {
      const op = forOperation(key, operationID);
      if (!op || op.done !== undefined) return { accept: false };
      op.done = checking;
      const row = op.row;
      op.row = undefined;
      return { accept: true, row: checking ? row : undefined };
    },
    status(key: string, operationID: string, row: StatusResult) {
      const op = forOperation(key, operationID);
      if (!op) return undefined;
      if (op.done) return row;
      if (op.done === undefined) op.row = row;
      return undefined;
    },
    clear() { latest.clear(); },
  };
}
