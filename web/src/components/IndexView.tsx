import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import { toast } from '../toast';
import type { PageMeta, Workspace } from '../types';
import { PageIcon } from '../pageIcon';
import { compare, formatMoment } from '../format';
import { plural, t } from '../i18n';
import { tagColorClass } from '../tags';
import { Clock, FileText, Library, Lock, Share2, Star, Table2, Users, Workflow } from 'lucide-react';
import GraphView from './GraphView';

type SortKey = 'title' | 'in' | 'out' | 'updated';
type Mode = 'recent' | 'favorites' | 'shared' | 'private' | 'all' | 'tree' | 'graph';

// The library: every page of this instance (documents + databases, database rows
// excluded), the way a shelf is browsed rather than a list is read — by what was
// open lately, what is starred, what the workspace sees, what only you see.
//
// It used to be an "index" with two columns of link counts, which answered a
// question nobody asks first. The counts are still here (last two columns), but
// they no longer decide the shape.
//
// The agent view stays a tab of its own: the same indented tree the MCP
// list_pages returns, each node carrying a stable Markdown link. Read-only
// throughout; touches nothing in the block / collab store.

/** Tabs, built in a function — a module-level array would resolve t() once at
    import and keep that language for the session. */
function tabs(): { id: Mode; label: string; icon: React.ReactNode }[] {
  return [
    { id: 'recent', label: t('Recently used'), icon: <Clock size={14} /> },
    { id: 'favorites', label: t('Favorites'), icon: <Star size={14} /> },
    { id: 'shared', label: t('Shared'), icon: <Users size={14} /> },
    { id: 'private', label: t('Private'), icon: <Lock size={14} /> },
    { id: 'all', label: t('All pages'), icon: <Library size={14} /> },
    { id: 'graph', label: t('Graph'), icon: <Share2 size={14} /> },
    { id: 'tree', label: t('Tree · agent view'), icon: <Workflow size={14} /> },
  ];
}

/** The pages this browser opened last, newest first. localStorage, written by
    App's rememberRecent — per browser, so the phone and the laptop disagree.
    Moving it onto the account is its own small job (see W112 for why that
    matters); until then this is honest about what it knows. */
function recentIDs(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem('salt-recents') ?? '[]');
    return Array.isArray(v) ? (v as string[]) : [];
  } catch {
    return [];
  }
}

export default function IndexView({
  pages,
  favorites,
  workspaces,
  currentWs,
  onNavigate,
  onClose,
}: {
  pages: PageMeta[];
  favorites: string[];
  workspaces: Workspace[];
  currentWs: string;
  onNavigate: (id: string) => void;
  onClose: () => void;
}) {
  const [edges, setEdges] = useState<{ source: string; target: string }[]>([]);
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<SortKey>('title');
  const recents = useMemo(recentIDs, []);
  // "Recently used" is the right shelf to land on — except on a fresh browser,
  // where it is empty and the library would greet you with nothing at all.
  const [mode, setMode] = useState<Mode>(recents.length ? 'recent' : 'all');
  const [people, setPeople] = useState<Map<string, string>>(new Map());
  // Which workspace this shelf shows. It opens on the one you are working in,
  // because that is what "my pages" means to somebody who has seven of them —
  // and the sidebar has always been scoped that way, so a library showing
  // everything was the odd one out. Everything is one pick away.
  //
  // Deliberately not remembered: it is a view filter like the sort order beside
  // it, and a remembered filter is the thing people forget they set and then
  // report as "half my pages are gone".
  const [wsFilter, setWsFilter] = useState<string>(currentWs);
  const [spaces, setSpaces] = useState<Map<string, string>>(new Map());

  useEffect(() => {
    void api.graph().then((g) => setEdges(g.edges)).catch(() => {});
    // Who created a page is stored as an id; the roster turns it into a name.
    // The workspace names come from the same call — a top-level page has no
    // parent to show under "source", and its workspace is the honest answer.
    void api
      .listWorkspaces()
      .then((ws) => {
        setSpaces(new Map(ws.map((w) => [w.id, w.name])));
        return Promise.all(ws.map((w) => api.listMembers(w.id).catch(() => [])));
      })
      .then((lists) => {
        const m = new Map<string, string>();
        for (const p of lists.flat()) m.set(p.userId, p.name);
        setPeople(m);
      })
      .catch(() => {});
  }, []);

  const { outCount, inCount } = useMemo(() => {
    const out = new Map<string, number>();
    const inc = new Map<string, number>();
    for (const e of edges) {
      out.set(e.source, (out.get(e.source) ?? 0) + 1);
      inc.set(e.target, (inc.get(e.target) ?? 0) + 1);
    }
    return { outCount: out, inCount: inc };
  }, [edges]);

  // One list feeds the shelves, the tab counts AND the graph, so the filter is
  // applied once here rather than at each of the three — that is what keeps a
  // count from disagreeing with what is under it.
  const live = useMemo(
    () => pages.filter((p) => !p.trashed && (!wsFilter || p.workspaceId === wsFilter)),
    [pages, wsFilter],
  );

  const rows = useMemo(() => {
    const q = query.toLowerCase();
    const favSet = new Set(favorites);
    const shelf = live.filter((p) => {
      switch (mode) {
        case 'favorites':
          return favSet.has(p.id);
        case 'shared':
          return p.visibility !== 'private';
        case 'private':
          return p.visibility === 'private';
        case 'recent':
          return recents.includes(p.id);
        default:
          return true;
      }
    });
    const list = shelf
      .filter((p) => (p.title || 'Untitled').toLowerCase().includes(q))
      .map((p) => ({
        page: p,
        out: outCount.get(p.id) ?? 0,
        in: inCount.get(p.id) ?? 0,
      }));
    // "Recently used" carries its own order — the order they were opened in.
    // Sorting it by name would throw away the only thing that shelf knows.
    if (mode === 'recent' && sort === 'title') {
      list.sort((a, b) => recents.indexOf(a.page.id) - recents.indexOf(b.page.id));
      return list;
    }
    list.sort((a, b) => {
      switch (sort) {
        case 'in':
          return b.in - a.in || compare(a.page.title, b.page.title);
        case 'out':
          return b.out - a.out || compare(a.page.title, b.page.title);
        case 'updated': {
          // ISO timestamps sort correctly as plain strings. Locale collation
          // would be slower and, with numeric ordering on, actively wrong.
          const av = a.page.updatedAt || '';
          const bv = b.page.updatedAt || '';
          return bv < av ? -1 : bv > av ? 1 : 0;
        }
        default:
          return compare(a.page.title || 'Untitled', b.page.title || 'Untitled');
      }
    });
    return list;
  }, [live, query, sort, outCount, inCount, mode, favorites, recents]);

  const orphans = rows.filter((r) => r.in === 0 && r.out === 0).length;
  const titleOf = (id: string | null) =>
    id ? live.find((p) => p.id === id)?.title || t('Untitled') : '';

  // A count per shelf, so the tabs say how much is behind them before you
  // click — an empty shelf you can see is not a library that looks broken.
  const counts = useMemo(() => {
    const favSet = new Set(favorites);
    return {
      recent: live.filter((p) => recents.includes(p.id)).length,
      favorites: live.filter((p) => favSet.has(p.id)).length,
      shared: live.filter((p) => p.visibility !== 'private').length,
      private: live.filter((p) => p.visibility === 'private').length,
      all: live.length,
      tree: live.length,
    } as Record<Mode, number>;
  }, [live, favorites, recents]);

  // Parent → children map for the tree view (same hierarchy the sidebar shows).
  const childrenMap = useMemo(() => {
    const ids = new Set(live.map((p) => p.id));
    const m = new Map<string, PageMeta[]>();
    for (const p of live) {
      const key = p.parentId && ids.has(p.parentId) ? p.parentId : '';
      m.set(key, [...(m.get(key) ?? []), p]);
    }
    for (const list of m.values()) {
      list.sort((a, b) => (a.position ?? 0) - (b.position ?? 0) || compare(a.title || '', b.title || ''));
    }
    return m;
  }, [live]);

  const copyMd = (id: string) => {
    void navigator.clipboard?.writeText(window.location.origin + '/api/export/' + id);
    toast(t('Markdown link copied'));
  };

  const q = query.toLowerCase();
  const treeRows: React.ReactNode[] = [];
  // Depth-first walk that mirrors the sidebar hierarchy. A node stays visible
  // if it or any descendant matches the filter; returns whether a match was
  // found in its subtree so parents can keep themselves on screen.
  const renderSubtree = (key: string, depth: number): boolean => {
    let anyMatch = false;
    for (const p of childrenMap.get(key) ?? []) {
      const before = treeRows.length;
      const descendantMatch = renderSubtree(p.id, depth + 1);
      const matches = (p.title || 'Untitled').toLowerCase().includes(q);
      if (query && !matches && !descendantMatch) {
        // Drop any descendant rows we tentatively rendered for a non-matching branch.
        treeRows.length = before;
        continue;
      }
      anyMatch = anyMatch || matches || descendantMatch;
      const isDb = p.type === 'collection';
      // Insert this node BEFORE its already-rendered descendants.
      treeRows.splice(before, 0,
        <div className="idx-node" style={{ paddingLeft: 6 + depth * 18 }} key={p.id}>
          <button className="idx-node-main" onClick={() => onNavigate(p.id)} title={p.id}>
            <span className="idx-node-icon"><PageIcon icon={p.icon} size={14} fallback={isDb ? '🗄' : '📄'} /></span>
            <span className={'idx-node-title' + (query && matches ? ' idx-hit' : '')}>{p.title || 'Untitled'}</span>
            {isDb && <span className="index-badge" title={t('Collection')}><Table2 size={11} /></span>}
          </button>
          <code className="idx-node-id">{p.id.slice(0, 8)}</code>
          <button className="idx-md" title={t('Copy Markdown link')} onClick={() => copyMd(p.id)}>md</button>
        </div>,
      );
    }
    return anyMatch;
  };
  if (mode === 'tree') renderSubtree('', 0);

  return (
    <div className="index-view">
      <div className="index-head">
        <h1>
          <Library size={20} /> {t('Library')}
        </h1>
        <button className="btn-sm" onClick={onClose}>{t('Close')}</button>
      </div>
      <div className="index-controls">
        <div className="index-modes">
          {tabs().map((tab) => (
            <button
              key={tab.id}
              className={'index-mode' + (mode === tab.id ? ' active' : '')}
              onClick={() => setMode(tab.id)}
            >
              {tab.icon} {tab.label}
              {tab.id !== 'tree' && tab.id !== 'graph' && (
                <span className="index-mode-count">{counts[tab.id]}</span>
              )}
            </button>
          ))}
        </div>
        {/* Only when there is a choice to make. One workspace and this is a
            dropdown with a single entry, which is furniture. */}
        {workspaces.length > 1 && (
          <select
            className="prop-select index-ws"
            value={wsFilter}
            onChange={(e) => setWsFilter(e.target.value)}
            aria-label={t('Workspace')}
          >
            <option value="">{t('All workspaces')}</option>
            {workspaces.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        )}
        <input
          className="prop-input index-search"
          placeholder={t('Filter pages…')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label={t('Filter pages')}
        />
        {mode !== 'tree' && mode !== 'graph' && (
          <select className="prop-select" value={sort} onChange={(e) => setSort(e.target.value as SortKey)} aria-label={t('Sort')}>
            <option value="title">{mode === 'recent' ? t('Last opened') : t('Name (A–Z)')}</option>
            <option value="updated">{t('Recently changed')}</option>
            <option value="in">{t('Most backlinks')}</option>
            <option value="out">{t('Most outgoing links')}</option>
          </select>
        )}
        {/* plural(), not t(): the catalog holds "{n} pages" as plural FORMS, so
            t() would hand back the key and the count read "7 pages" in German. */}
        <span className="index-stat">
          {mode === 'graph' ? '' : plural(rows.length, '{n} page', '{n} pages')}
          {mode === 'all' && ` · ${t('{n} without links', { n: orphans })}`}
        </span>
      </div>

      {mode === 'graph' ? (
        <GraphView pages={live} edges={edges} onNavigate={onNavigate} />
      ) : mode !== 'tree' ? (
        <div className="table-wrap">
          <table className="db-table index-table">
            <thead>
              <tr>
                <th>{t('Page')}</th>
                <th>{t('Created by')}</th>
                <th>{t('Source')}</th>
                <th>{t('Changed')}</th>
                <th title={t('Outgoing @-links')}>{t('→ Links')}</th>
                <th title={t('Incoming links (backlinks)')}>{t('← Backlinks')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ page, out, in: inc }) => (
                <tr key={page.id}>
                  {/* One row carries what a shelf card carries: the icon, the
                      title, and the first line of what is inside. description
                      if the page has one, else the server-derived snippet —
                      both were already in the payload and shown nowhere. */}
                  <td>
                    <button className="idx-row" onClick={() => onNavigate(page.id)}>
                      <span className="idx-row-icon">
                        <PageIcon
                          icon={page.icon}
                          size={17}
                          fallback={page.type === 'collection' ? <Table2 size={16} /> : <FileText size={16} />}
                        />
                      </span>
                      <span className="idx-row-main">
                        <span className="idx-row-title">
                          {page.title || t('Untitled')}
                          {page.type === 'collection' && (
                            <span className="index-badge" title={t('Collection')}>
                              <Table2 size={11} />
                            </span>
                          )}
                        </span>
                        {(page.description || page.snippet) && (
                          <span className="idx-row-sub">{page.description || page.snippet}</span>
                        )}
                        {page.tags?.length > 0 && (
                          <span className="idx-row-tags">
                            {page.tags.slice(0, 4).map((tag) => (
                              <span key={tag} className={'tag-chip ' + tagColorClass(tag, {})}>
                                #{tag}
                              </span>
                            ))}
                          </span>
                        )}
                      </span>
                      {page.thumb && <img className="idx-row-thumb" src={page.thumb} alt="" />}
                    </button>
                  </td>
                  <td className="idx-dim">{people.get(page.ownerId) ?? ''}</td>
                  {/* Where it sits: the parent page, or the lock for a page only
                      its owner sees — the two answers to "where does this come
                      from" that the shelf tabs sort by. */}
                  <td className="idx-dim">
                    {page.parentId ? (
                      <button className="idx-src" onClick={() => onNavigate(page.parentId!)}>
                        {titleOf(page.parentId)}
                      </button>
                    ) : page.visibility === 'private' ? (
                      <span className="idx-src-priv">
                        <Lock size={11} /> {t('Private')}
                      </span>
                    ) : (
                      // No parent, not private: the workspace is where it comes
                      // from. Better than the empty cell that used to sit here.
                      spaces.get(page.workspaceId) ?? ''
                    )}
                  </td>
                  <td className="idx-dim">{page.updatedAt ? formatMoment(page.updatedAt) : ''}</td>
                  <td>{out || ''}</td>
                  <td>{inc || ''}</td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={6} className="db-empty">{t('No pages.')}</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="idx-tree-wrap">
          <p className="dialog-hint idx-tree-hint">
            {t('This is how an agent reads the structure of your workspace, the same way')}{' '}
            <code>list_pages</code> {t('does over MCP.')}{' '}
            {t('Every row carries a stable Markdown link — copy')} <code>md</code>{' '}
            {t('and it stays searchable Markdown, wherever it lives.')}
          </p>
          <div className="idx-tree">
            {treeRows.length ? treeRows : <div className="db-empty">{t('No pages.')}</div>}
          </div>
        </div>
      )}
    </div>
  );
}
