/// <reference types="svelte" />
/// <reference types="vite/client" />

// Icons built from assets/svg/ by vite-plugin-icons.ts.
declare module 'virtual:icons' {
  // color: data URI of the colour original; mono: inline single-colour SVG.
  export const brand: Record<string, { color?: string; mono?: string }>;
  // Inline single-colour SVG markup.
  export const ui: Record<string, string>;
}
