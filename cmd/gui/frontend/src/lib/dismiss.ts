// Dismiss Svelte action — closes a modal when the user clicks its backdrop
// or presses Escape. Put it on the overlay element that wraps the dialog.
//
// Why an action instead of `on:click` on the overlay:
//   - A backdrop click is a mouse-only affordance; Escape gives keyboard
//     users the same way out, which is what Svelte's a11y check asks for.
//   - Only clicks on the backdrop itself count, so the dialog inside no
//     longer needs `on:click|stopPropagation` to stay open.
//   - One place for the rule instead of a pair of handlers per modal.
//
// Usage:
//   <div class="overlay" use:dismiss={() => (myModal = null)}>
//     <div class="modal" role="dialog" aria-modal="true">…</div>
//   </div>
//
// Pass `undefined` (or update to it) to make a modal non-dismissable, e.g.
// while it is busy. With modals stacked, only the most recently mounted
// one reacts to Escape.

type DismissHandler = (() => void) | undefined;

const stack: HTMLElement[] = [];

export function dismiss(node: HTMLElement, handler: DismissHandler) {
  let current = handler;
  stack.push(node);

  function onClick(e: MouseEvent) {
    if (e.target === node && current) current();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || stack[stack.length - 1] !== node || !current) return;
    e.preventDefault();
    current();
  }

  node.addEventListener('click', onClick);
  window.addEventListener('keydown', onKeydown);

  return {
    update(next: DismissHandler) {
      current = next;
    },
    destroy() {
      node.removeEventListener('click', onClick);
      window.removeEventListener('keydown', onKeydown);
      const i = stack.lastIndexOf(node);
      if (i >= 0) stack.splice(i, 1);
    },
  };
}
