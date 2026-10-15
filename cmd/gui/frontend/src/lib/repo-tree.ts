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
export function repoTreeRows(sources: Record<string, SourceDTO>, nesting: Record<string, string>, collapsed: Set<string>): Record<string, RepoRow[]> {
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
  const rows: Record<string, RepoRow[]> = Object.fromEntries(Object.keys(sources).map(key => [key, []]));
  const walk = (key: string, source: string, depth: number) => {
    const ref = refs.get(key)!;
    rows[source].push({ key, ...ref, depth, childCount: (children.get(key) || []).length, descendants: descendantsOf(key) });
    if (!collapsed.has(key)) for (const child of children.get(key) || []) walk(child, source, depth + 1);
  };
  for (const [key, ref] of refs) if (!parents.has(key)) walk(key, ref.sourceKey, 0);
  return rows;
}
