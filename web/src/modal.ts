import { useEffect, useRef } from 'react';

// Global single-modal coordinator. Every modal calls useExclusiveModal(onClose)
// on mount: it announces itself on the "salt:modal" event (which closes any
// other open modal and collapses the sidebar drawer via App's listener) and
// closes itself when a *different* modal announces. Confirm/prompt dialogs
// (DialogHost) deliberately do NOT participate, so a confirmation can layer on
// top of an open modal.

export function announceModal(): symbol {
  const id = Symbol('modal');
  window.dispatchEvent(new CustomEvent('salt:modal', { detail: id }));
  return id;
}

export function useExclusiveModal(onClose: () => void) {
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const myId = announceModal();
    const onOther = (e: Event) => {
      if ((e as CustomEvent<symbol>).detail !== myId) closeRef.current();
    };
    window.addEventListener('salt:modal', onOther);
    return () => window.removeEventListener('salt:modal', onOther);
  }, []);
}

// Dropdown menus (⋯, share, workspace, tag colours, tree context) used to close
// only on mouse-leave, so clicking elsewhere left them hanging open — and on
// touch there is no leave event at all. useMenuDismiss closes on an outside
// pointer-down and on Escape. Pass the menu's wrapper ref; clicks INSIDE the
// wrapper (including the toggle button) are ignored so the button keeps its own
// toggle behaviour.
export function useMenuDismiss(
  open: boolean,
  ref: React.RefObject<HTMLElement | null>,
  onClose: () => void,
) {
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      const el = ref.current;
      if (el && !el.contains(e.target as Node)) closeRef.current();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeRef.current();
    };
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, ref]);
}
