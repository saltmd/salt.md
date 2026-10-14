import { useCallback, useEffect, useMemo, useState } from 'react';
import { CalendarDays, Columns3, LayoutGrid, List, Lock, Table2 } from 'lucide-react';
import { api, ApiError } from '../api';
import { plural, t } from '../i18n';
import { firstWeekday, formatDay, formatMonth, formatMonthYear, toDayString, weekdayNames } from '../format';
import { PageIcon } from '../pageIcon';
import { tagColorClass } from '../tags';
import PropertyValue from './PropertyValue';
import GalleryView from './GalleryView';
import ListView from './ListView';
import type { PropDef, PublicCollectionConfig, ViewDef } from '../types';

interface Row {
  id: string;
  title: string;
  icon: string;
  cover: string;
  props: Record<string, unknown>;
  tags?: string[];
}

// Standalone, unauthenticated collection page served at /public/{token}.
// Mirrors the in-app collection look (tabs, table/board/gallery/list) but is
// strictly read-only: no creating, no editing, no drag. Row detail opens only
// when the owner enabled it (allow_detail).
export default function PublicCollection({ token }: { token: string }) {
  const [password, setPassword] = useState('');
  const [needPassword, setNeedPassword] = useState(false);
  const [pwWrong, setPwWrong] = useState(false);
  const [state, setState] = useState<'loading' | 'ready' | 'notfound'>('loading');
  const [cfg, setCfg] = useState<PublicCollectionConfig | null>(null);
  const [viewId, setViewId] = useState('');
  const [rows, setRows] = useState<Row[]>([]);
  const [related, setRelated] = useState<Record<string, { title: string; icon: string }>>({});
  const [total, setTotal] = useState(0);
  const [rowsLoading, setRowsLoading] = useState(false);
  const [detail, setDetail] = useState<{ id: string; title: string; icon: string; content: string; props: Record<string, unknown> } | null>(null);

  const load = useCallback(
    async (pw?: string) => {
      setState('loading');
      setPwWrong(false);
      try {
        const c = await api.publicCollection(token, pw || undefined);
        setCfg(c);
        setViewId(c.views[0]?.id ?? '');
        setNeedPassword(false);
        setState('ready');
        // A working password is remembered for this tab only: a reload
        // reopens straight away, a new tab or a closed browser asks again.
        // The server keeps no session — this never leaves the browser.
        if (pw) {
          try {
            sessionStorage.setItem(`salt:share-pw:${token}`, pw);
          } catch {
            // Private mode — every visit asks, as before.
          }
        }
      } catch (e) {
        if (e instanceof ApiError && e.status === 403) {
          setNeedPassword(true);
          setPwWrong(!!pw);
          setState('ready');
          try {
            sessionStorage.removeItem(`salt:share-pw:${token}`);
          } catch {
            // Nothing stored, nothing to forget.
          }
          return;
        }
        setState('notfound');
      }
    },
    [token],
  );

  useEffect(() => {
    // A ?pw= query opens a password link directly (the API accepts it too);
    // a wrong one lands on the dialog with the value ready to correct.
    // Consumed once: the secret moves from the address into memory, so no
    // history entry and no glance at the bar carries it further. Documents
    // never need this — their gate POSTs the password in the form body.
    // Without ?pw=, a password remembered by this tab (see load) reopens
    // the link without asking again.
    const params = new URLSearchParams(window.location.search);
    let q = params.get('pw');
    if (q) {
      params.delete('pw');
      const rest = params.toString();
      history.replaceState(null, '', window.location.pathname + (rest ? '?' + rest : '') + window.location.hash);
      setPassword(q);
    } else {
      try {
        q = sessionStorage.getItem(`salt:share-pw:${token}`);
      } catch {
        q = null;
      }
      if (q) setPassword(q);
    }
    void load(q || undefined);
  }, [load, token]);

  const view: ViewDef | undefined = useMemo(
    () => cfg?.views.find((v) => v.id === viewId) ?? cfg?.views[0],
    [cfg, viewId],
  );
  const schema = useMemo(() => cfg?.schema ?? [], [cfg]);
  const hidden = useMemo(() => new Set(view?.hidden ?? []), [view]);
  const visibleSchema = useMemo(() => schema.filter((p) => !hidden.has(p.id)), [schema, hidden]);

  useEffect(() => {
    if (!cfg || !view) return;
    let alive = true;
    setRowsLoading(true);
    (async () => {
      let acc: Row[] = [];
      let total = 0;
      let rel: Record<string, { title: string; icon: string }> = {};
      for (;;) {
        const res = await api.publicCollectionRows(token, view.id, acc.length, password || undefined);
        if (!alive) return;
        total = res.total;
        rel = { ...rel, ...res.related };
        acc = [...acc, ...res.rows.map((r) => ({ ...r, props: r.props || {} }))];
        setRows(acc);
        setRelated(rel);
        setTotal(total);
        if (acc.length >= res.total || res.rows.length === 0) break;
      }
      if (alive) setRowsLoading(false);
    })().catch(() => alive && setRowsLoading(false));
    return () => {
      alive = false;
    };
  }, [cfg, view, token, password]);

  const openRow = async (id: string) => {
    if (!cfg?.allowDetail || !view) return;
    try {
      const d = await api.publicCollectionRow(token, id, view.id, password || undefined);
      setDetail({ ...d, props: (d.props as Record<string, unknown>) ?? {} });
    } catch {
      // Row detail off or gone — stay on the collection.
    }
  };

  if (state === 'loading') {
    return (
      <div className="public-form-page">
        <div className="form-card public-form-loading">{t('Loading…')}</div>
      </div>
    );
  }
  if (state === 'notfound' || (!needPassword && !cfg)) {
    return (
      <div className="public-form-page">
        <div className="form-card form-done">
          <h2>{t('Page not found')}</h2>
          <p>{t('This link is not valid or has been switched off.')}</p>
        </div>
      </div>
    );
  }
  if (needPassword && !cfg) {
    return (
      <div className="public-form-page lock-gate">
        <div className="form-card">
          <div className="public-form-head">
            <Lock size={28} />
            <h1 className="form-heading form-heading-static">{t('Protected page')}</h1>
          </div>
          <p className="form-desc form-desc-static">{t('This page is protected by a password.')}</p>
          {pwWrong && <p className="form-error">{t('Wrong password.')}</p>}
          <label className="form-field">
            <input
              className="form-input lock-input"
              type="password"
              autoFocus
              placeholder={t('Password')}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void load(password);
              }}
            />
          </label>
          <div className="form-actions">
            <button className="btn primary lock-btn" disabled={!password} onClick={() => void load(password)}>
              {t('Open')}
            </button>
          </div>
        </div>
      </div>
    );
  }
  if (!cfg || !view) return null;

  const tabIcon = (ty: ViewDef['type']) => {
    const sz = 14;
    if (ty === 'board') return <Columns3 size={sz} />;
    if (ty === 'gallery') return <LayoutGrid size={sz} />;
    if (ty === 'list') return <List size={sz} />;
    if (ty === 'calendar') return <CalendarDays size={sz} />;
    return <Table2 size={sz} />;
  };

  return (
    <div className="public-collection-page">
      <div className="public-collection">
        {cfg.cover && <div className="public-collection-cover" style={coverStyle(cfg.cover)} />}
        <div className="public-collection-head">
          {cfg.icon && (
            <span className="public-collection-icon">
              <PageIcon icon={cfg.icon} size={40} />
            </span>
          )}
          <h1 className="public-collection-title">{cfg.title || t('Untitled')}</h1>
          {cfg.description && <p className="public-collection-desc">{cfg.description}</p>}
        </div>
        {cfg.views.length > 1 && (
          <div className="view-tabs public-tabs">
            {cfg.views.map((v) => (
              <button
                key={v.id}
                className={'view-tab view-tab--' + v.type + (v.id === view.id ? ' active' : '')}
                onClick={() => setViewId(v.id)}
              >
                <span className="view-tab-ic">{tabIcon(v.type)}</span>
                {v.name}
              </button>
            ))}
          </div>
        )}
        <div className="public-collection-body">
          {rowsLoading && rows.length === 0 ? (
            <div className="db-empty">{t('Loading…')}</div>
          ) : view.type === 'board' ? (
            <PublicBoard
              rows={rows}
              schema={visibleSchema}
              groupBy={view.groupBy ?? ''}
              related={related}
              allowDetail={cfg.allowDetail}
              onOpen={openRow}
            />
          ) : view.type === 'gallery' ? (
            <GalleryView
              rows={rows}
              schema={visibleSchema}
              emptyLabel={t('No rows')}
              tagColors={{}}
              onNavigate={openRow}
              onSetProp={() => {}}
              onSetOptions={() => {}}
              readOnly
            />
          ) : view.type === 'list' ? (
            <ListView
              rows={rows}
              schema={visibleSchema}
              emptyLabel={t('No rows')}
              tagColors={{}}
              onNavigate={openRow}
              onSetProp={() => {}}
              onSetOptions={() => {}}
              readOnly
            />
          ) : view.type === 'calendar' ? (
            <PublicCalendar
              rows={rows}
              schema={visibleSchema}
              dateProp={view.dateProp ?? ''}
              endDateProp={view.endDateProp ?? ''}
              allowDetail={cfg.allowDetail}
              onOpen={openRow}
              mode={view.calMode ?? 'month'}
            />
          ) : view.type === 'timeline' ? (
            <PublicTimeline
              rows={rows}
              schema={visibleSchema}
              startProp={view.dateProp ?? ''}
              endProp={view.endDateProp ?? ''}
              allowDetail={cfg.allowDetail}
              onOpen={openRow}
              mode={view.tlMode ?? 'month'}
              hideLabels={view.tlHideLabels ?? false}
            />
          ) : (
            <PublicTable
              rows={rows}
              schema={visibleSchema}
              related={related}
              allowDetail={cfg.allowDetail}
              onOpen={openRow}
            />
          )}
          {total > rows.length && <div className="db-empty">{plural(rows.length, '{n} row', '{n} rows')}</div>}
        </div>
        <div className="public-form-footer">
          {t('Made with')} <b>salt.md</b>
        </div>
      </div>
      {detail && (
        <div className="public-detail-backdrop" onClick={() => setDetail(null)}>
          <div className="public-detail" onClick={(e) => e.stopPropagation()}>
            <div className="public-detail-head">
              {detail.icon && <PageIcon icon={detail.icon} size={22} />}
              <h2>{detail.title || t('Untitled')}</h2>
              <button className="btn-sm" onClick={() => setDetail(null)}>
                ✕
              </button>
            </div>
            <PublicDetailProps schema={visibleSchema} values={detail.props} related={related} />
            <PublicDetailContent content={detail.content} />
          </div>
        </div>
      )}
    </div>
  );
}

function coverStyle(cover: string): React.CSSProperties {
  if (!cover) return {};
  if (cover.startsWith('gradient:')) return { background: cover.slice('gradient:'.length) };
  return { backgroundImage: `url(${cover})`, backgroundSize: 'cover', backgroundPosition: 'center' };
}

// Relation cells resolve titles from the `related` map the server returns —
// the anonymous reader cannot query the relation target collection directly.
function PublicPropValue({
  def,
  value,
  related,
}: {
  def: PropDef;
  value: unknown;
  related: Record<string, { title: string; icon: string }>;
}) {
  if (def.type === 'relation') {
    const ids = Array.isArray(value) ? (value as string[]) : value ? [value as string] : [];
    if (ids.length === 0) return null;
    return (
      <span className="prop-multi">
        {ids.slice(0, 3).map((id) => (
          <span key={id} className="prop-chip relation-chip" style={{ background: '#3b6fb52e', color: '#3b6fb5' }}>
            {related[id]?.icon && (
              <span className="relation-icon">
                <PageIcon icon={related[id].icon} size={14} />
              </span>
            )}
            {related[id]?.title || ''}
          </span>
        ))}
        {ids.length > 3 && <span className="prop-chip relation-more">+{ids.length - 3}</span>}
      </span>
    );
  }
  return <PropertyValue def={def} value={value} readOnly compact maxChips={2} />;
}

function PublicTable({
  rows,
  schema,
  related,
  allowDetail,
  onOpen,
}: {
  rows: Row[];
  schema: PropDef[];
  related: Record<string, { title: string; icon: string }>;
  allowDetail: boolean;
  onOpen: (id: string) => void;
}) {
  if (rows.length === 0) return <div className="db-empty">{t('No rows')}</div>;
  return (
    <div className="table-wrap">
      <table className="db-table">
        <thead>
          <tr>
            <th className="db-title-col">{t('Name')}</th>
            {schema.map((p) => (
              <th key={p.id}>{p.name}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.id}>
              <td className="db-title-col">
                {allowDetail ? (
                  <button className="db-title-link" onClick={() => void onOpen(r.id)}>
                    {r.icon && (
                      <span className="inline-icon">
                        <PageIcon icon={r.icon} size={14} />{' '}
                      </span>
                    )}
                    {r.title || t('Untitled')}
                  </button>
                ) : (
                  <span className="db-title-inner">
                    {r.icon && (
                      <span className="inline-icon">
                        <PageIcon icon={r.icon} size={14} />{' '}
                      </span>
                    )}
                    {r.title || t('Untitled')}
                  </span>
                )}
              </td>
              {schema.map((p) => (
                <td key={p.id}>
                  <PublicPropValue def={p} value={r.props[p.id]} related={related} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="db-calc-row">
            <td className="db-title-col db-calc-cell">
              {plural(rows.length, '{n} row', '{n} rows')}
            </td>
            {schema.map((p) => (
              <td key={p.id} className="db-calc-cell" />
            ))}
          </tr>
        </tfoot>
      </table>
    </div>
  );
}

function PublicBoard({
  rows,
  schema,
  groupBy,
  related,
  allowDetail,
  onOpen,
}: {
  rows: Row[];
  schema: PropDef[];
  groupBy: string;
  related: Record<string, { title: string; icon: string }>;
  allowDetail: boolean;
  onOpen: (id: string) => void;
}) {
  const prop = schema.find((p) => p.id === groupBy);
  const showProps = schema.filter((p) => p.id !== groupBy).slice(0, 3);
  if (!prop || (prop.type !== 'select' && prop.type !== 'multiselect' && prop.type !== 'relation')) {
    return <div className="board-empty">{t('This board needs a Select property to group by.')}</div>;
  }
  const options =
    prop.type === 'relation'
      ? Object.entries(related).map(([id, r]) => ({ id, name: r.title, color: '#999' }))
      : (prop.options ?? []);
  const UNSET = '__unset__';
  const columns = [...options, { id: UNSET, name: t('No {name}', { name: prop.name }), color: '#999' }];
  const rowsFor = (optId: string) =>
    rows.filter((r) => {
      const v = r.props[groupBy];
      if (optId === UNSET) return !v || (Array.isArray(v) && v.length === 0);
      if (Array.isArray(v)) return v.includes(optId);
      return v === optId;
    });
  return (
    <div className="board">
      {columns.map((col) => {
        const cards = rowsFor(col.id);
        if (col.id === UNSET && cards.length === 0) return null;
        return (
          <div key={col.id} className="board-col" style={col.id !== UNSET ? ({ '--col-c': col.color } as React.CSSProperties) : undefined}>
            <div className="board-col-head">
              <span className="board-col-name">{col.name}</span>
              <span className="board-col-count">{cards.length}</span>
            </div>
            {cards.map((r) => (
              <div
                key={r.id}
                className={'board-card' + (allowDetail ? ' is-clickable' : '')}
                onClick={allowDetail ? () => void onOpen(r.id) : undefined}
              >
                <div className="card-title">
                  {r.icon && (
                    <span className="inline-icon">
                      <PageIcon icon={r.icon} size={14} />{' '}
                    </span>
                  )}
                  {r.title || t('Untitled')}
                </div>
                {showProps.map((p) => {
                  const v = r.props[p.id];
                  if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) return null;
                  return (
                    <div key={p.id} className="card-prop">
                      <PublicPropValue def={p} value={v} related={related} />
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}

// Read-only month grid mirroring the in-app calendar: same layout and CSS,
// no drag, no create. Events open the row only when the owner enabled detail.
function PublicCalendar({
  rows,
  schema,
  dateProp,
  endDateProp,
  allowDetail,
  onOpen,
  mode,
}: {
  rows: Row[];
  schema: PropDef[];
  dateProp: string;
  endDateProp: string;
  allowDetail: boolean;
  onOpen: (id: string) => void;
  mode: 'day' | 'week' | 'month' | '2months' | 'year';
}) {
  const [cursor, setCursor] = useState(() => {
    const n = new Date();
    return new Date(n.getFullYear(), n.getMonth(), n.getDate());
  });

  if (!dateProp || !schema.some((p) => p.id === dateProp && p.type === 'date')) {
    return (
      <div className="board-empty">
        {t('This calendar needs a {type} property. Open ⚙ Properties to add one.', { type: t('Date') })}
      </div>
    );
  }

  const byDate = new Map<string, Row[]>();
  for (const r of rows) {
    const v = r.props[dateProp];
    if (typeof v !== 'string' || !v) continue;
    const key = v.slice(0, 10);
    (byDate.get(key) ?? byDate.set(key, []).get(key)!).push(r);
  }

  // Ranges, exactly as inside: a row with an end date is one bar across the
  // days it covers, and only its first day stays a chip in `byDate`.
  type Span = { row: Row; from: string; to: string };
  const spans: Span[] = [];
  for (const r of rows) {
    const v = r.props[dateProp];
    if (typeof v !== 'string' || !v) continue;
    const from = v.slice(0, 10);
    let to = from;
    if (endDateProp) {
      const e = r.props[endDateProp];
      if (typeof e === 'string' && e) {
        const end = e.slice(0, 10);
        if (end > from) to = end;
      }
    }
    if (to > from) spans.push({ row: r, from, to });
  }
  const spanIds = new Set(spans.map((s) => s.row.id));
  const covered = new Set<string>();
  for (const s of spans) {
    const d = new Date(s.from + 'T00:00:00');
    const end = new Date(s.to + 'T00:00:00');
    for (let i = 0; d <= end && i < 400; i++, d.setDate(d.getDate() + 1)) covered.add(toDayString(d));
  }
  const lanesFor = (days: (Date | null)[]): Span[] => {
    const keys = days.filter((d): d is Date => !!d).map((d) => toDayString(d));
    if (keys.length === 0) return [];
    const lo = keys[0];
    const hi = keys[keys.length - 1];
    return spans
      .filter((s) => s.from <= hi && s.to >= lo)
      .sort((a, b) => (a.from !== b.from ? (a.from < b.from ? -1 : 1) : a.row.id < b.row.id ? -1 : 1));
  };
  const spanTitle = (s: Span) =>
    (s.row.title || t('Untitled')) + ' — ' + formatDay(s.from, 'dayMonth') + ' → ' + formatDay(s.to, 'dayMonth');

  const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
  const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
  const monthStarts: Date[] =
    mode === '2months' ? [first, new Date(first.getFullYear(), first.getMonth() + 1, 1)] : [first];
  const monthCells = (first: Date) => {
    const startWeekday = (first.getDay() - firstWeekday() + 7) % 7;
    const daysInMonth = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
    const cells: (Date | null)[] = [];
    for (let i = 0; i < startWeekday; i++) cells.push(null);
    for (let d = 1; d <= daysInMonth; d++) cells.push(new Date(first.getFullYear(), first.getMonth(), d));
    while (cells.length % 7 !== 0) cells.push(null);
    return cells;
  };
  const today = toDayString(new Date());
  const weekStart = addDays(cursor, -((cursor.getDay() - firstWeekday() + 7) % 7));
  const weekDays: Date[] = [];
  for (let k = 0; k < 7; k++) weekDays.push(addDays(weekStart, k));
  const second = monthStarts[1];
  const monthLabel =
    mode === 'day'
      ? formatDay(toDayString(cursor))
      : mode === 'week'
        ? formatDay(toDayString(weekStart)) + ' — ' + formatDay(toDayString(addDays(weekStart, 6)))
        : mode === 'year'
          ? String(cursor.getFullYear())
          : mode === '2months' && second
            ? formatMonth(monthStarts[0].getFullYear(), monthStarts[0].getMonth(), 'long') +
              ' — ' +
              formatMonth(second.getFullYear(), second.getMonth(), 'long')
            : formatMonth(cursor.getFullYear(), cursor.getMonth(), 'long');
  const step = (delta: number) => {
    if (mode === 'day') setCursor((c) => addDays(c, delta));
    else if (mode === 'week') setCursor((c) => addDays(c, delta * 7));
    else if (mode === 'year') setCursor((c) => new Date(c.getFullYear() + delta, c.getMonth(), 1));
    // Month modes always slide by one month — even showing two — so no month
    // is ever skipped over.
    else setCursor((c) => new Date(c.getFullYear(), c.getMonth() + delta, 1));
  };
  const goToday = () => {
    const n = new Date();
    setCursor(new Date(n.getFullYear(), n.getMonth(), n.getDate()));
  };
  // A public visitor sees what the owner set; the year grid drills down by
  // navigating its own state, exactly like inside.
  const yearMonths: Date[] = [];
  for (let m = 0; m < 12; m++) yearMonths.push(new Date(cursor.getFullYear(), m, 1));
  const eventButton = (r: Row, d: Date | null) =>
    allowDetail ? (
      <button key={r.id} className="calendar-event" onClick={() => void onOpen(r.id)} title={(r.title || t('Untitled')) + (d ? ' — ' + formatDay(toDayString(d)) : '')}>
        {r.icon && <span className="inline-icon"><PageIcon icon={r.icon} size={14} /> </span>}
        {r.title || t('Untitled')}
      </button>
    ) : (
      <span key={r.id} className="calendar-event is-static" title={(r.title || t('Untitled')) + (d ? ' — ' + formatDay(toDayString(d)) : '')}>
        {r.icon && <span className="inline-icon"><PageIcon icon={r.icon} size={14} /> </span>}
        {r.title || t('Untitled')}
      </span>
    );
  const dayCell = (d: Date | null, i: number | string, lanes: Span[] = [], rowKeys: string[] = []) => {
    const key = d ? toDayString(d) : '';
    const dayRows = (d ? byDate.get(key) ?? [] : []).filter((r) => !spanIds.has(r.id));
    const laneSlot = (s: Span) => {
      const covers = !!d && s.from <= key && s.to >= key;
      if (!covers) return <div key={'l' + s.row.id} className="cal-lane" />;
      const isStart = s.from === key;
      const isEnd = s.to === key;
      // The label gets the whole bar, not just its first cell — same as the
      // signed-in calendar (see CollectionView): this segment is as wide as the
      // bar is in this row, and the cells it reaches past draw nothing, so a
      // translucent colour is laid down once and the bar has no step in it.
      if (!isStart && rowKeys.includes(s.from)) {
        return <div key={'l' + s.row.id} className="cal-lane" />;
      }
      // The last day of this week row bounds it, so a bar running into the next
      // week stops at the edge.
      let cells = 1;
      let endsHere = isEnd;
      if (isStart) {
        const at = rowKeys.indexOf(key);
        cells = 0;
        for (let k = Math.max(at, 0); k < rowKeys.length && rowKeys[k] <= s.to; k++) cells++;
        if (cells === 0) cells = 1;
        endsHere = rowKeys.length === 0 || s.to <= rowKeys[rowKeys.length - 1];
      }
      return (
        <button
          key={'sp' + s.row.id}
          className={
            'cal-span' +
            (isStart ? ' is-start' : '') +
            (isEnd || (isStart && endsHere) ? ' is-end' : '')
          }
          style={isStart ? ({ '--span-cells': cells } as React.CSSProperties) : undefined}
          onClick={allowDetail ? () => void onOpen(s.row.id) : undefined}
          title={spanTitle(s)}
        >
          {isStart && (
            <>
              {s.row.icon && (
                <span className="inline-icon">
                  <PageIcon icon={s.row.icon} size={14} />{' '}
                </span>
              )}
              {s.row.title || t('Untitled')}
            </>
          )}
        </button>
      );
    };
    return (
      <div key={i} className={'calendar-cell' + (d ? '' : ' empty') + (key === today ? ' today' : '')}>
        {d && <div className="calendar-daynum">{d.getDate()}</div>}
        {lanes.map(laneSlot)}
        {dayRows.map((r) => eventButton(r, d))}
      </div>
    );
  };

  return (
    <div className="calendar">
      <div className="calendar-head">
        <button className="btn-sm" onClick={() => step(-1)} aria-label={t('Previous month')}>‹</button>
        <span className="calendar-title">{monthLabel}</span>
        <button className="btn-sm" onClick={() => step(1)} aria-label={t('Next month')}>›</button>
        <button className="btn-sm" onClick={goToday}>{t('Today')}</button>
      </div>
      {mode === 'day' ? (
        <div className="cal-day-list">
          {(byDate.get(toDayString(cursor)) ?? []).map((r) => eventButton(r, cursor))}
          {(byDate.get(toDayString(cursor)) ?? []).length === 0 && (
            <div className="db-empty">{t('Nothing on this day.')}</div>
          )}
        </div>
      ) : mode === 'week' ? (
        <div className="calendar-grid">
          {weekdayNames().map((d) => (
            <div key={d} className="calendar-dow">{d}</div>
          ))}
          {weekDays.map((d, i) => dayCell(d, i, lanesFor(weekDays), weekDays.map((x) => toDayString(x))))}
        </div>
      ) : mode === 'year' ? (
        <div className="cal-year">
          {yearMonths.map((first, mi) => (
            <div key={mi} className="cal-mini">
              <div className="cal-mini-title">{formatMonth(first.getFullYear(), first.getMonth(), 'long')}</div>
              <div className="cal-mini-grid">
                {monthCells(first).map((d, i) => {
                  const key = d ? toDayString(d) : '';
                  const has = d && ((byDate.get(key) ?? []).length > 0 || covered.has(key));
                  const dim = d && d.getMonth() !== first.getMonth();
                  return (
                    <span
                      key={i}
                      className={
                        'cal-mini-day' +
                        (!d ? ' empty' : '') +
                        (key === today ? ' today' : '') +
                        (has ? ' has-events' : '') +
                        (dim ? ' dim' : '')
                      }
                      title={d ? formatDay(toDayString(d)) : undefined}
                    >
                      {d?.getDate() ?? ''}
                    </span>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="cal-grids">
          {(mode === '2months' ? monthStarts : [new Date(cursor.getFullYear(), cursor.getMonth(), 1)]).map((first, mi) => {
            const cells = monthCells(first);
            const rowKeys = (r: number) =>
              cells
                .slice(r * 7, r * 7 + 7)
                .map((c) => (c ? toDayString(c) : ''))
                .filter((k) => k !== '');
            const rowLanes: Span[][] = [];
            for (let r = 0; r * 7 < cells.length; r++) rowLanes.push(lanesFor(cells.slice(r * 7, r * 7 + 7)));
            return (
              <div key={mi} className="calendar-grid-wrap">
                {mode === '2months' && (
                  <div className="cal-grid-title">{formatMonth(first.getFullYear(), first.getMonth(), 'long')}</div>
                )}
                <div className="calendar-grid">
                  {weekdayNames().map((d) => (
                    <div key={d} className="calendar-dow">{d}</div>
                  ))}
                  {cells.map((d, i) =>
                    dayCell(
                      d,
                      `${mi}-${i}`,
                      rowLanes[Math.floor(i / 7)],
                      rowKeys(Math.floor(i / 7)),
                    ),
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// Read-only Gantt mirroring the in-app timeline: same geometry and CSS, no
// editing. Bars/labels open the row only when the owner enabled detail.
function PublicTimeline({
  rows,
  schema,
  startProp,
  endProp,
  allowDetail,
  onOpen,
  mode,
  hideLabels,
}: {
  rows: Row[];
  schema: PropDef[];
  startProp: string;
  endProp: string;
  allowDetail: boolean;
  onOpen: (id: string) => void;
  mode: 'day' | 'week' | 'month' | '2months' | 'year';
  hideLabels?: boolean;
}) {
  const DAY = 26; // px per day, same as inside
  const YEARW = 140; // px per month in year mode, same as inside
  const LABELW = 190;
  // The owner's choice of a collapsed title column, honoured here too.
  const labelW = hideLabels ? 0 : LABELW;
  const dayNum = (iso: string) => {
    const [y, m, d] = iso.slice(0, 10).split('-').map(Number);
    return Math.floor(Date.UTC(y, m - 1, d) / 86400000);
  };
  const isoDay = (n: number) => new Date(n * 86400000).toISOString().slice(0, 10);

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

  const today = dayNum(toDayString(new Date()));
  const dataMin = items.length ? Math.min(...items.map((i) => i.start)) : today;
  const dataMax = items.length ? Math.max(...items.map((i) => i.end)) : today;
  const [cursor, setCursor] = useState(() =>
    today < dataMin - 3 ? dataMin : today > dataMax + 4 ? dataMax : today,
  );
  const [hovered, setHovered] = useState<string | null>(null);

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
  // Same proportional geometry as inside: spans narrower than a month fill
  // the width, wider ones keep 26px days and scroll.
  const gridMin = isYear ? monthCols.length * YEARW : dayCols.length * DAY;
  const isWeekend = (n: number) => {
    const wd = new Date(n * 86400000).getUTCDay();
    return wd === 0 || wd === 6;
  };

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
  // The weekends of the visible range, as runs — see the timeline inside.
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
        ? formatDay(isoDay(dayCols[0])) + ' — ' + formatDay(isoDay(dayCols[dayCols.length - 1]))
        : mode === 'year'
          ? String(yearOf(cursor))
          : dayCols.length > 0 && monthOf(dayCols[0]) === monthOf(dayCols[dayCols.length - 1])
            ? formatMonth(yearOf(dayCols[0]), monthOf(dayCols[0]), 'long')
            : formatMonth(yearOf(dayCols[0]), monthOf(dayCols[0]), 'long') +
              ' — ' +
              formatMonth(yearOf(dayCols[dayCols.length - 1]), monthOf(dayCols[dayCols.length - 1]), 'long');
  const step = (delta: number) => {
    if (mode === 'day') setCursor((c) => c + delta);
    else if (mode === 'week') setCursor((c) => c + delta * 7);
    else if (mode === 'year')
      setCursor((c) => {
        const d = new Date(c * 86400000);
        return Math.floor(Date.UTC(d.getUTCFullYear() + delta, d.getUTCMonth(), 1) / 86400000);
      });
    else
      setCursor((c) => {
        const d = new Date(c * 86400000);
        return Math.floor(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + delta, 1) / 86400000);
      });
  };
  const goToday = () => setCursor(today);

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
  const tipFor = (row: Row, it: Item) => {
    const sv = typeof row.props[startProp] === 'string' ? (row.props[startProp] as string) : '';
    const ev = endProp && typeof row.props[endProp] === 'string' ? (row.props[endProp] as string) : '';
    return (
      (row.title || t('Untitled')) +
      (sv ? ' — ' + formatDay(sv) + (it.end !== it.start && ev ? ' → ' + formatDay(ev) : '') : '')
    );
  };

  return (
    <div className="timeline">
      <div className="tl-controls">
        <button className="btn-sm" onClick={() => step(-1)} aria-label={t('Previous month')}>‹</button>
        <span className="tl-title">{title}</span>
        <button className="btn-sm" onClick={() => step(1)} aria-label={t('Next month')}>›</button>
        <button className="btn-sm" onClick={goToday}>{t('Today')}</button>
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
                        (hoveredItem && covers(hoveredItem, n) ? ' is-span' : '') +
                        (hoveredItem && covers(hoveredItem, n) && !covers(hoveredItem, n - 1) ? ' is-span-first' : '') +
                        (hoveredItem && covers(hoveredItem, n) && !covers(hoveredItem, n + 1) ? ' is-span-last' : '')
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
              const g = barGeom(it);
              if (!g) return null;
              return (
                <div key={row.id} className="tl-row">
                  {!hideLabels && (
                    <div
                      className={'tl-label' + (allowDetail ? '' : ' is-static')}
                      style={{ width: LABELW }}
                      onClick={allowDetail ? () => void onOpen(row.id) : undefined}
                      onMouseEnter={() => setHovered(row.id)}
                      onMouseLeave={() => setHovered((h) => (h === row.id ? null : h))}
                      title={tipFor(row, it)}
                    >
                      {row.icon && (
                        <span className="inline-icon">
                          <PageIcon icon={row.icon} size={14} />
                        </span>
                      )}
                      <span className="tl-label-text">{row.title || t('Untitled')}</span>
                    </div>
                  )}
                  <div className="tl-track">
                    {weekends.map((w, k) => (
                      <div key={k} className="tl-weekend" style={{ left: w.left, width: w.width }} />
                    ))}
                    {todayLeft && !isYear && <div className="tl-today-line" style={{ left: todayLeft }} />}
                    <div
                      className={'tl-bar' + (allowDetail ? '' : ' is-static')}
                      style={{ left: g.left, width: g.width }}
                      onClick={allowDetail ? () => void onOpen(row.id) : undefined}
                      onMouseEnter={() => setHovered(row.id)}
                      onMouseLeave={() => setHovered((h) => (h === row.id ? null : h))}
                      title={tipFor(row, it)}
                    >
                      <span className="tl-bar-label">{row.title || t('Untitled')}</span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}


function PublicDetailProps({
  schema,
  values,
  related,
}: {
  schema: PropDef[];
  values: Record<string, unknown>;
  related: Record<string, { title: string; icon: string }>;
}) {
  const shown = schema.filter((p) => {
    const v = values[p.id];
    return !(v === undefined || v === '' || v === null || (Array.isArray(v) && v.length === 0));
  });
  if (shown.length === 0) return null;
  return (
    <div className="public-detail-props">
      {shown.map((p) => (
        <div key={p.id} className="public-detail-prop">
          <span className="public-detail-prop-name">{p.name}</span>
          <PublicPropValue def={p} value={values[p.id]} related={related} />
        </div>
      ))}
    </div>
  );
}

// Row content is BlockNote JSON. Render an honest plain-text extract —
// headings, lists and paragraphs as text, nothing executable.
function PublicDetailContent({ content }: { content: string }) {
  const blocks = useMemo(() => {
    try {
      const v = JSON.parse(content || '[]');
      return Array.isArray(v) ? v : [];
    } catch {
      return [];
    }
  }, [content]);
  if (blocks.length === 0) return null;
  const textOf = (b: Record<string, unknown>): string => {
    const c = b.content;
    if (!Array.isArray(c)) return '';
    return c
      .map((n) => (typeof n === 'object' && n !== null && 'text' in (n as object) ? String((n as { text: unknown }).text) : ''))
      .join('');
  };
  return (
    <div className="public-detail-content">
      {blocks.map((b, i) => {
        const block = b as Record<string, unknown>;
        const text = textOf(block);
        if (!text.trim()) return null;
        const type = String(block.type ?? 'paragraph');
        if (type === 'heading') return <h3 key={i}>{text}</h3>;
        if (type === 'bulletListItem') return <li key={i}>{text}</li>;
        if (type === 'checkListItem')
          return (
            <li key={i} className="public-check">
              {(block.props as { checked?: boolean } | undefined)?.checked ? '☑ ' : '☐ '}
              {text}
            </li>
          );
        return <p key={i}>{text}</p>;
      })}
    </div>
  );
}
