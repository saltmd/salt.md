import { useEffect, useMemo, useState } from 'react';
import { Bot, FileText, Search, Trash2, Undo2, X } from 'lucide-react';
import type { PageMeta } from '../types';
import { PageIcon } from '../pageIcon';
import { plural, t } from '../i18n';
import { formatMoment, formatRelative } from '../format';
import Portal from './Portal';

/** Case and accents do not count when searching the trash. */
function fold(s: string): string {
  return s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase();
}

interface Props {
  /** The entries: pages in the trash whose parent is not. */
  roots: PageMeta[];
  /** Everything the sidebar holds, so the pages inside an entry can be searched and counted. */
  pages: PageMeta[];
  onRestore: (id: string) => void;
  onDeleteForever: (id: string) => void;
}

/** The trash at the bottom of the sidebar.
 *
 *  Closed, it is the same single row it always was. Open, it answers the three
 *  questions the old list could not: what is this (a preview, without
 *  restoring it), who threw it away and when (from the activity log — an agent
 *  says so), and where is the thing I am looking for (a search across every
 *  entry and everything inside it). Newest first: what went a minute ago is the
 *  likeliest thing to want back. */
export default function TrashSection({ roots, pages, onRestore, onDeleteForever }: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [previewId, setPreviewId] = useState<string | null>(null);

  // What each entry holds: every trashed page below it, however deep.
  const inside = useMemo(() => {
    const kids = new Map<string, PageMeta[]>();
    for (const p of pages) {
      if (!p.trashed || !p.parentId) continue;
      const list = kids.get(p.parentId);
      if (list) list.push(p);
      else kids.set(p.parentId, [p]);
    }
    const out = new Map<string, PageMeta[]>();
    for (const root of roots) {
      const acc: PageMeta[] = [];
      const walk = (id: string) => {
        for (const c of kids.get(id) ?? []) {
          acc.push(c);
          walk(c.id);
        }
      };
      walk(root.id);
      out.set(root.id, acc);
    }
    return out;
  }, [roots, pages]);

  // ISO timestamps in one format order as strings do.
  const sorted = useMemo(
    () =>
      [...roots].sort((a, b) => {
        const ta = a.trashedAt ?? '';
        const tb = b.trashedAt ?? '';
        return ta === tb ? 0 : ta < tb ? 1 : -1;
      }),
    [roots],
  );

  const q = fold(query.trim());
  const shown = q
    ? sorted.filter((root) =>
        [root, ...(inside.get(root.id) ?? [])].some((p) => fold(p.title + ' ' + (p.snippet ?? '')).includes(q)),
      )
    : sorted;

  // Restored or deleted for good, from the preview or anywhere else: the entry
  // is gone from the list, and the preview goes with it.
  const previewPage = previewId ? (roots.find((r) => r.id === previewId) ?? null) : null;

  return (
    <div className={open ? 'trash-section open' : 'trash-section'}>
      <button className="trash-toggle" onClick={() => setOpen(!open)}>
        <span className="sidebar-item-label"><Trash2 size={15} /> {t('Trash')}</span> <span className="trash-count">{roots.length}</span>
      </button>
      {open && (
        <>
          <label className="trash-search">
            <Search size={13} />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('Search the trash')}
              aria-label={t('Search the trash')}
            />
          </label>
          {shown.map((p) => (
            <div key={p.id} className="trash-item">
              <button className="trash-peek" title={t('Preview')} onClick={() => setPreviewId(p.id)}>
                <span className="tree-icon"><PageIcon icon={p.icon} size={15} fallback={<FileText size={15} />} /></span>
                <span className="trash-text">
                  <span className="tree-title">{p.title || t('Untitled')}</span>
                  <TrashedWhen page={p} />
                </span>
              </button>
              <button title={t('Restore')} onClick={() => onRestore(p.id)}>
                <Undo2 size={14} />
              </button>
              <button title={t('Delete forever')} className="danger" onClick={() => onDeleteForever(p.id)}>
                <X size={14} />
              </button>
            </div>
          ))}
          {q && shown.length === 0 && <div className="trash-empty">{t('Nothing in the trash matches.')}</div>}
        </>
      )}
      {previewPage && (
        <TrashPreview
          page={previewPage}
          inside={inside.get(previewPage.id)?.length ?? 0}
          onClose={() => setPreviewId(null)}
          onRestore={onRestore}
          onDeleteForever={onDeleteForever}
        />
      )}
    </div>
  );
}

/** "Deleted by … · 2 hours ago" — the name only when the activity log could
 *  place the trashing, a robot when it was an agent, the exact time on hover. */
function TrashedWhen({ page }: { page: PageMeta }) {
  const when = formatRelative(page.trashedAt);
  if (!when) return null;
  return (
    <span className="trash-meta" title={formatMoment(page.trashedAt)}>
      {page.trashedByAgent && <Bot size={11} />}
      {page.trashedBy ? t('Deleted by {name} · {when}', { name: page.trashedBy, when }) : t('Deleted · {when}', { when })}
    </span>
  );
}

interface PreviewProps {
  page: PageMeta;
  inside: number;
  onClose: () => void;
  onRestore: (id: string) => void;
  onDeleteForever: (id: string) => void;
}

/** A look inside an entry without taking it out of the trash. The page arrives
 *  as a plain, script-free document from the server and sits in a frame that
 *  may not run anything either. */
function TrashPreview({ page, inside, onClose, onRestore, onDeleteForever }: PreviewProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const title = page.title || t('Untitled');
  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog wide trash-preview" role="dialog" aria-modal="true" aria-label={title}>
          <div className="trash-preview-head">
            <div>
              <div className="dialog-hint">{t('In the trash')}</div>
              <h2>{title}</h2>
              <div className="trash-preview-meta">
                <TrashedWhen page={page} />
                {inside > 0 && <span>{plural(inside, 'Contains {n} sub-page', 'Contains {n} sub-pages')}</span>}
              </div>
            </div>
            <button className="icon-btn" title={t('Close')} aria-label={t('Close')} onClick={onClose}>
              <X size={16} />
            </button>
          </div>
          <iframe
            className="trash-preview-frame"
            title={title}
            src={`/api/pages/${encodeURIComponent(page.id)}/preview`}
            sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
          />
          <div className="dialog-actions">
            <button className="btn danger" onClick={() => onDeleteForever(page.id)}>
              {t('Delete forever')}
            </button>
            <button className="btn primary" onClick={() => onRestore(page.id)}>
              {t('Restore')}
            </button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
