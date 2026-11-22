// A folder typed for the host in a remote window (#241). The native folder
// dialog would pick a folder of this computer, so a remote window takes a
// typed path and asks the host what it resolves to (ValidateRemotePath).
// This module has no runtime imports, so remotepath.test.mjs can load it
// alone.

// RemotePath is what the host answers for a typed folder.
export type RemotePath = { abs: string; exists: boolean; isDir: boolean; writable: boolean; error?: string };

// PathHint is the catalog key and parameters of the line under the field.
export type PathHint = { key: string; params: Record<string, string> };

// pathHint explains what the host said, or null before it answered.
export function pathHint(p: RemotePath | null): PathHint | null {
  if (!p) return null;
  if (p.error) return { key: 'remotePath.invalid', params: { reason: p.error } };
  if (p.exists && !p.isDir) return { key: 'remotePath.notDir', params: { abs: p.abs } };
  if (!p.writable) return { key: 'remotePath.notWritable', params: { abs: p.abs } };
  if (!p.exists) return { key: 'remotePath.willCreate', params: { abs: p.abs } };
  return { key: 'remotePath.ok', params: { abs: p.abs } };
}

// pathUsable reports whether the folder can be used: the host resolved it,
// and it is a folder that exists or can be created, and can be written.
export function pathUsable(p: RemotePath | null): boolean {
  return !!p && !p.error && p.writable && (!p.exists || p.isDir);
}

// debounce runs fn once ms after the last call.
export function debounce<A extends unknown[]>(fn: (...args: A) => void, ms: number): (...args: A) => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return (...args: A) => {
    if (timer !== undefined) clearTimeout(timer);
    timer = setTimeout(() => { timer = undefined; fn(...args); }, ms);
  };
}
