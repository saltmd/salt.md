import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import { onRefresh } from '../pwa';
import Portal from './Portal';
import AnchoredMenu from './AnchoredMenu';
import { useMenuDismiss } from '../modal';
import { useBoardDrag } from '../boardDrag';
import { tagColorClass } from '../tags';
import { compare, firstWeekday, formatDay, formatMonth, formatMonthYear, toDayString, weekdayNames } from '../format';
import { plural, t } from '../i18n';
import { confirm, promptText } from '../dialog';
import type {
  CollectionConfig,
  Filter,
  FilterOp,
  Page,
  PageMeta,
  PropDef,
  PropOption,
  PropType,
  Sort,
  SpanMode,
  ViewDef,
} from '../types';
import PropertyValue, { PersonStack, idList, loadRelationOptions, type RelOption } from './PropertyValue';
import { openPage, openPageAux } from '../pageOpen';
import { AgentDot } from './AgentBadge';
import { planCard, isBlank, zoneOf, contactKind, needsLabel } from '../cardLayout';
import SchemaEditor from './SchemaEditor';
import GalleryView from './GalleryView';
import ListView from './ListView';
import { PageIcon } from '../pageIcon';
import { toast } from '../toast';
import {
  Table2,
  Columns3,
  LayoutGrid,
  CalendarDays,
  List,
  Settings2,
  Plus,
  Filter as FilterIcon,
  ArrowUpDown,
  Eye,
  EyeOff,
  AlignLeft,
  Calendar,
  Hash,
  CircleDot,
  Tags,
  SquareCheck,
  User,
  Link2,
  Sigma,
  ClipboardList,
  Check,
  Send,
  Share2,
  Globe,
  GanttChartSquare,
  MessageSquare,
  Mail,
  Phone,
  MapPin,
  CornerUpRight,
  MoreHorizontal,
  Pencil,
  Trash2,
  SquareArrowOutUpRight,
  ExternalLink,
  ArrowLeft,
  ArrowRight, ChevronDown, History, PanelLeftOpen, PanelLeftClose} from 'lucide-react';

// Small type glyph shown next to each property (Notion-style visibility panel).
function propTypeIcon(t: PropDef['type']) {
  const s = 14;
  switch (t) {
    case 'select':
      return <CircleDot size={s} />;
    case 'multiselect':
      return <Tags size={s} />;
    case 'date':
      return <Calendar size={s} />;
    case 'number':
      return <Hash size={s} />;
    case 'checkbox':
      return <SquareCheck size={s} />;
    case 'person':
      return <User size={s} />;
    case 'relation':
      return <Link2 size={s} />;
    case 'rollup':
    case 'formula':
      return <Sigma size={s} />;
    case 'lastActivity':
      return <History size={s} />;
    default:
      return <AlignLeft size={s} />;
  }
}

/** A collection shown inside a document rather than as its own page.
 *
 *  The rows stay the collection's: an edit in an embed is an edit everywhere.
 *  What an embed keeps to itself is how it LOOKS at them — which view, and the
 *  filters and sort it applies there — so filtering the Tasks embedded in
 *  Project A to Project A no longer filters Tasks for everybody, everywhere
 *  (#26). A view without an entry shows what the collection's view says, which
 *  is every embed written before this existed. */
export interface EmbedState {
  viewId: string;
  views: Record<string, { filters?: Filter[]; sort?: ViewDef['sort'] }>;
  onViewId: (viewId: string) => void;
  onView: (viewId: string, patch: { filters?: Filter[]; sort?: ViewDef['sort'] }) => void;
}

interface Props {
  collectionId: string;
  pages: Map<string, PageMeta>;
  tagColors: Record<string, string>;
  onNavigate: (id: string) => void;
  /** ⌘/Ctrl-click and middle-click on a row open a second tab; a plain click
   *  navigates the one you are in (see pageOpen). */
  onOpenInNewTab?: (id: string) => void;
  onPagesChanged: () => void;
  embed?: EmbedState;
}

interface Row {
  id: string;
  title: string;
  icon: string;
  cover: string;
  props: Record<string, unknown>;
  position: number;
  tags?: string[];
}

const UNSET = '__unset__';

// The value to store for a group option, respecting the property's type.
// A relation holds an array of ids, same shape as a multiselect — dragging a
// card into the "salt.md" column therefore means "this task belongs to salt.md".
function groupValueFor(schema: PropDef[], propId: string, optId: string): unknown {
  const prop = schema.find((p) => p.id === propId);
  return prop?.type === 'multiselect' || prop?.type === 'relation' ? [optId] : optId;
}

function isEmptyVal(v: unknown): boolean {
  if (Array.isArray(v)) return v.length === 0;
  return v === undefined || v === '' || v === null || v === false;
}

/** What a condition compares against: the set if there is one, else the single
 *  value. Never both — the same rule the server applies (rowFilter.vals). */
export function filterValues(f: Filter): string[] {
  if (f.values?.length) return f.values;
  return f.value === '' ? [] : [f.value];
}

/** Is this condition finished enough to mean anything? An unfinished one must
 *  not filter: comparing against the empty string emptied the table the moment
 *  somebody added "Date is …", before they had typed a thing, which is what
 *  made the date filter look broken. `is_empty` is the deliberate version. */
export function filterIsArmed(f: Filter): boolean {
  const op = f.op ?? (f.value === '' ? 'is_not_empty' : 'is');
  if (op === 'is_empty' || op === 'is_not_empty') return true;
  if (op === 'between') return f.value !== '' && (f.value2 ?? '') !== '';
  return filterValues(f).length > 0;
}

function matchesFilter(row: Row, f: Filter): boolean {
  const v = row.props[f.property];
  const op = f.op ?? (f.value === '' ? 'is_not_empty' : 'is');
  if (!filterIsArmed(f)) return true;
  const vals = filterValues(f);
  // One value or several, the question is the same: does the cell hold any of
  // them. A cell may itself be a list (multiselect, relation).
  const holdsAny = () =>
    Array.isArray(v)
      ? vals.some((x) => v.includes(x))
      : vals.some((x) =>
          x === 'true' || x === 'false' ? String(v === true) === x : String(v ?? '') === x,
        );
  switch (op) {
    case 'is_empty':
      return isEmptyVal(v);
    case 'is_not_empty':
      return !isEmptyVal(v);
    case 'contains': {
      const needle = f.value.toLowerCase();
      if (Array.isArray(v)) return v.some((x) => String(x).toLowerCase().includes(needle));
      return String(v ?? '').toLowerCase().includes(needle);
    }
    case 'gt':
    case 'lt': {
      const nv = Number(v);
      const nf = Number(f.value);
      const cmp = !Number.isNaN(nv) && !Number.isNaN(nf) ? nv - nf : compare(String(v ?? ''), f.value);
      return op === 'gt' ? cmp > 0 : cmp < 0;
    }
    case 'between': {
      // Inclusive at both ends — a range named by two dates contains them.
      // ISO dates compare correctly as text, so only numbers need Number().
      const hi = f.value2 ?? '';
      const nv = Number(v);
      if (!Number.isNaN(nv) && !Number.isNaN(Number(f.value)) && !Number.isNaN(Number(hi))) {
        return nv >= Number(f.value) && nv <= Number(hi);
      }
      const sv = String(v ?? '');
      return sv !== '' && sv >= f.value && sv <= hi;
    }
    case 'is_not':
      return !holdsAny();
    case 'is':
    default:
      // Checkbox filters compare against a boolean; a never-toggled box has no
      // stored value, which must count as "false" (unchecked), not "no match".
      return holdsAny();
  }
}

// Apply a view's filters and sort to the row set.
function applyView(rows: Row[], view: ViewDef): Row[] {
  let out = rows;
  for (const f of view.filters ?? []) {
    out = out.filter((r) => matchesFilter(r, f));
  }
  const sort = view.sort;
  if (sort) {
    out = [...out].sort((a, b) => {
      const av = a.props[sort.property];
      const bv = b.props[sort.property];
      const as = Array.isArray(av) ? av.join(',') : String(av ?? '');
      const bs = Array.isArray(bv) ? bv.join(',') : String(bv ?? '');
      const cmp = typeof av === 'number' && typeof bv === 'number' ? av - bv : compare(as, bs);
      return sort.dir === 'desc' ? -cmp : cmp;
    });
  }
  return out;
}

export default function CollectionView({ collectionId, pages, tagColors, onNavigate, onOpenInNewTab, onPagesChanged, embed }: Props) {
  const [config, setConfig] = useState<CollectionConfig | null>(null);
  const [rows, setRows] = useState<Row[]>([]);
  const [viewId, setViewId] = useState<string>(embed?.viewId ?? '');
  const pickView = (id: string) => {
    setViewId(id);
    embed?.onViewId(id);
  };
  const [schemaOpen, setSchemaOpen] = useState(false);

  // Open comments per row. Trello shows on every card whether anything was
  // discussed there — on a sales board the course of the conversation sits
  // exactly there and not in the description. In ONE query for the whole
  // workspace, not per card (see handleCommentCounts).
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({});
  const workspaceId = pages.get(collectionId)?.workspaceId ?? '';
  useEffect(() => {
    if (!workspaceId) return;
    let alive = true;
    api
      .commentCounts(workspaceId)
      .then((c) => alive && setCommentCounts(c))
      .catch(() => {});
    return () => {
      alive = false;
    };
    // Deliberately hangs off workspaceId alone: `pages` gets a new identity on
    // every change and would set off an endless loop here.
  }, [workspaceId]);

  const loadConfig = useCallback(() => {
    void api.getCollection(collectionId).then((c) => {
      setConfig(c);
      setViewId((v) => v || c.views[0]?.id || '');
    });
  }, [collectionId]);

  const [total, setTotal] = useState(0);
  const [addViewOpen, setAddViewOpen] = useState(false);
  const addViewBtnRef = useRef<HTMLButtonElement>(null);
  const [viewMenuFor, setViewMenuFor] = useState<string | null>(null);
  // A view created right now opens its own settings: a timeline with no start
  // property picked yet is an empty grid until one is chosen, so the menu that
  // picks it opens itself. One shot — the flag clears the first time it shows.
  const [settingsFor, setSettingsFor] = useState<string | null>(null);
  const [addViewPos, setAddViewPos] = useState<React.CSSProperties | null>(null);
  // A right-click on a view's tab opens the same actions the ⋯ holds, for THAT
  // view, at the pointer (#24). The ⋯ only ever acted on the view you are on.
  const [tabMenu, setTabMenu] = useState<{ view: ViewDef; style: React.CSSProperties } | null>(null);
  useLayoutEffect(() => {
    if (!addViewOpen || !addViewBtnRef.current) {
      setAddViewPos(null);
      return;
    }
    const r = addViewBtnRef.current.getBoundingClientRect();
    setAddViewPos({
      position: 'fixed',
      left: Math.max(8, Math.min(r.left, window.innerWidth - 188)),
      top: r.bottom + 4,
      zIndex: 320,
    });
  }, [addViewOpen]);
  const PAGE = 200;

  // Rows are fetched from the server (filtered/paginated there), NOT from the
  // global page list — a database can have tens of thousands of rows that must
  // not choke the sidebar tree load.
  const savedView = config?.views.find((v) => v.id === viewId) ?? config?.views[0];
  // In a document, the embed's own filters and sort stand in for the view's.
  const own = savedView && embed?.views[savedView.id];
  const view0 = savedView && own ? { ...savedView, ...own } : savedView;
  // Server-side filter/sort (real Q25): the view's filters/sort become query
  // params so a 50k-row database is filtered in SQLite, not in the browser.
  // Only finished conditions travel. An unfinished one is not "match nothing",
  // it is "not asked yet".
  const serverFilters = (view0?.filters ?? [])
    .filter(filterIsArmed)
    .map((f) => ({ property: f.property, op: f.op, value: f.value, values: f.values, value2: f.value2 }));
  const serverSort = view0?.sort ?? null;
  const fsKey = JSON.stringify([serverFilters, serverSort]);

  // Loads ALL rows, in steps of 200 one after another. It used to stop at the
  // first page plus a "Load more" button — with the result that a board of 656
  // cards showed "Lost / Refunded 0" while 395 sat there: the column was not
  // empty, it was blind. The steps still protect the server (and draw something
  // on screen early); the epoch discards stale answers if the filter or the
  // database changes meanwhile.
  const epochRef = useRef(0);
  const loadRows = useCallback(async () => {
    const epoch = ++epochRef.current;
    let acc: Row[] = [];
    for (;;) {
      const res = await api.collectionRows(collectionId, {
        limit: PAGE,
        offset: acc.length,
        filters: serverFilters,
        sort: serverSort,
      });
      if (epoch !== epochRef.current) return; // inzwischen neu geladen
      const mapped: Row[] = res.rows.map((r) => ({
        id: r.id,
        title: r.title,
        icon: r.icon,
        cover: r.cover || '',
        props: r.props || {},
        position: r.position,
        tags: r.tags ?? [],
      }));
      acc = [...acc, ...mapped];
      setTotal(res.total);
      setRows(acc);
      if (acc.length >= res.total || mapped.length === 0) return;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collectionId, fsKey]);

  // Loads ALL rows — but NOT on every change anywhere in the workspace. This
  // effect used to hang off `pages`, and `pages` gets a new identity on every
  // SSE event: a database with 50k rows then crawled itself completely again
  // after somebody else renamed anything. Now only when the database, the
  // filter or the sorting changes.
  useEffect(() => {
    void loadRows();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collectionId, fsKey]);

  // Somebody else — a person in another browser, or an agent — moved a row in
  // THIS database. Reload it, so a card that gets pushed to "next" moves while
  // you are looking at it instead of on your next refresh.
  //
  // Only for this database, and debounced: an agent writing a batch of rows
  // sends one event per row, and each full reload walks every page of results.
  useEffect(() => {
    let timer: number | undefined;
    const onRows = (e: Event) => {
      if ((e as CustomEvent<string>).detail !== collectionId) return;
      window.clearTimeout(timer);
      timer = window.setTimeout(() => void loadRows(), 400);
    };
    window.addEventListener('salt:rows', onRows);
    // Somebody pulled down, or pressed sync. Not debounced and not conditional:
    // they asked for THIS table, now.
    const stopRefresh = onRefresh(() => {
      void loadRows();
      loadConfig();
    });
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('salt:rows', onRows);
      stopRefresh();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collectionId, fsKey]);

  useEffect(loadConfig, [loadConfig]);

  const view = view0;
  const schema = config?.schema ?? [];

  const saveConfig = async (next: CollectionConfig) => {
    setConfig(next);
    await api.putCollection(collectionId, next);
  };

  const addRow = async (presetProps?: Record<string, unknown>) => {
    // Deliberately does NOT onNavigate() into the new row: creating an item
    // from a collection view should keep you looking at the collection, same
    // as Notion. Opening it is the same click as any other existing row.
    await api.createPage(collectionId, 'Untitled', 'doc', presetProps);
    onPagesChanged();
  };

  // Persist an inline change to a select/multiselect property's options (create,
  // recolour, delete) — the cell editor edits the schema, not just the value.
  const setPropOptions = (propId: string, options: PropOption[]) => {
    if (!config) return;
    void saveConfig({
      ...config,
      schema: config.schema.map((p) => (p.id === propId ? { ...p, options } : p)),
    });
  };

  const setRowProp = async (rowId: string, propId: string, value: unknown) => {
    setRows((prev) =>
      prev.map((r) => (r.id === rowId ? { ...r, props: { ...r.props, [propId]: value } } : r)),
    );
    // Send only the changed key (field-level merge) so two devices editing
    // different properties of the same row don't overwrite each other.
    try {
      await api.updatePage(rowId, { propsPatch: { [propId]: value } });
    } catch {
      toast(t('Change not saved'));
    }
  };

  // Setting the group property from a drag/preset respects the property type:
  // a multi-select stores an array, a select stores a scalar.
  // Throwing a row away from the board. Asked for by name, because a card
  // carries no undo affordance of its own and the trash is not visible from
  // here — unlike the sidebar, where you can at least see what vanished.
  //
  // Nothing is destroyed: set_trashed is reversible, and the row list refreshes
  // from the rows event the server sends, so a second screen loses the card too.
  const trashRow = async (id: string, title: string) => {
    const ok = await confirm(
      t('Move “{title}” to the trash?', { title: title || t('Untitled') }),
      { confirmText: t('Move to trash'), danger: true },
    );
    if (!ok) return;
    try {
      await api.trashPage(id);
      await loadRows();
      onPagesChanged();
    } catch (e) {
      toast((e as Error).message || t('Could not be moved to the trash'));
    }
  };

  const setGroupValue = async (rowId: string, propId: string, optId: string) => {
    const prop = schema.find((p) => p.id === propId);
    if (prop?.type === 'multiselect' || prop?.type === 'relation') {
      await setRowProp(rowId, propId, optId === UNSET ? [] : [optId]);
    } else {
      await setRowProp(rowId, propId, optId === UNSET ? '' : optId);
    }
  };

  if (!config || !view) return <div className="editor-loading" />;

  const updateView = (patch: Partial<ViewDef>) => {
    // An embed keeps its filters and sort to itself (#26): set inside a
    // document, they must not change the collection's view everywhere else.
    if (embed && ('filters' in patch || 'sort' in patch)) {
      const { filters, sort, ...rest } = patch;
      embed.onView(view.id, {
        ...('filters' in patch ? { filters } : {}),
        ...('sort' in patch ? { sort } : {}),
      });
      if (Object.keys(rest).length === 0) return;
      patch = rest;
    }
    const next = {
      ...config,
      views: config.views.map((v) => (v.id === view.id ? { ...v, ...patch } : v)),
    };
    void saveConfig(next);
  };

  // Renaming and removing a view. Both were MCP-only: the ＋ made views and
  // nothing took them away, so a board somebody added by accident stayed for
  // good. The Properties dialog had three checkboxes for it, but they worked by
  // TYPE — they could not remove one of two boards, and never offered Table,
  // List, Timeline or Form at all.
  const renameView = async (v: ViewDef) => {
    const name = await promptText(t('Rename view'), { defaultValue: v.name, confirmText: t('Rename') });
    setViewMenuFor(null);
    const n = name?.trim();
    if (!n || n === v.name) return;
    void saveConfig({ ...config, views: config.views.map((x) => (x.id === v.id ? { ...x, name: n } : x)) });
  };

  // A collection with no view has nothing to render, so the last one stays.
  const removeView = (v: ViewDef) => {
    setViewMenuFor(null);
    if (config.views.length <= 1) return;
    const rest = config.views.filter((x) => x.id !== v.id);
    void saveConfig({ ...config, views: rest });
    if (v.id === viewId) setViewId(rest[0].id);
  };

  // Moving a view along the strip. The ＋ appends and nothing ever moved, so
  // the order a collection ended up with was the order its views happened to be
  // created in — and the one people look at most sat last.
  //
  // Two buttons rather than dragging: a tab strip is a short list, dragging it
  // needs a drop cursor and a touch fallback, and the ⋯ that holds these is
  // already the place the view is renamed and removed. Grey out at the ends
  // instead of hiding, so the pair does not jump around as you shuffle.
  const moveView = (v: ViewDef, delta: -1 | 1) => {
    const at = config.views.findIndex((x) => x.id === v.id);
    const to = at + delta;
    if (at < 0 || to < 0 || to >= config.views.length) return;
    const next = [...config.views];
    [next[at], next[to]] = [next[to], next[at]];
    void saveConfig({ ...config, views: next });
  };

  const addView = (type: ViewDef['type']) => {
    const labels: Record<ViewDef['type'], string> = {
      table: t('Table'),
      board: t('Board'),
      gallery: t('Gallery'),
      calendar: t('Calendar'),
      list: t('List'),
      form: t('Form'),
      timeline: t('Timeline'),
    };
    const nv: ViewDef = { id: 'v' + Math.random().toString(36).slice(2, 9), name: labels[type], type };
    if (type === 'board') nv.groupBy = schema.find((p) => p.type === 'select' || p.type === 'multiselect')?.id;
    if (type === 'calendar') nv.dateProp = schema.find((p) => p.type === 'date')?.id;
    if (type === 'timeline') {
      const dates = schema.filter((p) => p.type === 'date');
      nv.dateProp = dates[0]?.id;
      nv.endDateProp = dates[1]?.id;
    }
    void saveConfig({ ...config, views: [...config.views, nv] });
    setViewId(nv.id);
    setAddViewOpen(false);
    if (type === 'timeline') setSettingsFor(nv.id);
  };

  const viewRows = applyView(rows, view);
  const emptyLabel =
    rows.length > 0 && viewRows.length === 0
      ? t('No rows match the current filter.')
      : t('No rows yet. Click ＋ New above.');
  const tabIcon = (t: ViewDef['type']) => {
    const sz = 14;
    if (t === 'board') return <Columns3 size={sz} />;
    if (t === 'gallery') return <LayoutGrid size={sz} />;
    if (t === 'calendar') return <CalendarDays size={sz} />;
    if (t === 'list') return <List size={sz} />;
    if (t === 'form') return <ClipboardList size={sz} />;
    if (t === 'timeline') return <GanttChartSquare size={sz} />;
    return <Table2 size={sz} />;
  };

  // Columns hidden in this view are dropped from every renderer.
  const hidden = new Set(view.hidden ?? []);
  const visibleSchema = schema.filter((p) => !hidden.has(p.id));
  // Relation props that point back at this same collection — candidates for the
  // sub-item (task hierarchy) relation that the table can render as a tree.
  const selfRelProps = schema.filter((p) => p.type === 'relation' && p.relationCollection === collectionId);
  // Date props — the timeline's Start/End pickers choose from these.
  const dateProps = schema.filter((p) => p.type === 'date');

  const viewSwitcher = (
    <div className="collection-toolbar">
      <div className="view-tabs">
        {config.views.map((v) => (
          <span key={v.id} className="view-tab-wrap">
            <button
              className={'view-tab view-tab--' + v.type + (v.id === view.id ? ' active' : '')}
              onClick={() => pickView(v.id)}
              onDoubleClick={() => {
                if (!embed) void renameView(v);
              }}
              onContextMenu={(e) => {
                // Inside a document the views are the collection's business,
                // reached through "Open as page" — not something a reader of
                // the document should rename or remove by accident.
                if (embed) return;
                e.preventDefault();
                setTabMenu({
                  view: v,
                  style: {
                    position: 'fixed',
                    left: Math.max(8, Math.min(e.clientX, window.innerWidth - 208)),
                    top: Math.max(8, Math.min(e.clientY, window.innerHeight - 180)),
                    right: 'auto',
                    zIndex: 320,
                  },
                });
              }}
            >
              <span className="view-tab-ic">{tabIcon(v.type)}</span>
              {v.name}
            </button>
          </span>
        ))}
        {!embed && (
          <button
            ref={addViewBtnRef}
            className="view-add"
            title={t('Add view')}
            onClick={() => setAddViewOpen((o) => !o)}
          >
            <Plus size={15} />
          </button>
        )}
      </div>
      {tabMenu && (
        <Portal>
          <div className="fs-backdrop" onClick={() => setTabMenu(null)} onContextMenu={(e) => { e.preventDefault(); setTabMenu(null); }} />
          <div className="menu view-tab-menu" style={tabMenu.style}>
            <button onClick={() => { setTabMenu(null); void renameView(tabMenu.view); }}>
              <Pencil size={15} /> {t('Rename view')}
            </button>
            {config.views.length > 1 && (
              <>
                <button
                  disabled={config.views[0]?.id === tabMenu.view.id}
                  onClick={() => { setTabMenu(null); moveView(tabMenu.view, -1); }}
                >
                  <ArrowLeft size={15} /> {t('Move left')}
                </button>
                <button
                  disabled={config.views[config.views.length - 1]?.id === tabMenu.view.id}
                  onClick={() => { setTabMenu(null); moveView(tabMenu.view, 1); }}
                >
                  <ArrowRight size={15} /> {t('Move right')}
                </button>
                <button className="danger" onClick={() => { setTabMenu(null); removeView(tabMenu.view); }}>
                  <Trash2 size={15} /> {t('Remove view')}
                </button>
              </>
            )}
          </div>
        </Portal>
      )}
      {addViewOpen && addViewPos && (
        <Portal>
          <div className="fs-backdrop" onClick={() => setAddViewOpen(false)} />
          <div className="menu view-add-menu" style={addViewPos}>
            {/* `vt`, not `t` — the loop variable would shadow the translate
                function used in the labels right below it. */}
            {(['table', 'board', 'gallery', 'calendar', 'timeline', 'list', 'form'] as const).map((vt) => (
              <button key={vt} onClick={() => addView(vt)}>
                <span className="view-tab-ic">{tabIcon(vt)}</span>
                {{
                  table: t('Table'),
                  board: t('Board'),
                  gallery: t('Gallery'),
                  calendar: t('Calendar'),
                  timeline: t('Timeline'),
                  list: t('List'),
                  form: t('Form'),
                }[vt]}
              </button>
            ))}
          </div>
        </Portal>
      )}
      <div className="collection-actions">
        {view.type === 'timeline' && (
          <>
            <select
              className="subitems-select"
              title={t('Start date for the bars')}
              value={view.dateProp ?? ''}
              onChange={(e) => updateView({ dateProp: e.target.value || undefined })}
            >
              <option value="">{t('Start: (none)')}</option>
              {dateProps.map((p) => (
                <option key={p.id} value={p.id}>
                  {t('Start:')} {p.name}
                </option>
              ))}
            </select>
            <select
              className="subitems-select"
              title={t('End date for the bars (blank = one-day bars)')}
              value={view.endDateProp ?? ''}
              onChange={(e) => updateView({ endDateProp: e.target.value || undefined })}
            >
              <option value="">{t('End: (none)')}</option>
              {dateProps.map((p) => (
                <option key={p.id} value={p.id}>
                  {t('End:')} {p.name}
                </option>
              ))}
            </select>
          </>
        )}
        {view.type === 'table' && selfRelProps.length > 0 && (
          <select
            className="subitems-select"
            title={t('Sub-item relation for the tree view')}
            value={view.subItemProp ?? ''}
            onChange={(e) => updateView({ subItemProp: e.target.value || undefined })}
          >
            <option value="">{t('No sub-items')}</option>
            {selfRelProps.map((p) => (
              <option key={p.id} value={p.id}>
                ⿴ {p.name}
              </option>
            ))}
          </select>
        )}
        {view.type !== 'form' && <FilterSortControls schema={schema} view={view} onChange={updateView} />}
        {/* What the collection IS — its columns, its properties, its views —
            is edited on its own page. An embed is a window onto the rows, and
            carrying the whole toolbar into a document made it look like a
            second copy of the collection (#22). */}
        {!embed && <ColumnsControl schema={schema} view={view} onChange={updateView} />}
        {!embed && (
          <button className="btn-sm" onClick={() => setSchemaOpen(true)}>
            <Settings2 size={14} /> {t('Properties')}
          </button>
        )}
        {/* Renaming and removing the view you are on. It sat inside the tab as a
            ⋯ first, which looked like a smudge in the pill and had to reserve
            room in a row where nothing else does. This bar is already "settings
            for the current view" — filter, sort, group, columns — so it is
            where the last two belong. */}
        {!embed && (
        <div className="view-menu-wrap">
          <button
            className="btn-sm"
            title={t('View options')}
            onClick={() => setViewMenuFor((cur) => (cur ? null : view.id))}
          >
            <MoreHorizontal size={14} />
          </button>
          {viewMenuFor === view.id && (
            <>
              <div className="fs-backdrop" onClick={() => setViewMenuFor(null)} />
              <div className="menu view-tab-menu">
                <button onClick={() => void renameView(view)}>
                  <Pencil size={15} /> {t('Rename view')}
                </button>
                {config.views.length > 1 && (
                  <>
                    <button
                      disabled={config.views[0]?.id === view.id}
                      onClick={() => moveView(view, -1)}
                    >
                      <ArrowLeft size={15} /> {t('Move left')}
                    </button>
                    <button
                      disabled={config.views[config.views.length - 1]?.id === view.id}
                      onClick={() => moveView(view, 1)}
                    >
                      <ArrowRight size={15} /> {t('Move right')}
                    </button>
                  </>
                )}
                {config.views.length > 1 && (
                  <button className="danger" onClick={() => removeView(view)}>
                    <Trash2 size={15} /> {t('Remove view')}
                  </button>
                )}
              </div>
            </>
          )}
        </div>
        )}
        {view.type !== 'form' && (
          <button className="btn-sm primary" onClick={() => void addRow()}>
            <Plus size={14} /> {t('New')}
          </button>
        )}
      </div>
    </div>
  );

  return (
    <div
      className={
        'collection-scroll' +
        (view.type === 'board' ? ' is-board' : '') +
        (embed ? ' is-embed' : '') +
        (embed && (view.filters ?? []).some(filterIsArmed) ? ' is-filtered' : '')
      }
    >
      {viewSwitcher}
      {view.type === 'form' ? (
        <FormView
          collectionId={collectionId}
          schema={visibleSchema}
          view={view}
          onUpdateView={updateView}
          onSubmitted={onPagesChanged}
        />
      ) : view.type === 'board' ? (
        <BoardView
          rows={viewRows}
          schema={visibleSchema}
          groupBy={view.groupBy || schema.find((p) => p.type === 'select')?.id || ''}
          tagColors={tagColors}
          commentCounts={commentCounts}
          onNavigate={onNavigate}
          onOpenInNewTab={onOpenInNewTab}
          onSetProp={setRowProp}
          onSetOptions={setPropOptions}
          onDrop={(rowId, groupBy, optId) => {
            void setGroupValue(rowId, groupBy, optId);
          }}
          onTrashRow={trashRow}
          onAddInColumn={(groupBy, optId) =>
            void addRow(optId === UNSET ? {} : { [groupBy]: groupValueFor(schema, groupBy, optId) })
          }
        />
      ) : view.type === 'gallery' ? (
        <GalleryView
          rows={viewRows}
          schema={visibleSchema}
          emptyLabel={emptyLabel}
          tagColors={tagColors}
          onNavigate={onNavigate}
          onOpenInNewTab={onOpenInNewTab}
          onSetProp={setRowProp}
          onSetOptions={setPropOptions}
        />
      ) : view.type === 'calendar' ? (
        <CalendarView
          rows={viewRows}
          schema={visibleSchema}
          dateProp={view.dateProp || schema.find((p) => p.type === 'date')?.id || ''}
          tagColors={tagColors}
          onNavigate={onNavigate}
          onOpenInNewTab={onOpenInNewTab}
        />
      ) : view.type === 'list' ? (
        <ListView
          rows={viewRows}
          schema={visibleSchema}
          emptyLabel={emptyLabel}
          tagColors={tagColors}
          onNavigate={onNavigate}
          onOpenInNewTab={onOpenInNewTab}
          onSetProp={setRowProp}
          onSetOptions={setPropOptions}
        />
      ) : view.type === 'timeline' ? (
        <TimelineView
          rows={viewRows}
          schema={visibleSchema}
          startProp={view.dateProp || schema.find((p) => p.type === 'date')?.id || ''}
          endProp={view.endDateProp || ''}
          tagColors={tagColors}
          onNavigate={onNavigate}
          onOpenInNewTab={onOpenInNewTab}
          mode={view.tlMode ?? 'month'}
          onModeChange={(m) => updateView({ tlMode: m })}
          onViewChange={updateView}
          onAddRow={(preset) => addRow(preset)}
          onSetDate={(rowId, propId, iso) => void setRowProp(rowId, propId, iso)}
          hideLabels={view.tlHideLabels ?? false}
          onToggleLabels={() => updateView({ tlHideLabels: !view.tlHideLabels })}
          startWithSettings={settingsFor === view.id}
          onSettingsShown={() => setSettingsFor(null)}
        />
      ) : (
        <TableView
          rows={viewRows}
          schema={visibleSchema}
          emptyLabel={emptyLabel}
          tagColors={tagColors}
          subItemProp={
            view.subItemProp && selfRelProps.some((p) => p.id === view.subItemProp) ? view.subItemProp : undefined
          }
          colWidths={view.colWidths}
          onSetColWidths={(colWidths) => updateView({ colWidths })}
          onNavigate={onNavigate}
          onOpenInNewTab={onOpenInNewTab}
          onSetProp={setRowProp}
          onSetOptions={setPropOptions}
        />
      )}
      {schemaOpen && (
        <SchemaEditor
          config={config}
          collections={[...pages.values()]
            // A template is a snapshot, not a database rows can point at.
            .filter((p) => p.type === 'collection' && !p.trashed && !p.isTemplate)
            .map((p) => ({ id: p.id, title: p.title }))}
          onSave={saveConfig}
          onClose={() => setSchemaOpen(false)}
        />
      )}
    </div>
  );
}

// ---- Form view ----
// A Notion-style form: renders the collection's editable properties as fields;
// submitting creates a new row with the entered values. Computed types
// (relation/rollup/formula) can't be filled in and are skipped.
const FORM_TYPES: PropType[] = ['text', 'number', 'select', 'multiselect', 'date', 'checkbox', 'person'];

function FormView({
  collectionId,
  schema,
  view,
  onUpdateView,
  onSubmitted,
}: {
  collectionId: string;
  schema: PropDef[];
  view: ViewDef;
  onUpdateView: (patch: Partial<ViewDef>) => void;
  onSubmitted: () => void;
}) {
  const fields = schema.filter((p) => FORM_TYPES.includes(p.type));
  const [title, setTitle] = useState('');
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [shared, setShared] = useState(false);
  const [shareUrl, setShareUrl] = useState<string | null>(null);
  const [shareBusy, setShareBusy] = useState(false);

  useEffect(() => {
    api.formShareStatus(collectionId).then((s) => setShared(s.shared)).catch(() => {});
  }, [collectionId]);

  const doShare = async () => {
    setShareBusy(true);
    try {
      const { url } = await api.createFormShare(collectionId);
      // Backend returns an absolute URL on the external domain when one is
      // configured; only fall back to the current origin for a bare path.
      const full = url.startsWith('http') ? url : window.location.origin + url;
      setShareUrl(full);
      setShared(true);
      try {
        await navigator.clipboard?.writeText(full);
        toast(t('Public link copied'));
      } catch {
        toast(t('Public link created'));
      }
    } catch {
      toast(t('Sharing failed'));
    } finally {
      setShareBusy(false);
    }
  };

  const doUnshare = async () => {
    setShareBusy(true);
    try {
      await api.deleteFormShare(collectionId);
      setShared(false);
      setShareUrl(null);
      toast(t('Public link revoked'));
    } catch {
      toast(t('That did not work'));
    } finally {
      setShareBusy(false);
    }
  };

  const set = (id: string, v: unknown) => setValues((prev) => ({ ...prev, [id]: v }));
  const reset = () => {
    setTitle('');
    setValues({});
    setDone(false);
  };

  const submit = async () => {
    if (!title.trim() || busy) return;
    setBusy(true);
    try {
      const props: Record<string, unknown> = {};
      for (const f of fields) {
        const v = values[f.id];
        if (v === undefined || v === '' || v === null) continue;
        if (Array.isArray(v) && v.length === 0) continue;
        props[f.id] = v;
      }
      await api.createPage(collectionId, title.trim(), 'doc', props);
      onSubmitted();
      setDone(true);
    } catch {
      toast(t('Sending failed'));
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <div className="form-view">
        <div className="form-card form-done">
          <div className="form-done-ic">
            <Check size={30} />
          </div>
          <h2>{t('Sent')}</h2>
          <p>{t('Your answer has been saved.')}</p>
          <button className="btn-sm primary" onClick={reset}>
            {t('Send another answer')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="form-view">
      <div className="form-card">
        <div className="form-share-bar">
          {!shared ? (
            <button className="btn-sm" disabled={shareBusy} onClick={() => void doShare()}>
              <Share2 size={14} /> {t('Share publicly')}
            </button>
          ) : (
            <>
              <span className="form-share-live">
                <Globe size={13} /> {t('Public')}
              </span>
              <button className="btn-sm" disabled={shareBusy} onClick={() => void doShare()}>
                {t('Copy link')}
              </button>
              <button className="btn-sm" disabled={shareBusy} onClick={() => void doUnshare()}>
                {t('Revoke')}
              </button>
            </>
          )}
        </div>
        {shareUrl && (
          <input
            className="form-share-url"
            readOnly
            value={shareUrl}
            onFocus={(e) => e.currentTarget.select()}
          />
        )}
        <input
          className="form-heading"
          value={view.formTitle ?? ''}
          placeholder={t('Form')}
          onChange={(e) => onUpdateView({ formTitle: e.target.value })}
        />
        <textarea
          className="form-desc"
          value={view.formDesc ?? ''}
          placeholder={t('Description (optional): explains what the form is for.')}
          rows={2}
          onChange={(e) => onUpdateView({ formDesc: e.target.value })}
        />
        <div className="form-fields">
          <label className="form-field">
            <span className="form-label">
              {t('Title')} <b className="form-req">*</b>
            </span>
            <input
              className="form-input"
              value={title}
              placeholder={t('Name of the entry')}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void submit();
              }}
            />
          </label>
          {fields.map((f) => (
            <label key={f.id} className={'form-field' + (f.type === 'checkbox' ? ' form-field--check' : '')}>
              <span className="form-label">{f.name}</span>
              {formField(f, values[f.id], (v) => set(f.id, v))}
            </label>
          ))}
        </div>
        <div className="form-actions">
          <button className="btn-sm primary form-submit" disabled={busy || !title.trim()} onClick={() => void submit()}>
            <Send size={14} /> {view.formSubmit?.trim() || t('Submit')}
          </button>
        </div>
      </div>
    </div>
  );
}

export function formField(def: PropDef, value: unknown, onChange: (v: unknown) => void) {
  switch (def.type) {
    case 'number':
      return (
        <input
          className="form-input"
          type="number"
          value={(value as number) ?? ''}
          onChange={(e) => onChange(e.target.value === '' ? null : Number(e.target.value))}
        />
      );
    case 'date':
      return (
        <input
          className="form-input"
          type="date"
          value={(value as string) || ''}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    case 'checkbox':
      return (
        <input
          className="form-checkbox"
          type="checkbox"
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
        />
      );
    case 'select':
      return (
        <select className="form-input form-select" value={(value as string) || ''} onChange={(e) => onChange(e.target.value || null)}>
          <option value="">—</option>
          {def.options?.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
            </option>
          ))}
        </select>
      );
    case 'multiselect': {
      const vals = idList(value);
      if (!def.options?.length) return <span className="form-hint">{t('No options defined')}</span>;
      return (
        <div className="form-chips">
          {def.options.map((o) => {
            const on = vals.includes(o.id);
            return (
              <button
                type="button"
                key={o.id}
                className={'form-chip' + (on ? ' on' : '')}
                onClick={() => onChange(on ? vals.filter((x) => x !== o.id) : [...vals, o.id])}
              >
                {o.name}
              </button>
            );
          })}
        </div>
      );
    }
    case 'text':
    case 'person':
    default:
      return (
        <input className="form-input" value={(value as string) || ''} onChange={(e) => onChange(e.target.value)} />
      );
  }
}

// ---- Filter & sort controls ----

function FilterSortControls({
  schema,
  view,
  onChange,
}: {
  schema: PropDef[];
  view: ViewDef;
  onChange: (patch: Partial<ViewDef>) => void;
}) {
  const [open, setOpen] = useState<'filter' | 'sort' | 'group' | null>(null);
  const controlsRef = useRef<HTMLDivElement>(null);
  // Titles for every relation in this schema, so a filter on one can offer
  // rows to pick instead of demanding an id. Keyed by property id; the
  // underlying fetch is cached per collection.
  const [relOptions, setRelOptions] = useState<Record<string, RelOption[]>>({});
  useEffect(() => {
    let alive = true;
    for (const p of schema) {
      const target = p.type === 'relation' ? p.relationCollection : p.type === 'backrelation' ? p.backrelationCollection : '';
      if (!target) continue;
      void loadRelationOptions(target).then((o) => {
        if (alive) setRelOptions((prev) => (prev[p.id] ? prev : { ...prev, [p.id]: o }));
      });
    }
    return () => {
      alive = false;
    };
  }, [schema]);
  // Popover position, computed against the viewport. Portaled to <body> so no
  // transformed/overflow-clipping ancestor (the scrollable mobile toolbar, the
  // page-body) can trap or hide it — the bug where the field vanished behind a
  // grey overlay on mobile.
  const [pos, setPos] = useState<React.CSSProperties | null>(null);
  useLayoutEffect(() => {
    if (!open || !controlsRef.current) {
      setPos(null);
      return;
    }
    const r = controlsRef.current.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    if (vw <= 640) {
      setPos({ position: 'fixed', left: 8, right: 8, bottom: 8, maxHeight: vh * 0.72 });
    } else {
      // Wider than it was: a tick list of option names and a pair of date
      // fields need the room, and the old 300 is what everything was being
      // squeezed into.
      const width = 340;
      const left = Math.max(8, Math.min(r.right - width, vw - width - 8));
      setPos({ position: 'fixed', left, width, top: r.bottom + 6, maxHeight: vh - r.bottom - 24 });
    }
  }, [open]);
  const filters = view.filters ?? [];
  const sort = view.sort ?? null;
  const propName = (id: string) => schema.find((p) => p.id === id)?.name ?? id;

  const valuesFor = (propId: string): { value: string; label: string }[] => {
    const prop = schema.find((p) => p.id === propId);
    if (!prop) return [];
    if (prop.type === 'select' || prop.type === 'multiselect') {
      return (prop.options ?? []).map((o) => ({ value: o.id, label: o.name }));
    }
    if (prop.type === 'checkbox') {
      return [
        { value: 'true', label: 'Checked' },
        { value: 'false', label: 'Unchecked' },
      ];
    }
    // A relation stores page ids. Offering a free-text box here meant filtering
    // "System is salt.md" required typing a 32-character id nobody can see —
    // the property was filterable in theory and unusable in practice.
    if (prop.type === 'relation' || prop.type === 'backrelation') {
      return (relOptions[propId] ?? []).map((o) => ({
        value: o.id,
        label: o.title || t('Untitled'),
      }));
    }
    return [];
  };

  // Operators offered per property type. Built during render, never as a module
  // constant: a list of labels resolved once at import keeps its language for
  // the life of the tab. These read `is` / `is not` in a German interface for
  // exactly that reason — they were plain strings, which the checks cannot see
  // inside an object literal.
  const opsFor = (propId: string): { value: FilterOp; label: string }[] => {
    const prop = schema.find((p) => p.id === propId);
    const ty = prop?.type;
    const many = takesSeveral(propId);
    const base: { value: FilterOp; label: string }[] = [
      { value: 'is', label: many ? t('is any of') : t('is') },
      { value: 'is_not', label: many ? t('is none of') : t('is not') },
    ];
    if (ty === 'number' || ty === 'rollup' || ty === 'formula') {
      base.push(
        { value: 'gt', label: t('greater than') },
        { value: 'lt', label: t('less than') },
        { value: 'between', label: t('between') },
      );
    } else if (ty === 'date') {
      base.push(
        { value: 'gt', label: t('after') },
        { value: 'lt', label: t('before') },
        { value: 'between', label: t('between') },
      );
    } else if (ty === 'text' || ty === 'person' || ty === undefined) {
      base.push({ value: 'contains', label: t('contains') });
    }
    base.push({ value: 'is_empty', label: t('is empty') }, { value: 'is_not_empty', label: t('is not empty') });
    return base;
  };

  const hasValueInput = (op: FilterOp | undefined) => op !== 'is_empty' && op !== 'is_not_empty';

  /** Which properties can be asked about with SEVERAL values at once. Only the
   *  ones whose values come from a fixed list — a free-text box has nothing to
   *  tick, and a date uses a range instead. */
  function takesSeveral(propId: string): boolean {
    const ty = schema.find((p) => p.id === propId)?.type;
    return (
      ty === 'select' || ty === 'multiselect' || ty === 'relation' || ty === 'backrelation'
    );
  }

  return (
    <div className="fs-controls" ref={controlsRef}>
      <button
        className={'btn-sm' + (filters.length ? ' active' : '')}
        onClick={() => setOpen(open === 'filter' ? null : 'filter')}
      >
        <FilterIcon size={14} /> Filter{filters.length ? ` (${filters.length})` : ''}
      </button>
      <button
        className={'btn-sm' + (sort ? ' active' : '')}
        onClick={() => setOpen(open === 'sort' ? null : 'sort')}
      >
        <ArrowUpDown size={14} /> {t('Sort')}
      </button>
      {/* Only a board has columns to group into. The setting existed before
          this button did — it just took whatever select property came first,
          which is why nobody could put their tasks into one column per
          project. */}
      {view.type === 'board' && (
        <button
          className={'btn-sm' + (view.groupBy ? ' active' : '')}
          onClick={() => setOpen(open === 'group' ? null : 'group')}
        >
          <Columns3 size={14} /> {t('Group')}
        </button>
      )}

      {open && pos && (
        <Portal>
          <div className="fs-backdrop" onClick={() => setOpen(null)} />
          <div className="fs-popover" style={pos}>
            {open === 'filter' && (
              <>
          {filters.map((f, i) => {
            const op = f.op ?? (f.value === '' ? 'is_not_empty' : 'is');
            const options = valuesFor(f.property);
            const patch = (u: Partial<Filter>) => {
              const next = filters.slice();
              next[i] = { ...f, ...u };
              onChange({ filters: next });
            };
            const propType = schema.find((p) => p.id === f.property)?.type;
            const isDate = propType === 'date';
            const picked = filterValues(f);
            const several = takesSeveral(f.property) && (op === 'is' || op === 'is_not');
            // A tick list writes `values`, everything else writes `value` — the
            // two never travel together, so switching between them clears the
            // other rather than leaving a stale condition behind.
            const toggleValue = (v: string) => {
              const next = picked.includes(v) ? picked.filter((x) => x !== v) : [...picked, v];
              patch({ values: next, value: '' });
            };
            return (
              <div key={i} className="fs-filter">
                <div className="fs-filter-head">
                  <span className="fs-label">{propName(f.property)}</span>
                  <select
                    className="prop-select fs-op"
                    value={op}
                    onChange={(e) => {
                      const nextOp = e.target.value as FilterOp;
                      // Changing the operator keeps what still applies and drops
                      // what does not: a range's second date is meaningless under
                      // "after", a tick list under "contains".
                      patch({
                        op: nextOp,
                        value: hasValueInput(nextOp) ? f.value : '',
                        values: takesSeveral(f.property) && (nextOp === 'is' || nextOp === 'is_not') ? f.values : undefined,
                        value2: nextOp === 'between' ? f.value2 : undefined,
                      });
                    }}
                  >
                    {opsFor(f.property).map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                  <button
                    className="icon-btn danger fs-remove"
                    title={t('Remove filter')}
                    onClick={() => onChange({ filters: filters.filter((_, j) => j !== i) })}
                  >
                    ✕
                  </button>
                </div>
                {hasValueInput(op) && (
                  <div className="fs-filter-value">
                    {several && options.length > 0 ? (
                      <div className="fs-checks">
                        {options.map((o) => (
                          <label key={o.value} className={'fs-check' + (picked.includes(o.value) ? ' on' : '')}>
                            <input
                              type="checkbox"
                              checked={picked.includes(o.value)}
                              onChange={() => toggleValue(o.value)}
                            />
                            {o.label}
                          </label>
                        ))}
                      </div>
                    ) : op === 'between' ? (
                      <div className="fs-range">
                        <input
                          className="prop-input"
                          type={isDate ? 'date' : 'number'}
                          value={f.value}
                          onChange={(e) => patch({ value: e.target.value })}
                        />
                        <span className="fs-range-sep">–</span>
                        <input
                          className="prop-input"
                          type={isDate ? 'date' : 'number'}
                          value={f.value2 ?? ''}
                          onChange={(e) => patch({ value2: e.target.value })}
                        />
                      </div>
                    ) : options.length > 0 ? (
                      <select
                        className="prop-select"
                        value={f.value}
                        onChange={(e) => patch({ value: e.target.value, values: undefined })}
                      >
                        <option value="">—</option>
                        {options.map((o) => (
                          <option key={o.value} value={o.value}>
                            {o.label}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input
                        className="prop-input"
                        // A date needs a picker, not a box you have to spell
                        // 2026-08-18 into by hand. That, plus the empty
                        // condition emptying the table, was the whole of
                        // "date filtering does not work".
                        type={isDate ? 'date' : propType === 'number' ? 'number' : 'text'}
                        value={f.value}
                        placeholder={t('value')}
                        onChange={(e) => patch({ value: e.target.value, values: undefined })}
                      />
                    )}
                  </div>
                )}
                {!filterIsArmed(f) && <div className="fs-hint">{t('Not filtering yet. Pick a value.')}</div>}
              </div>
            );
          })}
          <select
            className="prop-select fs-add"
            value=""
            onChange={(e) => {
              if (!e.target.value) return;
              onChange({ filters: [...filters, { property: e.target.value, op: 'is', value: '' }] });
            }}
          >
            <option value="">{t('+ Add filter…')}</option>
            {schema.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
              </>
            )}
            {open === 'sort' && (
              <>
          <div className="fs-row">
            <select
              className="prop-select"
              value={sort?.property ?? ''}
              onChange={(e) =>
                onChange({
                  sort: e.target.value ? { property: e.target.value, dir: sort?.dir ?? 'asc' } : null,
                })
              }
            >
              <option value="">{t('No sort')}</option>
              {schema.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            {sort && (
              <select
                className="prop-select"
                value={sort.dir}
                onChange={(e) =>
                  onChange({ sort: { ...sort, dir: e.target.value as Sort['dir'] } })
                }
              >
                <option value="asc">{t('Ascending')}</option>
                <option value="desc">{t('Descending')}</option>
              </select>
            )}
          </div>
              </>
            )}
            {open === 'group' && (
              <div className="fs-row">
                <select
                  className="prop-select"
                  value={view.groupBy ?? ''}
                  onChange={(e) => onChange({ groupBy: e.target.value })}
                >
                  {/* A relation belongs here as much as a select does: "one
                      column per system" is the same question as "one column per
                      status", just answered by another database. */}
                  {schema
                    .filter(
                      (p) =>
                        p.type === 'select' || p.type === 'multiselect' || p.type === 'relation',
                    )
                    .map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                </select>
              </div>
            )}
          </div>
        </Portal>
      )}
    </div>
  );
}

// ---- Board (Kanban) ----

// What a board card shows, in fixed zones (W126, see cardLayout.ts). The old
// version rendered every property in schema order, so the card grew with the
// schema instead of with what matters — two unlabelled dates, a bare number,
// the same colleague once per person field, and four lines of address and
// phone that nobody reads on a card.
//
// Nothing here hides a field by cleverness: the order is fixed and the tail
// is counted, not dropped — "+3" opens in place and names what it holds.
function BoardCardProps({
  schema,
  row,
  groupBy,
  expanded,
  onToggleExpand,
  onSetProp,
  onSetOptions,
}: {
  schema: PropDef[];
  row: Row;
  groupBy: string;
  expanded: boolean;
  onToggleExpand: () => void;
  onSetProp: (rowId: string, propId: string, value: unknown) => void;
  onSetOptions: (propId: string, options: PropOption[]) => void;
}) {
  const defs = schema.filter((p) => p.id !== groupBy);
  const filled = defs.filter((p) => !isBlank(row.props[p.id]));
  const plan = planCard(filled, (p) => ({ def: p, value: row.props[p.id] }));

  // Empty select fields stay reachable: they are invisible until the card is
  // hovered, so a status can be set without opening the card — but they never
  // put "— — —" under every title.
  const emptySelects = defs.filter(
    (p) => (p.type === 'select' || p.type === 'multiselect') && isBlank(row.props[p.id]),
  );

  const factRow = (p: PropDef) => (
    <span key={p.id} className="card-fact">
      {needsLabel(p, plan.facts.length) && <span className="card-fact-label">{p.name}</span>}
      <PropertyValue def={p} value={row.props[p.id]} readOnly compact />
    </span>
  );

  const contactIcon = (p: PropDef) => {
    const kind = contactKind(p, row.props[p.id]);
    const value = String(row.props[p.id] ?? '');
    const Icon = kind === 'mail' ? Mail : kind === 'phone' ? Phone : kind === 'address' ? MapPin : Link2;
    return (
      <span key={p.id} className="card-contact" title={`${p.name}: ${value}`}>
        <Icon size={13} />
      </span>
    );
  };

  return (
    <div className="board-card-props">
      {(plan.chips.length > 0 || emptySelects.length > 0) && (
        <div className="card-chips">
          {[...plan.chips, ...emptySelects].map((p) => (
            <div
              key={p.id}
              className={'card-prop-edit' + (isBlank(row.props[p.id]) ? ' is-empty' : '')}
              onClick={(e) => e.stopPropagation()}
            >
              <PropertyValue
                def={p}
                value={row.props[p.id]}
                onChange={(nv) => onSetProp(row.id, p.id, nv)}
                onOptionsChange={(opts) => onSetOptions(p.id, opts)}
              />
            </div>
          ))}
        </div>
      )}
      {plan.facts.length > 0 && <div className="card-facts">{plan.facts.map(factRow)}</div>}
      {plan.notes.map((p) => (
        <p key={p.id} className="card-note">
          {String(row.props[p.id])}
        </p>
      ))}
      {(plan.contacts.length > 0 || plan.overflow.length > 0) && (
        <div className="card-footline">
          {plan.contacts.map(contactIcon)}
          {plan.overflow.length > 0 && (
            <button
              className="card-more"
              title={plan.overflow.map((p) => p.name).join(', ')}
              onClick={(e) => {
                e.stopPropagation();
                onToggleExpand();
              }}
            >
              {expanded ? t('less') : t('+{n} more', { n: String(plan.overflow.length) })}
            </button>
          )}
        </div>
      )}
      {expanded && plan.overflow.length > 0 && (
        <div className="card-facts card-overflow">
          {plan.overflow.map((p) =>
            zoneOf(p, row.props[p.id]) === 'fact' ? (
              factRow(p)
            ) : (
              <span key={p.id} className="card-fact">
                <span className="card-fact-label">{p.name}</span>
                <span className="card-fact-text">{String(row.props[p.id])}</span>
              </span>
            ),
          )}
        </div>
      )}
    </div>
  );
}

function BoardView({
  rows,
  schema,
  groupBy,
  tagColors,
  commentCounts,
  onNavigate,
  onOpenInNewTab,
  onSetProp,
  onSetOptions,
  onDrop,
  onTrashRow,
  onAddInColumn,
}: {
  rows: Row[];
  schema: PropDef[];
  groupBy: string;
  tagColors: Record<string, string>;
  commentCounts: Record<string, number>;
  onNavigate: (id: string) => void;
  onOpenInNewTab?: (id: string) => void;
  onSetProp: (rowId: string, propId: string, value: unknown) => void;
  onSetOptions: (propId: string, options: PropOption[]) => void;
  onDrop: (rowId: string, groupBy: string, optId: string) => void;
  onTrashRow: (id: string, title: string) => Promise<void>;
  onAddInColumn: (groupBy: string, optId: string) => void;
}) {
  // Keyed by "columnId:rowId" so a card that appears in multiple columns
  // (multiselect grouping) opens only the tapped copy's menu.
  const [moveMenu, setMoveMenu] = useState<string | null>(null);
  // Cards whose overflow ("+3") the reader opened. Kept here rather than per
  // card so it survives a re-render, and forgotten on leaving the view — an
  // opened card is a glance, not a setting.
  const [openCards, setOpenCards] = useState<Set<string>>(new Set());
  // Zeiger-basiertes Ziehen statt des ruckeligen nativen Drags (siehe boardDrag).
  const { drag, armedRow, startDrag, consumeClick } = useBoardDrag((rowId, toCol) =>
    onDrop(rowId, groupBy, toCol),
  );
  // When grouping by a relation, the columns are the rows of the target
  // collection. Loaded through the same cache the relation cells use, so the
  // column heading and the cell always say the same thing.
  const [relColumns, setRelColumns] = useState<RelOption[]>([]);
  const groupTarget =
    schema.find((p) => p.id === groupBy)?.type === 'relation'
      ? (schema.find((p) => p.id === groupBy)?.relationCollection ?? '')
      : '';
  useEffect(() => {
    if (!groupTarget) {
      setRelColumns([]);
      return;
    }
    let alive = true;
    void loadRelationOptions(groupTarget).then((o) => alive && setRelColumns(o));
    return () => {
      alive = false;
    };
  }, [groupTarget]);
  useEffect(() => {
    if (!moveMenu) return;
    const onDown = (e: MouseEvent) => {
      if (!(e.target as Element).closest?.('.card-move')) setMoveMenu(null);
    };
    document.addEventListener('pointerdown', onDown);
    return () => document.removeEventListener('pointerdown', onDown);
  }, [moveMenu]);
  const prop = schema.find((p) => p.id === groupBy);
  if (!prop || (prop.type !== 'select' && prop.type !== 'multiselect' && prop.type !== 'relation')) {
    return (
      <div className="board-empty">
        {t('This board needs a {type} property to group by. Open ⚙ Properties to add one.', { type: t('Select') })}
      </div>
    );
  }
  // Grouping by a relation turns the related rows into the columns: one column
  // per system, per customer, per whatever the rows point at. The rest of the
  // board does not need to know the difference — the ids are ids, and a
  // relation value is an array exactly like a multiselect's.
  const options: PropOption[] =
    prop.type === 'relation'
      ? relColumns.map((o) => ({ id: o.id, name: o.title || t('Untitled'), color: '#999' }))
      : (prop.options ?? []);
  const optionIds = new Set(options.map((o) => o.id));
  // The catch-all column was built by gluing "No " onto the property name, so
  // it stayed English in every language. It shows on every board.
  const columns = [...options, { id: UNSET, name: t('No {name}', { name: prop.name }), color: '#999' }];

  // A row whose value references a removed option would otherwise vanish from
  // every column; the UNSET column catches those so no card is lost.
  const isUngrouped = (v: unknown) => {
    if (v === undefined || v === '' || v === null) return true;
    if (Array.isArray(v)) return v.length === 0 || !v.some((x) => optionIds.has(x as string));
    return !optionIds.has(v as string);
  };

  const rowsFor = (optId: string) =>
    rows.filter((r) => {
      const v = r.props[groupBy];
      if (optId === UNSET) return isUngrouped(v);
      if (Array.isArray(v)) return v.includes(optId);
      return v === optId;
    });

  return (
    <div className={'board' + (drag ? ' is-dragging' : '')}>
      {columns.map((col) => (
        <div
          key={col.id}
          data-col={col.id}
          className={
            'board-col' +
            // Highlight the target column but not the source column — putting it
            // back there is not a change.
            (drag && drag.over === col.id && drag.fromCol !== col.id ? ' drag-over' : '')
          }
          // The column carries the colour of ITS option (choosable in the
          // properties dialog since W94) — the tint used to come from a
          // position-based rainbow nobody could influence.
          style={col.id !== UNSET ? ({ '--col-c': col.color } as React.CSSProperties) : undefined}
        >
          <div className="board-col-head">
            <span className="board-col-head-left">
              <span className="board-chip" style={{ background: col.color + '33', color: col.color }}>
                {col.name}
              </span>
              <span className="board-count">{rowsFor(col.id).length}</span>
            </span>
            <button className="board-add" title={t('New')} onClick={() => onAddInColumn(groupBy, col.id)}>
              <Plus size={14} />
            </button>
          </div>
          <div className="board-cards">
            {rowsFor(col.id).map((r) => (
              <div
                key={r.id}
                className={
                  'board-card' +
                  (drag?.rowId === r.id && drag.fromCol === col.id ? ' is-dragging' : '') +
                  (armedRow === r.id ? ' is-armed' : '')
                }
                onPointerDown={(e) => startDrag(e, r.id, col.id, r.title || 'Untitled')}
                onClick={(e) => {
                  // Do NOT open after a drag — otherwise every move jumps
                  // straight into the card.
                  if (consumeClick()) return;
                  openPage(e, r.id, { onNavigate, onOpenInNewTab });
                }}
                onAuxClick={(e) => openPageAux(e, r.id, { onNavigate, onOpenInNewTab })}
                // Right-click opens the card's own menu, the same one behind
                // the ⋯. On a board the card IS the object in front of you, and
                // aiming at a small mark in its corner to reach "open" or
                // "trash" is the slow way round.
                onContextMenu={(e) => {
                  e.preventDefault();
                  setMoveMenu(`${col.id}:${r.id}`);
                }}
              >
                <div className="board-card-top">
                  <div className="board-card-title">
                    {r.icon && <span className="inline-icon"><PageIcon icon={r.icon} size={14} /> </span>}
                    {r.title || 'Untitled'}
                  </div>
                  {/* OUTSIDE the title: it clamps to four lines, and anything
                      inside a clamped box is part of the text flow — the mark
                      landed between the words, wherever they happened to end.
                      One stack in the corner instead, agent at the FRONT: the
                      people are who it belongs to, the agent is who is touching
                      it right now, and right-now belongs on top. */}
                  <div className="card-marks">
                    <AgentDot pageId={r.id} />
                    {/* Who is on this card — one stack of faces, deduped
                        across all person fields (W126). */}
                    <PersonStack
                      values={schema
                        .filter((p) => p.type === 'person' && p.id !== groupBy)
                        .flatMap((p) => {
                          const v = r.props[p.id];
                          return Array.isArray(v) ? v.map(String) : [String(v ?? '')];
                        })}
                    />
                  </div>
                  {/* On a board the CARD is the object you have in front of
                      you, and for a long time the only thing this could do was
                      send it to another column — you could not open it from
                      here and you certainly could not throw it away. Touch
                      devices also cannot HTML5-drag, so the column list has to
                      stay whatever else joins it. */}
                  <div className="card-move" onClick={(e) => e.stopPropagation()}>
                    <button
                      className="card-move-btn"
                      title={t('More')}
                      onClick={() =>
                        setMoveMenu(moveMenu === `${col.id}:${r.id}` ? null : `${col.id}:${r.id}`)
                      }
                    >
                      ⋯
                    </button>
                    {moveMenu === `${col.id}:${r.id}` && (
                      <div className="menu card-move-menu">
                        <button
                          onClick={() => {
                            setMoveMenu(null);
                            onNavigate(r.id);
                          }}
                        >
                          <SquareArrowOutUpRight size={15} /> {t('Open')}
                        </button>
                        {/* The visible way to a second tab, as in the sidebar's
                            row menu: ⌘-click and middle-click work too, but a
                            menu that stays silent about them leaves most people
                            without a way to ask. */}
                        <button
                          onClick={() => {
                            setMoveMenu(null);
                            (onOpenInNewTab ?? onNavigate)(r.id);
                          }}
                        >
                          <ExternalLink size={15} /> {t('Open in new tab')}
                        </button>
                        {columns.filter((c) => !rowsFor(c.id).some((x) => x.id === r.id)).length > 0 && (
                          <div className="menu-label">{t('Move to')}</div>
                        )}
                        {columns
                          .filter((c) => !rowsFor(c.id).some((x) => x.id === r.id))
                          .map((c) => (
                            <button
                              key={c.id}
                              onClick={() => {
                                setMoveMenu(null);
                                onDrop(r.id, groupBy, c.id);
                              }}
                            >
                              <CornerUpRight size={15} /> {c.name}
                            </button>
                          ))}
                        <button
                          className="danger"
                          onClick={() => {
                            setMoveMenu(null);
                            void onTrashRow(r.id, r.title);
                          }}
                        >
                          <Trash2 size={15} /> {t('Move to trash')}
                        </button>
                      </div>
                    )}
                  </div>
                </div>
                {!!r.tags?.length && (
                  <div className="db-row-tags card-tags">
                    {r.tags.map((t) => (
                      <span key={t} className={'row-tag ' + tagColorClass(t, tagColors)}>#{t}</span>
                    ))}
                  </div>
                )}
                {!!commentCounts[r.id] && (
                  <div className="card-comments" title={commentCounts[r.id] + t(' open comments')}>
                    <MessageSquare size={11} /> {commentCounts[r.id]}
                  </div>
                )}
                <BoardCardProps
                  schema={schema}
                  row={r}
                  groupBy={groupBy}
                  expanded={openCards.has(r.id)}
                  onToggleExpand={() =>
                    setOpenCards((prev) => {
                      const next = new Set(prev);
                      if (next.has(r.id)) next.delete(r.id);
                      else next.add(r.id);
                      return next;
                    })
                  }
                  onSetProp={onSetProp}
                  onSetOptions={onSetOptions}
                />
              </div>
            ))}
          </div>
        </div>
      ))}

      {/* Die schwebende Karte folgt dem Zeiger. Als letztes Kind mit
          position:fixed so it sits above everything; pointer-events:none so it
          does not disturb hit testing underneath. */}
      {drag && drag.title && (
        <div
          className="board-drag-ghost"
          style={{ width: drag.width, left: drag.x - drag.dx, top: drag.y - drag.dy }}
        >
          {drag.title}
        </div>
      )}
    </div>
  );
}

// ---- Table ----

/** A column heading with the grip that resizes it.
 *
 *  The grip hangs off an inner BLOCK, not off the <th> itself. In a table with
 *  border-collapse: collapse the cell does not act as a containing block a
 *  browser will hit-test against: the handle painted in exactly the right place,
 *  the pointer went to the <th> underneath, and dragging did nothing at all
 *  while looking entirely correct. */
function ColHead({
  label,
  onResize,
  onReset,
}: {
  label: string;
  onResize: (e: React.PointerEvent<HTMLSpanElement>) => void;
  onReset: () => void;
}) {
  return (
    <span className="th-inner">
      {label}
      <span
        className="col-resize"
        title={t('Drag to resize, double-click to fit the content')}
        onPointerDown={onResize}
        onDoubleClick={onReset}
      />
    </span>
  );
}

/** The name column's key in colWidths — the title is not a property, so it has
 *  no id of its own. Nothing the interface generates can collide with it: prop
 *  ids are slugged to [a-z0-9-] (see SchemaEditor). An agent writing a schema
 *  over MCP could pick this exact string, since safePropID does allow '_', and
 *  then the two columns would share one width. Cosmetic, and not worth a
 *  guard that would have to be kept in step on both sides. */
const TITLE_COL = '__title';

function TableView({
  rows,
  schema,
  emptyLabel,
  tagColors,
  subItemProp,
  colWidths,
  onSetColWidths,
  onNavigate,
  onOpenInNewTab,
  onSetProp,
  onSetOptions,
}: {
  rows: Row[];
  schema: PropDef[];
  emptyLabel: string;
  tagColors: Record<string, string>;
  subItemProp?: string;
  colWidths?: Record<string, number>;
  onSetColWidths: (next: Record<string, number>) => void;
  onNavigate: (id: string) => void;
  onOpenInNewTab?: (id: string) => void;
  onSetProp: (rowId: string, propId: string, value: unknown) => void;
  onSetOptions: (propId: string, options: PropOption[]) => void;
}) {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  // The width being dragged right now. Kept out of the stored widths so the
  // drag is local and instant: writing to the view on every pointermove would
  // put one request per pixel on the wire and re-render the whole table.
  const [drag, setDrag] = useState<{ key: string; px: number } | null>(null);

  const widthOf = (key: string) =>
    drag?.key === key ? drag.px : colWidths?.[key];

  // A dragged column is pinned: width alone is a suggestion under auto layout,
  // so the three together are what actually hold it.
  const colStyle = (key: string): React.CSSProperties | undefined => {
    const px = widthOf(key);
    return px ? { width: px, minWidth: px, maxWidth: px } : undefined;
  };

  const startResize = (key: string) => (e: React.PointerEvent<HTMLSpanElement>) => {
    e.stopPropagation();
    // Deliberately NOT preventDefault: doing that on a pointerdown cancels the
    // compatibility mouse events behind it, dblclick included — and the
    // double-click that gives a column back to its content silently stopped
    // happening. Text selection is held off with a body class instead.
    const th = e.currentTarget.closest('th');
    const from = th ? th.getBoundingClientRect().width : 160;
    const x0 = e.clientX;
    const widthAt = (clientX: number) => Math.max(64, Math.round(from + clientX - x0));
    document.body.classList.add('is-col-resizing');
    const move = (ev: PointerEvent) => setDrag({ key, px: widthAt(ev.clientX) });
    // `pointercancel` as well as `pointerup`: on touch the browser can take the
    // gesture over (a long press, an OS swipe) and sends the cancel instead of
    // the up. Without it the body would keep `is-col-resizing` — no text
    // selectable anywhere — until a reload.
    const end = (ev: PointerEvent) => {
      document.removeEventListener('pointermove', move);
      document.removeEventListener('pointerup', end);
      document.removeEventListener('pointercancel', end);
      document.body.classList.remove('is-col-resizing');
      const px = widthAt(ev.clientX);
      setDrag(null);
      // A click that moved nothing is not a resize. Without this the two clicks
      // of a double-click each stored the unchanged width first, and the reset
      // behind them had to undo two writes nobody asked for.
      if (px !== colWidths?.[key] && !(px === Math.round(from) && !colWidths?.[key])) {
        onSetColWidths({ ...(colWidths ?? {}), [key]: px });
      }
    };
    document.addEventListener('pointermove', move);
    document.addEventListener('pointerup', end);
    document.addEventListener('pointercancel', end);
  };

  // Double-click gives a column back to the content: the entry is REMOVED
  // rather than set to some computed number, so it goes on adapting as rows
  // change instead of freezing at whatever it happened to be that day.
  const resetCol = (key: string) => () => {
    if (!colWidths?.[key]) return;
    const next = { ...colWidths };
    delete next[key];
    onSetColWidths(next);
  };
  const toggleTree = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });

  // When a sub-item (self-relation) prop is active, flatten the rows into a
  // DFS-ordered list carrying each row's tree depth + whether it has children,
  // honouring collapse state. Otherwise every row is a depth-0 leaf. A visited
  // set per branch prevents an accidental relation cycle from looping forever.
  const rowById = new Map(rows.map((r) => [r.id, r]));
  const childIdsOf = (r: Row): string[] =>
    subItemProp && Array.isArray(r.props[subItemProp])
      ? (r.props[subItemProp] as string[]).filter((cid) => rowById.has(cid))
      : [];
  const ordered: { row: Row; depth: number; hasKids: boolean }[] = [];
  if (subItemProp) {
    const childSet = new Set<string>();
    for (const r of rows) for (const cid of childIdsOf(r)) childSet.add(cid);
    const roots = rows.filter((r) => !childSet.has(r.id));
    const visit = (r: Row, depth: number, seen: Set<string>) => {
      const kids = childIdsOf(r);
      ordered.push({ row: r, depth, hasKids: kids.length > 0 });
      if (kids.length && !collapsed.has(r.id)) {
        for (const cid of kids) {
          if (!seen.has(cid)) visit(rowById.get(cid)!, depth + 1, new Set([...seen, r.id]));
        }
      }
    };
    for (const r of roots) visit(r, 0, new Set([r.id]));
  } else {
    for (const r of rows) ordered.push({ row: r, depth: 0, hasKids: false });
  }

  // Per-column footer aggregate: numeric columns (number/rollup/formula) show a
  // sum, everything else a count of filled cells — a lightweight Notion-style
  // calc row so a table gives totals at a glance.
  const footer = (p: PropDef): string => {
    const isNumeric = p.type === 'number' || p.type === 'rollup' || p.type === 'formula';
    if (isNumeric) {
      let sum = 0;
      let any = false;
      for (const r of rows) {
        const n = Number(r.props[p.id]);
        if (r.props[p.id] != null && r.props[p.id] !== '' && !Number.isNaN(n)) {
          sum += n;
          any = true;
        }
      }
      if (!any) return '';
      return 'Σ ' + String(Math.round(sum * 1e6) / 1e6);
    }
    const filled = rows.filter((r) => {
      const v = r.props[p.id];
      if (Array.isArray(v)) return v.length > 0;
      return v !== undefined && v !== '' && v !== null && v !== false;
    }).length;
    return filled ? t('{n} filled', { n: filled }) : '';
  };

  return (
    <div className="table-wrap">
      <table className="db-table">
        <thead>
          <tr>
            <th className="db-title-col" style={colStyle(TITLE_COL)}>
              <ColHead label={t('Name')} onResize={startResize(TITLE_COL)} onReset={resetCol(TITLE_COL)} />
            </th>
            {schema.map((p) => (
              <th key={p.id} style={colStyle(p.id)}>
                <ColHead label={p.name} onResize={startResize(p.id)} onReset={resetCol(p.id)} />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {ordered.map(({ row: r, depth, hasKids }) => (
            <tr key={r.id}>
              <td className="db-title-col" style={colStyle(TITLE_COL)}>
                <span className="db-title-inner" style={depth ? { paddingLeft: depth * 20 } : undefined}>
                  {subItemProp ? (
                    hasKids ? (
                      <button
                        className="db-tree-toggle"
                        onClick={() => toggleTree(r.id)}
                        aria-label={collapsed.has(r.id) ? t('Expand') : t('Collapse')}
                      >
                        {collapsed.has(r.id) ? '▸' : '▾'}
                      </button>
                    ) : (
                      <span className="db-tree-spacer" />
                    )
                  ) : null}
                  <button
                    className="db-title-link"
                    onClick={(e) => openPage(e, r.id, { onNavigate, onOpenInNewTab })}
                    onAuxClick={(e) => openPageAux(e, r.id, { onNavigate, onOpenInNewTab })}
                  >
                    {r.icon && <span className="inline-icon"><PageIcon icon={r.icon} size={14} /> </span>}
                    {r.title || 'Untitled'}
                  </button>
                  {/* Presence was drawn on the board card and nowhere else, so
                      anybody working in the table saw nothing at all — which is
                      most of the time. The mark belongs wherever a row is
                      listed, not only where it happens to be a card. */}
                  <AgentDot pageId={r.id} />
                </span>
                {!!r.tags?.length && (
                  <span className="db-row-tags">
                    {r.tags.map((t) => (
                      <span key={t} className={'row-tag ' + tagColorClass(t, tagColors)}>#{t}</span>
                    ))}
                  </span>
                )}
              </td>
              {schema.map((p) => (
                <td key={p.id} style={colStyle(p.id)}>
                  <PropertyValue
                    def={p}
                    value={r.props[p.id]}
                    onChange={(v) => onSetProp(r.id, p.id, v)}
                    onOptionsChange={(opts) => onSetOptions(p.id, opts)}
                    maxChips={2}
                  />
                </td>
              ))}
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td colSpan={schema.length + 1} className="db-empty">
                {emptyLabel}
              </td>
            </tr>
          )}
        </tbody>
        {rows.length > 0 && (
          <tfoot>
            <tr className="db-calc-row">
              {/* German, hard-coded, in an English-first product — and every
                  check read clean. The JSX rule structurally cannot see a
                  string with an expression inside it, and one word without an
                  umlaut is below the line rule's threshold. It was found by
                  reading the code for the wiki. */}
              <td className="db-title-col db-calc-cell">
                {plural(rows.length, '{n} row', '{n} rows')}
              </td>
              {schema.map((p) => (
                <td key={p.id} className="db-calc-cell">
                  {footer(p)}
                </td>
              ))}
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}

// ---- Column visibility control ----

function ColumnsControl({
  schema,
  view,
  onChange,
}: {
  schema: PropDef[];
  view: ViewDef;
  onChange: (patch: Partial<ViewDef>) => void;
}) {
  const [open, setOpen] = useState(false);
  const btnRef = useRef<HTMLButtonElement>(null);
  const [pos, setPos] = useState<React.CSSProperties | null>(null);
  useLayoutEffect(() => {
    if (!open || !btnRef.current) {
      setPos(null);
      return;
    }
    const r = btnRef.current.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    if (vw <= 640) {
      setPos({ position: 'fixed', left: 8, right: 8, bottom: 8, maxHeight: vh * 0.72 });
    } else {
      const width = 268;
      setPos({
        position: 'fixed',
        width,
        left: Math.max(8, Math.min(r.right - width, vw - width - 8)),
        top: r.bottom + 6,
        maxHeight: vh - r.bottom - 24,
      });
    }
  }, [open]);

  const hidden = new Set(view.hidden ?? []);
  const shown = schema.filter((p) => !hidden.has(p.id));
  const hiddenProps = schema.filter((p) => hidden.has(p.id));
  const toggle = (id: string) => {
    const next = new Set(hidden);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange({ hidden: [...next] });
  };
  const propRow = (p: PropDef, isHidden: boolean) => (
    <button key={p.id} className="col-row" onClick={() => toggle(p.id)}>
      <span className="col-row-ic">{propTypeIcon(p.type)}</span>
      <span className="col-row-name">{p.name}</span>
      {isHidden ? <EyeOff className="col-row-eye off" size={16} /> : <Eye className="col-row-eye" size={16} />}
    </button>
  );

  return (
    <div className="fs-controls">
      <button ref={btnRef} className={'btn-sm' + (hidden.size ? ' active' : '')} onClick={() => setOpen((o) => !o)}>
        <Eye size={14} /> {t('Columns')}
        {hidden.size ? ` (${shown.length}/${schema.length})` : ''}
      </button>
      {open && pos && (
        <Portal>
          <div className="fs-backdrop" onClick={() => setOpen(false)} />
          <div className="fs-popover col-popover" style={pos}>
            <div className="col-section-head">
              <span>{t('Shown')}</span>
              {shown.length > 0 && (
                <button className="col-bulk" onClick={() => onChange({ hidden: schema.map((p) => p.id) })}>
                  {t('Hide all')}
                </button>
              )}
            </div>
            {shown.map((p) => propRow(p, false))}
            {shown.length === 0 && <div className="col-empty">{t('Nothing shown')}</div>}
            <div className="col-section-head">
              <span>{t('Hidden')}</span>
              {hiddenProps.length > 0 && (
                <button className="col-bulk" onClick={() => onChange({ hidden: [] })}>
                  {t('Show all')}
                </button>
              )}
            </div>
            {hiddenProps.map((p) => propRow(p, true))}
            {hiddenProps.length === 0 && <div className="col-empty">{t('Nothing hidden')}</div>}
          </div>
        </Portal>
      )}
    </div>
  );
}

// ---- Calendar view ----

const ymd = toDayString;

// ---- Timeline / Gantt view ----
// Rows are positioned on a horizontal day-grid by a start date property, and
// span to an optional end date (else a single-day bar). The title column is
// sticky so it stays visible while the time axis scrolls.
// A first-look dot on a view's settings button, once per kind of view.
// Same localStorage key family the interface store uses, so a later shared
// helper reads what this wrote.
const HINT_PREFIX = 'salt:hint:';
function useSettingsHint(name: string): [boolean, () => void] {
  const [show, setShow] = useState(() => {
    try {
      return localStorage.getItem(HINT_PREFIX + name) !== '1';
    } catch {
      return false;
    }
  });
  const seen = () => {
    if (!show) return;
    try {
      localStorage.setItem(HINT_PREFIX + name, '1');
    } catch {
      /* best-effort persistence */
    }
    setShow(false);
  };
  return [show, seen];
}

function TimelineView({
  rows,
  schema,
  startProp,
  endProp,
  onNavigate,
  onOpenInNewTab,
  mode,
  onModeChange,
  onViewChange,
  onAddRow,
  onSetDate,
  hideLabels,
  onToggleLabels,
  startWithSettings,
  onSettingsShown,
}: {
  rows: Row[];
  schema: PropDef[];
  startProp: string;
  endProp: string;
  tagColors: Record<string, string>;
  onNavigate: (id: string) => void;
  onOpenInNewTab?: (id: string) => void;
  mode: SpanMode;
  onModeChange: (m: SpanMode) => void;
  onViewChange: (patch: Partial<ViewDef>) => void;
  onAddRow: (presetProps?: Record<string, unknown>) => void;
  onSetDate: (rowId: string, propId: string, iso: string) => void;
  hideLabels?: boolean;
  onToggleLabels?: () => void;
  startWithSettings?: boolean;
  onSettingsShown?: () => void;
}) {
  const DAY = 26; // px per day
  const YEARW = 140; // px per month in year mode
  const LABELW = 190; // sticky title column width
  // Collapsed, the title column takes no room at all and the grid keeps the
  // width: the bars carry their own titles, so the axis is what is left to look
  // at. `onToggleLabels` is absent where nothing may be collapsed (a public
  // link shows the owner's choice).
  const labelW = hideLabels ? 0 : LABELW;
  const dayNum = (iso: string) => {
    const [y, m, d] = iso.slice(0, 10).split('-').map(Number);
    return Math.floor(Date.UTC(y, m - 1, d) / 86400000);
  };
  const isoDay = (n: number) => new Date(n * 86400000).toISOString().slice(0, 10);
  // A date property may carry a time ("2026-06-12T14:30"). Stretching or
  // carrying a bar changes the DAY only: the time the row already had must
  // survive, or a 14:30 meeting dragged an afternoon silently becomes
  // midnight. The day is written through this so the suffix rides along.
  const isoDayKeepTime = (row: Row, propId: string, n: number) => {
    const raw = row.props[propId];
    const day = isoDay(n);
    return typeof raw === 'string' && raw.length > 10 ? day + raw.slice(10) : day;
  };

  type Item = { row: Row; start: number; end: number };
  const items: Item[] = [];
  for (const r of rows) {
    const sv = r.props[startProp];
    if (typeof sv !== 'string' || !sv) continue;
    const start = dayNum(sv);
    let end = start;
    if (endProp) {
      const ev = r.props[endProp];
      if (typeof ev === 'string' && ev) end = Math.max(start, dayNum(ev));
    }
    items.push({ row: r, start, end });
  }

  const today = dayNum(ymd(new Date()));
  const dataMin = items.length ? Math.min(...items.map((i) => i.start)) : today;
  const dataMax = items.length ? Math.max(...items.map((i) => i.end)) : today;
  const [cursor, setCursor] = useState(() =>
    today < dataMin - 3 ? dataMin : today > dataMax + 4 ? dataMax : today,
  );
  const [hovered, setHovered] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(!!startWithSettings);
  const [settingsHint, settingsSeen] = useSettingsHint('timeline-settings');
  // A click outside the settings popover closes it — the wrap holds the button
  // and the menu together, so the button's own click still toggles.
  const tlSettingsRef = useRef<HTMLDivElement>(null);
  useMenuDismiss(settingsOpen, tlSettingsRef, () => setSettingsOpen(false));
  // The flag belongs to the moment the view was made, not to the view: once the
  // popover has been open, a later remount must not open it again.
  useEffect(() => {
    if (startWithSettings) onSettingsShown?.();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [startWithSettings]);
  // Edge-resize drag: which row, which edge, which day it is over right now.
  // Persisted on pointer-up only; until then the bar is drawn from this.
  const [drag, setDrag] = useState<{ id: string; edge: 'start' | 'end' | 'move'; day: number } | null>(null);
  // Mirrored in a ref, so the end of a gesture reads the day it had reached from
  // here rather than from React state — see finish, which must not read a day
  // the NEXT gesture has already put into the state.
  const dragRef = useRef<typeof drag>(null);
  const setDragBoth = (v: typeof drag) => {
    dragRef.current = v;
    setDrag(v);
  };
  // A resize ends in a click: the browser sends one after the gesture even when
  // the finger barely moved, and cancelling the pointerdown does not reliably
  // stop it on touch. That click must not open the row whose edge was held. A
  // timestamp rather than a flag, because on touch the click may never arrive at
  // all — a flag left armed would then swallow the NEXT honest tap, which is a
  // bug of its own. The window is short and re-armed when the gesture ends, so
  // it covers a press of any length and expires on its own.
  const navGuard = useRef(0);
  const swallowClick = () => Date.now() - navGuard.current < 700;
  // The live gesture's own way out. A browser that takes a gesture over without
  // saying so leaves the previous drag running: its window listeners stay
  // attached, so it keeps following the NEXT finger and its finish can fire
  // during the next gesture. Starting one drag therefore ends any other first,
  // which is also why the handle below can promise "one at a time".
  const dragCleanup = useRef<((armGuard: boolean) => void) | null>(null);
  // While an edge is held, nothing on the page may start a text selection: the
  // browser would take the gesture over (see finish below) and, on iOS, leave a
  // callout sitting on top of the interface that swallows taps. The class goes
  // on the body rather than the bar so a drag that wanders off the bar is
  // covered, and the cleanup takes it off on unmount too — a stuck
  // `user-select: none` is exactly the bug it prevents.
  useEffect(() => {
    document.body.classList.toggle('is-tl-resizing', !!drag);
    return () => document.body.classList.remove('is-tl-resizing');
  }, [drag]);

  if (!startProp || !schema.some((p) => p.id === startProp && p.type === 'date')) {
    return (
      <div className="board-empty">
        {t('This timeline needs a {type} property as its start. Open ⚙ Properties to add one.', { type: t('Date') })}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="board-empty">
        {t('No entries with a date yet. Set a start date so they appear on the timeline.')}
      </div>
    );
  }

  // Visible window per mode. Day modes count days; year mode counts
  // fixed-width months.
  const yearOf = (n: number) => new Date(n * 86400000).getUTCFullYear();
  const monthOf = (n: number) => new Date(n * 86400000).getUTCMonth();
  let dayCols: number[] = [];
  let monthCols: { y: number; mo: number }[] = [];
  if (mode === 'day') {
    dayCols = [cursor];
  } else if (mode === 'week') {
    const ws = cursor - ((new Date(cursor * 86400000).getUTCDay() - firstWeekday() + 7) % 7);
    dayCols = Array.from({ length: 7 }, (_, k) => ws + k);
  } else if (mode === 'year') {
    const y = yearOf(cursor);
    monthCols = Array.from({ length: 12 }, (_, mo) => ({ y, mo }));
  } else {
    const nMonths = mode === '2months' ? 2 : 1;
    const m0 = { y: yearOf(cursor), mo: monthOf(cursor) };
    const firstDay = Math.floor(Date.UTC(m0.y, m0.mo, 1) / 86400000);
    const lastDay = Math.floor(Date.UTC(m0.y, m0.mo + nMonths, 1) / 86400000) - 1;
    dayCols = Array.from({ length: lastDay - firstDay + 1 }, (_, k) => firstDay + k);
  }
  const isYear = monthCols.length > 0;
  // Grid geometry is proportional so spans narrower than a month stretch to
  // the full width: day columns share the track equally (never narrower than
  // 26px — below that the track scrolls), year columns split it in twelve.
  const gridMin = isYear ? monthCols.length * YEARW : dayCols.length * DAY;
  // Handles live only on day-granular spans of a ranged timeline: without an
  // end property there is nothing to stretch to, and month columns are too
  // coarse for day precision.
  const resizable = !!endProp && !isYear;
  // One gesture, three meanings: pulling the left edge, pulling the right edge,
  // or carrying the whole bar. `move` is the delta in days, the other two are
  // the day the edge has reached.
  const beginBarDrag = (e: React.PointerEvent, id: string, edge: 'start' | 'end' | 'move') => {
    // A control on the bar is pressed, not carried.
    if (edge === 'move' && (e.target as HTMLElement).closest('button, .tl-resize')) return;
    e.stopPropagation();
    // On a finger the drag starts with a hold, exactly like a board card: a
    // swipe that starts on a bar is a scroll of the axis, not a move, and the
    // fingers that scroll must never feel a bar stick to them. The timer below
    // arms the same gesture a mouse starts at once.
    // The React event is dead by the time the timer below runs (its
    // currentTarget reads null), so capture what the armed half needs now.
    const press = {
      track: (e.currentTarget as HTMLElement).closest('.tl-track') as HTMLElement | null,
      pointerType: e.pointerType,
      x: e.clientX,
    };
    if (e.pointerType === 'touch' || e.pointerType === 'pen') {
      const x0 = press.x;
      const y0 = e.clientY;
      let settled = false;
      const disarm = () => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timer);
        window.removeEventListener('pointermove', watch);
        window.removeEventListener('pointerup', giveUp);
        window.removeEventListener('pointercancel', giveUp);
      };
      const watch = (ev: PointerEvent) => {
        // Past the wobble: it was a scroll, and the browser — whose
        // touch-action lets it scroll from here — already owns it.
        if (Math.hypot(ev.clientX - x0, ev.clientY - y0) > 10) disarm();
      };
      const giveUp = () => disarm();
      const timer = window.setTimeout(() => {
        if (settled) return;
        settled = true;
        window.removeEventListener('pointermove', watch);
        window.removeEventListener('pointerup', giveUp);
        window.removeEventListener('pointercancel', giveUp);
        beginArmed(press, id, edge);
      }, 320);
      window.addEventListener('pointermove', watch);
      window.addEventListener('pointerup', giveUp);
      window.addEventListener('pointercancel', giveUp);
      return;
    }
    beginArmed(press, id, edge);
  };
  // What a mouse starts at once and a finger starts after the hold above.
  // Plain values, never the React event: the finger's half runs a hold later,
  // when the event's currentTarget already reads null.
  const beginArmed = (
    press: { track: HTMLElement | null; pointerType: string; x: number },
    id: string,
    edge: 'start' | 'end' | 'move',
  ) => {
    // Deliberately NOT preventDefault, for the same reason the table's column
    // resize does not: cancelling a pointerdown cancels the compatibility events
    // behind it, and on a phone it is one more thing between the finger and the
    // browser's own gesture handling. What it used to hold off is held off
    // elsewhere — `touch-action` lets the axis scroll from a bar or a handle
    // until the hold arms the drag, `user-select: none` stops the selection,
    // and the click guard below stops the click.
    // Whatever was dragging stops here (see dragCleanup): one gesture at a time,
    // so a leaked one cannot ride along with this one. No click guard from that
    // end — the click that follows belongs to this press.
    dragCleanup.current?.(false);
    // An edge press is never a click, so the guard goes up at the press — even
    // if the end of the gesture is never reported. A press in the middle of a
    // bar is a move, and a move that goes nowhere is just a click: the guard
    // for that one is armed at the end, and only if the bar actually moved.
    if (edge !== 'move') navGuard.current = Date.now();
    const { track } = press;
    const it = items.find((i) => i.row.id === id);
    if (!track || !it) return;
    const rect = track.getBoundingClientRect();
    const rawEnd = endProp ? it.row.props[endProp] : undefined;
    const rowHasEnd = typeof rawEnd === 'string' && !!rawEnd;
    const fixed = edge === 'start' ? it.end : it.start;
    const x0 = press.x;
    setDragBoth({ id, edge, day: edge === 'move' ? 0 : edge === 'start' ? it.start : it.end });
    const move = (ev: PointerEvent) => {
      if (edge === 'move') {
        const perDay = rect.width / dayCols.length;
        setDragBoth({ id, edge, day: Math.round((ev.clientX - x0) / perDay) });
        return;
      }
      const n = Math.min(
        dayCols[dayCols.length - 1],
        Math.max(dayCols[0], dayCols[0] + Math.floor(((ev.clientX - rect.left) / rect.width) * dayCols.length)),
      );
      setDragBoth({ id, edge, day: edge === 'start' ? Math.min(n, fixed) : Math.max(n, fixed) });
    };
    // The stretched bar and the date badge are drawn from `drag` alone, so every
    // way this gesture can end has to clear it. A finger lifting is `pointerup`;
    // the browser taking the gesture over — a long press that turns into a text
    // selection, a phone call, an OS swipe — is `pointercancel`, and on touch it
    // arrives INSTEAD of the up. Listening for one and not the other leaves the
    // bar stuck mid-drag with its badge hanging over the timeline, and the next
    // stray touch keeps moving it. Where the drag got to is what the user saw,
    // so a cancelled drag stores the day it had reached, exactly like a lift.
    //
    // `blur` and a hidden document are the same story one level down: the gesture
    // is over the moment the app stops being what the finger is on.
    const finish = (armGuard: boolean) => {
      window.removeEventListener('touchmove', onTouchMove);
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', onEnd);
      window.removeEventListener('pointercancel', onEnd);
      window.removeEventListener('blur', onEnd);
      window.removeEventListener('pointerdown', onOtherDown, true);
      document.removeEventListener('visibilitychange', onHide);
      if (dragCleanup.current === finish) dragCleanup.current = null;
      // The click that follows a gesture belongs to it and must not open the row
      // whose edge was held — so the guard is armed here as well as at the press,
      // which covers a press of any length. Not when the gesture was ended by a
      // NEW press though: the click that follows that one is that press's own.
      // The move decides for itself, above.
      if (armGuard && edge !== 'move') navGuard.current = Date.now();
      // Read from the ref, not from the state updater: React may run an updater
      // more than once and this one writes to the server, and — the reason this
      // is a ref at all — a gesture that ends late must not commit whatever the
      // NEXT gesture has put into the state in the meantime.
      const reached = dragRef.current && dragRef.current.id === id ? dragRef.current : null;
      setDragBoth(null);
      if (!reached) return;
      if (reached.edge === 'move') {
        // Nothing moved: the click that follows is an honest one.
        if (reached.day === 0) {
          navGuard.current = 0;
          return;
        }
        navGuard.current = Date.now();
        void onSetDate(id, startProp, isoDayKeepTime(it.row, startProp, it.start + reached.day));
        // A move keeps whatever the row already had: a one-day row stays one
        // day instead of growing an end nobody asked for.
        if (endProp && rowHasEnd) void onSetDate(id, endProp, isoDayKeepTime(it.row, endProp, it.end + reached.day));
        return;
      }
      const movedProp = reached.edge === 'start' ? startProp : endProp;
      void onSetDate(id, movedProp, isoDayKeepTime(it.row, movedProp, reached.day));
      // Pulling the start of a one-day row makes it a range: the end keeps the
      // day the row started on, so the bar grows rather than slides.
      if (reached.edge === 'start' && endProp && !rowHasEnd) {
        void onSetDate(id, endProp, isoDay(it.start));
      }
    };
    const onEnd = () => finish(true);
    const onHide = () => {
      if (document.hidden) finish(true);
    };
    // A gesture can also end without the browser ever saying so — a phone that
    // takes the touch over for a system swipe, a lost pointerup — and then the
    // badge hangs over the timeline and the bar follows the next finger, which
    // reads as "nothing on the page answers any more" until a reload. A finger
    // that is gone cannot press again, so ANY press that starts after this one
    // proves the old gesture is over: it ends here, and that press goes on to do
    // whatever it was meant to do. (No pointer-id comparison: a press is a press,
    // and the ids of a finger and a mouse are not comparable anyway.)
    const onOtherDown = () => finish(false);
    dragCleanup.current = finish;
    // An armed finger drag must not scroll along with the pointer. This cannot
    // go through `touch-action` — read when the gesture begins, changing it
    // later does nothing to the running gesture — so the hard way, like the
    // board's: passive false, so preventDefault is allowed.
    const touchArmed = press.pointerType === 'touch' || press.pointerType === 'pen';
    const onTouchMove = (ev: TouchEvent) => {
      ev.preventDefault();
    };
    if (touchArmed) window.addEventListener('touchmove', onTouchMove, { passive: false });
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', onEnd);
    window.addEventListener('pointercancel', onEnd);
    window.addEventListener('blur', onEnd);
    window.addEventListener('pointerdown', onOtherDown, true);
    document.addEventListener('visibilitychange', onHide);
  };

  // Month header segments across the visible range, in percent of the grid.
  const months: { label: string; left: string; width: string; from: number; to: number }[] = [];
  if (!isYear) {
    const min = dayCols[0];
    const max = dayCols[dayCols.length - 1];
    const n = dayCols.length;
    let c = min;
    while (c <= max) {
      const d = new Date(c * 86400000);
      const y = d.getUTCFullYear();
      const mo = d.getUTCMonth();
      const nextMonthDay = Math.floor(Date.UTC(y, mo + 1, 1) / 86400000);
      const segEnd = Math.min(nextMonthDay - 1, max);
      months.push({
        label: formatMonthYear(y, mo),
        left: `${((c - min) / n) * 100}%`,
        width: `${((segEnd - c + 1) / n) * 100}%`,
        from: c,
        to: segEnd,
      });
      c = nextMonthDay;
    }
  } else {
    monthCols.forEach((m, i) => {
      const from = Math.floor(Date.UTC(m.y, m.mo, 1) / 86400000);
      const to = Math.floor(Date.UTC(m.y, m.mo + 1, 1) / 86400000) - 1;
      months.push({
        label: formatMonthYear(m.y, m.mo),
        left: `${(i / monthCols.length) * 100}%`,
        width: `${(1 / monthCols.length) * 100}%`,
        from,
        to,
      });
    });
  }
  // Saturday and Sunday, in the same UTC day-number space as everything else
  // here: the weekend a Gantt is read against, whatever a region calls the start
  // of its week.
  const isWeekend = (n: number) => {
    const wd = new Date(n * 86400000).getUTCDay();
    return wd === 0 || wd === 6;
  };
  // The weekends of the visible range, as runs, so the body draws one band per
  // weekend instead of one per day. Percent of the grid, like every other
  // position here — the day columns share the track equally.
  const weekends: { left: string; width: string }[] = [];
  if (!isYear) {
    const n = dayCols.length;
    let i = 0;
    while (i < n) {
      if (!isWeekend(dayCols[i])) {
        i++;
        continue;
      }
      let j = i;
      while (j + 1 < n && isWeekend(dayCols[j + 1])) j++;
      weekends.push({ left: `${(i / n) * 100}%`, width: `${((j - i + 1) / n) * 100}%` });
      i = j + 1;
    }
  }
  const todayLeft = ((): string | null => {
    if (!isYear) {
      if (today < dayCols[0] || today > dayCols[dayCols.length - 1]) return null;
      return `${((today - dayCols[0]) / dayCols.length) * 100}%`;
    }
    const mi = monthCols.findIndex((m) => m.y === yearOf(today) && m.mo === monthOf(today));
    if (mi < 0) return null;
    const mStart = Math.floor(Date.UTC(monthCols[mi].y, monthCols[mi].mo, 1) / 86400000);
    const mDays = Math.floor(Date.UTC(monthCols[mi].y, monthCols[mi].mo + 1, 1) / 86400000) - mStart;
    return `${((mi + (today - mStart) / mDays) / monthCols.length) * 100}%`;
  })();

  const title =
    mode === 'day'
      ? formatDay(isoDay(cursor))
      : mode === 'week'
        ? formatDay(isoDay(dayCols[0])) + ' - ' + formatDay(isoDay(dayCols[dayCols.length - 1]))
        : mode === 'year'
          ? String(yearOf(cursor))
          : dayCols.length > 0 && monthOf(dayCols[0]) === monthOf(dayCols[dayCols.length - 1])
            ? formatMonth(yearOf(dayCols[0]), monthOf(dayCols[0]), 'long')
            : formatMonth(yearOf(dayCols[0]), monthOf(dayCols[0]), 'long') +
              ' - ' +
              formatMonth(yearOf(dayCols[dayCols.length - 1]), monthOf(dayCols[dayCols.length - 1]), 'long');
  const step = (delta: number) => {
    if (mode === 'day') setCursor((c) => c + delta);
    else if (mode === 'week') setCursor((c) => c + delta * 7);
    else if (mode === 'year')
      setCursor((c) => {
        const d = new Date(c * 86400000);
        return Math.floor(Date.UTC(d.getUTCFullYear() + delta, d.getUTCMonth(), 1) / 86400000);
      });
    // Month modes always slide by one month — even showing two — so no month
    // is ever skipped over.
    else
      setCursor((c) => {
        const d = new Date(c * 86400000);
        return Math.floor(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + delta, 1) / 86400000);
      });
  };
  const goToday = () => setCursor(today);

  // Bar geometry in percent of the grid, clamped to the window. Null when
  // the item is fully outside.
  const barGeom = (it: Item) => {
    if (!isYear) {
      const n = dayCols.length;
      const s = Math.max(it.start, dayCols[0]);
      const e = Math.min(it.end, dayCols[n - 1]);
      if (e < s) return null;
      return { left: `${((s - dayCols[0]) / n) * 100}%`, width: `calc(${(((e - s + 1) / n) * 100).toFixed(4)}% - 4px)` };
    }
    const ser = (n: number) => yearOf(n) * 12 + monthOf(n);
    const base = yearOf(cursor) * 12;
    const a = Math.max(0, ser(it.start) - base);
    const b = Math.min(11, ser(it.end) - base);
    if (b < a) return null;
    return { left: `${(a / 12) * 100}%`, width: `calc(${(((b - a + 1) / 12) * 100).toFixed(4)}% - 4px)` };
  };
  const covers = (it: Item, n: number) =>
    isYear
      ? yearOf(n) * 12 + monthOf(n) >= yearOf(it.start) * 12 + monthOf(it.start) &&
        yearOf(n) * 12 + monthOf(n) <= yearOf(it.end) * 12 + monthOf(it.end)
      : n >= it.start && n <= it.end;
  const hoveredItem = hovered ? items.find((i) => i.row.id === hovered) ?? null : null;
  // While resizing, the highlight follows the dragged edge rather than the
  // stored span.
  const spanItem = drag
    ? (() => {
        const base = items.find((i) => i.row.id === drag.id);
        if (!base) return hoveredItem;
        return {
          ...base,
          start: drag.edge === 'start' ? drag.day : base.start,
          end: drag.edge === 'end' ? drag.day : base.end,
        };
      })()
    : hoveredItem;
  const tipFor = (row: Row, it: Item) => {
    const sv = typeof row.props[startProp] === 'string' ? (row.props[startProp] as string) : '';
    const ev = endProp && typeof row.props[endProp] === 'string' ? (row.props[endProp] as string) : '';
    return (
      (row.title || t('Untitled')) +
      (sv ? ' - ' + formatDay(sv) + (it.end !== it.start && ev ? ' → ' + formatDay(ev) : '') : '')
    );
  };

  const dateProps = schema.filter((p) => p.type === 'date');

  return (
    <div className="timeline">
      <div className="tl-controls">
        {onToggleLabels && (
          <button
            className="btn-sm tl-collapse"
            onClick={onToggleLabels}
            aria-label={hideLabels ? t('Show the title column') : t('Hide the title column')}
            title={hideLabels ? t('Show the title column') : t('Hide the title column')}
          >
            {hideLabels ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />}
          </button>
        )}
        <button className="btn-sm" onClick={() => step(-1)} aria-label={t('Previous month')}>‹</button>
        <span className="tl-title">{title}</span>
        <button className="btn-sm" onClick={() => step(1)} aria-label={t('Next month')}>›</button>
        <button className="btn-sm" onClick={goToday}>{t('Today')}</button>
        <div className="tl-right">
          <div className="tl-settings-wrap" ref={tlSettingsRef}>
            <button
              className={'btn-sm' + (settingsHint ? ' has-dot' : '')}
              title={t('Timeline settings: start and end')}
              onClick={() => {
                setSettingsOpen((o) => !o);
                settingsSeen();
              }}
              aria-expanded={settingsOpen}
            >
              <Settings2 size={14} />
            </button>
            {settingsOpen && (
              <AnchoredMenu className="menu tl-settings-menu">
                <div className="tl-menu-head">{t('Timeline settings')}</div>
                <label className="tl-settings-row">
                  <span>{t('Start')}</span>
                  <select
                    className="subitems-select"
                    value={startProp}
                    onChange={(e) => onViewChange({ dateProp: e.target.value || undefined })}
                  >
                    <option value="">{t('Start: —')}</option>
                    {dateProps.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="tl-settings-row">
                  <span>{t('End')}</span>
                  <select
                    className="subitems-select"
                    value={endProp}
                    onChange={(e) => onViewChange({ endDateProp: e.target.value || undefined })}
                  >
                    <option value="">{t('End: (none)')}</option>
                    {dateProps.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </label>
              </AnchoredMenu>
            )}
          </div>
          <ModePicker mode={mode} onChange={onModeChange} />
        </div>
      </div>
      <div className="tl-scroll">
        <div className="tl-inner" style={{ minWidth: labelW + gridMin }}>
          <div className="tl-header">
            <div className="tl-corner" style={{ width: labelW }} />
            <div className="tl-head-right">
              <div className="tl-months">
                {months.map((m, i) => (
                  <div
                    key={i}
                    className="tl-month"
                    style={{ left: m.left, width: m.width }}
                  >
                    {m.label}
                  </div>
                ))}
                {todayLeft && <div className="tl-today-tick" style={{ left: todayLeft }} title={t('Today')} />}
              </div>
              {!isYear && (
                <div
                  className="tl-days"
                  style={{ gridTemplateColumns: `repeat(${dayCols.length}, minmax(26px, 1fr))` }}
                >
                  {dayCols.map((n) => (
                    <div
                      key={n}
                      className={
                        'tl-day' +
                        (isWeekend(n) ? ' is-weekend' : '') +
                        (n === today ? ' is-today' : '') +
                        (spanItem && covers(spanItem, n) ? ' is-span' : '') +
                        (spanItem && covers(spanItem, n) && !covers(spanItem, n - 1) ? ' is-span-first' : '') +
                        (spanItem && covers(spanItem, n) && !covers(spanItem, n + 1) ? ' is-span-last' : '')
                      }
                    >
                      {new Date(n * 86400000).getUTCDate()}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
          <div className="tl-body">
            {items.map((it) => {
              const { row } = it;
              const label = (
                <div
                  className={'tl-label' + (hovered === row.id ? ' is-hovered' : '')}
                  style={{ width: labelW }}
                  onClick={(e) => openPage(e, row.id, { onNavigate, onOpenInNewTab })}
                  onAuxClick={(e) => openPageAux(e, row.id, { onNavigate, onOpenInNewTab })}
                  // The mirror of the bar's hover below: lighting the title
                  // lights the bar, so the pair reads as one thing from either
                  // end.
                  onMouseEnter={() => setHovered(row.id)}
                  onMouseLeave={() => setHovered((h) => (h === row.id ? null : h))}
                  title={tipFor(row, it)}
                >
                  {row.icon && (
                    <span className="inline-icon">
                      <PageIcon icon={row.icon} size={14} />
                    </span>
                  )}
                  <span className="tl-label-text">{row.title || 'Untitled'}</span>
                </div>
              );
              const g = barGeom(it);
              // A row wholly outside the window keeps its label and loses only
              // its bar — but the track itself, with the weekend bands and the
              // today line, stays: they belong to the window, not to the row,
              // and a row without them reads as a hole cut out of the axis.
              const trackChrome = (
                <>
                  {weekends.map((w, k) => (
                    <div key={k} className="tl-weekend" style={{ left: w.left, width: w.width }} />
                  ))}
                  {todayLeft && !isYear && <div className="tl-today-line" style={{ left: todayLeft }} />}
                </>
              );
              // Outside the window a row keeps its label and loses only its
              // bar: dropping the row entirely hid entries the moment a saved
              // timeline opened on a single month.
              if (!g) {
                return (
                  <div key={row.id} className="tl-row" data-tl-row={row.id}>
                    {!hideLabels && label}
                    <div className="tl-track">{trackChrome}</div>
                  </div>
                );
              }
              // While dragging, the bar follows the pointer; the dates persist
              // on release only.
              const dg =
                drag && drag.id === row.id
                  ? barGeom({
                      ...it,
                      start: drag.edge === 'start' ? drag.day : it.start,
                      end: drag.edge === 'end' ? drag.day : it.end,
                    })
                  : null;
              const geom = dg ?? g;
              return (
                <div key={row.id} className="tl-row" data-tl-row={row.id}>
                  {!hideLabels && label}
                  <div className="tl-track">
                    {trackChrome}
                    <div
                      className={
                        'tl-bar' +
                        (drag && drag.id === row.id ? ' is-dragging' : '') +
                        (hovered === row.id ? ' is-hovered' : '')
                      }
                      style={{ left: geom.left, width: geom.width }}
                      onPointerDown={(e) => beginBarDrag(e, row.id, 'move')}
                      onClick={(e) => {
                        if (swallowClick()) return;
                        openPage(e, row.id, { onNavigate, onOpenInNewTab });
                      }}
                      onAuxClick={(e) => openPageAux(e, row.id, { onNavigate, onOpenInNewTab })}
                      onMouseEnter={() => setHovered(row.id)}
                      onMouseLeave={() => setHovered((h) => (h === row.id ? null : h))}
                      title={tipFor(row, it)}
                    >
                      <span className="tl-bar-label">{row.title || 'Untitled'}</span>
                      {resizable && (
                        <>
                          {/* The browser's own long-press menu is what takes the
                              drag away in the first place; on a handle it has
                              nothing useful to offer, so it is never shown. */}
                          <span
                            className="tl-resize tl-resize-l"
                            onPointerDown={(e) => beginBarDrag(e, row.id, 'start')}
                            onContextMenu={(e) => e.preventDefault()}
                          />
                          <span
                            className="tl-resize tl-resize-r"
                            onPointerDown={(e) => beginBarDrag(e, row.id, 'end')}
                            onContextMenu={(e) => e.preventDefault()}
                          />
                        </>
                      )}
                    </div>
                    {/* The live date hangs off an edge being pulled. A bar
                        being carried does not need it: the bar itself is
                        already under the pointer, and the date only got in the
                        way of seeing where it would land. */}
                    {drag && drag.id === row.id && drag.edge !== 'move' && (
                      <span
                        className="tl-drag-tip"
                        style={{
                          left:
                            drag.edge === 'start'
                              ? geom.left
                              : `calc(${geom.left} + ${geom.width})`,
                        }}
                      >
                        {formatDay(isoDay(drag.day), 'dayMonth')}
                      </span>
                    )}
                  </div>
                </div>
              );
            })}
            <div className="tl-row">
              {!hideLabels && (
                <div className="tl-label tl-add" style={{ width: LABELW }}>
                  <button className="btn-sm" onClick={() => void onAddRow({ [startProp]: isoDay(cursor) })}>
                    <Plus size={14} /> {t('New')}
                  </button>
                </div>
              )}
              <div className="tl-track" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// Day/Week/Month/2-months/Year switch shared by the calendar and the
// timeline headers: same control, same place, same labels.
function ModePicker({ mode, onChange }: { mode: SpanMode; onChange: (m: SpanMode) => void }) {
  const [open, setOpen] = useState(false);
  const label =
    mode === 'day'
      ? t('Day')
      : mode === 'week'
        ? t('Week')
        : mode === 'year'
          ? t('Year')
          : mode === '2months'
            ? t('2 months')
            : t('Month');
  const pick = (m: SpanMode) => {
    setOpen(false);
    if (m !== mode) onChange(m);
  };
  return (
    <div className="cal-range-wrap">
      <button className="btn-sm" onClick={() => setOpen((o) => !o)} aria-haspopup="listbox" aria-expanded={open}>
        {label} <ChevronDown size={14} />
      </button>
      {open && (
        <>
          <div className="fs-backdrop" onClick={() => setOpen(false)} />
          <div className="menu view-tab-menu" role="listbox">
            {(['day', 'week', 'month', '2months', 'year'] as const).map((m) => (
              <button
                key={m}
                role="option"
                aria-selected={mode === m}
                onClick={() => pick(m)}
              >
                {m === 'day'
                  ? t('Day')
                  : m === 'week'
                    ? t('Week')
                    : m === 'year'
                      ? t('Year')
                      : m === '2months'
                        ? t('2 months')
                        : t('Month')}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function CalendarView({
  rows,
  schema,
  dateProp,
  tagColors,
  onNavigate,
  onOpenInNewTab,
}: {
  rows: Row[];
  schema: PropDef[];
  dateProp: string;
  tagColors: Record<string, string>;
  onNavigate: (id: string) => void;
  onOpenInNewTab?: (id: string) => void;
}) {
  const [month, setMonth] = useState(() => {
    const n = new Date();
    return new Date(n.getFullYear(), n.getMonth(), 1);
  });

  if (!dateProp || !schema.some((p) => p.id === dateProp && p.type === 'date')) {
    return (
      <div className="board-empty">
        {t('This calendar needs a {type} property. Open ⚙ Properties to add one.', { type: t('Date') })}
      </div>
    );
  }

  // Bucket rows by their YYYY-MM-DD date value.
  const byDate = new Map<string, Row[]>();
  for (const r of rows) {
    const v = r.props[dateProp];
    if (typeof v !== 'string' || !v) continue;
    const key = v.slice(0, 10);
    (byDate.get(key) ?? byDate.set(key, []).get(key)!).push(r);
  }

  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  // Where the week starts is a property of the language, not of the code:
  // Monday across most of Europe, Sunday in the US and Japan, Saturday in much
  // of the Arab world. This used to be hardcoded to Monday.
  const startWeekday = (first.getDay() - firstWeekday() + 7) % 7;
  const daysInMonth = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate();
  const cells: (Date | null)[] = [];
  for (let i = 0; i < startWeekday; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(new Date(month.getFullYear(), month.getMonth(), d));
  while (cells.length % 7 !== 0) cells.push(null);

  const today = ymd(new Date());
  const monthLabel = formatMonth(month.getFullYear(), month.getMonth(), 'long');
  const step = (delta: number) => setMonth(new Date(month.getFullYear(), month.getMonth() + delta, 1));

  return (
    <div className="calendar">
      <div className="calendar-head">
        <button className="btn-sm" onClick={() => step(-1)} aria-label={t('Previous month')}>‹</button>
        <span className="calendar-title">{monthLabel}</span>
        <button className="btn-sm" onClick={() => step(1)} aria-label={t('Next month')}>›</button>
        <button className="btn-sm" onClick={() => setMonth(new Date(new Date().getFullYear(), new Date().getMonth(), 1))}>{t('Today')}</button>
      </div>
      <div className="calendar-grid">
        {weekdayNames().map((d) => (
          <div key={d} className="calendar-dow">{d}</div>
        ))}
        {cells.map((d, i) => {
          const key = d ? ymd(d) : '';
          const dayRows = d ? byDate.get(key) ?? [] : [];
          return (
            <div key={i} className={'calendar-cell' + (d ? '' : ' empty') + (key === today ? ' today' : '')}>
              {d && <div className="calendar-daynum">{d.getDate()}</div>}
              {dayRows.map((r) => (
                <button
                  key={r.id}
                  className="calendar-event"
                  onClick={(e) => openPage(e, r.id, { onNavigate, onOpenInNewTab })}
                  onAuxClick={(e) => openPageAux(e, r.id, { onNavigate, onOpenInNewTab })}
                  title={r.title}
                >
                  {r.icon && <span className="inline-icon"><PageIcon icon={r.icon} size={14} /> </span>}
                  {r.title || 'Untitled'}
                  {!!r.tags?.length && (
                    <span className="cal-event-tags">
                      {r.tags.map((t) => (
                        <span key={t} className={'row-tag ' + tagColorClass(t, tagColors)}>#{t}</span>
                      ))}
                    </span>
                  )}
                </button>
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

export type { Row, Page };
