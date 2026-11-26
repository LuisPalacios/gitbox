---
paths:
  - "cmd/gui/frontend/**"
---

# Frontend CSS colors

Never use CSS `color-mix()` in `cmd/gui/frontend`: older WebKitGTK builds drop the whole declaration without an error (#340).

- A mix with `transparent` becomes `rgba()` over a per-theme channel token, such as `rgba(var(--alert-rgb), 0.14)`.
- A mix with an opaque color becomes a per-theme token in the `[data-theme]` blocks of `style.css`, with its recipe in `RECIPES` of `lib/css-colors.test.mjs`.
- A color chosen at run time is mixed in TypeScript, as `lib/tint.ts` does.

`lib/css-colors.test.mjs` fails on any use outside comments and recomputes each token from its mix in both themes.
