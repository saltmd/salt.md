import { useCallback, useEffect, useRef, useState } from 'react';

// Pointer-based dragging for the kanban board.
//
// Underneath this used to be the native HTML5 drag (`draggable`), and that
// feels bad for several reasons: the browser paints a pale ghost copy, there is
// no indication of WHERE the card will land, no auto-scroll, and on touch it
// does not work at all — which is why there was a separate ⋯ menu.
//
// This version uses pointer events (mouse and finger in one): a real floating
// card follows the pointer, the target column highlights, and on release the
// card moves. Hit testing goes through `data-col` on the column element rather
// than refs passed down — which keeps the caller lean.

export interface DragState {
  rowId: string;
  fromCol: string;
  title: string;
  width: number;
  // current pointer position and grab offset (where the card was taken hold of)
  x: number;
  y: number;
  dx: number;
  dy: number;
  over: string | null; // column under the pointer
}

const START_THRESHOLD = 5; // px before a click becomes a drag

// On a finger the rule is different. Five pixels are crossed instantly while
// scrolling — the card stuck to the finger instead of the board scrolling. So:
// hold first, then drag. Anybody who swipes before that scrolls.
const TOUCH_HOLD_MS = 320;
// How much wobble during the hold still counts as "holding". Above that it was
// a swipe, and the drag is never armed in the first place.
const TOUCH_HOLD_SLOP = 10;

export function useBoardDrag(onMove: (rowId: string, toCol: string) => void) {
  const [drag, setDrag] = useState<DragState | null>(null);
  // Which card is armed after the hold but has not moved yet. On the iPhone
  // there is no navigator.vibrate — without a visible signal nothing at all
  // happens between the hold and the first movement, and nobody knows whether
  // the card is on the finger now.
  const [armedRow, setArmedRow] = useState<string | null>(null);

  // Live data for the window listeners, so they do not have to be rebound on
  // every position update.
  const live = useRef<{
    rowId: string;
    fromCol: string;
    title: string;
    width: number;
    dx: number;
    dy: number;
    startX: number;
    startY: number;
    started: boolean;
    over: string | null;
    touch: boolean;
    // armed: on a finger only true after TOUCH_HOLD_MS. Before that every
    // movement belongs to scrolling.
    armed: boolean;
    holdTimer: number | null;
  } | null>(null);
  // Whether a drag really happened — this tells a click (navigate) from a drag
  // (move). A ref, because the pointerup handler needs it straight away.
  const draggedRef = useRef(false);

  const colUnder = (x: number, y: number): string | null => {
    for (const el of document.elementsFromPoint(x, y)) {
      const col = (el as HTMLElement).closest?.('[data-col]');
      if (col) return col.getAttribute('data-col');
    }
    return null;
  };

  useEffect(() => {
    const onPointerMove = (e: PointerEvent) => {
      const l = live.current;
      if (!l) return;
      const moved = Math.hypot(e.clientX - l.startX, e.clientY - l.startY);
      if (!l.started) {
        if (l.touch && !l.armed) {
          // Still inside the hold window: whoever swipes wants to scroll. Give
          // the drag up entirely, or the card snaps shut mid-scroll later.
          if (moved > TOUCH_HOLD_SLOP) {
            if (l.holdTimer) clearTimeout(l.holdTimer);
            live.current = null;
            setArmedRow(null);
          }
          return;
        }
        if (moved < START_THRESHOLD) return;
        l.started = true;
        draggedRef.current = true;
      }
      l.over = colUnder(e.clientX, e.clientY);
      setDrag({
        rowId: l.rowId,
        fromCol: l.fromCol,
        title: l.title,
        width: l.width,
        x: e.clientX,
        y: e.clientY,
        dx: l.dx,
        dy: l.dy,
        over: l.over,
      });

      // Scroll along at the top and bottom edge of a column, so cards can be
      // dragged into long lists without letting go.
      const cards = (document.elementFromPoint(e.clientX, e.clientY) as HTMLElement)?.closest?.(
        '.board-cards',
      ) as HTMLElement | null;
      if (cards) {
        const r = cards.getBoundingClientRect();
        if (e.clientY - r.top < 40) cards.scrollTop -= 12;
        else if (r.bottom - e.clientY < 40) cards.scrollTop += 12;
      }
    };

    const onPointerUp = () => {
      const l = live.current;
      if (l?.holdTimer) clearTimeout(l.holdTimer);
      live.current = null;
      setDrag(null);
      setArmedRow(null);
      if (l && l.started && l.over && l.over !== l.fromCol) {
        onMove(l.rowId, l.over);
      }
      // draggedRef stays set until the next click, so the card's click
      // handler can suppress navigation.
    };

    // The moment a real drag starts, the page must not scroll along. That
    // cannot be done through `touch-action`: the value is read when the gesture
    // begins, and changing it later has no effect on the running gesture. So the
    // hard way — passive: false, so preventDefault is allowed.
    const onTouchMove = (e: TouchEvent) => {
      if (live.current?.started) e.preventDefault();
    };

    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerUp);
    window.addEventListener('touchmove', onTouchMove, { passive: false });
    return () => {
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('pointercancel', onPointerUp);
      window.removeEventListener('touchmove', onTouchMove);
    };
  }, [onMove]);

  const startDrag = useCallback(
    (e: React.PointerEvent, rowId: string, fromCol: string, title: string) => {
      // Left mouse button or finger only, and not on a control of the card
      // (the ⋯ menu, a select chip) — there you want to click, not drag.
      if (e.button !== 0) return;
      if ((e.target as Element).closest?.('.card-move, .card-prop-edit, a, button')) return;
      const card = (e.currentTarget as HTMLElement).getBoundingClientRect();
      draggedRef.current = false;
      const touch = e.pointerType === 'touch' || e.pointerType === 'pen';
      live.current = {
        rowId,
        fromCol,
        title,
        width: card.width,
        dx: e.clientX - card.left,
        dy: e.clientY - card.top,
        startX: e.clientX,
        startY: e.clientY,
        started: false,
        over: fromCol,
        touch,
        armed: !touch, // mouse: armed at once. Finger: only after the hold.
        holdTimer: null,
      };
      if (touch) {
        const l = live.current;
        l.holdTimer = window.setTimeout(() => {
          if (live.current !== l) return;
          l.armed = true;
          setArmedRow(l.rowId);
          navigator.vibrate?.(12);
        }, TOUCH_HOLD_MS);
      }
    },
    [],
  );

  // The card click asks this: if a drag just happened, do NOT navigate.
  const consumeClick = useCallback(() => {
    if (draggedRef.current) {
      draggedRef.current = false;
      return true; // Klick gehoerte zu einem Ziehen — schlucken
    }
    return false;
  }, []);

  return { drag, armedRow, startDrag, consumeClick };
}
