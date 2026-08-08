import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import type { Backlink, PageMeta, SaltFile } from '../types';
import { PageIcon } from '../pageIcon';
import { FilePreview, isPreviewable } from './FilePreview';
import { formatBytes } from '../format';
import { t } from '../i18n';
import { CornerDownRight, FileText, Link2, PanelRightClose, Table2 } from 'lucide-react';

// What a document carries but never showed: the pages below it, the files
// hanging off it, and who points at it. All three existed — parent_id has
// always built the tree, the file index (W125) has always known which page an
// upload belongs to, and backlinks were already collected for the strip under
// the body — but a reader had to hunt through the sidebar or scroll to the end
// to find any of it. The panel is a view onto data that was already there.

const PANEL_KEY = 'salt-structure-open';

export function structurePanelOpen(): boolean {
  return localStorage.getItem(PANEL_KEY) === '1';
}

export function setStructurePanelOpen(open: boolean): void {
  if (open) localStorage.setItem(PANEL_KEY, '1');
  else localStorage.removeItem(PANEL_KEY);
}

type TreeItem = { page: PageMeta; depth: number };

// The chain from the top down to (but not including) this page. The panel used
// to look only downwards, so standing on a sub-page you could see everything
// below you and nothing about where you were — the breadcrumb in the topbar
// knows, but that is above the reading line and easy to miss. Nearest parent
// last, so the list reads top-down like the tree does.
function ancestors(pageId: string, pagesById: Map<string, PageMeta>): PageMeta[] {
	const out: PageMeta[] = [];
	const seen = new Set<string>([pageId]);
	let cur = pagesById.get(pageId)?.parentId ?? null;
	while (cur && !seen.has(cur)) {
		seen.add(cur); // a cycle cannot happen, but a bad import must not hang the panel
		const p = pagesById.get(cur);
		if (!p) break; // a database row's parent is not in the tree map — stop there
		out.unshift(p);
		cur = p.parentId;
	}
	return out;
}

// Children of `rootId`, depth-first, so the panel shows the shape of the
// subtree and not just its first level. Database rows are absent by design:
// the page tree endpoint leaves them out (they belong in the collection view,
// and there can be tens of thousands), so a collection simply shows no
// sub-pages here.
function subtree(rootId: string, pagesById: Map<string, PageMeta>): TreeItem[] {
  const byParent = new Map<string, PageMeta[]>();
  for (const p of pagesById.values()) {
    if (p.trashed || !p.parentId) continue;
    const list = byParent.get(p.parentId);
    if (list) list.push(p);
    else byParent.set(p.parentId, [p]);
  }
  const out: TreeItem[] = [];
  const walk = (id: string, depth: number) => {
    const kids = byParent.get(id);
    if (!kids) return;
    for (const p of [...kids].sort((a, b) => a.position - b.position)) {
      out.push({ page: p, depth });
      walk(p.id, depth + 1); // cycles are impossible: moves reject them server-side
    }
  };
  walk(rootId, 0);
  return out;
}

// A file's kind carries colour because the list is scanned, not read: the eye
// finds "the spreadsheet" faster than it reads three names. Kinds outside this
// table stay neutral rather than getting an arbitrary hue.
const EXT_CLASS: Record<string, string> = {
  pdf: 'ext-pdf',
  xls: 'ext-sheet',
  xlsx: 'ext-sheet',
  csv: 'ext-sheet',
  doc: 'ext-doc',
  docx: 'ext-doc',
  ppt: 'ext-slide',
  pptx: 'ext-slide',
};

function Section({ label, count, children }: { label: string; count: number; children: React.ReactNode }) {
  return (
    <div className="structure-section">
      <div className="structure-label">
        {label} <span className="structure-count">· {count}</span>
      </div>
      {children}
    </div>
  );
}

export default function StructurePanel({
  pageId,
  isCollection,
  pagesById,
  onNavigate,
  onClose,
}: {
  pageId: string;
  isCollection: boolean;
  pagesById: Map<string, PageMeta>;
  onNavigate: (id: string | null) => void;
  onClose: () => void;
}) {
  const [files, setFiles] = useState<SaltFile[]>([]);
  const [links, setLinks] = useState<Backlink[]>([]);
  const [preview, setPreview] = useState<{ name: string; url: string } | null>(null);

  const pages = useMemo(() => subtree(pageId, pagesById), [pageId, pagesById]);
  const above = useMemo(() => ancestors(pageId, pagesById), [pageId, pagesById]);

  useEffect(() => {
    let alive = true;
    api
      .listFiles({ under: pageId })
      .then((f) => alive && setFiles(f))
      .catch(() => alive && setFiles([]));
    api
      .backlinks(pageId)
      .then((l) => alive && setLinks(l))
      .catch(() => alive && setLinks([]));
    return () => {
      alive = false;
    };
  }, [pageId, pagesById]);

  const openFile = (f: SaltFile) => {
    const url = '/files/' + f.name;
    if (isPreviewable(url)) setPreview({ name: f.displayName || f.name, url });
    else window.open(url, '_blank', 'noopener');
  };

  return (
    <aside className="structure-panel" aria-label={t('Structure')}>
      <div className="structure-head">
        <span>{t('Structure')}</span>
        <button className="icon-btn" title={t('Hide structure')} onClick={onClose}>
          <PanelRightClose size={17} />
        </button>
      </div>

      {/* The head holds still and only this scrolls — the same shape as the
          comments panel, which is the other thing that can stand here. */}
      <div className="structure-scroll">

        {/* Where this page sits, before what sits under it. Shown only when
            there is somewhere to go: on a top-level page the section would be an
            empty box saying nothing. */}
        {above.length > 0 && (
          <div className="structure-section structure-above">
            {above.map((p, i) => (
              <button
                key={p.id}
                className="structure-item"
                style={{ paddingLeft: 8 + i * 10 }}
                onClick={() => onNavigate(p.id)}
                title={p.title || t('Untitled')}
              >
                <span className="structure-icon">
                  <PageIcon
                    icon={p.icon}
                    size={14}
                    fallback={p.type === 'collection' ? <Table2 size={14} /> : <FileText size={14} />}
                  />
                </span>
                <span className="structure-text">{p.title || t('Untitled')}</span>
              </button>
            ))}
            {/* The current page closes the chain, so the panel shows a position
                and not just a list of strangers. Not a button: you are here. */}
            <div className="structure-item structure-here" style={{ paddingLeft: 8 + above.length * 10 }}>
              <span className="structure-icon">
                <CornerDownRight size={13} />
              </span>
              <span className="structure-text">{t('This page')}</span>
            </div>
          </div>
        )}

        {/* A database's rows ARE its sub-pages, but they live in the table and
            are deliberately kept out of the page tree. Showing an empty
            "0 sub-pages" next to a table full of rows would read as a bug, so
            the section stays out entirely — the files below still count. */}
        {!isCollection && (
          <Section label={t('Sub-pages')} count={pages.length}>
            {pages.length === 0 && <div className="structure-empty">{t('No sub-pages')}</div>}
            {pages.map(({ page, depth }) => (
              <button
                key={page.id}
                className="structure-item"
                style={{ paddingLeft: 8 + Math.min(depth, 4) * 14 }}
                onClick={() => onNavigate(page.id)}
                title={page.title || t('Untitled')}
              >
                {/* Depth reads as a turn, not as blank space: indentation alone
                    is ambiguous once a title wraps or a list gets long. */}
                <span className="structure-icon">
                  {depth > 0 ? (
                    <CornerDownRight size={13} />
                  ) : (
                    <PageIcon
                      icon={page.icon}
                      size={14}
                      fallback={page.type === 'collection' ? <Table2 size={14} /> : <FileText size={14} />}
                    />
                  )}
                </span>
                <span className="structure-text">{page.title || t('Untitled')}</span>
              </button>
            ))}
          </Section>
        )}

        <Section label={t('Files')} count={files.length}>
          {files.length === 0 && <div className="structure-empty">{t('No files')}</div>}
          {files.map((f) => {
            const ext = f.ext.replace('.', '').toLowerCase();
            return (
              <button
                key={f.name}
                className="structure-item structure-file"
                onClick={() => openFile(f)}
                title={f.pageId === pageId ? f.displayName : `${f.displayName} — ${f.pageTitle}`}
              >
                <span className={'structure-icon structure-ext ' + (EXT_CLASS[ext] ?? '')}>
                  {ext.slice(0, 4) || '?'}
                </span>
                <span className="structure-text">
                  {f.displayName || f.name}
                  {/* Two files can carry the same name and differ only in which
                      sub-page they hang off — "real.pdf" twice over tells the
                      reader nothing. The source page is named whenever it is not
                      the document already on screen. */}
                  {f.pageId !== pageId && f.pageTitle && (
                    <span className="structure-from">{f.pageTitle}</span>
                  )}
                </span>
                <span className="structure-size">{formatBytes(f.size)}</span>
              </button>
            );
          })}
        </Section>

        <Section label={t('Linked from')} count={links.length}>
          {links.length === 0 && <div className="structure-empty">{t('Nothing links here')}</div>}
          {links.map((l) => (
            <button
              key={l.id}
              className="structure-item"
              onClick={() => onNavigate(l.id)}
              title={l.title || t('Untitled')}
            >
              <span className="structure-icon">
                <Link2 size={14} />
              </span>
              <span className="structure-text">{l.title || t('Untitled')}</span>
            </button>
          ))}
        </Section>

      </div>

      {preview && (
        <FilePreview name={preview.name} url={preview.url} onClose={() => setPreview(null)} />
      )}
    </aside>
  );
}
