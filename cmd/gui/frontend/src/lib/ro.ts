// The read-only remote action (#240). Put use:ro on a control that would
// change the host, or launch a local program on one of its paths: in a
// read-only remote window it shows disabled, its title explains why, and
// a click does nothing. Elsewhere it changes nothing. The bridge refuses
// the same calls (readonly.ts), so a control without it still fails safely.

import { get } from 'svelte/store';
import { remoteReadOnly } from './stores';
import { t } from './i18n';
import { offTip, type RemoteOff } from './remotehost';

export function ro(node: HTMLElement) {
  let active = false;
  let savedTitle: string | null = null;
  const block = (e: Event) => {
    if (!active) return;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  const blockKey = (e: KeyboardEvent) => { if (e.key === 'Enter' || e.key === ' ') block(e); };
  node.addEventListener('click', block, true);
  node.addEventListener('keydown', blockKey, true);

  const apply = (on: boolean) => {
    if (on === active) {
      if (on) node.title = get(t)('remote.readOnlyTip');
      return;
    }
    active = on;
    node.classList.toggle('ro-off', on);
    if (on) {
      savedTitle = node.getAttribute('title');
      node.title = get(t)('remote.readOnlyTip');
      node.setAttribute('aria-disabled', 'true');
    } else {
      if (savedTitle === null) node.removeAttribute('title'); else node.title = savedTitle;
      node.removeAttribute('aria-disabled');
    }
  };
  const offStore = remoteReadOnly.subscribe(apply);
  const offLang = t.subscribe(() => apply(active));
  return {
    destroy() {
      offStore();
      offLang();
      node.removeEventListener('click', block, true);
      node.removeEventListener('keydown', blockKey, true);
    },
  };
}

// remoteOff (#243): put use:remoteOff={reason} on a launcher that a remote
// window cannot run: the file manager, the account folder and the host's
// config file ('unavailable'), or an editor that cannot open remote
// folders ('editor'; lib/remotehost.ts decides). With a reason it shows
// disabled, its title says why, and a click does nothing; with '' it
// changes nothing. The Go side refuses the same launches.
export function remoteOff(node: HTMLElement, reason: RemoteOff) {
  let current: RemoteOff = '';
  let savedTitle: string | null = null;
  const block = (e: Event) => {
    if (!current) return;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  const blockKey = (e: KeyboardEvent) => { if (e.key === 'Enter' || e.key === ' ') block(e); };
  node.addEventListener('click', block, true);
  node.addEventListener('keydown', blockKey, true);

  const apply = (next: RemoteOff) => {
    if (next && !current) savedTitle = node.getAttribute('title');
    if (!next && current) {
      if (savedTitle === null) node.removeAttribute('title'); else node.title = savedTitle;
      node.removeAttribute('aria-disabled');
    }
    current = next;
    node.classList.toggle('ro-off', !!next);
    if (next) {
      node.title = get(t)(offTip(next));
      node.setAttribute('aria-disabled', 'true');
    }
  };
  apply(reason);
  const offLang = t.subscribe(() => { if (current) node.title = get(t)(offTip(current)); });
  return {
    update(next: RemoteOff) { apply(next); },
    destroy() {
      offLang();
      node.removeEventListener('click', block, true);
      node.removeEventListener('keydown', blockKey, true);
    },
  };
}
