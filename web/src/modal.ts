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

/** True while something modal is covering the app.
 *
 *  Read off the DOM rather than kept in a counter, and on purpose: every modal
 *  in this codebase renders a `.modal-overlay` (most also mark the panel
 *  inside it `aria-modal`), including the confirm/prompt dialogs that
 *  deliberately stay out of the coordinator above. A counter would have to be
 *  opted into one modal at a time, and the one that forgot would be the one
 *  that misbehaves — whereas a query is right for every modal that already
 *  looks like a modal, including the next one somebody writes.
 *
 *  What it is for: shortcuts that MOVE FOCUS in the application behind the
 *  modal (the arrow keys entering the sidebar) must do nothing while a modal
 *  owns the screen. */
export function modalOpen(): boolean {
  if (typeof document === 'undefined') return false;
  return !!document.querySelector('.modal-overlay, [aria-modal="true"]');
}
