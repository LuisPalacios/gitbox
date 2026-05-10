// Tooltip Svelte action — shows a styled popup after a hover delay and
// removes it when the cursor leaves. Used on Gear-panel row labels so
// every setting carries a one-line "what does this do?" hint without
// crowding the row itself.
//
// Why a custom action and not the native `title` attribute:
//   - Native tooltips have inconsistent timing across OS / browser.
//   - They pin to the cursor instead of the element, which makes them
//     awkward when the user hovers a label they're explaining.
//   - They can't be themed.
//
// Usage:
//   <span use:tooltip={"Folder where clones live"}>Root folder</span>
//   <span use:tooltip={{ text: "...", delay: 800 }}>Label</span>

const DEFAULT_DELAY_MS = 500;

type TooltipParam = string | { text: string; delay?: number };

interface TooltipState {
  param: TooltipParam;
  timer: number | null;
  popup: HTMLDivElement | null;
}

function paramText(p: TooltipParam): string {
  return typeof p === 'string' ? p : (p?.text || '');
}

function paramDelay(p: TooltipParam): number {
  return typeof p === 'string' ? DEFAULT_DELAY_MS : (p?.delay ?? DEFAULT_DELAY_MS);
}

// Position the popup just below and slightly right of the anchor, but
// flip above when the popup would clip the viewport bottom. Keeps the
// horizontal edge inside the viewport with a transform.
function positionPopup(popup: HTMLElement, anchor: HTMLElement) {
  const rect = anchor.getBoundingClientRect();
  const padding = 8;
  popup.style.position = 'fixed';
  popup.style.zIndex = '9999';
  popup.style.maxWidth = `${Math.min(360, window.innerWidth - 2 * padding)}px`;

  // Initial guess: below + left-aligned to anchor.
  popup.style.left = `${Math.round(rect.left)}px`;
  popup.style.top = `${Math.round(rect.bottom + 6)}px`;

  // Vertical flip if it would clip below the viewport.
  const popupRect = popup.getBoundingClientRect();
  if (popupRect.bottom > window.innerHeight - padding) {
    popup.style.top = `${Math.round(rect.top - popupRect.height - 6)}px`;
  }

  // Horizontal clamp.
  const post = popup.getBoundingClientRect();
  let dx = 0;
  if (post.right > window.innerWidth - padding) {
    dx = window.innerWidth - padding - post.right;
  } else if (post.left < padding) {
    dx = padding - post.left;
  }
  if (dx !== 0) {
    popup.style.left = `${Math.round(parseFloat(popup.style.left) + dx)}px`;
  }
}

export function tooltip(node: HTMLElement, param: TooltipParam) {
  const state: TooltipState = { param, timer: null, popup: null };

  function show() {
    const text = paramText(state.param);
    if (!text) return;
    state.popup = document.createElement('div');
    state.popup.className = 'gitbox-tooltip';
    state.popup.textContent = text;
    state.popup.setAttribute('role', 'tooltip');
    document.body.appendChild(state.popup);
    positionPopup(state.popup, node);
  }

  function hide() {
    if (state.timer !== null) {
      window.clearTimeout(state.timer);
      state.timer = null;
    }
    if (state.popup) {
      state.popup.remove();
      state.popup = null;
    }
  }

  function onEnter() {
    hide(); // clear any stale popup before scheduling a fresh one
    if (!paramText(state.param)) return;
    state.timer = window.setTimeout(show, paramDelay(state.param));
  }

  // Intercept focus too so keyboard users get the hint as well.
  node.addEventListener('mouseenter', onEnter);
  node.addEventListener('focus', onEnter);
  node.addEventListener('mouseleave', hide);
  node.addEventListener('blur', hide);
  // Mouse leaving the page or the parent container (e.g. settings panel
  // collapsing) still has to clean the popup, hence the global guards.
  window.addEventListener('scroll', hide, true);
  window.addEventListener('resize', hide);

  return {
    update(next: TooltipParam) {
      state.param = next;
      // If the popup is currently shown, refresh its text so the user
      // sees the new content without hover/leave gymnastics.
      if (state.popup) state.popup.textContent = paramText(next);
    },
    destroy() {
      node.removeEventListener('mouseenter', onEnter);
      node.removeEventListener('focus', onEnter);
      node.removeEventListener('mouseleave', hide);
      node.removeEventListener('blur', hide);
      window.removeEventListener('scroll', hide, true);
      window.removeEventListener('resize', hide);
      hide();
    },
  };
}
