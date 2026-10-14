/// <reference types="svelte" />
/// <reference types="vite/client" />

// Icons built from assets/svg/ by vite-plugin-icons.ts.
declare module 'virtual:icons' {
  // color: data URI of the colour original; mono: inline single-colour SVG;
  // invert: listed in brand/invert-light.txt (dark body, inverted in light theme).
  export const brand: Record<string, { color?: string; mono?: string; invert?: boolean }>;
  // Inline single-colour SVG markup.
  export const ui: Record<string, string>;
}
