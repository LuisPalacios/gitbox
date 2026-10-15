import type { SourceDTO } from './types';

export interface RepoRow {
  key: string;
  sourceKey: string;
  repoName: string;
  depth: number;
  childCount: number;
  descendants: string[];
}

// I keep ownership in each row even when a clone lives under another source's container.
// With a rank (lower = needs me more), siblings sort stably by the worst rank
// in their subtree, so a parent rises with its worst nested clone.
export function repoTreeRows(sources: Record<string, SourceDTO>, nesting: Record<string, string>, collapsed: Set<string>, rank?: (key: string) => number): Record<string, RepoRow[]> {
  const refs = new Map<string, { sourceKey: string; repoName: string }>();
  for (const [sourceKey, source] of Object.entries(sources)) {
    const order = [...new Set([...(source.repoOrder || []), ...Object.keys(source.repos)])];
    for (const repoName of order) {
      if (source.repos[repoName]) refs.set(`${sourceKey}/${repoName}`, { sourceKey, repoName });
    }
  }
  const parents = new Map<string, string>();
  for (const key of refs.keys()) {
    const parent = nesting[key];
    if (parent && parent !== key && refs.has(parent)) parents.set(key, parent);
  }
  // I break malformed cycles before walking, so every clone has exactly one row.
  for (const key of refs.keys()) {
    const seen = new Set([key]);
    let parent = parents.get(key);
    while (parent) {
      if (seen.has(parent)) { parents.delete(key); break; }
      seen.add(parent);
      parent = parents.get(parent);
    }
  }
  const children = new Map<string, string[]>();
  for (const key of refs.keys()) {
    const parent = parents.get(key);
    if (parent) children.set(parent, [...(children.get(parent) || []), key]);
  }
  const descendantsOf = (key: string): string[] => (children.get(key) || []).flatMap(child => [child, ...descendantsOf(child)]);
  const worst = new Map<string, number>();
  const worstOf = (key: string): number => {
    let w = worst.get(key);
    if (w === undefined) {
      w = Math.min(rank!(key), ...(children.get(key) || []).map(worstOf));
      worst.set(key, w);
    }
    return w;
  };
  const ordered = (keys: string[]): string[] => rank ? [...keys].sort((a, b) => worstOf(a) - worstOf(b)) : keys;
  const rows: Record<string, RepoRow[]> = Object.fromEntries(Object.keys(sources).map(key => [key, []]));
  const walk = (key: string, source: string, depth: number) => {
    const ref = refs.get(key)!;
    rows[source].push({ key, ...ref, depth, childCount: (children.get(key) || []).length, descendants: descendantsOf(key) });
    if (!collapsed.has(key)) for (const child of ordered(children.get(key) || [])) walk(child, source, depth + 1);
  };
  for (const key of ordered([...refs.keys()].filter(key => !parents.has(key)))) walk(key, refs.get(key)!.sourceKey, 0);
  return rows;
}
