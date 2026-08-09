import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { FontPref } from '../App';
import type { PageMeta, User, Workspace } from '../types';
import UserMenu from './UserMenu';
import WorkspaceMembers from './WorkspaceMembers';
import WorkspaceRules from './WorkspaceRules';
import FileList from './FileList';
import { promptText } from '../dialog';
import { toast } from '../toast';
import Portal from './Portal';
import IconPicker from './IconPicker';
import { PageIcon } from '../pageIcon';
import { compare } from '../format';
import { plural, t } from '../i18n';
import AgentConnectModal from './AgentConnect';
import BreakGlassLog from './BreakGlassLog';
import TemplateGallery from './TemplateGallery';
import BlueprintLibrary from './BlueprintLibrary';
import WorkspaceSettings from './WorkspaceSettings';
import StrandedWorkspaces from './StrandedWorkspaces';
import { useExclusiveModal, useMenuDismiss } from '../modal';
import { Sun, Moon, Search, Library, Plus, Table2, FileText, Trash2, LayoutTemplate, Tag, ChevronRight, ChevronDown, Users, Check, Download, Upload, Image, PanelLeftClose, PanelLeftOpen, Pencil, Star, ShieldAlert, ScrollText, Paperclip, SquareArrowOutUpRight, Copy, CornerUpRight, CornerLeftUp, Undo2, X, MoreHorizontal, Settings2 } from 'lucide-react';
import { AgentDot } from './AgentBadge';
import { tagColorClass } from '../tags';
import { childrenForSection, topLevelForDocs } from '../treeMode';
import ThemeSwitch, { type ThemePref } from '../ThemeSwitch';

interface Props {
  pages: PageMeta[];
  favorites: string[];
  workspaces: Workspace[];
  currentWs: string;
  tagColors: Record<string, string>;
  onSwitchWorkspace: (id: string) => void;
  onWorkspacesChanged: () => void;
  user: User;
  onUserChanged?: (u: User) => void;
  // Does the instance let non-admins create workspaces? (W97)
  canCreateWorkspace?: boolean;
  currentId: string | null;
  open: boolean;
  onCollapse: () => void;
  onNavigate: (id: string) => void;
  onOpenInNewTab: (id: string) => void;
  onCreate: (parentId: string | null, type?: 'doc' | 'collection') => void;
  onTrash: (id: string) => void;
  onDuplicate: (id: string) => void;
  onRestore: (id: string) => void;
  onDeleteForever: (id: string) => void;
  onMove: (id: string, parentId: string | null, position: number) => void;
  onToggleFavorite: (id: string) => void;
  onOpenSearch: () => void;
  onOpenIndex: () => void;
  theme: 'light' | 'dark';
  themePref: ThemePref;
  onSetTheme: (v: ThemePref) => void;
  onLogout: () => void;
  // Bear-style notes mode: the notes list (middle column) is the only place
  // documents appear, so the sidebar drops its document tree and its tag chips
  // filter THAT list instead of the tree.
  notesMode?: boolean;
  activeTag?: string | null;
  onSelectTag?: (tag: string | null) => void;
  // Raw user setting (independent of viewport) + toggle, shown in the UserMenu.
  notesModeSetting?: boolean;
  onToggleNotesMode?: () => void;
  // Choice of typeface: it belongs to the person, not to the instance.
  fontPref?: FontPref;
  onSetFont?: (f: FontPref) => void;
  // Desktop collapse state: when collapsed, the sidebar appears as a hover
  // overlay and the collapse button flips into a "pin open" button — so users
  // can re-pin right from the overlay instead of hunting the hamburger behind it.
  collapsed?: boolean;
  onExpand?: () => void;
}

interface DropTarget {
  id: string;
  zone: 'before' | 'after' | 'inside';
}

// Everything TreeItem needs from Sidebar. TreeItem lives at module level so
// its component identity is stable — defining it inside Sidebar would remount
// the whole tree (and kill drag state) on every render.
interface TreeCtx {
  childrenMap: Map<string, PageMeta[]>;
  // One tree, or Documents and Collections apart. Reaches TreeItem because the
  // rule about which children to show depends on it.
  mixed: boolean;
  expanded: Set<string>;
  currentId: string | null;
  favorites: Set<string>;
  dropTarget: DropTarget | null;
  menuFor: string | null;
  onNavigate: (id: string) => void;
  onOpenInNewTab: (id: string) => void;
  toggleExpand: (id: string) => void;
  onCreateChild: (id: string) => void;
  // Two things a person could not do at all until now, while an agent could do
  // both over MCP: put a database INSIDE a page, and get one back out again.
  // The capabilities were always there (onCreate takes a type, onMove takes a
  // null parent) — nothing offered them.
  onCreateChildDb: (id: string) => void;
  // Which item's ＋ menu is open. Its own state rather than a flag on menuFor:
  // both can be anchored to the same row and only one may ever be open.
  addFor: string | null;
  setAddFor: (id: string | null) => void;
  onMoveToTop: (id: string) => void;
  onShowFiles: (id: string, title: string) => void;
  setMenuFor: (id: string | null) => void;
  onTrash: (id: string) => void;
  onDuplicate: (id: string) => void;
  onToggleFavorite: (id: string) => void;
  workspaces: Workspace[];
  onMoveToWorkspace: (pageId: string, wsId: string, wsName: string) => void;
  dragStart: (id: string, e: React.DragEvent) => void;
  dragOver: (p: PageMeta, e: React.DragEvent) => void;
  dragLeave: (id: string) => void;
  drop: (p: PageMeta, e: React.DragEvent) => void;
  dragEnd: () => void;
}

// DbRows lazily loads and lists a database's rows when it is expanded in the
// tree — Notion-style "open a database to see its entries". A row that carries
// sub-pages (those rows travel in /api/pages since W124) gets a chevron and
// unfolds its subtree as regular tree items: DB → row → dossier pages, four
// levels deep if need be, instead of the sub-pages floating flat under
// Documents with no hint of their parent.
function DbRows({
  collectionId,
  workspaceId,
  depth,
  ctx,
}: {
  collectionId: string;
  // The rows live in the same workspace as their database — without it the
  // "move to workspace" list offers the one they are already in.
  workspaceId: string;
  depth: number;
  ctx: TreeCtx;
}) {
  const [rows, setRows] = useState<{ id: string; title: string; icon: string }[] | null>(null);
  useEffect(() => {
    let alive = true;
    const load = () =>
      void api
        .collectionRows(collectionId, { limit: 50 })
        .then((res) => alive && setRows(res.rows.map((r) => ({ id: r.id, title: r.title, icon: r.icon }))))
        .catch(() => alive && setRows([]));
    load();
    // Fetched ONCE when unfolded and never again, which meant a row added or
    // thrown away anywhere else simply did not appear or disappear here — the
    // event arrived, and nothing was listening. Same signal the collection view
    // uses, and narrowed the same way: only when it names THIS database, or a
    // list with fifty thousand rows would re-crawl itself on every rename
    // anywhere.
    const onRows = (e: Event) => {
      if ((e as CustomEvent<string>).detail === collectionId) load();
    };
    window.addEventListener('salt:rows', onRows);
    return () => {
      alive = false;
      window.removeEventListener('salt:rows', onRows);
    };
  }, [collectionId]);
  const pad = { paddingLeft: 6 + depth * 14 };
  if (rows === null) return <div className="tree-db-empty" style={pad}>{t('Loading…')}</div>;
  // Rows with sub-pages exist in the tree data regardless of the lazy window
  // of 50 — append any that the window missed, so a subtree is never
  // unreachable just because its row sorts late.
  const inTree = ctx.childrenMap.get(collectionId) ?? [];
  // A database nested inside another one is NOT one of its rows — it is a page
  // that happens to live there. Drawn as a row it got the row's markup, which
  // has a ＋ and no ⋯ menu: so once a database had been dragged into another
  // one, the interface offered no way whatsoever to get it back out. It renders
  // as a proper tree item now, with the full menu.
  const nestedDbs = inTree.filter((p) => p.type === 'collection');
  const isNestedDb = (id: string) => nestedDbs.some((d) => d.id === id);
  const missing = inTree.filter((w) => !isNestedDb(w.id) && !rows.some((r) => r.id === w.id));
  const all = [
    ...rows.filter((r) => !isNestedDb(r.id)),
    ...missing.map((m) => ({ id: m.id, title: m.title, icon: m.icon })),
  ];
  if (all.length === 0 && nestedDbs.length === 0)
    return <div className="tree-db-empty" style={pad}>{t('No entries')}</div>;
  return (
    <div className="tree-db-rows">
      {nestedDbs.map((d) => (
        <TreeItem key={d.id} p={d} depth={depth} ctx={ctx} section="dbs" />
      ))}
      {all.map((r) => {
        const kids = ctx.childrenMap.get(r.id) ?? [];
        const isOpen = ctx.expanded.has(r.id);
        return (
          <div key={r.id}>
            <div
              className="tree-db-row"
              style={pad}
              onClick={() => ctx.onNavigate(r.id)}
              onContextMenu={(e) => {
                e.preventDefault();
                ctx.setAddFor(null);
                ctx.setMenuFor(r.id);
              }}
            >
              {kids.length > 0 ? (
                <button
                  className="chevron"
                  onClick={(e) => {
                    e.stopPropagation();
                    ctx.toggleExpand(r.id);
                  }}
                >
                  {isOpen ? '▾' : '▸'}
                </button>
              ) : (
                <span className="chevron spacer" />
              )}
              <span className="tree-icon"><PageIcon icon={r.icon} size={14} fallback={<FileText size={14} />} /></span>
              <span className="tree-title">{r.title || 'Untitled'}</span>
              <AgentDot pageId={r.id} />
              {/* Until now a row could only get sub-pages through MCP — the
                  interface offered no way at all, so the dossier under a deal
                  was something an agent could build and a person could not. */}
              {/* The same ＋ and the same ⋯ as a tree item. A row had only the
                  ＋ until now, so a page filed under a database could not be
                  duplicated, exported or thrown away from here at all — you had
                  to open it first and hope its own menu had what you wanted. */}
              <RowActions id={r.id} title={r.title} parentId={collectionId} workspaceId={workspaceId} ctx={ctx} />
            </div>
            {isOpen && kids.map((k) => <TreeItem key={k.id} p={k} depth={depth + 1} ctx={ctx} section="dbs" />)}
          </div>
        );
      })}
    </div>
  );
}

// A collapsible sidebar section with a count and its own "new" action.
// Sections keep the sidebar navigable once a workspace grows: collapse what you
// don't need instead of scrolling one endless list. The open/closed state is
// remembered per section.
function SidebarSection({
  id,
  label,
  icon,
  count,
  createTitle,
  onCreate,
  defaultOpen = true,
  openOnGrowth = false,
  children,
}: {
  id: string;
  label: string;
  icon: React.ReactNode;
  count: number;
  createTitle?: string;
  onCreate?: () => void;
  defaultOpen?: boolean;
  /** Open this section when its count grows.
   *
   *  Saving a template was invisible: the section exists only once there is a
   *  template AND starts collapsed, so the one action whose entire result lives
   *  in there looked like nothing had happened at all. Watching the count is
   *  local to this component — the button that saves lives in a different one
   *  and has no way to reach in here. */
  openOnGrowth?: boolean;
  children: React.ReactNode;
}) {
  const key = 'salt-sec-' + id;
  const [open, setOpen] = useState(() => {
    const v = localStorage.getItem(key);
    return v === null ? defaultOpen : v === '1';
  });
  const prevCount = useRef(count);
  useEffect(() => {
    if (openOnGrowth && count > prevCount.current) {
      setOpen(true);
      try {
        localStorage.setItem(key, '1');
      } catch {
        /* private mode */
      }
    }
    prevCount.current = count;
  }, [count, openOnGrowth, key]);
  const toggle = () =>
    setOpen((o) => {
      try {
        localStorage.setItem(key, o ? '0' : '1');
      } catch {
        /* private mode */
      }
      return !o;
    });

  return (
    <div className={'sb-section' + (open ? ' open' : '')}>
      <div className="sb-section-head">
        {/* A topic looks like a row, only bolder: icon on the left, title,
            count, chevron on the right. No more micro-heading — one single row
            language for the whole sidebar. */}
        <button className="sb-section-toggle" onClick={toggle} aria-expanded={open}>
          <span className="sb-section-icon">{icon}</span>
          <span className="sb-section-label">{label}</span>
          <span className="sb-section-count">{count}</span>
          <span className="sb-section-caret">
            {open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
          </span>
        </button>
        {onCreate && (
          <button
            className="sb-section-add"
            title={createTitle}
            aria-label={createTitle}
            onClick={(e) => {
              e.stopPropagation();
              if (!open) toggle();
              onCreate();
            }}
          >
            <Plus size={14} />
          </button>
        )}
      </div>
      {open && <div className="sb-section-body">{children}</div>}
    </div>
  );
}

// One flat row — no chevron, no nesting. Used by the favourites and template
// lists, where the tree hierarchy would be noise.
function FlatRow({
  p,
  active,
  parentLabel,
  onNavigate,
  action,
}: {
  p: PageMeta;
  active: boolean;
  parentLabel?: string;
  onNavigate: (id: string) => void;
  action?: React.ReactNode;
}) {
  return (
    <div className={'tree-item sb-flat' + (active ? ' active' : '')} onClick={() => onNavigate(p.id)}>
      <span className="chevron spacer" />
      <span className="tree-icon">
        <PageIcon
          icon={p.icon}
          size={15}
          fallback={p.type === 'collection' ? <Table2 size={15} /> : <FileText size={15} />}
        />
      </span>
      <span className="sb-flat-text">
        <span className="tree-title">{p.title || 'Untitled'}</span>
        {parentLabel && <span className="sb-flat-parent">{parentLabel}</span>}
      </span>
      {action && (
        <span className="tree-actions" onClick={(e) => e.stopPropagation()}>
          {action}
        </span>
      )}
    </div>
  );
}

// The row menu, as its own component because a database ROW needs exactly the
// same one. Two copies would be two answers to "what can you do with a page",
// and the row's copy would be the one that quietly falls behind.
function RowActions({
  id,
  title,
  parentId,
  workspaceId,
  ctx,
}: {
  id: string;
  title: string;
  parentId: string;
  workspaceId?: string;
  ctx: TreeCtx;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  useMenuDismiss(ctx.menuFor === id, ref, () => ctx.setMenuFor(null));
  useMenuDismiss(ctx.addFor === id, ref, () => ctx.setAddFor(null));
  return (
    <span className="tree-actions" onClick={(e) => e.stopPropagation()} ref={ref}>
      <button
        title={t('Add inside')}
        onClick={() => {
          ctx.setMenuFor(null);
          ctx.setAddFor(ctx.addFor === id ? null : id);
        }}
      >
        +
      </button>
      {ctx.addFor === id && <AddMenu id={id} ctx={ctx} />}
      <button
        title={t('More')}
        onClick={() => {
          ctx.setAddFor(null);
          ctx.setMenuFor(ctx.menuFor === id ? null : id);
        }}
      >
        <MoreHorizontal size={13} />
      </button>
      {ctx.menuFor === id && (
        <PageMenu id={id} title={title} parentId={parentId} workspaceId={workspaceId} ctx={ctx} />
      )}
    </span>
  );
}

// What the ＋ on a tree item or a database row offers.
//
// It used to create a document, silently, and the choice existed only in the ⋯
// beside it — so "how do I put a database under this?" had an answer nobody
// found. Now the ＋ asks, with the same two options the menu has.
//
// The section headers keep their direct ＋: under "Documents" and "Collections"
// the section IS the answer, and asking there would be a question with one
// sensible reply. Only where the type is genuinely open does it get asked.
function AddMenu({ id, ctx }: { id: string; ctx: TreeCtx }) {
  return (
    <div className="menu add-menu">
      <button
        onClick={() => {
          ctx.setAddFor(null);
          ctx.onCreateChild(id);
        }}
      >
        <FileText size={16} /> {t('Page')}
      </button>
      <button
        onClick={() => {
          ctx.setAddFor(null);
          ctx.onCreateChildDb(id);
        }}
      >
        <Table2 size={16} /> {t('Collection')}
      </button>
    </div>
  );
}

function PageMenu({
  id,
  title,
  parentId,
  workspaceId,
  ctx,
}: {
  id: string;
  title: string;
  parentId?: string | null;
  workspaceId?: string;
  ctx: TreeCtx;
}) {
  return (
    <div className="menu">
              {/* The visible way to get a second tab — ⌘-click and middle-click
                  exist too, but nothing in the interface said so. */}
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  ctx.onOpenInNewTab(id);
                }}
              >
                <SquareArrowOutUpRight size={16} /> {t('Open in new tab')}
              </button>
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  ctx.onToggleFavorite(id);
                }}
              >
                <Star size={16} fill={ctx.favorites.has(id) ? 'currentColor' : 'none'} />{' '}
                {ctx.favorites.has(id) ? t('Remove from favorites') : t('Add to favorites')}
              </button>
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  ctx.onDuplicate(id);
                }}
              >
                <Copy size={16} /> {t('Duplicate')}
              </button>
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  ctx.onCreateChildDb(id);
                }}
              >
                <Table2 size={16} /> {t('New collection inside')}
              </button>
              {/* Only when there is a parent to leave. A database dragged into
                  another one had no way back at all: it lives in that one's
                  subtree, and nothing in the interface moved it out. */}
              {parentId && (
                <button
                  onClick={() => {
                    ctx.setMenuFor(null);
                    ctx.onMoveToTop(id);
                  }}
                >
                  <CornerLeftUp size={16} /> {t('Move to top level')}
                </button>
              )}
              {ctx.workspaces.length > 1 && (
                <div className="menu-sub">
                  <div className="menu-label">{t('Move to workspace')}</div>
                  {ctx.workspaces
                    .filter((w) => w.id !== workspaceId)
                    .map((w) => (
                      <button
                        key={w.id}
                        onClick={() => {
                          ctx.setMenuFor(null);
                          ctx.onMoveToWorkspace(id, w.id, w.name);
                        }}
                      >
                        <CornerUpRight size={16} /> {w.name}
                      </button>
                    ))}
                </div>
              )}
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  ctx.onShowFiles(id, title || t('Untitled'));
                }}
              >
                <Paperclip size={16} /> {t('Files in this subtree')}
              </button>
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  // A template is a SNAPSHOT: the copy becomes the template and
                  // keeps this moment's state; the page here stays a normal page.
                  // (Flagging the page itself kept template and original one
                  // object — editing either changed "the template".)
                  void api
                    .duplicatePage(id, false, true)
                    .then(() => toast(t('Saved as template')))
                    .catch(() => toast(t('Could not save as template')));
                }}
              >
                <LayoutTemplate size={16} /> {t('Save as template')}
              </button>
              <button
                onClick={() => {
                  ctx.setMenuFor(null);
                  api.download(`/api/export/${id}`);
                }}
              >
                <Download size={16} /> {t('Export Markdown')}
              </button>
              <button
                className="danger"
                onClick={() => {
                  ctx.setMenuFor(null);
                  ctx.onTrash(id);
                }}
              >
                <Trash2 size={16} /> {t('Move to trash')}
              </button>
    </div>
  );
}

function TreeItem({
  p,
  depth,
  ctx,
  section = 'docs',
}: {
  p: PageMeta;
  depth: number;
  ctx: TreeCtx;
  section?: 'docs' | 'dbs';
}) {
  // A database filed under a document is still a database, and the two counts
  // above the trees have always said so: Documents excludes collections,
  // Collections counts every one of them. The trees did not — Documents drew
  // any nested collection, and Collections showed only the top-level ones, so
  // the same database was in the wrong section AND missing from the right one
  // while both numbers looked correct. Sections now match their counts.
  const allKids = ctx.childrenMap.get(p.id) ?? [];
  // Hide a child database only when the Collections section is there to show
  // it — see treeMode.ts. In mixed mode it is not, and filtering here made a
  // database under a document disappear from the interface entirely while the
  // page count went on counting it.
  const kids = childrenForSection(allKids, section, ctx.mixed);
  const isDb = p.type === 'collection';
  // Databases have no tree children (their rows are excluded from /api/pages) but
  // can be expanded to lazily reveal their rows — so they get a chevron too.
  const hasExpand = kids.length > 0 || isDb;
  const isExpanded = ctx.expanded.has(p.id);
  const dt = ctx.dropTarget?.id === p.id ? ctx.dropTarget.zone : null;
  // Row context menu (⋯): close on outside click / Escape, not just mouse-leave.
  const actionsRef = useRef<HTMLSpanElement>(null);
  useMenuDismiss(ctx.menuFor === p.id, actionsRef, () => ctx.setMenuFor(null));
  useMenuDismiss(ctx.addFor === p.id, actionsRef, () => ctx.setAddFor(null));
  return (
    <div>
      <div
        className={
          'tree-item' +
          (p.id === ctx.currentId ? ' active' : '') +
          (dt === 'inside' ? ' drop-inside' : '') +
          (dt === 'before' ? ' drop-before' : '') +
          (dt === 'after' ? ' drop-after' : '')
        }
        style={{ paddingLeft: 6 + depth * 14 }}
        draggable
        onDragStart={(e) => ctx.dragStart(p.id, e)}
        onDragOver={(e) => ctx.dragOver(p, e)}
        onDragLeave={() => ctx.dragLeave(p.id)}
        onDrop={(e) => ctx.drop(p, e)}
        onDragEnd={ctx.dragEnd}
        onClick={(e) => (e.metaKey || e.ctrlKey ? ctx.onOpenInNewTab(p.id) : ctx.onNavigate(p.id))}
        // The whole row answers a right-click with the same menu the ⋯ offers.
        // Hunting for a three-dot button that only appears on hover is the
        // slowest way to reach an action everybody knows is there.
        onContextMenu={(e) => {
          e.preventDefault();
          ctx.setAddFor(null);
          ctx.setMenuFor(p.id);
        }}
        onAuxClick={(e) => {
          if (e.button === 1) {
            e.preventDefault();
            ctx.onOpenInNewTab(p.id);
          }
        }}
      >
        {hasExpand ? (
          <button
            className="chevron"
            onClick={(e) => {
              e.stopPropagation();
              ctx.toggleExpand(p.id);
            }}
          >
            {isExpanded ? '▾' : '▸'}
          </button>
        ) : (
          <span className="chevron spacer" />
        )}
        <span className="tree-icon"><PageIcon icon={p.icon} size={15} fallback={p.type === 'collection' ? <Table2 size={15} /> : <FileText size={15} />} /></span>
        <span className="tree-title">{p.title || 'Untitled'}</span>
        <span className="tree-actions" onClick={(e) => e.stopPropagation()} ref={actionsRef}>
          <button
            title={t('Add inside')}
            onClick={() => {
              ctx.setMenuFor(null);
              ctx.setAddFor(ctx.addFor === p.id ? null : p.id);
            }}
          >
            +
          </button>
          {ctx.addFor === p.id && <AddMenu id={p.id} ctx={ctx} />}
          <button
            title={t('More')}
            onClick={() => {
              ctx.setAddFor(null);
              ctx.setMenuFor(ctx.menuFor === p.id ? null : p.id);
            }}
          >
            ⋯
          </button>
          {ctx.menuFor === p.id && (
            <PageMenu id={p.id} title={p.title} parentId={p.parentId} workspaceId={p.workspaceId} ctx={ctx} />
          )}
        </span>
      </div>
      {isExpanded && isDb && <DbRows collectionId={p.id} workspaceId={p.workspaceId} depth={depth + 1} ctx={ctx} />}
      {isExpanded &&
        !isDb &&
        kids.map((k) => <TreeItem key={k.id} p={k} depth={depth + 1} ctx={ctx} section={section} />)}
    </div>
  );
}

export default function Sidebar({
  pages,
  favorites,
  workspaces,
  currentWs,
  tagColors,
  onSwitchWorkspace,
  onWorkspacesChanged,
  user,
  onUserChanged,
  canCreateWorkspace = true,
  currentId,
  open,
  onCollapse,
  onNavigate,
  onOpenInNewTab,
  onCreate,
  onTrash,
  onDuplicate,
  onRestore,
  onDeleteForever,
  onMove,
  onToggleFavorite,
  onOpenSearch,
  onOpenIndex,
  theme,
  themePref,
  onSetTheme,
  onLogout,
  notesMode = false,
  activeTag = null,
  onSelectTag,
  notesModeSetting = false,
  onToggleNotesMode,
  fontPref = 'brand',
  onSetFont,
  collapsed = false,
  onExpand,
}: Props) {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [trashOpen, setTrashOpen] = useState(false);
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const [addFor, setAddFor] = useState<string | null>(null);
  const [dropTarget, setDropTarget] = useState<DropTarget | null>(null);
  const dragId = useRef<string | null>(null);
  const favSet = useMemo(() => new Set(favorites), [favorites]);
  const byIdAll = useMemo(() => new Map(pages.map((p) => [p.id, p])), [pages]);
  const favPages = useMemo(
    () =>
      favorites
        .map((id) => byIdAll.get(id))
        .filter((p): p is PageMeta => !!p && !p.trashed),
    [favorites, byIdAll],
  );

  const [wsMenuOpen, setWsMenuOpen] = useState(false);
  const wsMenuRef = useRef<HTMLDivElement>(null);
  useMenuDismiss(wsMenuOpen, wsMenuRef, () => setWsMenuOpen(false));
  const [membersOpen, setMembersOpen] = useState(false);
  const [rulesOpen, setRulesOpen] = useState(false);
  // The file view: for the whole workspace, or scoped to one page's subtree
  // ("every document for this customer").
  const [filesFor, setFilesFor] = useState<{ under?: string; title?: string } | null>(null);
  const [wsImageOpen, setWsImageOpen] = useState(false);
  const [agentOpen, setAgentOpen] = useState(false);
  const [tagFilter, setTagFilter] = useState<string | null>(null);
  // Only show the pages of the selected workspace (fall back to all if the
  // list hasn't loaded a workspace id yet).
  const inWs = (p: PageMeta) => !currentWs || p.workspaceId === currentWs;

  // Tag counts across the current workspace's live pages (derived locally from
  // the already-loaded tree list — no extra request).
  // Aggregate case-insensitively (key by lower-case, keep first-seen spelling)
  // so "Work" and "work" are one chip — matching the server's tag dedupe.
  const tagCounts = useMemo(() => {
    const count = new Map<string, number>();
    const label = new Map<string, string>();
    for (const p of pages) {
      if (p.trashed || !inWs(p)) continue;
      for (const t of p.tags ?? []) {
        const k = t.toLowerCase();
        if (!label.has(k)) label.set(k, t);
        count.set(k, (count.get(k) ?? 0) + 1);
      }
    }
    return [...count.entries()]
      .map(([k, n]) => [label.get(k) as string, n] as [string, number])
      // compare() already folds case, so the toLowerCase() this replaced was
      // doing the job twice — and doing the accented half of it wrong.
      .sort((a, b) => b[1] - a[1] || compare(a[0], b[0]));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pages, currentWs]);
  const taggedPages = useMemo(
    () =>
      tagFilter
        ? pages.filter(
            (p) =>
              !p.trashed &&
              inWs(p) &&
              (p.tags ?? []).some((t) => t.toLowerCase() === tagFilter.toLowerCase()),
          )
        : [],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [pages, currentWs, tagFilter],
  );

  // Templates come from their own endpoint: they are deliberately not in the
  // page tree any more, so `pages` no longer carries them. Refetched whenever
  // the page list changes, which is the signal that something was saved,
  // instantiated or thrown away.
  const [allTemplates, setAllTemplates] = useState<PageMeta[]>([]);
  useEffect(() => {
    let alive = true;
    void api
      .templates()
      .then((t) => alive && setAllTemplates(t))
      .catch(() => alive && setAllTemplates([]));
    return () => {
      alive = false;
    };
  }, [pages]);
  const templatePages = useMemo(
    () => allTemplates.filter((p) => !p.trashed && inWs(p)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [allTemplates, currentWs],
  );

  const instantiateTemplate = async (id: string) => {
    try {
      const r = await api.duplicatePage(id, true);
      onNavigate(r.id);
    } catch {
      toast(t('The template could not be used'));
    }
  };
  // ⋯ menu on a template row (remove the flag / trash the snapshot).
  const [tplMenuFor, setTplMenuFor] = useState<string | null>(null);
  const [galleryOpen, setGalleryOpen] = useState(false);
  const unflagTemplate = (id: string) =>
    void api.updatePage(id, { isTemplate: false }).catch(() => toast(t('Could not be changed')));

  // Which of the two sidebar shapes this workspace uses. Needed by the tree
  // itself, not just by the sections — see treeMode.ts.
  const mixed = workspaces.find((w) => w.id === currentWs)?.treeMode === 'mixed';
  const { childrenMap, parentKeyOf, trashRoots } = useMemo(() => {
    const visible = pages.filter((p) => !p.trashed && inWs(p));
    const visibleIds = new Set(visible.map((p) => p.id));
    const childrenMap = new Map<string, PageMeta[]>();
    const parentKeyOf = new Map<string, string>();
    for (const p of visible) {
      const key = p.parentId && visibleIds.has(p.parentId) ? p.parentId : '';
      parentKeyOf.set(p.id, key);
      childrenMap.set(key, [...(childrenMap.get(key) ?? []), p]);
    }
    for (const list of childrenMap.values()) {
      list.sort((a, b) => a.position - b.position || a.id.localeCompare(b.id)); // i18n-ok: opaque IDs, only a stable tiebreaker
    }
    const trashedIds = new Set(pages.filter((p) => p.trashed).map((p) => p.id));
    const trashRoots = pages.filter(
      (p) => p.trashed && inWs(p) && (!p.parentId || !trashedIds.has(p.parentId)),
    );
    return { childrenMap, parentKeyOf, trashRoots };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pages, currentWs]);

  const byId = useMemo(() => new Map(pages.map((p) => [p.id, p])), [pages]);

  const isAncestor = (maybeAncestor: string, id: string): boolean => {
    let cur = byId.get(id);
    let guard = 0;
    while (cur?.parentId && guard++ < 100) {
      if (cur.parentId === maybeAncestor) return true;
      cur = byId.get(cur.parentId);
    }
    return false;
  };

  const canDropOn = (targetId: string) => {
    const id = dragId.current;
    return !!id && id !== targetId && !isAncestor(id, targetId);
  };

  const expand = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });

  const toggleExpand = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const handleDrop = (target: PageMeta, zone: DropTarget['zone']) => {
    const id = dragId.current;
    if (!id || !canDropOn(target.id)) return;
    if (zone === 'inside') {
      const kids = (childrenMap.get(target.id) ?? []).filter((k) => k.id !== id);
      const pos = kids.length ? kids[kids.length - 1].position + 1 : 1;
      onMove(id, target.id, pos);
      expand(target.id);
    } else {
      const key = parentKeyOf.get(target.id) ?? '';
      const siblings = (childrenMap.get(key) ?? []).filter((k) => k.id !== id);
      const idx = siblings.findIndex((s) => s.id === target.id);
      let pos: number;
      if (zone === 'before') {
        const prev = siblings[idx - 1];
        pos = prev ? (prev.position + target.position) / 2 : target.position - 1;
      } else {
        const next = siblings[idx + 1];
        pos = next ? (target.position + next.position) / 2 : target.position + 1;
      }
      onMove(id, key === '' ? null : key, pos);
    }
  };

  const ctx: TreeCtx = {
    childrenMap,
    mixed,
    expanded,
    onOpenInNewTab,
    currentId,
    favorites: favSet,
    dropTarget,
    menuFor,
    addFor,
    onNavigate,
    toggleExpand,
    onCreateChild: (id) => {
      onCreate(id);
      expand(id);
    },
    onCreateChildDb: (id) => {
      onCreate(id, 'collection');
      expand(id);
    },
    onMoveToTop: (id) => {
      const roots = (childrenMap.get('') ?? []).filter((k) => k.id !== id);
      onMove(id, null, roots.length ? roots[roots.length - 1].position + 1 : 1);
    },
    onShowFiles: (id, title) => setFilesFor({ under: id, title }),
    setMenuFor,
    setAddFor,
    onTrash,
    onDuplicate,
    onToggleFavorite,
    workspaces,
    onMoveToWorkspace: (pageId, wsId, wsName) => {
      // The move takes the whole subtree along and puts the page at the top
      // level in the target — the previous parent stays behind.
      void api
        .updatePage(pageId, { workspaceId: wsId })
        .then(() => {
          toast(t('Moved to “{name}”').replace('{name}', wsName));
          // The page tree updates itself through the server's change feed
          // (pagesChanged); only the workspace counters need catching up here.
          onWorkspacesChanged();
        })
        .catch((e: Error) => toast(e.message || t('Moving failed')));
    },
    dragStart: (id, e) => {
      dragId.current = id;
      // Firefox ignores drags without data.
      e.dataTransfer.setData('text/plain', id);
      e.dataTransfer.effectAllowed = 'move';
    },
    dragOver: (p, e) => {
      if (!canDropOn(p.id)) return; // no preventDefault → browser shows no-drop
      e.preventDefault();
      const rect = e.currentTarget.getBoundingClientRect();
      const y = e.clientY - rect.top;
      const zone: DropTarget['zone'] =
        y < rect.height * 0.3 ? 'before' : y > rect.height * 0.7 ? 'after' : 'inside';
      setDropTarget((t) => (t?.id === p.id && t.zone === zone ? t : { id: p.id, zone }));
    },
    dragLeave: (id) => setDropTarget((t) => (t?.id === id ? null : t)),
    drop: (p, e) => {
      e.preventDefault();
      const t = dropTarget;
      setDropTarget(null);
      if (t?.id === p.id) handleDrop(p, t.zone);
    },
    dragEnd: () => {
      dragId.current = null;
      setDropTarget(null);
    },
  };

  const activeWs = workspaces.find((w) => w.id === currentWs);
  // Split the top level into documents vs databases so the sidebar shows them
  // as clearly separated sections (nested items stay under their parent).
  // MIXED: one tree, everything where it was filed. A documentation workspace
  // wants this — there a database genuinely belongs under its document, and
  // hoisting it into a separate section tears it away from what it documents.
  // SPLIT (the default) keeps the two sections, which is right when the
  // databases ARE the thing and the documents are notes beside them.
  //
  // Both readings are correct, for different workspaces. Earlier today they
  // fought each other: nested databases were moved into the Collections
  // section, which fixed one case by breaking the other. A setting settles it.
  const topLevel = childrenMap.get('') ?? [];
  const topDocs = topLevelForDocs(topLevel, mixed);
  // Filtering searches the WHOLE section, not just its top level — a nested
  // page you can't see is exactly the one you're trying to find. Hits render
  // flat (with their parent as context) because tree indentation is noise once
  // the list is already narrowed down.
  //
  // A page whose ancestor chain passes through a database belongs to that
  // database's subtree, not to Documents (W124) — the rows themselves and the
  // dossier pages under them live in the Databases section now.
  const chainHasDb = (p: PageMeta): boolean => {
    let cur = p.parentId ? byId.get(p.parentId) : undefined;
    let guard = 0;
    while (cur && guard++ < 100) {
      if (cur.type === 'collection') return true;
      cur = cur.parentId ? byId.get(cur.parentId) : undefined;
    }
    return false;
  };
  const allDocs = useMemo(
    () =>
      pages.filter(
        (p) =>
          !p.trashed && !p.isTemplate && inWs(p) && !chainHasDb(p) && (mixed || p.type !== 'collection'),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [pages, currentWs],
  );
  // `!p.isTemplate` for the same reason allDocs carries it: a template is a
  // snapshot that stands on its own, not a page in the tree. Without it here, a
  // collection saved as a template showed up under Collections as an ordinary
  // database — and deleting that apparent duplicate deleted the template.
  const allDbs = useMemo(
    () => pages.filter((p) => !p.trashed && !p.isTemplate && inWs(p) && p.type === 'collection'),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [pages, currentWs],
  );
  // A database filed under a document is a root here, not a top-level page —
  // otherwise it shows nowhere at all while still being counted. Only a
  // database inside ANOTHER database stays put, because there it belongs to
  // that one's subtree (a dossier under a row).
  const topDbs = useMemo(
    () => allDbs.filter((p) => !chainHasDb(p)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [allDbs, byId],
  );
  const renameWorkspace = async () => {
    if (!activeWs) return;
    const name = await promptText(t('Rename workspace'), {
      defaultValue: activeWs.name,
      placeholder: t('New name'),
      confirmText: t('Rename'),
    });
    if (!name?.trim() || name.trim() === activeWs.name) return;
    try {
      await api.updateWorkspace(activeWs.id, { name: name.trim() });
      onWorkspacesChanged();
      toast(t('Workspace renamed'));
    } catch (e) {
      toast((e as Error).message || t('Renaming failed'));
    }
  };

  // Deleting takes every page in the workspace with it, so the name has to be
  // typed out — a yes/no dialog is too easy to click through by accident.
  const deleteWorkspace = async () => {
    if (!activeWs) return;
    const typed = await promptText(
      t('Irrevocably delete “{name}” and EVERY page in it?', { name: activeWs.name }) +
        '\n\n' +
        t('Type the workspace name to confirm:'),
      { placeholder: activeWs.name, confirmText: t('Delete permanently') },
    );
    if (typed === null) return;
    try {
      await api.deleteWorkspace(activeWs.id, typed.trim());
      toast(t('Workspace deleted'));
      const next = workspaces.find((w) => w.id !== activeWs.id);
      onWorkspacesChanged();
      if (next) onSwitchWorkspace(next.id);
    } catch (e) {
      toast((e as Error).message || t('Deleting failed'));
    }
  };

  // A new workspace is two decisions, not one: what it is called, and what it
  // starts from. Everything that makes a workspace usable is invisible — the
  // rules, the option ids, the derived columns, the view filters — so "empty" is
  // rarely what somebody actually wants. The shelf asks both questions at once
  // and shows what each answer gets you; a name prompt could only ask the easy
  // one.
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [wsSettingsOpen, setWsSettingsOpen] = useState(false);
  const newWorkspace = () => {
    setWsMenuOpen(false);
    setLibraryOpen(true);
  };

  const [bgLogOpen, setBgLogOpen] = useState(false);
  const [strandedOpen, setStrandedOpen] = useState(false);
  // "Open to everyone": new accounts join this workspace automatically. Replaces
  // the old silent rule that dropped every newcomer into the oldest workspace.
  const toggleAutoJoin = async () => {
    if (!activeWs) return;
    const next = !activeWs.autoJoin;
    try {
      await api.updateWorkspace(activeWs.id, { autoJoin: next });
      onWorkspacesChanged();
      toast(
        next
          ? t('“{name}” is now open to every new account.', { name: activeWs.name })
          : t('“{name}” is no longer assigned to new accounts.', { name: activeWs.name }),
      );
    } catch (e) {
      toast((e as Error).message || t('Could not be changed'));
    }
  };

  const wsImportRef = useRef<HTMLInputElement | null>(null);
  const importWorkspace = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ''; // so the same file can be picked again
    if (!file) return;
    toast(t('Importing workspace…'));
    try {
      const fd = new FormData();
      fd.append('file', file);
      const res = await fetch('/api/workspaces/import', { method: 'POST', body: fd });
      if (!res.ok) {
        const err = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(err?.error || t('Import failed (HTTP {status})', { status: res.status }));
      }
      const out = (await res.json()) as { workspaceId: string; name: string; pages: number };
      toast(t('Imported “{name}” — {pages}', { name: out.name, pages: plural(out.pages, '{n} page', '{n} pages') }));
      onWorkspacesChanged();
      onSwitchWorkspace(out.workspaceId);
    } catch (err) {
      toast((err as Error).message || t('Import failed'));
    }
  };

  return (
    <aside className={'sidebar' + (open ? ' open' : '')}>
      <div className="sidebar-header">
        <div className="ws-switcher" ref={wsMenuRef}>
          <button className="ws-btn" onClick={() => setWsMenuOpen((o) => !o)}>
            <WorkspaceAvatar ws={activeWs} />
            <strong>{activeWs?.name ?? 'salt.md'}</strong>
            <ChevronDown size={14} className="ws-caret" />
          </button>
          {wsMenuOpen && (
            <div className="menu ws-menu">
              {workspaces.map((w) => (
                <button
                  key={w.id}
                  className={'ws-menu-item' + (w.id === currentWs ? ' active' : '')}
                  onClick={() => {
                    onSwitchWorkspace(w.id);
                    setWsMenuOpen(false);
                  }}
                >
                  <WorkspaceAvatar ws={w} />
                  <span className="ws-menu-name">{w.name}</span>
                  {w.personal && <span className="ws-tag">{t('own space')}</span>}
                  {w.autoJoin && !w.personal && <span className="ws-tag">{t('open to all')}</span>}
                  {w.id === currentWs && <Check size={14} />}
                </button>
              ))}
              <div className="menu-sep" />
              {/* One entry instead of the sixteen that used to stand here. A
                  menu is a list of actions you take now; most of what was in it
                  are settings you look at and compare, which is a different
                  shape and now has its own dialog. */}
              {activeWs?.role === 'admin' && (
                <button className="menu-item" onClick={() => { setWsMenuOpen(false); setWsSettingsOpen(true); }}>
                  <Settings2 size={15} /> {t('Workspace settings')}
                </button>
              )}
              {canCreateWorkspace && (
                <button className="menu-item" onClick={newWorkspace}>
                  <Plus size={15} /> {t('New workspace')}
                </button>
              )}
              {user.orgRole === 'owner' && (
                <button
                  className="menu-item"
                  title={t('Workspaces nobody can look after any more')}
                  onClick={() => { setWsMenuOpen(false); setStrandedOpen(true); }}
                >
                  <ShieldAlert size={15} /> {t('With nobody in charge…')}
                </button>
              )}
              {/* Deleting lives in the settings dialog now, next to everything else
                  about this workspace — offering it in two places invites the
                  accident. */}
            </div>
          )}
          <input
            ref={wsImportRef}
            type="file"
            accept=".zip"
            style={{ display: 'none' }}
            onChange={(e) => void importWorkspace(e)}
          />
        </div>
        <div className="sidebar-header-actions">
          <button className="icon-btn" title={t('Library — every page')} onClick={onOpenIndex}>
            <Library size={17} />
          </button>
          {collapsed ? (
            <button
              className="icon-btn collapse-btn pin-btn"
              title={t('Pin the sidebar')}
              onClick={() => onExpand?.()}
            >
              <PanelLeftOpen size={17} />
            </button>
          ) : (
            <button
              className="icon-btn collapse-btn"
              title={t('Collapse the sidebar')}
              onClick={(e) => {
                // This button lives inside the sidebar, so after the click it keeps
                // focus and the collapsed sidebar would stay revealed via the
                // :focus-within rule. Blur it so the sidebar actually slides away.
                e.currentTarget.blur();
                onCollapse();
              }}
            >
              <PanelLeftClose size={17} />
            </button>
          )}
        </div>
      </div>
      <button className="sidebar-search" onClick={onOpenSearch}>
        <span className="sidebar-item-label"><Search size={15} /> {t('Search')}</span>
        <span className="kbd">⌘K</span>
      </button>
      {favPages.length > 0 && (
        <SidebarSection id="fav" label={t('Favourites')} icon={<Star size={17} />} count={favPages.length}>
          {favPages.map((p) => (
              <FlatRow
                key={p.id}
                p={p}
                active={p.id === currentId}
                onNavigate={onNavigate}
                action={
                  <button title={t('Remove from favourites')} onClick={() => onToggleFavorite(p.id)}>
                    ★
                  </button>
                }
              />
            ))
          }
        </SidebarSection>
      )}
      {tagCounts.length > 0 && (
        <SidebarSection id="tags" label="Tags" icon={<Tag size={17} />} count={tagCounts.length}>
          <div className="tag-cloud">
              {tagCounts.map(([t, n]) => (
                <button
                  key={t}
                  className={
                    'tag-chip ' +
                    tagColorClass(t, tagColors) +
                    ((notesMode ? activeTag === t : tagFilter === t) ? ' active' : '')
                  }
                  onClick={() =>
                    notesMode
                      ? onSelectTag?.(activeTag === t ? null : t)
                      : setTagFilter((cur) => (cur === t ? null : t))
                  }
                  title={plural(n, '{n} page', '{n} pages')}
                >
                  #{t} <span className="tag-chip-count">{n}</span>
                </button>
              ))}
          </div>
        </SidebarSection>
      )}
      {tagFilter ? (
        <div className="tree tag-filtered">
          <div className="tag-filter-banner">
            <span>
              <Tag size={13} /> #{tagFilter}
            </span>
            <button className="tag-filter-clear" onClick={() => setTagFilter(null)}>
              {t('Clear filter ×')}
            </button>
          </div>
          {taggedPages.length === 0 && <div className="section-label">{t('No pages.')}</div>}
          {taggedPages.map((p) => (
            <div
              key={p.id}
              className={'tree-item' + (p.id === currentId ? ' active' : '')}
              style={{ paddingLeft: 6 }}
              onClick={() => onNavigate(p.id)}
            >
              <span className="chevron spacer" />
              <span className="tree-icon">
                <PageIcon icon={p.icon} size={15} fallback={p.type === 'collection' ? <Table2 size={15} /> : <FileText size={15} />} />
              </span>
              <span className="tree-title">{p.title || 'Untitled'}</span>
          {/* Same reason as in the table: seeing who is on something should not
              depend on which view you happen to have open. */}
          <AgentDot pageId={p.id} />
            </div>
          ))}
        </div>
      ) : (
        <div className="tree">
          {/* In notes mode the middle column IS the document list — repeating
              the tree here would be pure duplication (user feedback W58). */}
          {!notesMode && (
            <SidebarSection
              id="docs"
              // In mixed mode this is the ONLY section, and it holds databases
              // too — calling it "Documents" would be a lie. "Pages" is the
              // honest word: in this product a database IS a page.
              label={mixed ? t('Pages') : t('Documents')}
              icon={<FileText size={17} />}
              count={allDocs.length}
              createTitle={t('New page')}
              onCreate={() => onCreate(null)}
            >
              {topDocs.length ? (
                topDocs.map((p) => <TreeItem key={p.id} p={p} depth={0} ctx={ctx} />)
              ) : (
                <div className="sb-empty">{t('No pages yet')}</div>
              )}
            </SidebarSection>
          )}
          {/* Hidden in mixed mode: one tree means one tree. Leaving it would
              show every database twice, which is how "one pile" turns into two
              piles that disagree. */}
          {!mixed && (
          <SidebarSection
            id="dbs"
            label={t('Collections')}
            icon={<Table2 size={17} />}
            count={allDbs.length}
            createTitle={t('New collection')}
            onCreate={() => onCreate(null, 'collection')}
          >
            {topDbs.length ? (
              topDbs.map((p) => <TreeItem key={p.id} p={p} depth={0} ctx={ctx} section="dbs" />)
            ) : (
              <div className="sb-empty">{t('No collection yet')}</div>
            )}
          </SidebarSection>
          )}
        </div>
      )}
      {/* The ＋ of the Templates section means "start from a template" — which
          is the gallery, where you can see one before copying it. */}
      {templatePages.length > 0 && (
        <SidebarSection
          id="tpl"
          label={t('Templates')}
          icon={<LayoutTemplate size={17} />}
          count={templatePages.length}
          /* Open by default. The section only exists once there is a template,
             so it appears at the moment somebody saves one — collapsed, it
             looked like the save had done nothing, which is how this was
             reported as broken twice. Collapsing it is remembered, so anybody
             who does not want it open says so once. */
          openOnGrowth
          createTitle={t('New page from a template')}
          onCreate={() => setGalleryOpen(true)}
        >
          {templatePages.map((p) => (
              <div
                key={p.id}
                className={'tree-item sb-flat' + (p.id === currentId ? ' active' : '')}
                /* Clicking a template USES it: it makes a page from the template and
                   opens THAT. It used to open the template itself, which is what every
                   other row in this sidebar does and therefore the obvious thing to try
                   — so people worked inside the template, and deleting it took their
                   document with it. No dependency in the data; a straight path to losing
                   work all the same. Editing the template is the deliberate act now. */
                title={t('New page from this template')}
                onClick={() => void instantiateTemplate(p.id)}
              >
                <span className="chevron spacer" />
                <span className="tree-icon">
                  <PageIcon icon={p.icon} size={15} fallback={<LayoutTemplate size={15} />} />
                </span>
                <span className="tree-title">{p.title || 'Untitled'}</span>
                <span className="tree-actions" onClick={(e) => e.stopPropagation()}>
                  <button title={t('New page from this template')} onClick={() => void instantiateTemplate(p.id)}>
                    ＋
                  </button>
                  <button
                    title={t('More')}
                    onClick={() => setTplMenuFor(tplMenuFor === p.id ? null : p.id)}
                  >
                    ⋯
                  </button>
                  {tplMenuFor === p.id && (
                    <div className="menu">
                      <button
                        onClick={() => {
                          setTplMenuFor(null);
                          onNavigate(p.id);
                        }}
                      >
                        <Pencil size={16} /> {t('Edit the template itself')}
                      </button>
                      <button
                        onClick={() => {
                          setTplMenuFor(null);
                          // Back to a normal page: it leaves this list and
                          // reappears in the tree.
                          unflagTemplate(p.id);
                        }}
                      >
                        <LayoutTemplate size={16} /> {t('Remove template flag')}
                      </button>
                      <button
                        className="danger"
                        onClick={() => {
                          setTplMenuFor(null);
                          onTrash(p.id);
                        }}
                      >
                        <Trash2 size={16} /> {t('Move to trash')}
                      </button>
                    </div>
                  )}
                </span>
              </div>
            ))
          }
        </SidebarSection>
      )}
      {trashRoots.length > 0 && (
        <div className="trash-section">
          <button className="trash-toggle" onClick={() => setTrashOpen(!trashOpen)}>
            <span className="sidebar-item-label"><Trash2 size={15} /> {t('Trash')}</span> <span className="trash-count">{trashRoots.length}</span>
          </button>
          {trashOpen &&
            trashRoots.map((p) => (
              <div key={p.id} className="trash-item">
                <span className="tree-icon"><PageIcon icon={p.icon} size={15} fallback={<FileText size={15} />} /></span>
                <span className="tree-title">{p.title || 'Untitled'}</span>
                <button title={t('Restore')} onClick={() => onRestore(p.id)}>
                  <Undo2 size={14} />
                </button>
                <button
                  title={t('Delete forever')}
                  className="danger"
                  onClick={() => onDeleteForever(p.id)}
                >
                  <X size={14} />
                </button>
              </div>
            ))}
        </div>
      )}
      <div className="sidebar-footer">
        <div className="sidebar-footer-row">
          <UserMenu
            user={user}
            onUserChanged={onUserChanged}
            onLogout={onLogout}
            onOpenAgents={() => setAgentOpen(true)}
            notesMode={notesModeSetting}
            onToggleNotesMode={onToggleNotesMode}
            fontPref={fontPref}
            onSetFont={onSetFont}
          />
          <ThemeSwitch value={themePref} onChange={onSetTheme} />
        </div>
        {membersOpen && currentWs && (
          <WorkspaceMembers
            workspaceId={currentWs}
            myUserId={user.id}
            myRole={activeWs?.role ?? 'member'}
            onClose={() => setMembersOpen(false)}
            onChanged={onWorkspacesChanged}
          />
        )}
        {filesFor && currentWs && (
          <FileList
            workspaceId={currentWs}
            under={filesFor.under}
            underTitle={filesFor.title}
            onOpenPage={onNavigate}
            onClose={() => setFilesFor(null)}
          />
        )}
        {rulesOpen && currentWs && (
          <WorkspaceRules
            workspaceId={currentWs}
            initial={activeWs?.rules ?? ''}
            proposal={activeWs?.rulesProposal ?? ''}
            proposalBy={activeWs?.rulesProposalBy ?? ''}
            proposalAt={activeWs?.rulesProposalAt ?? ''}
            canEdit={activeWs?.role === 'admin'}
            onClose={() => setRulesOpen(false)}
            onSaved={onWorkspacesChanged}
          />
        )}
        {strandedOpen && (
          <StrandedWorkspaces onClose={() => setStrandedOpen(false)} onChanged={onWorkspacesChanged} />
        )}
        {wsSettingsOpen && activeWs && (
          <WorkspaceSettings
            ws={activeWs}
            isOwner={user.orgRole === 'owner'}
            onChanged={onWorkspacesChanged}
            onOpenMembers={() => { setWsSettingsOpen(false); setMembersOpen(true); }}
            onOpenRules={() => { setWsSettingsOpen(false); setRulesOpen(true); }}
            onOpenFiles={() => { setWsSettingsOpen(false); setFilesFor({}); }}
            onOpenBreakGlass={() => { setWsSettingsOpen(false); setBgLogOpen(true); }}
            onOpenImage={() => { setWsSettingsOpen(false); setWsImageOpen(true); }}
            onDelete={() => { setWsSettingsOpen(false); void deleteWorkspace(); }}
            onImport={() => { setWsSettingsOpen(false); wsImportRef.current?.click(); }}
            onClose={() => setWsSettingsOpen(false)}
          />
        )}
        {libraryOpen && (
          <BlueprintLibrary
            workspaces={workspaces}
            onCreated={(id) => {
              onWorkspacesChanged();
              onSwitchWorkspace(id);
            }}
            onClose={() => setLibraryOpen(false)}
          />
        )}
        {bgLogOpen && currentWs && (
          <BreakGlassLog
            workspaceId={currentWs}
            workspaceName={activeWs?.name ?? 'Workspace'}
            onClose={() => setBgLogOpen(false)}
          />
        )}
        {agentOpen && (
          <AgentConnectModal workspaces={workspaces} currentWs={currentWs} onClose={() => setAgentOpen(false)} />
        )}
        {galleryOpen && (
          <TemplateGallery
            templates={templatePages}
            onUse={instantiateTemplate}
            onUnflag={unflagTemplate}
            onTrash={onTrash}
            onClose={() => setGalleryOpen(false)}
          />
        )}
        {wsImageOpen && activeWs && (
          <WorkspaceImageModal
            ws={activeWs}
            onClose={() => setWsImageOpen(false)}
            onChanged={onWorkspacesChanged}
          />
        )}
      </div>
    </aside>
  );
}

// WorkspaceAvatar: the workspace's uploaded logo, else its emoji, else its
// initial in a colour derived from the name — replaces the old 🧂 placeholder.
function WorkspaceAvatar({ ws }: { ws?: Workspace }) {
  if (ws?.image) return <img className="ws-img" src={ws.image} alt="" />;
  // Not only an emoji: a workspace icon takes the same four forms a page icon
  // does, and printing it raw showed "lucide:Rocket" as text.
  if (ws?.icon) return <span className="ws-emoji"><PageIcon icon={ws.icon} size={16} /></span>;
  const name = ws?.name ?? 'salt.md';
  return <span className={'ws-letter ' + tagColorClass(name)}>{name.charAt(0).toUpperCase()}</span>;
}

// WorkspaceImageModal: pick an emoji or upload a logo image for the workspace.
function WorkspaceImageModal({
  ws,
  onClose,
  onChanged,
}: {
  ws: Workspace;
  onClose: () => void;
  onChanged: () => void;
}) {
  useExclusiveModal(onClose);
  const [emoji, setEmoji] = useState(ws.icon);
  const [busy, setBusy] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  // The picker hangs off this button and leaves the dialog through a Portal —
  // .dialog scrolls, and inside it the list was cut off after one row.
  const pickerBtnRef = useRef<HTMLButtonElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const saveEmoji = async (val: string = emoji) => {
    if (!val.trim()) return; // never let an empty "save" wipe an existing logo
    setBusy(true);
    try {
      // Setting an emoji clears any image, and vice-versa (one identity at a time).
      await api.updateWorkspace(ws.id, { icon: val.trim(), image: '' });
      onChanged();
      onClose();
    } catch (e) {
      toast((e as Error).message || t('Saving failed'));
    } finally {
      setBusy(false);
    }
  };

  const uploadImage = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    setBusy(true);
    try {
      const url = await api.upload(file);
      await api.updateWorkspace(ws.id, { image: url, icon: '' });
      onChanged();
      onClose();
    } catch (err) {
      toast((err as Error).message || t('Upload failed'));
    } finally {
      setBusy(false);
    }
  };

  const clear = async () => {
    setBusy(true);
    try {
      await api.updateWorkspace(ws.id, { icon: '', image: '' });
      onChanged();
      onClose();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Workspace picture')}>
          <h2>{t('Workspace picture')}</h2>
          <p className="dialog-hint">{t('Pick an emoji or upload a logo (a company or project logo, say).')}</p>
          <label className="dialog-hint">{t('Emoji')}</label>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', position: 'relative' }}>
            <button className="btn icon-trigger" ref={pickerBtnRef} onClick={() => setPickerOpen((o) => !o)}>
              {emoji || '🙂'} {t('Choose…')}
            </button>
            <input
              className="prop-input"
              style={{ width: 90, textAlign: 'center', fontSize: 20 }}
              value={emoji}
              placeholder={t('or type one')}
              maxLength={4}
              onChange={(e) => setEmoji(e.target.value)}
            />
            <button className="btn primary" disabled={busy || !emoji.trim()} onClick={() => void saveEmoji()}>{t('Save')}</button>
            {pickerOpen && (
              <IconPicker
                anchor={pickerBtnRef}
                onPick={(e) => {
                  setPickerOpen(false);
                  setEmoji(e);
                  void saveEmoji(e);
                }}
                onRemove={() => {
                  setPickerOpen(false);
                  setEmoji('');
                }}
                onClose={() => setPickerOpen(false)}
              />
            )}
          </div>
          <div className="menu-sep" style={{ margin: '12px 0' }} />
          <label className="dialog-hint">{t('Upload a logo')}</label>
          <input ref={fileRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={(e) => void uploadImage(e)} />
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" disabled={busy} onClick={() => fileRef.current?.click()}>{t('Choose a picture…')}</button>
            {(ws.icon || ws.image) && (
              <button className="btn danger" disabled={busy} onClick={() => void clear()}>{t('Remove')}</button>
            )}
          </div>
          <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
        </div>
      </div>
    </Portal>
  );
}
