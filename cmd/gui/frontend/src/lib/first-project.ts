// Decide whether the selected download actually produced a usable checkout.
// clone:done also reports skipped transfers, and other rows can finish first.
export interface FirstCloneResult {
  source: string;
  repo: string;
  path?: string;
  error?: string;
  skipped?: boolean;
  warnings?: string[];
}

interface Checkout {
  source: string;
  repo: string;
  path: string;
  state: string;
}

export async function firstCloneOutcome(
  event: FirstCloneResult,
  destination: string,
  readStatus: () => Promise<Checkout[]>,
): Promise<'ready' | 'attention' | 'failed'> {
  if (!event.path || event.path !== destination || event.skipped || event.error) return 'failed';
  const verified = (await readStatus()).some(row =>
    row.source === event.source && row.repo === event.repo && row.path === destination
    && ['clean', 'dirty', 'ahead', 'behind', 'diverged', 'conflict', 'no upstream', 'upstream gone'].includes(row.state));
  if (!verified) return 'failed';
  return event.warnings?.length ? 'attention' : 'ready';
}

// The route the first project starts on (#355): a root that already holds
// clones, or may (a sampled count), offers one of them first. The guide can
// still switch to a download.
export function firstProjectRoute(preview: { exists: boolean; existingClones: number; sampled: boolean } | null): 'adopt' | 'download' {
  return preview?.exists && (preview.existingClones > 0 || preview.sampled) ? 'adopt' : 'download';
}
