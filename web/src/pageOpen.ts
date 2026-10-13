// How a link to a page opens, wherever it is clicked — a row in a table, a card
// on a board, a note in the notes column. It is the sidebar's rule, in one
// place so no view can spell it differently: a plain click navigates the tab
// you are in (the way a browser tab works), while ⌘-click (Ctrl-click on
// Windows and Linux) and the middle button add a tab beside the active one.
//
// `onOpenInNewTab` is optional because a view can be rendered without one — an
// embedded collection reaches it through the editor's context, but a bare
// component in a test may not. Plain navigation is then the honest fallback.

import { getPrefs } from './i18n';

export type PageOpeners = {
  onNavigate: (id: string) => void;
  onOpenInNewTab?: (id: string) => void;
};

const open = (id: string, o: PageOpeners) => (o.onOpenInNewTab ?? o.onNavigate)(id);

/** A click, with or without ⌘/Ctrl. */
export function openPage(
  e: { metaKey?: boolean; ctrlKey?: boolean },
  id: string,
  o: PageOpeners,
): void {
  if (e.metaKey || e.ctrlKey) open(id, o);
  else o.onNavigate(id);
}

/** The middle button, which a browser reports on `auxclick` — as every button
 *  but the left one — rather than as a `click`. */
export function openPageAux(
  e: { button?: number; preventDefault: () => void },
  id: string,
  o: PageOpeners,
): void {
  if (e.button !== 1) return;
  e.preventDefault();
  open(id, o);
}

// ---- collection rows ----

// Rows follow the sidebar rule: a plain click navigates the current tab,
// ⌘/Ctrl (or the middle button) adds a tab beside it. The account can opt
// into always opening rows in a new tab instead (server prefs, default off);
// the modifier then changes nothing, exactly as in a browser.
export const collectionRowNewTab = (): boolean => getPrefs().openRowsNewTab === true;

/** A click on a collection row, with or without ⌘/Ctrl. */
export function openCollectionRow(
  e: { metaKey?: boolean; ctrlKey?: boolean },
  id: string,
  o: PageOpeners,
): void {
  if (collectionRowNewTab() || e.metaKey || e.ctrlKey) open(id, o);
  else o.onNavigate(id);
}
