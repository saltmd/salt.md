import { useEffect, useState } from 'react';
import { createReactBlockSpec } from '@blocknote/react';
import { Table2 } from 'lucide-react';
import { useBlockCtx } from './blockContext';
import { MERMAID_REV, renderMermaid } from './mermaidLoader';
import { EXCALIDRAW_REV, renderExcalidraw, type DrawError } from './excalidrawLoader';
import { PageIcon } from './pageIcon';
import CollectionView from './components/CollectionView';
import { plural, t } from './i18n';

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

const CALLOUT_EMOJIS = ['💡', '⚠️', '❗', '✅', '📌', '🔥', 'ℹ️'];

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
        <div className="bn-callout">
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

export const databaseSpec = createReactBlockSpec(
  {
    type: 'database',
    propSchema: {
      collectionId: { default: '' },
    },
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { block, editor } = props;
      const collectionId = (block.props as { collectionId: string }).collectionId;
      const { pagesById, tagColors, onNavigate, onPagesChanged } = useBlockCtx();
      const [q, setQ] = useState('');

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
            onPagesChanged={onPagesChanged}
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
// column, and pulling column edges to resize. Both are worth having and neither
// is worth a licence that has to be renegotiated the day part of salt.md is
// closed.
export const columnsSpec = createReactBlockSpec(
  {
    type: 'columns',
    propSchema: {
      // 2 or 3. Stored so the layout survives a reload, and so the export knows
      // how to divide the page without measuring anything.
      count: { default: 2 },
    },
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { block, editor } = props;
      const count = Number((block.props as { count: number }).count) || 2;
      const set = (n: number) => editor.updateBlock(block, { props: { count: n } } as never);
      // No marking the block element from here, however tempting: writing an
      // attribute onto DOM that lives INSIDE the editor makes ProseMirror
      // re-render the node, which re-runs this, which writes again. The first
      // version did exactly that and hung the page. The layout is reached from
      // CSS instead — see .bn-block[data-content-type] in styles.css.
      return (
        <div className="bn-columns" data-count={count} contentEditable={false}>
          <div className="bn-columns-bar">
            {[2, 3].map((n) => (
              <button
                key={n}
                type="button"
                className={'bn-columns-n' + (n === count ? ' is-on' : '')}
                onClick={() => set(n)}
                title={plural(n, '{n} column', '{n} columns')}
              >
                {n}
              </button>
            ))}
          </div>
        </div>
      );
    },
  },
);

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

// ---- Excalidraw ----
//
// The elaborate case: a drawing somebody made by hand, or that an agent
// produced as a .excalidraw file. salt.md does not edit it — this is a reader.
//
// Built exactly like the diagram block, for the same reason: the FILE is the
// truth, the picture is derived and stored, because the print view runs on the
// server and cannot draw. Without the stored picture every drawing would be
// missing from every PDF.
//
// The library costs about 1.4 MB over the wire, so it is fetched only when a
// page actually holds a drawing — and only until the picture exists. Everybody
// after that gets the picture and never loads it at all.
export const excalidrawSpec = createReactBlockSpec(
  {
    type: 'excalidraw',
    propSchema: {
      url: { default: '' },
      name: { default: '' },
      svg: { default: '' },
      rev: { default: 0 },
    },
    content: 'none',
  } as const,
  {
    render: (props) => {
      const { block, editor } = props;
      const p = block.props as { url: string; name: string; svg: string; rev: number };
      const url = String(p.url ?? '');
      const name = String(p.name ?? '');
      const svg = String(p.svg ?? '');
      const rev = Number(p.rev ?? 0);
      const stale = !!svg && rev !== EXCALIDRAW_REV;
      const [error, setError] = useState<DrawError>('');

      useEffect(() => {
        let alive = true;
        if (!url || (svg && !stale)) return;
        void renderExcalidraw(url).then((r) => {
          if (!alive) return;
          setError(r.error);
          if (r.svg) {
            editor.updateBlock(block, { props: { svg: r.svg, rev: EXCALIDRAW_REV } } as never);
          }
        });
        return () => {
          alive = false;
        };
      }, [url, rev]);

      if (svg && !stale && !error) {
        return (
          <div className="bn-excalidraw" contentEditable={false}>
            <img src={svgDataURI(svg)} alt={name} style={diagramSize(svg)} />
            <a className="bn-excalidraw-file" href={url} download={name || 'drawing.excalidraw'}>
              {name || t('Drawing')}
            </a>
          </div>
        );
      }
      // Said in the reader's language, and saying what to DO where there is
      // something to do. A file with no bytes in it is the commonest of these
      // and has nothing to do with drawing at all.
      const said: Record<Exclude<DrawError, ''>, string> = {
        unreadable: t('This file could not be read. Is it really an Excalidraw drawing?'),
        empty: t('This drawing is empty.'),
        notADrawing: t('This file holds no drawing.'),
        noLibrary: t('The drawing could not be loaded. Reload the page.'),
        failed: t('This drawing could not be drawn.'),
      };
      return (
        <div className="bn-excalidraw" contentEditable={false}>
          <span className="bn-mermaid-empty">{error ? said[error] : t('Drawing the file…')}</span>
        </div>
      );
    },
  },
);
