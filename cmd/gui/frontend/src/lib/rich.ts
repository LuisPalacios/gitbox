// Inline markup in translated sentences (issue #140): `x` is code, **x** is
// strong and *x* is emphasis, so a whole sentence stays one catalog entry
// even when part of it is a config key. Rich.svelte renders the segments;
// it splits the template before filling {vars}, so a value never turns
// into markup.
//
// Kept free of runtime imports so lib/i18n.test.mjs can load it on its own.

export type RichKind = 'text' | 'code' | 'strong' | 'em';

export interface RichSegment { kind: RichKind; text: string }

const MARKUP = /`([^`]+)`|\*\*(\S(?:[^*]*\S)?)\*\*|\*(\S(?:[^*]*\S)?)\*/g;

export function richSegments(template: string): RichSegment[] {
  const out: RichSegment[] = [];
  let last = 0;
  for (const m of template.matchAll(MARKUP)) {
    if (m.index > last) out.push({ kind: 'text', text: template.slice(last, m.index) });
    if (m[1] !== undefined) out.push({ kind: 'code', text: m[1] });
    else if (m[2] !== undefined) out.push({ kind: 'strong', text: m[2] });
    else out.push({ kind: 'em', text: m[3] });
    last = m.index + m[0].length;
  }
  if (last < template.length) out.push({ kind: 'text', text: template.slice(last) });
  return out;
}
