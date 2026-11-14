import type { RepoDetailView } from './types';

type DetailResponse = Omit<RepoDetailView, 'changed' | 'untracked' | 'incoming'> & {
  changed?: RepoDetailView['changed'] | null;
  untracked?: string[] | null;
  incoming?: RepoDetailView['incoming'] | null;
};

// I discard superseded reads, including when the same clone opens again.
export function createDetailLoader(read: (source: string, repo: string) => Promise<DetailResponse>) {
  let generation = 0;
  return {
    cancel() { generation++; },
    async load(source: string, repo: string): Promise<RepoDetailView | null> {
      const request = ++generation;
      try {
        const raw = await read(source, repo);
        if (request !== generation) return null;
        return { ...raw, changed: raw.changed || [], untracked: raw.untracked || [], incoming: raw.incoming || [] };
      } catch (error) {
        if (request !== generation) return null;
        return { branch: '', ahead: 0, behind: 0, changed: [], untracked: [], incoming: [], error: String(error) };
      }
    },
  };
}
