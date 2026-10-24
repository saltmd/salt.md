import { useEffect, useRef, useState } from 'react';
import { createReactBlockSpec } from '@blocknote/react';
import { Table2 } from 'lucide-react';
import { useBlockCtx } from './blockContext';
import { MERMAID_REV, renderMermaid } from './mermaidLoader';
import { PageIcon } from './pageIcon';
import { openPage, openPageAux } from './pageOpen';
import CollectionView, { type EmbedState } from './components/CollectionView';
import { plural, t } from './i18n';
import {
  MIN_PCT,
  PRESETS,
  equalWidths,
  formatWidths,
  gridTemplate,
  moveDivider,
  parseWidths,
  sameWidths,
} from './columnWidths';

// Custom salt.md block types (Welle 17): callout, table of contents, bookmark.
// Columns are ours (columnsSpec, at the end of this file) and wired in pageLink.tsx.

// safeHref collapses any non-http(s)/mailto URL to '#'. A bookmark url can be
// planted via realtime collab or the API (which bypass the input handler's
// https:// normalization), so `javascript:` values must never reach an <a href>.
function safeHref(u: string): string {
  try {
    const p = new URL(u, window.location.origin).protocol.toLowerCase();
    return p === 'http:' || p === 'https:' || p === 'mailto:' ? u : '#';
  } catch {
    return '#';
  }
}

// ---- Callout ----
// An emphasized box with a leading emoji. Clicking the emoji cycles through a
// preset list (kept simple on purpose — no popover to fight the editor focus).

const CALLOUT_TONES: Record<string, string> = {
  '💡': 'neutral',
  '⚠️': 'warning',
  '❗': 'danger',
  '✅': 'success',
  '📌': 'neutral',
  '🔥': 'amber',
  'ℹ️': 'info',
};
const CALLOUT_EMOJIS = Object.keys(CALLOUT_TONES);

export const calloutSpec = createReactBlockSpec(
  {
    type: 'callout',
    propSchema: {
      emoji: { default: '💡' },
    },
    content: 'inline',
  } as const,
  {
    render: (props) => {
      const { block, editor, contentRef } = props;
      const emoji = (block.props as { emoji: string }).emoji || '💡';
      const cycle = () => {
        const i = CALLOUT_EMOJIS.indexOf(emoji);
        const next = CALLOUT_EMOJIS[(i + 1) % CALLOUT_EMOJIS.length];
        editor.updateBlock(block, { props: { emoji: next } } as never);
      };
      return (
        <div className="bn-callout" data-tone={CALLOUT_TONES[emoji] ?? 'neutral'}>
          <button
            type="button"
            className="bn-callout-emoji"
            contentEditable={false}
            title={t('Change symbol')}
            onClick={cycle}
          >
            {emoji}
          </button>
          <div className="bn-callout-content" ref={contentRef} />
        </div>
      );
    },
  },
);

// ---- Table of contents ----
// Lists the document's headings; clicking scrolls to the heading. Recomputed
// on every document change while mounted.

interface TocEntry {
  id: string;
  level: number;
  text: string;
}

function collectHeadings(blocks: unknown[], out: TocEntry[]) {
  for (const b of blocks as {
    id?: string;
    type?: string;
    props?: { level?: number };
    content?: { text?: string }[];
    children?: unknown[];
  }[]) {
    if (b?.type === 'heading' && b.id) {
      const text = (b.content ?? [])
        .map((c) => c.text ?? '')
        .join('')
        .trim();
      out.push({ id: b.id, level: b.props?.level ?? 1, text: text || t('Untitled') });
    }
    if (b?.children?.length) collectHeadings(b.children, out);
  }
}

export const tocSpec = createReactBlockSpec(
  {
    type: 'toc',
    propSchema: {},
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { editor } = props;
      const [entries, setEntries] = useState<TocEntry[]>([]);
      useEffect(() => {
        const compute = () => {
          const out: TocEntry[] = [];
          collectHeadings(editor.document as unknown[], out);
          setEntries(out);
        };
        compute();
        const unsub = editor.onChange?.(compute);
        return () => {
          if (typeof unsub === 'function') unsub();
        };
      }, [editor]);
      return (
        <div className="bn-toc" contentEditable={false}>
          <div className="bn-toc-title">{t('Contents')}</div>
          {entries.length === 0 && <div className="bn-toc-empty">{t('No headings.')}</div>}
          {entries.map((e) => (
            <button
              key={e.id}
              type="button"
              className="bn-toc-entry"
              style={{ paddingLeft: 8 + (e.level - 1) * 14 }}
              onClick={() => {
                document
                  .querySelector(`[data-id="${e.id}"]`)
                  ?.scrollIntoView({ behavior: 'smooth', block: 'start' });
              }}
            >
              {e.text}
            </button>
          ))}
        </div>
      );
    },
  },
);

// ---- Sub-pages ----
// The page's direct children as an inline block, next to Contents. The same
// data the Structure panel shows, but placed by the author in the document.
export const subpagesSpec = createReactBlockSpec(
  {
    type: 'subpages',
    propSchema: {},
    content: 'none',
  } as const,
  {
    render: () => {
      const { pagesById, pageId, onNavigate, onOpenInNewTab } = useBlockCtx();
      if (!pageId) return null;
      const openers = { onNavigate, onOpenInNewTab };
      const kids = [...pagesById.values()]
        .filter((p) => p.parentId === pageId && !p.trashed)
        .sort((a, b) => (a.position ?? 0) - (b.position ?? 0));
      return (
        <div className="bn-toc bn-subpages" contentEditable={false}>
          <div className="bn-toc-title">{t('Sub-pages')}</div>
          {kids.length === 0 && <div className="bn-toc-empty">{t('No sub-pages')}</div>}
          {kids.map((p) => (
            <button
              key={p.id}
              type="button"
              className="bn-toc-entry"
              onClick={(e) => openPage(e, p.id, openers)}
              onAuxClick={(e) => openPageAux(e, p.id, openers)}
              title={p.title || t('Untitled')}
            >
              <PageIcon icon={p.icon} size={14} /> {p.title || t('Untitled')}
            </button>
          ))}
        </div>
      );
    },
  },
);

// ---- Bookmark / embed ----
// Paste a URL: YouTube/Vimeo render as an embedded player, everything else as
// a link card. Stored as a plain {url} prop so export stays trivial.

function embedSrc(url: string): string | null {
  try {
    const u = new URL(url);
    const host = u.hostname.replace(/^www\./, '');
    if (host === 'youtube.com' || host === 'm.youtube.com') {
      const v = u.searchParams.get('v');
      if (v) return `https://www.youtube-nocookie.com/embed/${v}`;
    }
    if (host === 'youtu.be') {
      const id = u.pathname.slice(1).split('/')[0];
      if (id) return `https://www.youtube-nocookie.com/embed/${id}`;
    }
    if (host === 'vimeo.com') {
      const id = u.pathname.slice(1).split('/')[0];
      if (/^\d+$/.test(id)) return `https://player.vimeo.com/video/${id}`;
    }
  } catch {
    /* not a URL */
  }
  return null;
}

export const bookmarkSpec = createReactBlockSpec(
  {
    type: 'bookmark',
    propSchema: {
      url: { default: '' },
    },
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { block, editor } = props;
      const url = (block.props as { url: string }).url;
      const [draft, setDraft] = useState('');

      if (!url) {
        return (
          <div className="bn-bookmark-input" contentEditable={false}>
            <input
              className="prop-input"
              placeholder={t('Paste a link (https://…) and press Enter')}
              value={draft}
              autoFocus
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                e.stopPropagation();
                if (e.key === 'Enter' && draft.trim()) {
                  let u = draft.trim();
                  if (!/^https?:\/\//i.test(u)) u = 'https://' + u;
                  editor.updateBlock(block, { props: { url: u } } as never);
                }
              }}
            />
          </div>
        );
      }

      const src = embedSrc(url);
      if (src) {
        return (
          <div className="bn-embed" contentEditable={false}>
            <iframe
              src={src}
              title={url}
              loading="lazy"
              allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
              allowFullScreen
            />
          </div>
        );
      }
      let host = url;
      try {
        host = new URL(url).hostname.replace(/^www\./, '');
      } catch {
        /* keep raw */
      }
      return (
        <a
          className="bn-bookmark"
          href={safeHref(url)}
          target="_blank"
          rel="noopener noreferrer"
          contentEditable={false}
        >
          <span className="bn-bookmark-icon">🔖</span>
          <span className="bn-bookmark-body">
            <span className="bn-bookmark-url">{url}</span>
            <span className="bn-bookmark-host">{host}</span>
          </span>
        </a>
      );
    },
  },
);

// ---- Embedded database ----
//
// Notion's model: a database is not a page type but a BLOCK. It can stand as
// its own page OR sit inside a document with text above and below it. This
// block is the second form.
//
// Only the database's id is stored — the database itself stays ONE object in
// ONE place. The block is a view onto it, not a copy, so the same database can
// appear in several documents and an edit shows up everywhere at once.
//
// The reason this block exists: until now you had to create an introductory
// document AND a database separately, because a database page cannot have a
// body of text. Now both live in one document.

type EmbedLocal = { viewId: string; views: EmbedState['views'] };

// Tolerant on purpose: the prop is free text in a document anybody (or any
// agent) can write, and a malformed value must cost the embed its filters, not
// the document its rendering.
function parseEmbedLocal(raw: string): EmbedLocal {
  try {
    const v = raw ? JSON.parse(raw) : null;
    if (v && typeof v === 'object') {
      return {
        viewId: typeof v.viewId === 'string' ? v.viewId : '',
        views: v.views && typeof v.views === 'object' ? v.views : {},
      };
    }
  } catch {
    /* fall through */
  }
  return { viewId: '', views: {} };
}

export const databaseSpec = createReactBlockSpec(
  {
    type: 'database',
    propSchema: {
      collectionId: { default: '' },
      // This embed's own way of looking at the collection, as JSON: the view
      // it shows, and per view the filters and sort it applies (#26). Empty
      // means "as the collection's views say" — every embed written before.
      local: { default: '' },
    },
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { block, editor } = props;
      const collectionId = (block.props as { collectionId: string }).collectionId;
      const localRaw = (block.props as { local?: string }).local ?? '';
      const { pagesById, tagColors, onNavigate, onOpenInNewTab, onPagesChanged } = useBlockCtx();
      const [q, setQ] = useState('');
      // Somebody who may read the document but not change it can still filter
      // what they see; it just stays with them instead of being written back.
      const [unsaved, setUnsaved] = useState<EmbedLocal | null>(null);
      const local = unsaved ?? parseEmbedLocal(localRaw);
      const keep = (next: EmbedLocal) => {
        if (editor.isEditable) editor.updateBlock(block, { props: { local: JSON.stringify(next) } } as never);
        else setUnsaved(next);
      };
      const embed: EmbedState = {
        viewId: local.viewId,
        views: local.views,
        onViewId: (viewId) => keep({ ...local, viewId }),
        onView: (viewId, patch) =>
          keep({ ...local, views: { ...local.views, [viewId]: { ...local.views[viewId], ...patch } } }),
      };

      if (!collectionId) {
        const dbs = [...pagesById.values()]
          .filter((p) => p.type === 'collection' && !p.trashed)
          .filter((p) => (p.title || '').toLowerCase().includes(q.trim().toLowerCase()));
        return (
          <div className="bn-db-picker" contentEditable={false}>
            <input
              className="prop-input"
              placeholder={t('Search collections…')}
              value={q}
              autoFocus
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={(e) => e.stopPropagation()}
            />
            <div className="bn-db-picker-list">
              {dbs.length === 0 && <div className="bn-db-picker-empty">{t('No collection found')}</div>}
              {dbs.slice(0, 8).map((d) => (
                <button
                  key={d.id}
                  type="button"
                  onClick={() => editor.updateBlock(block, { props: { collectionId: d.id } } as never)}
                >
                  <PageIcon icon={d.icon} size={15} fallback={<Table2 size={15} />} />{' '}
                  {d.title || t('Untitled')}
                </button>
              ))}
            </div>
          </div>
        );
      }

      const db = pagesById.get(collectionId);
      if (!db) {
        // Deleted, or in a workspace this reader is not allowed to see. Not a
        // crash — an honest note.
        return (
          <div className="bn-db-missing" contentEditable={false}>
            {t('This collection is no longer available.')}
          </div>
        );
      }
      return (
        <div className="bn-db-embed" contentEditable={false}>
          <button className="bn-db-title" type="button" onClick={() => onNavigate(collectionId)}>
            <PageIcon icon={db.icon} size={16} fallback={<Table2 size={16} />} /> {db.title || t('Untitled')}
            <span className="bn-db-open">{t('Open as page ↗')}</span>
          </button>
          <CollectionView
            collectionId={collectionId}
            pages={pagesById}
            tagColors={tagColors}
            onNavigate={onNavigate}
            onOpenInNewTab={onOpenInNewTab}
            onPagesChanged={onPagesChanged}
            embed={embed}
          />
        </div>
      );
    },
  },
);

// ---- Columns ----
//
// Blocks side by side. This used to be @blocknote/xl-multi-column, BlockNote's
// paid tier; ours is a fraction of it on purpose.
//
// The trick is that we build no container at all. Every BlockNote block already
// carries CHILDREN — that is how indenting a list works — and BlockNote renders
// them into a .bn-block-group of their own. So a columns block holds nothing
// itself and only lays its children out in a row. Each child is one column, and
// everything that already works about nesting keeps working: dragging a block
// in, indenting into it, the drag handle, undo.
//
// What we give up against the paid one: dragging a block SIDEWAYS to make a new
// column. Worth having, and not worth a licence that has to be renegotiated the
// day part of salt.md is closed. Column WIDTHS are ours: preset layouts on the
// bar, and a grip between columns that drags like a table column's.
export const columnsSpec = createReactBlockSpec(
  {
    type: 'columns',
    propSchema: {
      // 2 or 3. Stored so the layout survives a reload, and so the export knows
      // how to divide the page without measuring anything.
      count: { default: 2 },
      // The widths in percent, "70,30". Empty is equal columns. See
      // columnWidths.ts; the server reads it with the same rule.
      widths: { default: '' },
    },
    content: 'none',
  } as const,
  {
    render: (props) => <ColumnsBlock block={props.block as never} editor={props.editor as never} />,
  },
);

type ColumnsEditor = {
  updateBlock: (b: unknown, update: unknown) => void;
  insertBlocks: (blocks: unknown[], ref: unknown, place: 'before' | 'after') => void;
  removeBlocks: (blocks: unknown[]) => void;
  getBlock: (id: string) => ColumnsBlockData | undefined;
  isEditable: boolean;
};
type ColumnsBlockData = {
  id: string;
  props: { count?: number; widths?: string };
  children: { id: string; type: string; content?: unknown[]; children: unknown[] }[];
};

// The row the columns sit in is the .bn-block-group BELOW this block, so the
// divider cannot live in it. It lives in this block's own strip, laid over the
// group, and is placed from the group's measured box — measured, never written:
// see the note in ColumnsBlock about marking editor DOM.
type Geometry = { left: number; top: number; width: number; gap: number; height: number };

function ColumnsBlock({ block, editor }: { block: ColumnsBlockData; editor: ColumnsEditor }) {
  const count = Number(block.props.count) === 3 ? 3 : 2;
  const stored = parseWidths(block.props.widths, count);
  // The widths being dragged right now. Kept out of the block so the drag is
  // local and instant: updating the block on every pointermove would put one
  // edit per pixel into the undo history and over the wire to collaborators.
  const [drag, setDrag] = useState<number[] | null>(null);
  const widths = drag ?? stored ?? equalWidths(count);
  // Where the tracks really are. Equal columns are 1fr each, a true third, not
  // the 33/33/34 that equalWidths hands out for storing: drawn from that the
  // divider sat up to 3px off the gap's middle.
  const drawn = drag ?? stored ?? Array<number>(count).fill(100 / count);
  const strip = useRef<HTMLDivElement>(null);
  const [geo, setGeo] = useState<Geometry | null>(null);

  useEffect(() => {
    const row = strip.current?.closest('.bn-block')?.querySelector(':scope > .bn-block-group');
    if (!(row instanceof HTMLElement) || !strip.current) return setGeo(null);
    const measure = () => {
      const r = row.getBoundingClientRect();
      const s = strip.current!.getBoundingClientRect();
      const gap = parseFloat(getComputedStyle(row).columnGap) || 0;
      setGeo({ left: r.left - s.left, top: r.top - s.top, width: r.width, height: r.height, gap });
    };
    measure();
    // The strip too: the row can stay the same size while the strip above it
    // grows or shrinks (the bar wrapping on a narrow screen), and the row's top
    // is measured from the strip.
    const ro = new ResizeObserver(measure);
    ro.observe(row);
    ro.observe(strip.current);
    return () => ro.disconnect();
  }, [count, block.children.length]);

  const commit = (w: number[]) => {
    editor.updateBlock(block, { props: { count, widths: formatWidths(w) } });
  };

  // A layout from the bar. Changing the count changes the number of columns, so
  // the blocks have to follow: three columns with two children would be a third
  // track with nothing in it and no way to type there.
  const apply = (w: readonly number[]) => {
    const n = w.length;
    const live = editor.getBlock(block.id) ?? block;
    editor.updateBlock(block, { props: { count: n, widths: formatWidths(w) } });
    const kids = live.children;
    if (n > kids.length) {
      const fresh = Array.from({ length: n - kids.length }, () => ({ type: 'paragraph' }));
      if (kids.length) editor.insertBlocks(fresh, kids[kids.length - 1], 'after');
      else editor.updateBlock(block, { children: fresh });
    } else if (n < kids.length) {
      // Only an empty last column goes. One with something in it stays, and
      // wraps below, rather than a click on a layout deleting someone's text.
      const last = kids[kids.length - 1];
      if (last.type === 'paragraph' && !last.content?.length && !last.children.length) {
        editor.removeBlocks([last]);
      }
    }
  };

  const startDrag = (i: number) => (e: React.PointerEvent<HTMLDivElement>) => {
    if (!geo) return;
    e.stopPropagation();
    // Not preventDefault, for the reason given in CollectionView's startResize:
    // it cancels the mouse events behind the pointerdown, double-click included.
    // Text selection is held off with the same body class instead.
    const x0 = e.clientX;
    const from = widths;
    const track = geo.width - geo.gap * (count - 1);
    const at = (clientX: number) => moveDivider(from, i, ((clientX - x0) / track) * 100);
    document.body.classList.add('is-col-resizing');
    const move = (ev: PointerEvent) => setDrag(at(ev.clientX));
    const stop = () => {
      document.removeEventListener('pointermove', move);
      document.removeEventListener('pointerup', up);
      document.removeEventListener('pointercancel', stop);
      document.body.classList.remove('is-col-resizing');
      setDrag(null);
    };
    const up = (ev: PointerEvent) => {
      stop();
      const next = at(ev.clientX);
      if (!sameWidths(next, from)) commit(next);
    };
    document.addEventListener('pointermove', move);
    document.addEventListener('pointerup', up);
    // A touch the browser takes over for scrolling ends in pointercancel.
    document.addEventListener('pointercancel', stop);
  };

  // The same two keys that move a slider, 5% a press and 1% with Shift.
  const onKey = (i: number) => (e: React.KeyboardEvent<HTMLDivElement>) => {
    const dir = e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowRight' ? 1 : 0;
    if (!dir) return;
    e.preventDefault();
    e.stopPropagation();
    const next = moveDivider(widths, i, dir * (e.shiftKey ? 1 : 5));
    if (!sameWidths(next, widths)) commit(next);
  };

  // No marking the block element from here, however tempting: writing an
  // attribute onto DOM that lives INSIDE the editor makes ProseMirror
  // re-render the node, which re-runs this, which writes again. The first
  // version did exactly that and hung the page. The layout is reached from
  // CSS instead — see .bn-block[data-content-type] in styles.css — and the
  // widths by a <style> of our own, inside this block's non-editable strip,
  // aimed at the row by the block's id.
  const id = block.id.replace(/[^\w-]/g, '');
  const custom = drag ?? stored;
  // The id three times over: the stylesheet's own rules for two and three
  // columns reach through :has() and out-weigh a single attribute selector, and
  // repeating one is the plain way to count for more without !important.
  const row = `[data-id="${id}"]`.repeat(3);
  const tracks = geo && count > 1 ? geo : null;
  const editable = editor.isEditable;
  let x = 0;
  return (
    <div className="bn-columns" data-count={count} contentEditable={false} ref={strip}>
      {custom && (
        <style>{`.bn-block-outer${row} > .bn-block > .bn-block-group{grid-template-columns:${gridTemplate(custom)}}`}</style>
      )}
      {editable && (
        <div className="bn-columns-bar" role="group" aria-label={t('Column layout')}>
          {PRESETS.map((w) => {
            const on = w.length === count && sameWidths(w, stored ?? equalWidths(count));
            return (
              <button
                key={w.join('-')}
                type="button"
                className={'bn-columns-n' + (on ? ' is-on' : '')}
                aria-pressed={on}
                onClick={() => apply(w)}
                title={w.join(' / ')}
              >
                <span className="bn-columns-glyph" aria-hidden="true">
                  {w.map((n, i) => (
                    <i key={i} style={{ flex: n }} />
                  ))}
                </span>
                <span className="bn-columns-sr">{w.join(' / ')}</span>
              </button>
            );
          })}
        </div>
      )}
      {tracks &&
        widths.slice(0, -1).map((w, i) => {
          const room = tracks.width - tracks.gap * (count - 1);
          x += (drawn[i] / 100) * room;
          const centre = tracks.left + x + tracks.gap * i + tracks.gap / 2;
          // One element is both the divider and its handle, so the line can
          // never drift away from the thing that drags it. Where the page cannot
          // be edited it is the line alone: no role, no focus, no handlers.
          const place = { left: centre - 6, top: tracks.top, height: tracks.height };
          if (!editable) {
            return <div key={i} className="bn-columns-grip is-static" aria-hidden="true" style={place} />;
          }
          return (
            <div
              key={i}
              className="col-resize bn-columns-grip"
              role="separator"
              aria-orientation="vertical"
              aria-label={t('Column width')}
              aria-valuenow={w}
              aria-valuemin={MIN_PCT}
              aria-valuemax={100 - MIN_PCT}
              tabIndex={0}
              style={place}
              title={t('Drag to resize, double-click to reset')}
              // Not preventDefault on the POINTERdown (see startDrag), but on the
              // mousedown behind it: that is the one ProseMirror listens to, and
              // it would otherwise start a text or node selection in the row.
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
              }}
              onPointerDown={startDrag(i)}
              onKeyDown={onKey(i)}
              onDoubleClick={() => stored && commit(equalWidths(count))}
            />
          );
        })}
    </div>
  );
}

// The drawing as an image source. encodeURIComponent rather than base64: it
// keeps the markup readable in devtools and avoids a second copy in memory.
function svgDataURI(svg: string): string {
  return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
}

// An <img> holding an SVG with no width or height has no intrinsic size, so
// both come from the viewBox — the only place the drawing's real size is
// recorded.
//
// min(), and that is the whole rule: a diagram is shown at its own size and
// shrinks to fit a narrower column, but is NEVER blown up. Plain 100% made a
// tall narrow flowchart fill the column and run one and a half screens down the
// page, and gave a wide one no room at all. Both were the same mistake.
function diagramSize(svg: string): { width: string; aspectRatio?: string } {
  const m = /viewBox="0 0 ([\d.]+) ([\d.]+)"/.exec(svg);
  if (!m) return { width: '100%' };
  return { width: `min(100%, ${m[1]}px)`, aspectRatio: `${m[1]} / ${m[2]}` };
}

// ---- Diagram (Mermaid) ----
//
// A diagram written as text: "A --> B" rather than coordinates. That choice is
// the whole point — an agent can write one, and computing x/y for every box is
// exactly what an agent does badly.
//
// The block keeps two things: the SOURCE, which is the truth, and the rendered
// SVG, which is derived. The picture is stored because the print view is built
// by the server and has no way to draw one; without it every diagram would be
// missing from every PDF. Same lesson as the page icons.
export const mermaidSpec = createReactBlockSpec(
  {
    type: 'mermaid',
    propSchema: {
      code: { default: '' },
      svg: { default: '' },
      // Which renderer drew the picture. See MERMAID_REV.
      rev: { default: 0 },
    },
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { block, editor } = props;
      const code = String((block.props as { code: string }).code ?? '');
      const svg = String((block.props as { svg: string }).svg ?? '');
      const rev = Number((block.props as { rev: number }).rev ?? 0);
      const stale = !!svg && rev !== MERMAID_REV;
      const [editing, setEditing] = useState(!code);
      const [draft, setDraft] = useState(code);
      const [error, setError] = useState('');

      // Re-drawn whenever the source changes, and the result written back to the
      // block. Guarded so a diagram that has not changed does not rewrite the
      // page on every mount — an edit is a real change to the document.
      useEffect(() => {
        let alive = true;
        if (!code) return;
        void renderMermaid('mmd-' + block.id, code).then((r) => {
          if (!alive) return;
          setError(r.error);
          if (r.svg && (r.svg !== svg || rev !== MERMAID_REV)) {
            editor.updateBlock(block, { props: { svg: r.svg, rev: MERMAID_REV } } as never);
          }
        });
        return () => {
          alive = false;
        };
      }, [code, rev]);

      const save = () => {
        setEditing(false);
        if (draft !== code) editor.updateBlock(block, { props: { code: draft } } as never);
      };

      return (
        <div className="bn-mermaid" contentEditable={false}>
          {editing ? (
            <div className="bn-mermaid-edit">
              <textarea
                className="bn-mermaid-src"
                value={draft}
                autoFocus
                spellCheck={false}
                placeholder={"graph TD\n  A[Start] --> B[Done]"}
                onChange={(e) => setDraft(e.target.value)}
                onBlur={save}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    setDraft(code);
                    setEditing(false);
                  }
                  // Enter makes a new line; the diagram is saved on leaving the
                  // box, which is what every other multi-line field here does.
                  e.stopPropagation();
                }}
              />
              <p className="bn-mermaid-hint">{t('Leave the box to draw it. Escape discards.')}</p>
            </div>
          ) : svg && !error && !stale ? (
            // An <img>, not the SVG markup itself. This button lives inside
            // ProseMirror's DOM, and ProseMirror rebuilds a node view whenever
            // it likes — markup written into it (by React or by hand) was
            // cleared and never written back, so the picture was in the block
            // and the page showed an empty box. An image is ONE element with a
            // src attribute, which React owns and ProseMirror leaves alone.
            //
            // It is also safer: an SVG in an <img> cannot run anything.
            <button
              type="button"
              className="bn-mermaid-view"
              title={t('Click to edit the diagram')}
              onClick={() => {
                setDraft(code);
                setEditing(true);
              }}
            >
              <img src={svgDataURI(svg)} alt="" style={diagramSize(svg)} />
            </button>
          ) : (
            <button
              type="button"
              className="bn-mermaid-view"
              title={t('Click to edit the diagram')}
              onClick={() => {
                setDraft(code);
                setEditing(true);
              }}
            >
              <span className="bn-mermaid-empty">{error || t('Empty diagram')}</span>
            </button>
          )}
        </div>
      );
    },
  },
);
