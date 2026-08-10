import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  useCreateBlockNote,
  SuggestionMenuController,
  getDefaultReactSlashMenuItems,
} from '@blocknote/react';
import { filterSuggestionItems, insertOrUpdateBlockForSlashMenu } from '@blocknote/core';
import { en as coreEn } from '@blocknote/core/locales';
import { BlockNoteView } from '@blocknote/mantine';
import { api } from '../api';
import { toast } from '../toast';
import type { Backlink, CollectionConfig, Page, PageMeta, PropOption, User } from '../types';
import { SaltProvider } from '../collab';
import { plural, t } from '../i18n';
import PropertyValue from './PropertyValue';
import { saltSchema } from '../pageLink';
import IconPicker from './IconPicker';
import { PageIcon } from '../pageIcon';
import { BlockContext } from '../blockContext';
import CollectionView from './CollectionView';
import { HistoryModal } from './PageHistory';
import CommentsPanel, {
  COMMENTS_CHANGED,
  commentsPanelOpen,
  initials,
  setCommentsPanelOpen,
} from './CommentsPanel';
import NoteTrail from './NoteTrail';
import { FilePreview, isPreviewable } from './FilePreview';
import StructurePanel, { structurePanelOpen, setStructurePanelOpen } from './StructurePanel';
import { AgentPresence } from './AgentBadge';
import { usePeers, setPeers, clearPeers } from '../presence';
import { tagColorClass, TAG_PALETTE } from '../tags';
import { collectTags, suggestTags } from '../tagSuggest';
import { useMenuDismiss } from '../modal';
import { Menu, Star, Lock, LockOpen, Globe, MessageSquare, History, MoreHorizontal, Printer, FileCode, FileText, Upload, AlignLeft, Check, Image as ImageIcon , Smile, PanelRight, Link2, Trash2, FilePlus2, Columns2} from 'lucide-react';
import { blockTypeFor, carriesExternalFiles } from '../dropFiles';

export interface EditorProps {
  pageId: string;
  pagesById: Map<string, PageMeta>;
  user: User;
  theme: 'light' | 'dark';
  canEdit: boolean;
  favorite: boolean;
  tagColors: Record<string, string>;
  onSetTagColor: (tag: string, color: string) => void;
  onMenu: () => void;
  onToggleFavorite: (id: string) => void;
  onMetaChange: (id: string, patch: Partial<PageMeta>) => void;
  onMissing: (id: string) => void;
  onNavigate: (id: string | null) => void;
  onCreatePage: (parentId: string | null, type?: 'doc' | 'collection') => void;
  // Deleting was reachable from the sidebar tree only. A database ROW — and any
  // page filed under one — never appears there as a tree item, so nothing in the
  // interface could throw it away: the row menu offers ＋ alone, the board card's
  // ⋯ only moves between columns, and this menu had no entry. The page menu is
  // the one menu every page has, so it is the honest place for it.
  onTrash: (id: string) => void;
  onPagesChanged: () => void;
}

export default function Editor(props: EditorProps) {
  const [page, setPage] = useState<Page | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  // The panel is a reading preference, not a property of the page: it stays
  // put while you move through the workspace, like the comment column's.
  const [structureOpen, setStructureOpen] = useState(structurePanelOpen);
  // Comments now live in a panel beside the document rather than at its foot —
  // his colleagues work in them all day and reported the old shape as awkward:
  // at the very bottom, and two lines to write in.
  const [commentsOpen, setCommentsOpen] = useState(commentsPanelOpen);

  // Both panels occupy the same strip on the right, so opening one closes the
  // other. Sharing the width would leave two columns too narrow to be either.
  const showStructure = (on: boolean) => {
    setStructureOpen(on);
    setStructurePanelOpen(on);
    if (on) {
      setCommentsOpen(false);
      setCommentsPanelOpen(false);
    }
  };
  const showComments = (on: boolean) => {
    setCommentsOpen(on);
    setCommentsPanelOpen(on);
    if (on) {
      setStructureOpen(false);
      setStructurePanelOpen(false);
    }
  };
  const toggleStructure = () => showStructure(!structureOpen);

  useEffect(() => {
    let alive = true;
    setPage(null);
    setError(null);
    api
      .getPage(props.pageId)
      .then((p) => {
        if (alive) setPage(p);
      })
      .catch((e: Error) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [props.pageId, nonce]);

  if (error) {
    return (
      <div className="editor-error">
        <p>{t('This page could not be loaded.')}</p>
        <button className="btn" onClick={() => props.onMissing(props.pageId)}>
          {t('Back to workspace')}
        </button>
      </div>
    );
  }
  if (!page) return <div className="editor-loading" />;

  // A database carries no comments — its ROWS do, and each of those is a page
  // with its own panel. Suppressing the panel alone was not enough: the button
  // stayed, which is a promise the page cannot keep, and the column it holds
  // open stayed with it, so the table rendered 340px narrow beside nothing.
  const canComment = page.type !== 'collection';

  // The content renders INSIDE PageHeader's .page-body scroller so cover,
  // title and content scroll away together (only the topbar stays fixed).
  return (
    <div
      className={
        'editor-page' + (structureOpen || (commentsOpen && canComment) ? ' with-structure' : '')
      }
    >
      <PageHeader
        page={page}
        {...props}
        structureOpen={structureOpen}
        onToggleStructure={toggleStructure}
        commentsOpen={commentsOpen}
        onToggleComments={showComments}
        onLocalMeta={(patch) => setPage((p) => (p ? { ...p, ...patch } : p))}
      >
        {page.type === 'collection' ? (
          <CollectionView
            key={page.id}
            collectionId={page.id}
            pages={props.pagesById}
            tagColors={props.tagColors}
            onNavigate={props.onNavigate}
            onPagesChanged={props.onPagesChanged}
          />
        ) : (
          <CollabEditor
            key={page.id}
            structureOpen={structureOpen}
            page={page}
            user={props.user}
            theme={props.theme}
            canEdit={props.canEdit}
            pagesById={props.pagesById}
            tagColors={props.tagColors}
            onNavigate={props.onNavigate}
            onCreatePage={props.onCreatePage}
            onPagesChanged={props.onPagesChanged}
            onReset={() => setNonce((n) => n + 1)}
          />
        )}
      </PageHeader>
      {commentsOpen && canComment && (
        <CommentsPanel
          pageId={page.id}
          myUserId={props.user.id}
          open
          onClose={() => showComments(false)}
        />
      )}
      {structureOpen && (
        <StructurePanel
          pageId={page.id}
          isCollection={page.type === 'collection'}
          pagesById={props.pagesById}
          onNavigate={props.onNavigate}
          onClose={toggleStructure}
        />
      )}
    </div>
  );
}

// ---- shared header (breadcrumbs, title, icon) ----

const COVER_GRADIENTS = [
  // Light → dark (left → right): the page icon docks at the LEFT edge, so the
  // pale end sits behind it and keeps emoji/dark icons readable.
  'gradient:linear-gradient(120deg,#4fa872,#2f7d4f)',
  'gradient:linear-gradient(120deg,#6aa9e0,#3b6fb5)',
  'gradient:linear-gradient(120deg,#e0c56a,#b58a3b)',
  'gradient:linear-gradient(120deg,#b07de0,#7d4fb0)',
  'gradient:linear-gradient(120deg,#e0846a,#c4554d)',
  'gradient:linear-gradient(120deg,#6ad0d0,#3ba0a8)',
  // W96: more choice — soft two- and three-tone blends (aurora, sunset, sea),
  // still light to dark so the page emoji on the left docks legibly. On the
  // server side validCover lets any pure gradient through.
  'gradient:linear-gradient(120deg,#ffd3a5,#fd6585)',
  'gradient:linear-gradient(120deg,#a8edea,#5b86e5)',
  'gradient:linear-gradient(120deg,#f6d365,#fda085)',
  'gradient:linear-gradient(120deg,#d4fc79,#4a934a)',
  'gradient:linear-gradient(120deg,#e0c3fc,#8e63c9)',
  'gradient:linear-gradient(120deg,#f5efe6,#b8a389)',
  'gradient:linear-gradient(120deg,#fbc2eb,#a18cd1)',
  'gradient:linear-gradient(120deg,#fddb92,#d1858c)',
  'gradient:linear-gradient(120deg,#9be2d5,#2c7a7b)',
  'gradient:linear-gradient(120deg,#c9d6ff,#5c6bc0)',
  'gradient:linear-gradient(135deg,#ffecd2,#fcb69f 55%,#e0846a)',
  'gradient:linear-gradient(135deg,#a1c4fd,#c2e9fb 45%,#6aa9e0)',
  // Saturated, and on purpose: everything above is a soft blend, so a page that
  // wants to shout had nothing to reach for. The site's own beam colours.
  'gradient:linear-gradient(120deg,#ff2d60,#ff8a2d 25%,#ffd12d 45%,#22c55e 62%,#3b82f6 80%,#9333ea)',
  'gradient:linear-gradient(120deg,#22c55e,#3b82f6 55%,#9333ea)',
  'gradient:linear-gradient(120deg,#ffd12d,#ff8a2d 45%,#ff2d60)',
  'gradient:linear-gradient(120deg,#22d3ee,#3b82f6 50%,#9333ea)',
];

function coverStyle(cover: string): React.CSSProperties {
  if (cover.startsWith('gradient:')) return { background: cover.slice('gradient:'.length) };
  return { backgroundImage: `url(${cover})`, backgroundSize: 'cover', backgroundPosition: 'center' };
}

// A function, not a constant: a constant would resolve t() once at import time
// and keep whatever language happened to be active then.
const tagColorLabel = (c: string): string =>
  ({
    gray: t('Gray'),
    brown: t('Brown'),
    orange: t('Orange'),
    yellow: t('Yellow'),
    green: t('Green'),
    blue: t('Blue'),
    purple: t('Purple'),
    pink: t('Pink'),
    red: t('Red'),
  })[c] ?? c;

// A tag chip with a Notion-style colour picker: click the label to choose a
// colour (or "Default" = automatic), the × removes the tag.
function TagChip({
  tag,
  colors,
  canEdit,
  onRemove,
  onSetColor,
}: {
  tag: string;
  colors: Record<string, string>;
  canEdit: boolean;
  onRemove: () => void;
  onSetColor: (color: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLSpanElement>(null);
  useMenuDismiss(open, wrapRef, () => setOpen(false));
  const current = colors[tag.toLowerCase()] || '';
  return (
    <span className={'page-tag ' + tagColorClass(tag, colors)} ref={wrapRef}>
      <button
        className="page-tag-label"
        onClick={() => canEdit && setOpen((o) => !o)}
        title={canEdit ? t('Change colour') : undefined}
      >
        #{tag}
      </button>
      {canEdit && (
        <button
          className="page-tag-x"
          title={t('Remove tag')}
          aria-label={t('Remove tag {tag}', { tag })}
          onClick={onRemove}
        >
          ×
        </button>
      )}
      {open && (
        <div className="menu tag-color-menu">
          <div className="menu-label">{t('Colour')}</div>
          <button
            className="tag-color-opt"
            onClick={() => {
              onSetColor('');
              setOpen(false);
            }}
          >
            <span className="tag-swatch tag-gray" />
            <span className="tag-color-name">{t('Default')}</span>
            {!current && <Check size={14} />}
          </button>
          {TAG_PALETTE.map((c) => (
            <button
              key={c}
              className="tag-color-opt"
              onClick={() => {
                onSetColor(c);
                setOpen(false);
              }}
            >
              <span className={'tag-swatch tag-' + c} />
              <span className="tag-color-name">{tagColorLabel(c)}</span>
              {current === c && <Check size={14} />}
            </button>
          ))}
        </div>
      )}
    </span>
  );
}

// RowProperties renders a database row's typed properties as a Notion-style
// panel under the title (label · value), so a row's page shows its Status,
// Priority, etc. as real fields — not as text dumped into the body. Shown only
// when the page is a child of a collection. Reuses the same PropertyValue cells
// (and inline option editing) as the table/board.
function RowProperties({
  pageId,
  parentId,
  initialProps,
  canEdit,
}: {
  pageId: string;
  parentId: string;
  initialProps: Record<string, unknown>;
  canEdit: boolean;
}) {
  const [config, setConfig] = useState<CollectionConfig | null>(null);
  const [props, setProps] = useState<Record<string, unknown>>(initialProps ?? {});

  useEffect(() => {
    setProps(initialProps ?? {});
  }, [pageId, initialProps]);

  useEffect(() => {
    let alive = true;
    api
      .getCollection(parentId)
      .then((c) => alive && setConfig(c))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [parentId]);

  if (!config || config.schema.length === 0) return null;

  const setProp = async (propId: string, value: unknown) => {
    setProps((p) => ({ ...p, [propId]: value }));
    try {
      await api.updatePage(pageId, { propsPatch: { [propId]: value } });
    } catch {
      toast(t('Property not saved'));
    }
  };
  const setOptions = async (propId: string, options: PropOption[]) => {
    const next: CollectionConfig = {
      ...config,
      schema: config.schema.map((p) => (p.id === propId ? { ...p, options } : p)),
    };
    setConfig(next);
    try {
      await api.putCollection(parentId, next);
    } catch {
      toast(t('Options not saved'));
    }
  };

  return (
    <div className="row-props">
      {config.schema.map((p) => (
        <div key={p.id} className="row-prop">
          <div className="row-prop-label" title={p.name}>
            {p.name}
          </div>
          <div className="row-prop-value">
            <PropertyValue
              def={p}
              value={props[p.id]}
              onChange={canEdit ? (v) => setProp(p.id, v) : undefined}
              onOptionsChange={canEdit ? (o) => setOptions(p.id, o) : undefined}
              readOnly={!canEdit}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

function PageHeader({
  page,
  pageId,
  favorite,
  user,
  canEdit,
  tagColors,
  onSetTagColor,
  onMenu,
  onToggleFavorite,
  onMetaChange,
  onNavigate,
  onTrash,
  onLocalMeta,
  onPagesChanged,
  pagesById,
  structureOpen,
  onToggleStructure,
  commentsOpen,
  onToggleComments,
  children,
}: EditorProps & {
  page: Page;
  onLocalMeta: (patch: Partial<PageMeta>) => void;
  structureOpen: boolean;
  onToggleStructure: () => void;
  commentsOpen: boolean;
  onToggleComments: (open: boolean) => void;
  children?: React.ReactNode;
}) {
  const [title, setTitle] = useState(page.title);
  const [tags, setTags] = useState<string[]>(page.tags ?? []);
  const [tagDraft, setTagDraft] = useState('');
  const [tagSuggestOpen, setTagSuggestOpen] = useState(false);
  const [tagSel, setTagSel] = useState(0);
  const [description, setDescription] = useState(page.description ?? '');
  const [showDesc, setShowDesc] = useState(!!page.description);
  const importInput = useRef<HTMLInputElement>(null);
  const [icon, setIcon] = useState(page.icon);
  const [cover, setCover] = useState(page.cover);
  const [visibility, setVisibility] = useState(page.visibility);
  const [shareUrl, setShareUrl] = useState<string | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [shareExpiry, setShareExpiry] = useState(0); // days; 0 = never
  const [sharePassword, setSharePassword] = useState('');
  const [overflowOpen, setOverflowOpen] = useState(false);
  // Dropdowns must close on an outside click / Escape, not just mouse-leave.
  const shareWrapRef = useRef<HTMLDivElement>(null);
  const overflowWrapRef = useRef<HTMLDivElement>(null);
  useMenuDismiss(shareOpen, shareWrapRef, () => setShareOpen(false));
  useMenuDismiss(overflowOpen, overflowWrapRef, () => setOverflowOpen(false));
  const [historyOpen, setHistoryOpen] = useState(false);
  const [openComments, setOpenComments] = useState(0);
  // Same rule as in Editor, and it has to be asked here too: this is where the
  // button, the menu entries and the count live.
  const canComment = page.type !== 'collection';
  const peers = usePeers(pageId);
  // The counter in the header should be right before anybody has scrolled
  // down — you should SEE that comments exist without going to look.
  useEffect(() => {
    if (!canComment) return;
    let alive = true;
    const count = () =>
      api
        .listComments(pageId)
        .then((l) => alive && setOpenComments(l.filter((c) => !c.resolvedAt).length))
        .catch(() => {});
    void count();
    // The panel says when it changed something. An event rather than a prop:
    // the count has to be right whether the panel is open or not, so the header
    // owns its own fetch and the panel only nudges it.
    window.addEventListener(COMMENTS_CHANGED, count);
    return () => {
      alive = false;
      window.removeEventListener(COMMENTS_CHANGED, count);
    };
  }, [pageId, canComment]);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [coverMenuOpen, setCoverMenuOpen] = useState(false);
  const coverInput = useRef<HTMLInputElement>(null);
  const saveTimer = useRef<number | undefined>(undefined);
  const pendingMeta = useRef<{ title?: string; icon?: string; cover?: string; tags?: string[]; description?: string }>({});
  const bodyRef = useRef<HTMLDivElement>(null);
  const titleRef = useRef<HTMLTextAreaElement>(null);

  // Grow the title to fit its text (any length wraps to as many lines as needed,
  // like Notion) — on every edit and whenever the page (and thus title) changes.
  useLayoutEffect(() => {
    const el = titleRef.current;
    if (!el) return;
    const fit = () => {
      el.style.height = 'auto';
      el.style.height = el.scrollHeight + 'px';
    };
    fit();
    // scrollHeight depends on the WIDTH. When the column gets narrower — the
    // window shrinks, the sidebar collapses — the title wraps onto more lines
    // without the text changing at all. Without measuring again the old height
    // stayed and the last line disappeared behind the buttons below it.
    //
    // What is observed is the PARENT, not the field itself: fit() changes the
    // height of the field, and changing an element inside its own callback
    // creates a loop — which the browser quietly switches off. That is exactly
    // what the first version failed on.
    const box = el.parentElement;
    if (!box) return;
    const ro = new ResizeObserver(fit);
    ro.observe(box);
    return () => ro.disconnect();
  }, [title]);

  useEffect(() => {
    setTitle(page.title);
    setIcon(page.icon);
    setCover(page.cover);
    setVisibility(page.visibility);
  }, [page.title, page.icon, page.cover, page.visibility]);

  // Toggle a `scrolled` class on the page body (with hysteresis so it never
  // flaps) — CSS uses it to shrink the docked page icon once the collapsed
  // cover strip is pinned.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    let scrolled = false;
    const onScroll = () => {
      const s = el.scrollTop;
      if (!scrolled && s > 110) {
        scrolled = true;
        el.classList.add('scrolled');
      } else if (scrolled && s < 90) {
        scrolled = false;
        el.classList.remove('scrolled');
      }
    };
    onScroll();
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, []);

  const togglePrivate = () => {
    const next = visibility === 'private' ? 'workspace' : 'private';
    setVisibility(next);
    api.updatePage(pageId, { visibility: next }).catch(() => toast(t('Visibility not saved')));
  };

  const createShare = async (days: number, password: string) => {
    try {
      const res = await api.sharePage(pageId, days, password);
      // Absolute URL on the external domain when configured; else current origin.
      setShareUrl(res.url.startsWith('http') ? res.url : location.origin + res.url);
    } catch {
      toast(t('Sharing failed'));
    }
  };

  const openShare = async () => {
    setShareOpen((o) => !o);
    if (!shareUrl) await createShare(shareExpiry, sharePassword);
  };

  const changeExpiry = async (days: number) => {
    setShareExpiry(days);
    // Re-mint the link with the new settings (the server replaces the old token).
    await createShare(days, sharePassword);
  };

  const stopShare = async () => {
    await api.unsharePage(pageId).catch(() => {});
    setShareUrl(null);
    setShareOpen(false);
  };

  const saveMeta = (patch: { title?: string; icon?: string; cover?: string; tags?: string[]; description?: string }) => {
    onMetaChange(pageId, patch);
    onLocalMeta(patch);
    // Accumulate across fields so a title edit followed quickly by a cover
    // change doesn't cancel the title write — a single shared timer flushes
    // the merged patch.
    Object.assign(pendingMeta.current, patch);
    window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => {
      const merged = pendingMeta.current;
      pendingMeta.current = {};
      api.updatePage(pageId, merged).catch(() => {
        Object.assign(pendingMeta.current, merged); // keep for a later retry
        toast(t('Title/icon not saved'));
      });
    }, 500);
  };

  // Tags: client-side clean (strip '#', dedupe) is cosmetic — the server
  // re-normalizes authoritatively on save.
  const commitTags = (next: string[]) => {
    const clean: string[] = [];
    const seen = new Set<string>();
    for (let t of next) {
      t = t.trim().replace(/^#/, '').replace(/\s+/g, '-');
      if (!t) continue;
      const k = t.toLowerCase();
      if (seen.has(k)) continue;
      seen.add(k);
      clean.push(t);
    }
    setTags(clean);
    saveMeta({ tags: clean });
  };
  const addTag = (value?: string) => {
    const v = value ?? tagDraft;
    if (v.trim()) {
      commitTags([...tags, v]);
      setTagDraft('');
      setTagSuggestOpen(false);
      setTagSel(0);
    }
  };
  const removeTag = (t: string) => commitTags(tags.filter((x) => x !== t));

  // Every tag already in use is in the page metadata — no extra request needed
  // for this.
  const allTags = useMemo(() => collectTags(pagesById.values()), [pagesById]);
  const tagHits = useMemo(
    () => (tagSuggestOpen ? suggestTags(allTags, tagDraft, tags) : []),
    [tagSuggestOpen, allTags, tagDraft, tags],
  );

  const changeDescription = (v: string) => {
    setDescription(v);
    saveMeta({ description: v });
  };
  const removeDescription = () => {
    setShowDesc(false);
    changeDescription('');
  };

  const onImportFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    try {
      if (file.name.toLowerCase().endsWith('.zip')) {
        const r = await api.importZip(file);
        toast(
          t('Imported {pages}', { pages: plural(r.created, '{n} page', '{n} pages') }) +
            (r.skipped ? ', ' + t('{n} skipped', { n: r.skipped }) : ''),
        );
        onPagesChanged();
      } else {
        const text = await file.text();
        const r = await api.importMarkdown(null, '', text);
        toast(t('Page imported'));
        onPagesChanged();
        onNavigate(r.id);
      }
    } catch (err) {
      toast((err as Error).message || t('Import failed'));
    }
  };

  const setCoverValue = (value: string) => {
    setCover(value);
    setCoverMenuOpen(false);
    saveMeta({ cover: value });
  };

  const onCoverFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    const url = await api.upload(file);
    setCoverValue(url);
  };

  const breadcrumbs = useMemo(() => {
    const chain: PageMeta[] = [];
    // Database rows are excluded from the tree map, so seed the chain from the
    // loaded page itself when it isn't in pagesById — otherwise a row would show
    // no breadcrumb at all.
    let cur: PageMeta | undefined = pagesById.get(pageId) ?? page;
    let guard = 0;
    while (cur && guard++ < 30) {
      chain.unshift(cur);
      cur = cur.parentId ? pagesById.get(cur.parentId) : undefined;
    }
    return chain;
  }, [pagesById, pageId, page]);

  return (
    <>
      <header className="topbar">
        <button className="menu-btn topbar-menu" title={t('Menu')} onClick={onMenu}>
          <Menu size={18} />
        </button>
        <nav className="breadcrumbs">
          {breadcrumbs.map((b, i) => (
            <span key={b.id} className="crumb-wrap">
              {i > 0 && <span className="crumb-sep">/</span>}
              <button className="crumb" onClick={() => onNavigate(b.id)}>
                {(() => {
                  const ic = b.id === pageId ? icon : b.icon;
                  const tt = (b.id === pageId ? title : b.title) || 'Untitled';
                  return (
                    <>
                      {ic && (
                        <>
                          <PageIcon icon={ic} size={13} />{' '}
                        </>
                      )}
                      {tt}
                    </>
                  );
                })()}
              </button>
            </span>
          ))}
        </nav>
        <div className="topbar-right">
          {/* Which agent says it is working here. Beside the human presence,
              because it answers the same question. */}
          <AgentPresence pageId={pageId} />
          {/* Who else is on the page right now. Presence was already being SENT
              (awareness) but shown nowhere — two people worked in the same
              document without either of them noticing. */}
          {peers.length > 0 && (
            <div className="presence" title={t('Also here: {names}').replace('{names}', peers.map((p) => p.name).join(', '))}>
              {peers.slice(0, 3).map((p, i) => (
                <span key={i} className="presence-dot" style={{ background: p.avatar ? 'transparent' : p.color }}>
                  {p.avatar ? <img src={p.avatar} alt="" /> : initials(p.name)}
                </span>
              ))}
              {peers.length > 3 && <span className="presence-dot more">+{peers.length - 3}</span>}
            </div>
          )}
          {/* One icon, marked active when the panel is open — the same shape
              the structure button has. The struck-through variant read as
              "comments are off" rather than "the panel is closed", which is a
              different and alarming thing to say about a page. */}
          {canComment && (
          <button
            className={'icon-btn topbar-wide-only' + (commentsOpen ? ' active-star' : '')}
            title={commentsOpen ? t('Hide comments') : t('Show comments')}
            onClick={() => onToggleComments(!commentsOpen)}
          >
            <MessageSquare size={17} />
            {openComments > 0 && <span className="badge-count">{openComments}</span>}
          </button>
          )}
          <button
            className={'icon-btn' + (structureOpen ? ' active-star' : '')}
            title={structureOpen ? t('Hide structure') : t('Show structure')}
            onClick={onToggleStructure}
          >
            <PanelRight size={17} />
          </button>
          <button
            className={'icon-btn' + (favorite ? ' active-star' : '')}
            title={favorite ? t('Remove from favorites') : t('Add to favorites')}
            onClick={() => onToggleFavorite(pageId)}
          >
            <Star size={17} fill={favorite ? 'currentColor' : 'none'} />
          </button>
          <button
            className={'icon-btn topbar-wide-only' + (visibility === 'private' ? ' active-star' : '')}
            title={visibility === 'private' ? t('Private (only you) — click to share with the workspace') : t('Visible to the workspace — click to make it private')}
            onClick={togglePrivate}
          >
            {visibility === 'private' ? <Lock size={17} /> : <LockOpen size={17} />}
          </button>
          <div className="share-wrap" ref={shareWrapRef}>
            <button className="icon-btn topbar-wide-only" title={t('Share to web (read-only link)')} onClick={openShare}>
              <Globe size={17} />
            </button>
            {shareOpen && (
              <div className="menu share-menu">
                <div className="share-hint">Anyone with this link can view this page (read-only).</div>
                <input className="share-input" readOnly value={shareUrl ?? 'Creating…'} onFocus={(e) => e.currentTarget.select()} />
                <label className="share-expiry">
                  Expires:
                  <select
                    className="prop-select"
                    value={shareExpiry}
                    onChange={(e) => void changeExpiry(Number(e.target.value))}
                  >
                    <option value={0}>{t('Never')}</option>
                    <option value={1}>{t('In 1 day')}</option>
                    <option value={7}>{t('In 7 days')}</option>
                    <option value={30}>{t('In 30 days')}</option>
                  </select>
                </label>
                <input
                  className="share-input"
                  type="password"
                  placeholder={t('Password (optional)')}
                  value={sharePassword}
                  onChange={(e) => setSharePassword(e.target.value)}
                  onBlur={() => void createShare(shareExpiry, sharePassword)}
                />
                <div className="share-actions">
                  <button
                    className="btn-sm"
                    onClick={() => shareUrl && void navigator.clipboard.writeText(shareUrl)}
                  >
                    {t('Copy')}
                  </button>
                  <button className="btn-sm danger" onClick={stopShare}>
                    {t('Stop sharing')}
                  </button>
                </div>
              </div>
            )}
          </div>
          <div className="share-wrap" ref={overflowWrapRef}>
            <button
              className="icon-btn"
              title={t('More')}
              aria-label={t('More actions')}
              onClick={() => setOverflowOpen((o) => !o)}
            >
              <MoreHorizontal size={17} />
            </button>
            <input
              ref={importInput}
              type="file"
              accept=".md,.markdown,.zip"
              style={{ display: 'none' }}
              onChange={(e) => void onImportFile(e)}
            />
            {overflowOpen && (
              <div className="menu overflow-menu">
                {canEdit && !showDesc && !description && (
                  <button
                    className="menu-item"
                    onClick={() => {
                      setOverflowOpen(false);
                      setShowDesc(true);
                    }}
                  >
                    <AlignLeft size={15} /> {t('Add a description')}
                  </button>
                )}
                {canEdit && (showDesc || description) && (
                  <button
                    className="menu-item"
                    onClick={() => {
                      setOverflowOpen(false);
                      removeDescription();
                    }}
                  >
                    <AlignLeft size={15} /> {t('Remove description')}
                  </button>
                )}
                {canComment && (
                <button
                  className="menu-item"
                  onClick={() => {
                    setOverflowOpen(false);
                    // Always OPENS them — an entry that toggles would be a
                    // no-op half the time somebody reaches for it.
                    onToggleComments(true);
                  }}
                >
                  <MessageSquare size={15} /> {t('To the comments')}
                </button>
                )}
                <button
                  className="menu-item"
                  onClick={() => {
                    setOverflowOpen(false);
                    setHistoryOpen(true);
                  }}
                >
                  <History size={15} /> {t('Version history')}
                </button>
                {/* On a phone the topbar keeps only the star, the panel and this
                    menu — six icons side by side made the head of the page look
                    busier than its content. The three that step aside come back
                    here, so nothing becomes unreachable. */}
                <div className="menu-sep narrow-only" />
                {canComment && (
                <button
                  className="menu-item narrow-only"
                  onClick={() => {
                    setOverflowOpen(false);
                    onToggleComments(!commentsOpen);
                  }}
                >
                  <MessageSquare size={15} />{' '}
                  {commentsOpen ? t('Hide comments') : t('Show comments')}
                </button>
                )}
                <button
                  className="menu-item narrow-only"
                  onClick={() => {
                    setOverflowOpen(false);
                    togglePrivate();
                  }}
                >
                  {visibility === 'private' ? <Lock size={15} /> : <LockOpen size={15} />}{' '}
                  {visibility === 'private' ? t('Make it visible to the workspace') : t('Make it private')}
                </button>
                <button
                  className="menu-item narrow-only"
                  onClick={() => {
                    setOverflowOpen(false);
                    openShare();
                  }}
                >
                  <Globe size={15} /> {t('Share to web (read-only link)')}
                </button>
                {canEdit && (
                  <button
                    className="menu-item"
                    onClick={() => {
                      importInput.current?.click();
                      setOverflowOpen(false);
                    }}
                  >
                    <Upload size={15} /> Import (.md / .zip)
                  </button>
                )}
                <div className="menu-sep" />
                <div className="menu-label">{t('Export')}</div>
                <button
                  className="menu-item"
                  onClick={() => {
                    api.download(`/api/export/${pageId}`);
                    setOverflowOpen(false);
                  }}
                >
                  <FileText size={15} /> Markdown (.md)
                </button>
                <button
                  className="menu-item"
                  onClick={() => {
                    api.download(`/api/export/${pageId}?format=html`);
                    setOverflowOpen(false);
                  }}
                >
                  <FileCode size={15} /> {t('Web page (.html)')}
                </button>
                <button
                  className="menu-item"
                  onClick={() => {
                    setOverflowOpen(false);
                    if (page.type === 'collection') {
                      setTimeout(() => window.print(), 50);
                    } else {
                      // A clean standalone print/PDF view — works on mobile too,
                      // where window.print() is a no-op (share → save as PDF).
                      window.open(`/api/export/${pageId}?format=html&print=1`, '_blank');
                    }
                  }}
                >
                  <Printer size={15} /> {t('Print / as PDF')}
                </button>
                {canEdit && (
                  <button
                    className="menu-item danger"
                    onClick={() => {
                      setOverflowOpen(false);
                      onTrash(pageId);
                    }}
                  >
                    <Trash2 size={15} /> {t('Move to trash')}
                  </button>
                )}
              </div>
            )}
          </div>
        </div>
      </header>
      {/* Everything below the topbar scrolls as ONE page (Notion-style): the
          cover, icon, title, tags and the content leave the screen together —
          only the slim topbar stays. Crucial on mobile, where a static header
          left just a tiny scrolling window. */}
      <div className={'page-body' + (cover ? ' has-cover' : '')} ref={bodyRef}>
      {cover && (
        <div className="page-cover" style={coverStyle(cover)}>
          <div className="page-cover-actions">
            <input
              ref={coverInput}
              type="file"
              accept="image/*"
              style={{ display: 'none' }}
              onChange={onCoverFile}
            />
            <button className="cover-btn" onClick={() => setCoverMenuOpen((o) => !o)}>
              {t('Change cover')}
            </button>
            <button className="cover-btn" onClick={() => setCoverValue('')}>
              {t('Remove')}
            </button>
            {coverMenuOpen && (
              <CoverMenu
                onGradient={setCoverValue}
                onUpload={() => coverInput.current?.click()}
                onClose={() => setCoverMenuOpen(false)}
              />
            )}
          </div>
        </div>
      )}
      {/* The icon row lives OUTSIDE .page-head so it can dock (sticky) on the
          collapsed cover strip while scrolling — the big emoji stays fully
          visible instead of sliding half-hidden under the banner. */}
      <div className={'page-icon-row' + (page.type === 'collection' ? ' page-icon-row--db' : '')}>
        {icon && (
          <button className="page-icon icon-trigger" onClick={() => setIconPickerOpen((o) => !o)}>
            <PageIcon icon={icon} size={54} />
          </button>
        )}
        {iconPickerOpen && (
          <IconPicker
            pageId={pageId}
            onPick={(e) => {
              setIcon(e);
              setIconPickerOpen(false);
              saveMeta({ icon: e });
            }}
            onRemove={() => {
              setIcon('');
              setIconPickerOpen(false);
              saveMeta({ icon: '' });
            }}
            onClose={() => setIconPickerOpen(false)}
          />
        )}
        {historyOpen && (
          <HistoryModal
            pageId={pageId}
            onClose={() => setHistoryOpen(false)}
            onRestored={onPagesChanged}
          />
        )}
      </div>
      <div className={'page-head' + (cover ? ' with-cover' : '') + (page.type === 'collection' ? ' page-head--db' : '')}>
        <textarea
          ref={titleRef}
          className="page-title"
          value={title}
          placeholder={t('Untitled')}
          rows={1}
          onChange={(e) => {
            setTitle(e.target.value);
            saveMeta({ title: e.target.value });
          }}
          onKeyDown={(e) => {
            // A title is a single line of text: Enter never inserts a newline
            // (it jumps into the body instead of breaking the title).
            if (e.key === 'Enter') {
              e.preventDefault();
              bodyRef.current?.querySelector<HTMLElement>('[contenteditable="true"]')?.focus();
            }
          }}
        />
        {/* A permanently visible add row BELOW the title (rather than buttons
            hidden behind hover — unreachable on touch devices as soon as there
            was a cover). Shows only what is still missing: emoji, cover,
            description. */}
        {canEdit && (!icon || !cover || (!showDesc && !description)) && (
          <div className="head-adders">
            <input
              ref={coverInput}
              type="file"
              accept="image/*"
              style={{ display: 'none' }}
              onChange={onCoverFile}
            />
            {!icon && (
              <button className="add-btn" onClick={() => setIconPickerOpen((o) => !o)}>
                <Smile size={14} /> {t('Emoji')}
              </button>
            )}
            {!cover && (
              <button className="add-btn cover-trigger" onClick={() => setCoverMenuOpen((o) => !o)}>
                <ImageIcon size={14} /> {t('Cover')}
              </button>
            )}
            {!showDesc && !description && (
              <button className="add-btn" onClick={() => setShowDesc(true)}>
                <AlignLeft size={14} /> {t('Description')}
              </button>
            )}
            {/* Cover menu only here while NO cover exists — with one, the
                "Change cover" button top right renders it (W32c: two instances
                on the same state eat each other's clicks). */}
            {!cover && coverMenuOpen && (
              <CoverMenu
                onGradient={setCoverValue}
                onUpload={() => coverInput.current?.click()}
                onClose={() => setCoverMenuOpen(false)}
              />
            )}
          </div>
        )}
        {(showDesc || description) && (
          <div className="page-description">
            {canEdit ? (
              <textarea
                value={description}
                placeholder={t('Add a description…')}
                rows={1}
                autoFocus={showDesc && !description}
                onChange={(e) => {
                  changeDescription(e.target.value);
                  e.target.style.height = 'auto';
                  e.target.style.height = e.target.scrollHeight + 'px';
                }}
                onBlur={(e) => {
                  if (!e.target.value.trim()) setShowDesc(false);
                }}
                ref={(el) => {
                  if (el) {
                    el.style.height = 'auto';
                    el.style.height = el.scrollHeight + 'px';
                  }
                }}
              />
            ) : (
              <div>{description}</div>
            )}
          </div>
        )}
        {(canEdit || tags.length > 0) && (
          <div className="page-tags">
            {tags.map((t) => (
              <TagChip
                key={t}
                tag={t}
                colors={tagColors}
                canEdit={canEdit}
                onRemove={() => removeTag(t)}
                onSetColor={(c) => onSetTagColor(t, c)}
              />
            ))}
            {canEdit && (
              <span className="tag-input-wrap">
                <input
                  className="page-tag-input"
                  value={tagDraft}
                  placeholder={tags.length ? t('+ Tag') : t('+ Add a tag')}
                  onChange={(e) => {
                    setTagDraft(e.target.value);
                    setTagSuggestOpen(true);
                    setTagSel(0);
                  }}
                  onFocus={() => setTagSuggestOpen(true)}
                  onKeyDown={(e) => {
                    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                      if (!tagHits.length) return;
                      e.preventDefault();
                      const d = e.key === 'ArrowDown' ? 1 : -1;
                      setTagSel((i) => (i + d + tagHits.length) % tagHits.length);
                    } else if (e.key === 'Enter' || e.key === ',' || e.key === 'Tab') {
                      // A highlighted suggestion beats the raw text — otherwise
                      // Enter creates a duplicate after all.
                      const pick = tagSuggestOpen ? tagHits[tagSel] : undefined;
                      if (!pick && !tagDraft.trim()) return; // Tab darf normal weiterspringen
                      e.preventDefault();
                      addTag(pick?.tag);
                    } else if (e.key === 'Escape') {
                      setTagSuggestOpen(false);
                    } else if (e.key === 'Backspace' && !tagDraft && tags.length) {
                      removeTag(tags[tags.length - 1]);
                    }
                  }}
                  onBlur={() => {
                    // Clicks on a suggestion only fire after the blur.
                    setTimeout(() => {
                      setTagSuggestOpen(false);
                      addTag();
                    }, 120);
                  }}
                />
                {tagSuggestOpen && tagHits.length > 0 && (
                  <div className="tag-suggest">
                    {tagHits.map((s, i) => (
                      <button
                        key={s.tag}
                        type="button"
                        className={'tag-suggest-item' + (i === tagSel ? ' on' : '')}
                        onMouseDown={(e) => e.preventDefault()} // Blur zuvorkommen
                        onClick={() => addTag(s.tag)}
                      >
                        <span className={'tag-chip ' + tagColorClass(s.tag, tagColors)}>#{s.tag}</span>
                        <span className="tag-suggest-count">{s.count}</span>
                        {s.similar && <span className="tag-suggest-hint">{t('similar')}</span>}
                      </button>
                    ))}
                  </div>
                )}
              </span>
            )}
          </div>
        )}
        {page.parentId && pagesById.get(page.parentId)?.type === 'collection' && (
          <RowProperties
            pageId={pageId}
            parentId={page.parentId}
            initialProps={page.props}
            canEdit={canEdit}
          />
        )}
      </div>
      {children}
      {/* The raw trail stays at the foot of the document. Comments moved to a
          panel beside it (CommentsPanel) because people WORK in them; the trail
          is read, rarely, when somebody doubts the written-up version — and
          under the account it supports is exactly where it belongs.

          Only for documents and rows: a database page carries its table down
          here, and anything below that is lost. */}
      {page.type !== 'collection' && (
        <div className="trail-wrap">
          <NoteTrail pageId={pageId} canWrite={canEdit} />
        </div>
      )}
      </div>
    </>
  );
}

function CoverMenu({
  onGradient,
  onUpload,
  onClose,
}: {
  onGradient: (value: string) => void;
  onUpload: () => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      const t = e.target as Element;
      // Ignore this menu's own triggers (they toggle it); a click on the icon
      // trigger correctly falls through and closes the cover menu.
      if (t.closest?.('.cover-trigger, .cover-btn')) return;
      if (ref.current && !ref.current.contains(t)) onClose();
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [onClose]);
  return (
    <div className="cover-menu" ref={ref}>
      <button className="cover-upload" onClick={onUpload}>
        <Upload size={15} /> {t('Upload image')}
      </button>
      <div className="cover-grid">
        {COVER_GRADIENTS.map((g) => (
          <button
            key={g}
            className="cover-swatch"
            style={{ background: g.slice('gradient:'.length) }}
            onClick={() => onGradient(g)}
          />
        ))}
      </div>
    </div>
  );
}

// ---- realtime block editor ----

interface CollabProps {
  page: Page;
  user: User;
  theme: 'light' | 'dark';
  canEdit: boolean;
  pagesById: Map<string, PageMeta>;
  tagColors: Record<string, string>;
  onNavigate: (id: string | null) => void;
  onCreatePage: (parentId: string | null, type?: 'doc' | 'collection') => void;
  onPagesChanged: () => void;
  onReset: () => void;
  /** The panel lists linked references too; the strip under the body stands in
   *  while it is closed, so the information is never in two places at once and
   *  never in none. */
  structureOpen: boolean;
}

function CollabEditor({ page, user, theme, canEdit, onReset, ...rest }: CollabProps) {
  const [ready, setReady] = useState(false);
  const [provider, setProvider] = useState<SaltProvider | null>(null);
  const seedRef = useRef<unknown[] | null>(null);

  // The provider lives in the effect, not in the render. The earlier useMemo
  // approach broke twice over under StrictMode: the double render leaked one
  // ghost connection along with its presence (useRef is fresh on the second
  // pass, so the guard never saw anything), and the setup→cleanup→setup cycle
  // destroyed the committed provider for good — dev hung on a dead Y.Doc.
  // Here, setup→cleanup→setup simply produces a second provider.
  useEffect(() => {
    const p = new SaltProvider(
      page.id,
      (isNew) => {
        // Seed a brand-new CRDT doc from the page's stored content once.
        if (isNew && Array.isArray(page.content) && page.content.length > 0) {
          seedRef.current = page.content;
        }
        setReady(true);
      },
      onReset,
    );

    // Presence means "is looking at this page right now": a tab in a hidden
    // window must not show up as a peer for hours. Doc sync stays connected;
    // only the awareness state is withdrawn. IMPORTANT: setLocalState, not
    // setLocalStateField — after setLocalState(null) the latter is a silent
    // no-op (y-protocols checks state !== null), and presence would never come
    // back after switching tabs.
    const applyPresence = () => {
      if (document.visibilityState === 'hidden') {
        p.awareness.setLocalState(null);
      } else {
        p.awareness.setLocalState({
          user: { name: user.name, color: user.color, avatar: user.avatar },
        });
      }
    };
    applyPresence(); // deckt auch den nie fokussierten Hintergrund-Tab ab (startet hidden)

    // Awareness has long known about the others — you just never saw them.
    // Here they are written into the presence store the header reads.
    const pushPeers = () => {
      const mine = p.awareness.clientID;
      const out: { name: string; color: string; avatar?: string }[] = [];
      p.awareness.getStates().forEach((state: Record<string, unknown>, id: number) => {
        if (id === mine) return;
        const u = state.user as { name?: string; color?: string; avatar?: string } | undefined;
        if (u?.name) out.push({ name: u.name, color: u.color || '#888', avatar: u.avatar });
      });
      setPeers(page.id, out);
    };
    p.awareness.on('change', pushPeers);
    pushPeers();

    document.addEventListener('visibilitychange', applyPresence);
    setProvider(p);
    return () => {
      document.removeEventListener('visibilitychange', applyPresence);
      clearPeers(page.id);
      setReady(false); // the next provider (StrictMode) gates itself again
      p.destroy();
    };
    // page.id is constant over the lifetime (key={currentId} remounts)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page.id]);

  if (!provider || !ready) return <div className="editor-loading" />;
  return (
    <BlockContent
      provider={provider}
      pageId={page.id}
      seed={seedRef.current}
      hadContent={Array.isArray(page.content) && page.content.length > 0}
      user={user}
      theme={theme}
      canEdit={canEdit}
      {...rest}
    />
  );
}

// A BlockNote document counts as "empty" when it's a single empty paragraph.
function isEffectivelyEmpty(doc: unknown[]): boolean {
  if (!Array.isArray(doc) || doc.length === 0) return true;
  if (doc.length > 1) return false;
  const b = doc[0] as { type?: string; content?: unknown[] };
  return b?.type === 'paragraph' && (!b.content || b.content.length === 0);
}

function BlockContent({
  provider,
  pageId,
  seed,
  hadContent,
  user,
  theme,
  canEdit,
  pagesById,
  tagColors,
  onNavigate,
  onCreatePage,
  onPagesChanged,
  structureOpen,
}: {
  provider: SaltProvider;
  pageId: string;
  seed: unknown[] | null;
  hadContent: boolean;
  structureOpen: boolean;
  user: User;
  theme: 'light' | 'dark';
  canEdit: boolean;
  pagesById: Map<string, PageMeta>;
  tagColors: Record<string, string>;
  onNavigate: (id: string | null) => void;
  onCreatePage: (parentId: string | null, type?: 'doc' | 'collection') => void;
  onPagesChanged: () => void;
}) {
  const editor = useCreateBlockNote({
    schema: saltSchema,
    collaboration: {
      provider,
      fragment: provider.doc.getXmlFragment('blocknote'),
      user: { name: user.name, color: user.color },
    },
    // The page travels with the upload: it is what makes a dropped PDF
    // searchable (indexFileText) and what puts the file in the workspace's
    // file list. Without it a PDF added in the browser stayed invisible to
    // both, while the same PDF added by an agent did not — the MCP path has
    // always passed a page id.
    uploadFile: (file: File) => api.upload(file, pageId),
    // Column layout: edge-drop cursor + its dictionary entries.
    dictionary: coreEn,
  });

  const [preview, setPreview] = useState<{ name: string; url: string } | null>(null);

  // Slash menu: default items + column layout + our custom blocks.
  const getSlashItems = async (query: string) => {
    const custom = [
      {
        title: t('Columns'),
        subtext: t('Two blocks side by side'),
        aliases: ['columns', 'spalten', 'nebeneinander'], // i18n-ok: search aliases, deliberately multi
        group: 'Basic blocks',
        icon: <Columns2 size={18} />,
        // Inserted WITH its two children, because an empty columns block is a
        // control strip and nothing else — there would be no column to type in
        // and no obvious way to make one.
        onItemClick: () =>
          insertOrUpdateBlockForSlashMenu(editor, {
            type: 'columns',
            props: { count: 2 },
            children: [{ type: 'paragraph' }, { type: 'paragraph' }],
          } as never),
      },
      {
        title: t('Callout'),
        subtext: t('A highlighted note with an emoji'),
        aliases: ['callout', 'hinweis', 'info', 'warnung'], // i18n-ok: search aliases, deliberately multilingual so a German user can type it
        group: 'Basic blocks',
        icon: <span>💡</span>,
        onItemClick: () =>
          insertOrUpdateBlockForSlashMenu(editor, { type: 'callout' } as never),
      },
      {
        title: t('Bookmark / Embed'),
        subtext: t('A link card, or a YouTube/Vimeo player'),
        aliases: ['bookmark', 'embed', 'link', 'video', 'youtube'], // i18n-ok: search aliases, deliberately multilingual so a German user can type it
        group: 'Media',
        icon: <span>🔖</span>,
        onItemClick: () =>
          insertOrUpdateBlockForSlashMenu(editor, { type: 'bookmark' } as never),
      },
      {
        title: t('Embed a collection'),
        subtext: t('Show an existing collection inside the document'),
        aliases: ['datenbank', 'database', 'db', 'tabelle', 'board', 'kanban'], // i18n-ok: search aliases, deliberately multilingual so a German user can type it
        group: 'Basic blocks',
        icon: <span>▦</span>,
        onItemClick: () =>
          insertOrUpdateBlockForSlashMenu(editor, { type: 'database' } as never),
      },
      {
        title: t('Table of contents'),
        subtext: t('Auto-generated list of every heading'),
        aliases: ['toc', 'inhalt', 'inhaltsverzeichnis', 'outline'], // i18n-ok: search aliases, deliberately multilingual so a German user can type it
        group: 'Basic blocks',
        icon: <span>📑</span>,
        onItemClick: () => insertOrUpdateBlockForSlashMenu(editor, { type: 'toc' } as never),
      },
    ];
    return filterSuggestionItems(
      [
        ...getDefaultReactSlashMenuItems(editor),
        ...custom,
      ],
      query,
    );
  };

  // Page-link menu (shared by the "@" mention trigger and the "[[" wiki-link
  // trigger): existing pages plus a "create new page" action.
  const buildLinkItems = async (query: string) => {
    const q = query.toLowerCase();
    const matches = [...pagesById.values()]
      .filter((p) => !p.trashed && p.id !== provider.pageId)
      .filter((p) => (p.title || 'Untitled').toLowerCase().includes(q))
      .slice(0, 12);
    const items = matches.map((p) => ({
      title: p.title || 'Untitled',
      subtext: p.type === 'collection' ? 'Database' : 'Page',
      onItemClick: () =>
        editor.insertInlineContent([
          { type: 'pageLink', props: { pageId: p.id, label: p.title || 'Untitled' } },
          ' ',
        ]),
    }));
    if (query.trim()) {
      items.push({
        title: `Create "${query.trim()}"`,
        subtext: 'New page',
        onItemClick: async () => {
          try {
            const created = await api.createPage(null, query.trim());
            onPagesChanged();
            editor.insertInlineContent([
              { type: 'pageLink', props: { pageId: created.id, label: created.title || query.trim() } },
              ' ',
            ]);
          } catch (e) {
            console.error('salt.md: failed to create page from mention', e);
          }
        },
      });
    }
    return items;
  };

  const getMentionItems = (query: string) => buildLinkItems(query);
  // Wiki-links: the trigger is "[" (BlockNote uses single-char triggers), so the
  // text after it starts with a second "[" when the user types "[[". We strip
  // stray brackets ("[[Page]]") before matching.
  const getWikiItems = (raw: string) => buildLinkItems(raw.replace(/^\[+/, '').replace(/\]+$/, ''));

  // Seed initial content into an empty shared doc exactly once. If seeding
  // throws (e.g. a block shape BlockNote rejects), we must NOT enter the
  // materialize path, or the debounced write would persist an empty document
  // over the real stored content.
  const seededRef = useRef(false);
  const seedFailed = useRef(false);
  useEffect(() => {
    if (seededRef.current || !seed || seed.length === 0) return;
    seededRef.current = true;
    const frag = provider.doc.getXmlFragment('blocknote');
    if (frag.length === 0) {
      try {
        editor.replaceBlocks(editor.document, seed as never);
      } catch (e) {
        seedFailed.current = true;
        console.error('salt.md: failed to seed page content', e);
      }
    }
  }, [editor, provider, seed]);

  // Persist a materialized copy to pages.content so search, export, backlinks
  // and the Markdown/MCP API see current text. Debounced; skips the CRDT reset.
  const matTimer = useRef<number | undefined>(undefined);
  const dirty = useRef(false);
  useEffect(() => {
    const flush = (keepalive: boolean) => {
      if (seedFailed.current || !dirty.current) return;
      const doc = editor.document;
      // Guard against clobbering stored content with an accidentally-empty doc.
      if (hadContent && isEffectivelyEmpty(doc)) return;
      dirty.current = false;
      if (keepalive) {
        // Unmount (e.g. following a link chip): a normal fetch would be
        // cancelled, so use keepalive to guarantee the write — otherwise the
        // just-inserted @-mention's backlink would never be recorded.
        void fetch(`/api/pages/${provider.pageId}?materialize=1`, {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: doc }),
          keepalive: true,
        });
      } else {
        api.updatePage(provider.pageId, { content: doc }, { materialize: true }).catch(() => {
          dirty.current = true; // retry on next change / unmount flush
          toast(t('Page content not saved'));
        });
      }
    };
    const persist = () => {
      dirty.current = true;
      window.clearTimeout(matTimer.current);
      matTimer.current = window.setTimeout(() => flush(false), 1500);
    };
    const unsub = editor.onChange?.(persist);
    return () => {
      window.clearTimeout(matTimer.current);
      flush(true); // flush any pending edit before this editor unmounts
      if (typeof unsub === 'function') unsub();
    };
  }, [editor, provider, hadContent]);

  // A file block otherwise goes straight to the download folder — you cannot
  // glance at an attachment without leaving Salt and coming back. Catching the
  // click in the CAPTURE phase is what makes this work without touching
  // BlockNote's own file block: React registers capture listeners on its root
  // container, so this runs before the event ever reaches the block's own
  // handler, and stopPropagation keeps the download from firing underneath the
  // viewer. Anything we do not preview is simply left alone.
  const onFileClick = (e: React.MouseEvent) => {
    const target = e.target as HTMLElement;
    if (!target.closest('.bn-file-name-with-icon')) return;
    const id = target.closest('[data-id]')?.getAttribute('data-id');
    if (!id) return;
    const props = editor.getBlock(id)?.props as { url?: string; name?: string } | undefined;
    const url = props?.url;
    if (!url || !isPreviewable(url)) return;
    e.preventDefault();
    e.stopPropagation();
    setPreview({ name: props.name || url.split('/').pop() || url, url });
  };

  // Dropping a file from outside the browser. BlockNote handles a drop that
  // lands on the text; this covers the rest of the page, which is most of the
  // area somebody aims at — and where the browser's default is to navigate to
  // the file and throw the application away. See dropFiles.ts.
  const [dropping, setDropping] = useState(false);
  const dragDepth = useRef(0);
  const scrollRef = useRef<HTMLDivElement>(null);

  const onDragEnter = (e: React.DragEvent) => {
    if (!canEdit || !carriesExternalFiles(e.dataTransfer)) return;
    // dragenter/dragleave fire for every child element the pointer crosses, so
    // a plain boolean flickers the overlay away over every block. Counting
    // enter against leave is the standard fix and the only one that survives
    // nested content.
    dragDepth.current += 1;
    setDropping(true);
  };
  const onDragLeave = (e: React.DragEvent) => {
    if (!canEdit || !carriesExternalFiles(e.dataTransfer)) return;
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) setDropping(false);
  };
  const onDragOver = (e: React.DragEvent) => {
    if (!canEdit || !carriesExternalFiles(e.dataTransfer)) return;
    // Without this the drop event never fires at all — the browser only asks
    // "may I drop here?" once per dragover, and silence means no.
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
  };
  const onDrop = async (e: React.DragEvent) => {
    if (!canEdit || !carriesExternalFiles(e.dataTransfer)) return;
    dragDepth.current = 0;
    setDropping(false);
    // A drop that landed on the text belongs to BlockNote: it knows WHERE
    // between the blocks the pointer was, which is the whole reason to drop
    // there rather than at the end. Doing it here as well would insert twice.
    if ((e.target as Element)?.closest?.('.bn-editor')) return;
    e.preventDefault();
    const files = Array.from(e.dataTransfer.files || []);
    if (!files.length) return;
    // One at a time on purpose. Uploads are size-capped and the server sizes
    // PDF extraction to the memory it believes it has; ten at once from a
    // folder drag is the shape that took an instance down once already.
    //
    // A failure does not stop the rest — dropping five and getting four plus a
    // named failure beats getting nothing.
    let added = 0;
    for (const file of files) {
      try {
        const url = await api.upload(file, pageId);
        // Appended at the end: a drop outside the text names no position.
        editor.insertBlocks(
          [{ type: blockTypeFor(file), props: { url, name: file.name } } as never],
          editor.document[editor.document.length - 1],
          'after',
        );
        added++;
      } catch (err) {
        toast((err as Error).message || t('“{name}” was not uploaded', { name: file.name }));
      }
    }
    if (added > 1) toast(plural(added, '{n} file added', '{n} files added'));
  };

  const handlers = useRef({ enter: onDragEnter, leave: onDragLeave, over: onDragOver, drop: onDrop });
  handlers.current = { enter: onDragEnter, leave: onDragLeave, over: onDragOver, drop: onDrop };

  // Registered on the scroller that holds cover, title, tags AND content, not
  // just on the text area — dropping onto the title is a perfectly ordinary aim
  // and it is a strip this component does not render. Native listeners rather
  // than JSX props because that element belongs to PageHeader; the listeners
  // live exactly as long as a DOCUMENT editor is mounted, so a collection page
  // (whose body is a table) never grows a drop zone that would mean nothing.
  useEffect(() => {
    const zone = scrollRef.current?.closest('.page-body');
    if (!zone) return;
    const enter = (e: Event) => handlers.current.enter(e as unknown as React.DragEvent);
    const leave = (e: Event) => handlers.current.leave(e as unknown as React.DragEvent);
    const over = (e: Event) => handlers.current.over(e as unknown as React.DragEvent);
    const drop = (e: Event) => void handlers.current.drop(e as unknown as React.DragEvent);
    zone.addEventListener('dragenter', enter);
    zone.addEventListener('dragleave', leave);
    zone.addEventListener('dragover', over);
    zone.addEventListener('drop', drop);
    return () => {
      zone.removeEventListener('dragenter', enter);
      zone.removeEventListener('dragleave', leave);
      zone.removeEventListener('dragover', over);
      zone.removeEventListener('drop', drop);
      zone.classList.remove('is-dropping');
    };
    // Registered ONCE, reading the current handlers out of a ref. Re-binding on
    // every render would swap the pair out MID-DRAG — between the dragenter
    // that shows the overlay and the dragover that accepts the drop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // The outline sits on an element this component does not render, so it is set
  // rather than expressed in JSX.
  useEffect(() => {
    scrollRef.current?.closest('.page-body')?.classList.toggle('is-dropping', dropping);
  }, [dropping]);

  return (
    <div className="editor-scroll" ref={scrollRef}>
      {dropping && (
        <div className="drop-hint" aria-hidden>
          <FilePlus2 size={18} />
          {t('Drop to add to this page')}
        </div>
      )}
      <div className="editor-inner" onClickCapture={onFileClick}>
        {/* The database block renders inside the editor and would otherwise
            not reach the page list, the tag colours or navigation. */}
        <BlockContext.Provider value={{ pagesById, tagColors, onNavigate, onPagesChanged }}>
        <BlockNoteView editor={editor} theme={theme} editable={canEdit} slashMenu={false}>
          <SuggestionMenuController triggerCharacter="/" getItems={getSlashItems} />
          <SuggestionMenuController
            triggerCharacter="@"
            getItems={getMentionItems}
          />
          <SuggestionMenuController
            triggerCharacter="["
            getItems={getWikiItems}
          />
        </BlockNoteView>
        </BlockContext.Provider>
        {!structureOpen && (
          <Backlinks pageId={provider.pageId} pagesById={pagesById} onNavigate={onNavigate} />
        )}
      </div>
      {preview && (
        <FilePreview name={preview.name} url={preview.url} onClose={() => setPreview(null)} />
      )}
    </div>
  );
}

// "Linked references" — other pages that @-mention this one.
function Backlinks({
  pageId,
  pagesById,
  onNavigate,
}: {
  pageId: string;
  pagesById: Map<string, PageMeta>;
  onNavigate: (id: string | null) => void;
}) {
  const [links, setLinks] = useState<Backlink[]>([]);
  // Refetch when the page changes or the page list updates — App rebuilds the
  // pagesById map on every reload, so depending on its identity catches new
  // links from tree changes (size-only deps would miss same-count updates).
  useEffect(() => {
    let alive = true;
    api
      .backlinks(pageId)
      .then((l) => alive && setLinks(l))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [pageId, pagesById]);

  if (links.length === 0) return null;
  return (
    <div className="backlinks">
      {/* Same glyphs as the structure panel: this strip and the panel's
          "Linked from" show the same thing in two places, so they must not
          look like two different features. */}
      <div className="backlinks-head">
        <Link2 size={14} /> {t('Linked from')} · {links.length}
      </div>
      {links.map((l) => (
        <button key={l.id} className="backlink-item" onClick={() => onNavigate(l.id)}>
          <span className="tree-icon"><PageIcon icon={l.icon} size={14} fallback={<FileText size={14} />} /></span>
          {l.title || 'Untitled'}
        </button>
      ))}
    </div>
  );
}
