// Focus helpers shared by the modal dialogs (the composer sheet, the forum
// picker).

// trapTab keeps Tab and Shift+Tab inside the dialog, as aria-modal promises:
// from the last control Tab goes to the first, and from the first Shift+Tab
// goes to the last.
export function trapTab(e: KeyboardEvent, root: HTMLElement | null) {
  if (!root) return;
  const all = [...root.querySelectorAll<HTMLElement>(focusable)].filter((el) => !el.closest('[hidden]'));
  if (all.length === 0) return;
  const first = all[0];
  const last = all[all.length - 1];
  const at = document.activeElement;
  if (e.shiftKey && (at === first || !root.contains(at))) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && (at === last || !root.contains(at))) {
    e.preventDefault();
    first.focus();
  }
}

const focusable = 'button:not([disabled]), textarea:not([disabled]), input:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';
