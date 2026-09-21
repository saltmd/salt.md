import { useEffect, type RefObject } from 'react';
import { useShortcut, type Scope } from './keys';
import { modalOpen } from './modal';

// Focus regions: where the arrow keys mean something, and what they mean there.
//
// keys.ts answers "which feature owns this chord"; it cannot answer "where am
// I", and ↓ needs that first — it means the next page in the sidebar and the
// next line of a paragraph. So this module adds one concept: the focused
// REGION. Movement inside one is the region's business; ←/→ hand the keyboard
// to the region next door. Everything still goes through keys.ts, so the scope
// ranking, the typing guard and the help sheet keep working unchanged.
//
// THE DESIGN DECISION, because the rest follows from it: the DOM is the state.
// The focused item is `document.activeElement` and the order is document order.
// A selected-index in React state would have to be kept in step with a tree
// that nests, lazily loads rows, hides collapsed subtrees, filters by tag and
// reorders by drag; document order already is that list, for free — a collapsed
// subtree is not in the DOM, so it cannot be stepped into.
//
// A row opts in with markup alone: `data-nav-item`, `data-nav-key`, tabIndex
// -1. No registration per row, which is what keeps a 1700-line sidebar from
// growing a keyboard bureaucracy.

const ITEM = '[data-nav-item]';

/** Which way a keystroke is trying to go. Not the same as a chord: `up` is ↑
 *  and `k`, and a region only ever asks "am I allowed to leave this way". */
export type Dir = 'up' | 'down' | 'left' | 'right';

export type RegionOpts = {
  /** Stable region id: 'sidebar.tree', 'content'. What `next`/`prev` name. */
  id: string;
  ref: RefObject<HTMLElement | null>;
  scope?: Scope;
  /** The regions ←/→ hand over to. A missing side is a wall, and a wall lets
   *  the keystroke through rather than swallowing it. */
  prev?: string;
  next?: string;
  /** Arrows act even with the caret in a text field. Required for a region
   *  whose items ARE text surfaces (the document), wrong everywhere else. The
   *  j/k aliases are never registered when it is on: `j` belongs to the
   *  sentence being written. */
  whileTyping?: boolean;
  /** Veto, consulted before the focus moves. Return false and the keystroke is
   *  handed back to the browser untouched, so ↓ in a wrapped title still walks
   *  its visual lines — which is what makes `whileTyping` survivable. */
  canLeave?: (el: HTMLElement, dir: Dir) => boolean;
  /** Enter. */
  onActivate?: (el: HTMLElement, key: string) => void;
  /** → and ←. Return true when the region consumed the key (a tree unfolding a
   *  row); return false to let it pass to the neighbouring region. */
  onExpand?: (el: HTMLElement, key: string) => boolean;
  onCollapse?: (el: HTMLElement, key: string) => boolean;
  /** Help-sheet wording, written at the CALL SITE: → is "Expand" in a tree and
   *  something else in a table, and check-i18n only counts a string it can see
   *  as a literal `t('…')` there. A region that leaves these out does not appear
   *  in the sheet — right for the document, where ↑/↓ are how text works rather
   *  than a shortcut anybody needs taught. */
  labels?: {
    group?: () => string;
    next?: () => string;
    prev?: () => string;
    activate?: () => string;
    expand?: () => string;
    collapse?: () => string;
  };
};

const regions = new Map<string, RegionOpts>();

/** The item each region was last left on, keyed by `data-nav-key`. Without it,
 *  going into a document and coming back drops you at the top of the tree. */
const remembered = new Map<string, string>();

/** The region focus was last in — not the same as where it is now, the whole
 *  point being to answer ↑/↓ pressed from outside every region. A focusin
 *  listener keeps it honest for moves this module did not make, so a row
 *  reached by mouse or Tab counts the same as one reached with an arrow. */
let lastRegion: string | null = null;

let watching = false;

/** Attached once, lazily, by the first region that registers. */
function watchFocus() {
  if (watching || typeof window === 'undefined') return;
  watching = true;
  window.addEventListener(
    'focusin',
    (e) => {
      const id = regionContaining(e.target as Element | null);
      if (id) lastRegion = id;
    },
    true,
  );
}

const rootOf = (id: string) => regions.get(id)?.ref.current ?? null;
const keyOf = (el: HTMLElement) => el.dataset.navKey ?? '';

/** Items in document order, minus anything not currently on screen.
 *  checkVisibility also covers `content-visibility`, which offsetParent (the
 *  fallback for browsers without it) does not. */
function itemsOf(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(ITEM)].filter((el) =>
    el.checkVisibility ? el.checkVisibility() : el.offsetParent !== null,
  );
}

/** Where focus should actually land for an item. A document body is a nav item
 *  but not focusable — its ProseMirror surface is what takes a caret. Resolving
 *  it here is what lets the title and the body be two items of one region
 *  though two sibling components render them. */
const focusTargetOf = (el: HTMLElement): HTMLElement =>
  el.matches('input, textarea, [contenteditable="true"]')
    ? el
    : (el.querySelector<HTMLElement>('[contenteditable="true"], input, textarea') ?? el);

/** The item focus is inside, which is not the same as the focused element: in
 *  the document the caret sits in ProseMirror, several nodes below the item. */
function currentItem(root: HTMLElement): HTMLElement | null {
  const el = document.activeElement as HTMLElement | null;
  if (!el || !root.contains(el)) return null;
  const item = el.closest<HTMLElement>(ITEM);
  if (!item) return null;
  // A row contains buttons — chevron, ＋, ⋯ — and focus on one of THOSE belongs
  // to the button: Enter must open the menu, not the page underneath it. Text
  // fields are deliberately absent from the list; they are the document
  // region's own items, guarded by keys.ts instead.
  if (el !== item && el.closest('button, a, [role="button"], select')) return null;
  return item;
}

// ---- the pure parts, so check-nav.mjs can drive them without a browser ----

/** Neighbour index, or -1 at the edge. Deliberately NOT circular: wrapping
 *  costs the one thing a keyboard gives, knowing where you are without looking.
 *  From nowhere (-1), ↓ enters at the top. */
export function neighbour(length: number, i: number, d: 1 | -1): number {
  if (length === 0) return -1;
  if (i < 0) return d > 0 ? 0 : length - 1;
  const n = i + d;
  return n < 0 || n >= length ? -1 : n;
}

/** Who inherits the focus when the item at `i` is about to disappear: the row
 *  below, or the one above when it was the last. */
export function heir(length: number, i: number): number {
  if (i < 0 || length <= 1) return -1;
  return i + 1 < length ? i + 1 : i - 1;
}

/** Which region an arrow pressed from OUTSIDE every region should enter.
 *  `order` is the regions on screen, left to right; `from` is the one the
 *  keyboard has most to do with (physically containing the focus, else the one
 *  it was last in).
 *
 *  ← and → name a SIDE and are read literally. ↑ and ↓ name none, so they
 *  resume — and fall back to the FIRST region, not the last, because entering
 *  the sidebar lands on a row while entering the document lands a caret in the
 *  title: the guess is made in the direction that cannot damage anything. */
export function entryRegion(dir: Dir, order: string[], from: string | null): string | null {
  if (order.length === 0) return null;
  if (dir === 'left') return order[0];
  if (dir === 'right') return order[order.length - 1];
  return from && order.includes(from) ? from : order[0];
}

/** Is a caret allowed to leave a one-line text field downwards? Only from the
 *  very end, nothing selected. Measuring the "last visual line" of a textarea
 *  takes a mirrored div and a font metric and buys nothing: on a title that
 *  wraps, ↓ walks the lines and leaves on the press after the last. */
export const exitsDown = (s: { start: number; end: number; length: number }): boolean =>
  s.start === s.end && s.start === s.length;

/** ...and upwards or leftwards: from offset 0, nothing selected. */
export const exitsStart = (s: { start: number; end: number }): boolean =>
  s.start === s.end && s.start === 0;

/** True when the collapsed caret has no text before it anywhere in `surface`.
 *  A Range comparison rather than BlockNote's "first block, offset 0": it stays
 *  true inside tables, columns and nested lists, where a block-level check is
 *  answering a different question. */
export function nothingBefore(surface: Element): boolean {
  const sel = window.getSelection();
  if (!sel || !sel.isCollapsed || !sel.anchorNode || !surface.contains(sel.anchorNode)) return false;
  const r = document.createRange();
  r.selectNodeContents(surface);
  r.setEnd(sel.anchorNode, sel.anchorOffset);
  return r.toString().length === 0;
}

// ---- moving ----

/** Roving tabindex: the remembered item is the one Tab reaches, every other is
 *  reachable only from here. Rows carry tabIndex={-1} in their JSX and React
 *  never rewrites a prop it did not change, so promoting one by hand holds. */
function promote(root: HTMLElement, el: HTMLElement) {
  for (const it of root.querySelectorAll<HTMLElement>(ITEM)) {
    // Only items that came with a tabindex of their own. A title is a
    // <textarea>, already in the tab order; writing -1 onto it to mark it "not
    // current" would take it out of that order for the sake of a ring.
    if (it.hasAttribute('tabindex')) it.tabIndex = it === el ? 0 : -1;
  }
}

export function focusItem(regionId: string, el: HTMLElement | null): boolean {
  const root = rootOf(regionId);
  if (!root || !el) return false;
  promote(root, el);
  focusTargetOf(el).focus();
  // 'nearest' rather than centring: a list that jumps under the reader on every
  // keystroke is harder to follow than one that only scrolls when it must.
  el.scrollIntoView({ block: 'nearest' });
  const k = keyOf(el);
  if (k) remembered.set(regionId, k);
  lastRegion = regionId;
  return true;
}

/** Focus a region, at the item it was left on. `tries` is for the case that is
 *  not an edge case: Enter in the sidebar opens a page AND follows it, but the
 *  document mounts on a later render, so the region has no items yet when the
 *  key is released. Retrying waits for the thing; a setTimeout guesses. */
export function focusRegion(id: string, tries = 12): boolean {
  const root = rootOf(id);
  const list = root ? itemsOf(root) : [];
  if (list.length === 0) {
    if (tries > 0) requestAnimationFrame(() => focusRegion(id, tries - 1));
    return false;
  }
  const want = remembered.get(id);
  return focusItem(id, (want && list.find((el) => keyOf(el) === want)) || list[0]);
}

/** Focus one named item of a region ('body', 'title'). For the deliberate jumps
 *  that are not a step — Enter in a title going to the text it titles, or
 *  creating a page and landing in its title, which is what `tries` is for (see
 *  focusRegion: the item arrives a render and a fetch later).
 *
 *  `guard`, checked against the region's root before the item counts as
 *  found: a freshly created page navigates into the SAME 'content' region a
 *  moment before React swaps its contents, so without it the retry finds the
 *  outgoing page's title on the first try — a title, just the wrong one — and
 *  stops there instead of waiting for the page it was actually sent to. */
export function focusKey(
  regionId: string,
  key: string,
  tries = 0,
  guard?: (root: HTMLElement) => boolean,
): boolean {
  const root = rootOf(regionId);
  const el = root && (!guard || guard(root)) && itemsOf(root).find((it) => keyOf(it) === key);
  if (el) return focusItem(regionId, el);
  if (tries > 0) requestAnimationFrame(() => focusKey(regionId, key, tries - 1, guard));
  return false;
}

export const isInRegion = (id: string): boolean => {
  const root = rootOf(id);
  return !!root && !!currentItem(root);
};

/** The region whose ITEM has the focus, or null when focus is anywhere else —
 *  a row's ⋯ button, a toolbar, <body> after a click in the margin. */
export function focusedRegion(): string | null {
  for (const id of regions.keys()) if (isInRegion(id)) return id;
  return null;
}

/** The region whose root merely CONTAINS an element. Looser than focusedRegion
 *  on purpose: focus on the sidebar's search button is not on a row, but ↓ from
 *  there plainly means the tree and not the document. */
function regionContaining(el: Element | null): string | null {
  if (!el) return null;
  for (const [id, o] of regions) if (o.ref.current?.contains(el)) return id;
  return null;
}

/** The regions on screen, left to right, followed along the `prev`/`next` links
 *  the regions already declare for ←/→ — no second place to state the layout.
 *  Anything the walk misses is appended in registration order.
 *
 *  Only regions with items count: an unmounted editor or a sidebar filtered down
 *  to nothing is not somewhere focus can usefully land. */
function orderedRegions(): string[] {
  const live = [...regions.keys()].filter((id) => {
    const root = rootOf(id);
    return !!root && itemsOf(root).length > 0;
  });
  const set = new Set(live);
  const head = live.find((id) => {
    const p = regions.get(id)?.prev;
    return !p || !set.has(p);
  });
  const out: string[] = [];
  for (let cur = head; cur && set.has(cur) && !out.includes(cur); cur = regions.get(cur)?.next)
    out.push(cur);
  for (const id of live) if (!out.includes(id)) out.push(id);
  return out;
}

/** The `data-nav-key` focus is on in this region, or null. What an action
 *  shortcut ("trash this") uses as its target. */
export const focusedKey = (id: string): string | null => {
  const root = rootOf(id);
  const el = root && currentItem(root);
  return el ? keyOf(el) || null : null;
};

/** Run something that removes the focused row, and land the focus on its
 *  neighbour afterwards. Not a nicety: the element leaves the DOM with the focus
 *  on it, which drops focus to <body>, from where no arrow does anything and the
 *  only way back in is the mouse. */
export async function withFocusSurvival(regionId: string, act: () => void | Promise<void>) {
  const root = rootOf(regionId);
  const list = root ? itemsOf(root) : [];
  const i = root ? list.indexOf(currentItem(root) as HTMLElement) : -1;
  const goneKey = i >= 0 ? keyOf(list[i]) : null;
  const h = heir(list.length, i);
  const heirKey = h >= 0 ? keyOf(list[h]) : null;

  await act();

  // Wait for the row to actually GO rather than for one frame and a hope: even
  // a promise would only cover the request, not the re-render after it. If it
  // never leaves, the action failed, and the focus belongs where it was rather
  // than moved on the strength of a delete that did not happen.
  const land = (tries: number) => {
    const now = rootOf(regionId);
    const fresh = now ? itemsOf(now) : [];
    if (fresh.length === 0) return;
    const stillThere = goneKey ? fresh.some((el) => keyOf(el) === goneKey) : false;
    if (stillThere && tries > 0) {
      requestAnimationFrame(() => land(tries - 1));
      return;
    }
    const byKey = heirKey ? fresh.find((el) => keyOf(el) === heirKey) : null;
    // The heir can be gone too — it was a child of what we just deleted — so
    // fall back to whatever now stands where we were.
    focusItem(regionId, byKey ?? fresh[Math.min(Math.max(i, 0), fresh.length - 1)]);
  };
  requestAnimationFrame(() => land(12));
}

// ---- the hook ----

/** Make an element a focus region, with the arrow keys it implies. Ids are
 *  shared across regions on purpose (`nav.down` is `nav.down` everywhere): only
 *  the focused region's `when` passes, so exactly one is resolvable at a time
 *  and the help sheet shows one row for ↓ rather than one per region. */
export function useNavRegion(o: RegionOpts) {
  useEffect(() => {
    regions.set(o.id, o);
    watchFocus();
    return () => {
      if (regions.get(o.id) === o) regions.delete(o.id);
    };
  });

  const here = () => {
    const root = o.ref.current;
    return root && currentItem(root) ? root : null;
  };

  /** ↑/↓ inside the region. Returns false — and so hands the key back — at the
   *  edges and whenever the region vetoes the move. */
  const step = (d: 1 | -1, dir: Dir) => {
    const root = here();
    if (!root) return false;
    const cur = currentItem(root);
    if (cur && o.canLeave && !o.canLeave(cur, dir)) return false;
    const list = itemsOf(root);
    const n = neighbour(list.length, cur ? list.indexOf(cur) : -1, d);
    return n < 0 ? false : focusItem(o.id, list[n]);
  };

  const edge = (d: 1 | -1) => {
    const root = here();
    if (!root) return false;
    const list = itemsOf(root);
    return list.length === 0 ? false : focusItem(o.id, d > 0 ? list[list.length - 1] : list[0]);
  };

  /** ←/→: the region gets first refusal (unfold a row), then the neighbour. */
  const sideways = (dir: 'left' | 'right') => {
    const root = here();
    if (!root) return false;
    const cur = currentItem(root);
    const own = dir === 'right' ? o.onExpand : o.onCollapse;
    if (cur && own && own(cur, keyOf(cur))) return true;
    if (cur && o.canLeave && !o.canLeave(cur, dir)) return false;
    const to = dir === 'right' ? o.next : o.prev;
    // No retrying here, unlike Enter-then-follow: the neighbour either exists
    // now or the key belongs to the browser. Retrying across a dozen frames
    // meant ← at the start of a title with the sidebar hidden moved the caret
    // AND then yanked the focus away a moment later.
    return to ? focusRegion(to, 0) : false;
  };

  // Arrows follow the region's typing posture; the vim aliases never do, so `j`
  // stays a letter wherever a letter can be typed.
  const vert = (arrow: string, letter: string) => (o.whileTyping ? [arrow] : [arrow, letter]);

  useShortcut({
    id: 'nav.down',
    keys: vert('arrowdown', 'j'),
    scope: o.scope,
    whileTyping: o.whileTyping,
    when: () => !!here(),
    label: o.labels?.next,
    group: o.labels?.group,
    run: () => step(1, 'down'),
  });

  useShortcut({
    id: 'nav.up',
    keys: vert('arrowup', 'k'),
    scope: o.scope,
    whileTyping: o.whileTyping,
    when: () => !!here(),
    label: o.labels?.prev,
    group: o.labels?.group,
    run: () => step(-1, 'up'),
  });

  useShortcut({
    id: 'nav.first',
    keys: ['home'],
    scope: o.scope,
    when: () => !!here(),
    run: () => edge(-1),
  });

  useShortcut({
    id: 'nav.last',
    keys: ['end'],
    scope: o.scope,
    when: () => !!here(),
    run: () => edge(1),
  });

  useShortcut({
    id: 'nav.activate',
    keys: ['enter'],
    scope: o.scope,
    when: () => !!o.onActivate && !!here(),
    label: o.labels?.activate,
    group: o.labels?.group,
    run: () => {
      const root = here();
      const cur = root && currentItem(root);
      if (!cur) return false;
      o.onActivate?.(cur, keyOf(cur));
    },
  });

  useShortcut({
    id: 'nav.in',
    keys: ['arrowright'],
    scope: o.scope,
    whileTyping: o.whileTyping,
    when: () => !!here(),
    label: o.labels?.expand,
    group: o.labels?.group,
    run: () => sideways('right'),
  });

  useShortcut({
    id: 'nav.out',
    keys: ['arrowleft'],
    scope: o.scope,
    whileTyping: o.whileTyping,
    when: () => !!here(),
    label: o.labels?.collapse,
    group: o.labels?.group,
    run: () => sideways('left'),
  });
}

/** The way IN, for a keyboard that is not in a region yet — the one arrow-key
 *  behaviour that belongs to no region, since every shortcut in useNavRegion is
 *  conditioned on already being somewhere. Registered once, for the whole app.
 *
 *  Three things it deliberately does not do: fire under a modal (the sheet
 *  opened with `?` has no items of its own, and ↓ must not walk the tree behind
 *  it); fire while something is being typed (keys.ts's guard, free from leaving
 *  `whileTyping` off); or fire inside a widget that claims the arrows itself,
 *  which says so with `data-nav-skip` rather than by being listed here. */
export function useNavEntry(o: { labels?: { group?: () => string; prev?: () => string; next?: () => string } } = {}) {
  const enter = (dir: Dir) => {
    const el = document.activeElement as Element | null;
    if (el?.closest('[data-nav-skip]')) return false;
    const order = orderedRegions();
    const to = entryRegion(dir, order, regionContaining(el) ?? lastRegion);
    return to ? focusRegion(to, 0) : false;
  };

  /** Only from outside every region, and never from under a modal. The modal
   *  test belongs in `when` rather than in `enter` because `when` is what the
   *  help sheet filters on: otherwise the sheet lists these two rows next to a
   *  key that does nothing while you are reading it. */
  const idle = () => !modalOpen() && !focusedRegion();

  useShortcut({
    id: 'nav.enter.prev',
    keys: ['arrowleft'],
    when: idle,
    label: o.labels?.prev,
    group: o.labels?.group,
    run: () => enter('left'),
  });

  useShortcut({
    id: 'nav.enter.next',
    keys: ['arrowright'],
    when: idle,
    label: o.labels?.next,
    group: o.labels?.group,
    run: () => enter('right'),
  });

  // Unlabelled, and so absent from the sheet: ↑/↓ from nowhere is a way back in
  // rather than an action, and listing it would explain the mechanism.
  useShortcut({
    id: 'nav.enter.resume',
    keys: ['arrowup', 'arrowdown'],
    when: idle,
    run: (e) => enter(e.key === 'ArrowUp' ? 'up' : 'down'),
  });
}

/** Props that make an element a step in a region. Spread, so a row gains
 *  keyboard navigation in one expression and cannot half-gain it.
 *
 *  `keepTabOrder` where the tab order must be left alone: the element already
 *  takes focus (the title is a <textarea>), or the thing that takes the caret is
 *  a tabbable child, and a tabindex on the wrapper would add a useless stop. */
export const navItem = (key: string, o?: { keepTabOrder?: boolean }) => ({
  'data-nav-item': '',
  'data-nav-key': key,
  ...(o?.keepTabOrder ? {} : { tabIndex: -1 }),
});
