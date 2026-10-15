// Keyboard activation: an Enter/Space handler for clickable elements that
// aren't buttons (role="button"). Ignores keys bubbling up from controls
// inside them.
export function onActivateKey(fn: () => void) {
  return (e: KeyboardEvent) => {
    if ((e.key === 'Enter' || e.key === ' ') && e.target === e.currentTarget) {
      e.preventDefault();
      fn();
    }
  };
}
