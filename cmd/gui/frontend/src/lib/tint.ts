// The tint of remote windows (#331): the chosen color mixed at a fixed low
// weight into the top bar and the sidebar, so I tell a remote window from
// the local one at a glance. It is computed here, not with CSS color-mix(),
// which older WebKitGTK builds lack. The text of the tinted areas is nudged
// away from the background where the tint would bring it under 4.5:1.

// The presets, in the order the Remote hosts window shows them; the first
// is the default. They mirror config.RemoteTintPresets.
export const TINT_PRESETS = [
  { color: '#3b82f6', key: 'blue' },
  { color: '#14b8a6', key: 'teal' },
  { color: '#8b5cf6', key: 'violet' },
  { color: '#f59e0b', key: 'amber' },
  { color: '#f43f5e', key: 'rose' },
  { color: '#22c55e', key: 'green' },
] as const;
export const DEFAULT_TINT: string = TINT_PRESETS[0].color;

export type ThemeName = 'light' | 'dark';

// The theme tokens the tint starts from; style.css defines them, and a test
// keeps both in step.
export const THEME_TOKENS = {
  dark: {
    base: '#09090b', card: '#18181b', border: '#27272a',
    primary: '#fafafa', secondary: '#b4b4bd', muted: '#8e8e99', dim: '#71717a',
  },
  light: {
    base: '#fafafa', card: '#ffffff', border: '#e4e4e7',
    primary: '#18181b', secondary: '#52525b', muted: '#71717a', dim: '#a1a1aa',
  },
} as const;

// How much of the color goes into each surface: enough to see, little
// enough to stay calm. Light surfaces show a tint sooner than dark ones.
const WEIGHT: Record<ThemeName, { surface: number; border: number }> = {
  dark: { surface: 0.1, border: 0.3 },
  light: { surface: 0.07, border: 0.3 },
};

export const MIN_CONTRAST = 4.5;

export interface TintSurfaces {
  top: string;
  side: string;
  border: string;
  primary: string;
  secondary: string;
  muted: string;
  dim: string;
}

const rgb = (hex: string): [number, number, number] => {
  const h = hex.replace('#', '');
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16)) as [number, number, number];
};
const toHex = (c: number[]): string => '#' + c.map((v) => Math.round(Math.min(255, Math.max(0, v))).toString(16).padStart(2, '0')).join('');

// mix returns a with weight w of b blended in.
export function mix(a: string, b: string, w: number): string {
  const [x, y] = [rgb(a), rgb(b)];
  return toHex(x.map((v, i) => v * (1 - w) + y[i] * w));
}

// luminance and contrast follow WCAG 2.
export function luminance(hex: string): number {
  const [r, g, b] = rgb(hex).map((v) => {
    const s = v / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

// ensure moves fg toward white on a dark theme, black on a light one, until
// it reaches min against every background.
function ensure(fg: string, bgs: string[], min: number, theme: ThemeName): string {
  const target = theme === 'dark' ? '#ffffff' : '#000000';
  let out = fg;
  for (let step = 1; step <= 50 && bgs.some((bg) => contrast(out, bg) < min); step++) {
    out = mix(fg, target, step * 0.02);
  }
  return out;
}

// tintSurfaces is what a remote window paints with the color in a theme.
// The section labels (dim) were under 4.5:1 before any tint; they only keep
// the contrast they had.
export function tintSurfaces(color: string, theme: ThemeName): TintSurfaces {
  const tk = THEME_TOKENS[theme];
  const w = WEIGHT[theme];
  const top = mix(tk.base, color, w.surface);
  const side = mix(tk.card, color, w.surface);
  const bgs = [top, side];
  return {
    top, side,
    border: mix(tk.border, color, w.border),
    primary: ensure(tk.primary, bgs, MIN_CONTRAST, theme),
    secondary: ensure(tk.secondary, bgs, MIN_CONTRAST, theme),
    muted: ensure(tk.muted, bgs, MIN_CONTRAST, theme),
    dim: ensure(tk.dim, bgs, Math.min(contrast(tk.dim, tk.base), contrast(tk.dim, tk.card)), theme),
  };
}

// tintOf is the preset a saved value stands for, like config.RemoteTintOf.
export function tintOf(saved: string | undefined | null): string {
  const c = (saved || '').trim().toLowerCase();
  return TINT_PRESETS.some((p) => p.color === c) ? c : DEFAULT_TINT;
}

// applyTint paints a remote window, or clears the paint when color is null.
export function applyTint(root: HTMLElement, color: string | null, theme: ThemeName) {
  if (!color) {
    delete root.dataset.remoteTint;
    for (const k of ['top', 'side', 'border', 'primary', 'secondary', 'muted', 'dim']) root.style.removeProperty(`--tint-${k}`);
    return;
  }
  const s = tintSurfaces(color, theme);
  root.dataset.remoteTint = color;
  for (const [k, v] of Object.entries(s)) root.style.setProperty(`--tint-${k}`, v);
}
