import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { ChecklistItem, PropDef, PropOption } from '../types';
import Portal from './Portal';
import { OPTION_HEXES, optionPalette, optionSlug } from '../selectOptions';
import { daysUntil, formatDay, formatMoment, formatNumber } from '../format';
import { initials, nameColor } from './CommentsPanel';
import { Check, Link2 as LinkIcon, Plus, Trash2 } from 'lucide-react';
import { PageIcon } from '../pageIcon';
import { t } from '../i18n';

interface Props {
  def: PropDef;
  value: unknown;
  onChange?: (v: unknown) => void;
  onOptionsChange?: (options: PropOption[]) => void;
  readOnly?: boolean;
  compact?: boolean;
}

// idList reads a list-shaped value (relation, multiselect). A single id stored
// WITHOUT its list counts as a one-element list: agents used to write
// {"system": "abc"} and the server stored it verbatim, after which this cell
// rendered nothing at all while the row still grouped and filtered correctly —
// so the property looked switched off rather than misshapen. Writes are
// normalised on the server now; this keeps older rows readable.
export function idList(value: unknown): string[] {
  if (Array.isArray(value)) return value as string[];
  return typeof value === 'string' && value !== '' ? [value] : [];
}

function chip(opt: PropOption | undefined, fallback: string) {
  const color = opt?.color ?? '#999';
  return (
    <span className="prop-chip" style={{ background: color + '2e', color }}>
      {opt?.name ?? fallback}
    </span>
  );
}

// SelectCell is the Notion-style editor for select / multiselect cells: click to
// open a popover where you can search, create an option inline, pick each
// option's colour, or delete it. Rendered through a Portal with a viewport-
// clamped fixed position so it never runs off-screen or gets clipped by the
// scrolling table. Colour editing is a second panel (not a nested popover) so it
// stays on-screen on mobile.
function SelectCell({
  def,
  value,
  multi,
  onChange,
  onOptionsChange,
}: {
  def: PropDef;
  value: unknown;
  multi: boolean;
  onChange: (v: unknown) => void;
  onOptionsChange?: (options: PropOption[]) => void;
}) {
  // Robust against badly written schemas: when an option arrives as a bare
  // string (rather than {id, name}), `o.name.toLowerCase()` used to throw the
  // WHOLE view into its error state — one broken column made the database
  // unusable. The server normalises this on write now; this is the belt to go
  // with those braces, and it covers older data too.
  const options: PropOption[] = (def.options ?? [])
    .map((o) =>
      typeof o === 'string'
        ? ({ id: o, name: o, color: '' } as PropOption)
        : (o as PropOption),
    )
    .filter((o) => o && typeof o.name === 'string');
  const vals = multi ? idList(value) : value ? [String(value)] : [];
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [colorFor, setColorFor] = useState<string | null>(null); // option id being recoloured
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [pos, setPos] = useState<{
    left: number;
    top?: number;
    bottom?: number;
    width: number;
    maxHeight: number;
  } | null>(null);

  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return;
    const r = triggerRef.current.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const width = Math.min(Math.max(r.width, 240), vw - 16);
    const left = Math.max(8, Math.min(r.left, vw - width - 8));
    const spaceBelow = vh - r.bottom - 12;
    const spaceAbove = r.top - 12;
    const below = spaceBelow >= 240 || spaceBelow >= spaceAbove;
    setPos({
      left,
      width,
      top: below ? r.bottom + 4 : undefined,
      bottom: below ? undefined : vh - r.top + 4,
      maxHeight: Math.max(200, (below ? spaceBelow : spaceAbove)),
    });
  }, [open]);

  const close = () => {
    setOpen(false);
    setColorFor(null);
    setQ('');
  };
  const selected = (id: string) => vals.includes(id);
  const query = q.trim();
  const filtered = options.filter((o) => o.name.toLowerCase().includes(query.toLowerCase()));
  const exact = options.some((o) => o.name.toLowerCase() === query.toLowerCase());

  const pick = (id: string) => {
    if (multi) onChange(selected(id) ? vals.filter((v) => v !== id) : [...vals, id]);
    else {
      onChange(selected(id) ? '' : id);
      close();
    }
  };
  const create = () => {
    if (!query || !onOptionsChange) return;
    const oid = optionSlug(query, options);
    const color = OPTION_HEXES[options.length % OPTION_HEXES.length];
    onOptionsChange([...options, { id: oid, name: query, color }]);
    if (multi) {
      onChange([...vals, oid]);
      setQ('');
    } else {
      onChange(oid);
      close();
    }
  };
  const setColor = (oid: string, hex: string) => {
    onOptionsChange?.(options.map((o) => (o.id === oid ? { ...o, color: hex } : o)));
    setColorFor(null);
  };
  const remove = (oid: string) => {
    onOptionsChange?.(options.filter((o) => o.id !== oid));
    if (multi) onChange(vals.filter((v) => v !== oid));
    else if (String(value) === oid) onChange('');
    setColorFor(null);
  };

  const editing = colorFor ? options.find((o) => o.id === colorFor) : null;

  return (
    <div className="select-cell">
      <button ref={triggerRef} className="select-cell-value" onClick={() => setOpen((o) => !o)}>
        {vals.length ? (
          vals.map((id) => <span key={id}>{chip(options.find((o) => o.id === id), id)}</span>)
        ) : (
          <span className="prop-empty">—</span>
        )}
      </button>
      {open && pos && (
        <Portal>
          <div className="select-backdrop" onClick={close} />
          <div
            className="select-menu"
            style={{
              position: 'fixed',
              left: pos.left,
              top: pos.top,
              bottom: pos.bottom,
              width: pos.width,
              maxHeight: pos.maxHeight,
            }}
          >
            {editing ? (
              /* Colour / delete panel for a single option (keeps everything on-screen). */
              <>
                <button className="select-back" onClick={() => setColorFor(null)}>
                  ‹ {t('Back')}
                </button>
                <div className="select-editing-name">
                  <span className="prop-chip" style={{ background: editing.color + '2e', color: editing.color }}>
                    {editing.name}
                  </span>
                </div>
                <button className="tag-color-opt danger" onClick={() => remove(editing.id)}>
                  <Trash2 size={14} /> {t('Delete option')}
                </button>
                <div className="menu-label">{t('Colours')}</div>
                <div className="select-options">
                  {optionPalette().map((c) => (
                    <button key={c.hex} className="tag-color-opt" onClick={() => setColor(editing.id, c.hex)}>
                      <span className="tag-swatch" style={{ background: c.hex }} />
                      <span className="tag-color-name">{c.name}</span>
                      {editing.color === c.hex && <Check size={14} />}
                    </button>
                  ))}
                </div>
              </>
            ) : (
              <>
                <input
                  className="select-search"
                  autoFocus
                  placeholder={t('Search or create…')}
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      if (query && !exact) create();
                      else if (filtered[0]) pick(filtered[0].id);
                    } else if (e.key === 'Escape') close();
                  }}
                />
                <div className="select-options">
                  {filtered.map((o) => (
                    <div key={o.id} className={'select-option' + (selected(o.id) ? ' on' : '')}>
                      <button className="select-option-main" onClick={() => pick(o.id)}>
                        <span className="prop-chip" style={{ background: o.color + '2e', color: o.color }}>
                          {o.name}
                        </span>
                        <span className="select-option-check">{selected(o.id) && <Check size={15} />}</span>
                      </button>
                      {onOptionsChange && (
                        <button
                          className="select-option-more"
                          title={t('Colour / delete')}
                          onClick={() => setColorFor(o.id)}
                        >
                          ⋯
                        </button>
                      )}
                    </div>
                  ))}
                  {query && !exact && onOptionsChange && (
                    <button className="select-create" onClick={create}>
                      <Plus size={15} /> Anlegen: „{query}"
                    </button>
                  )}
                  {filtered.length === 0 && !query && (
                    <div className="select-empty">{t('No options — type to create one.')}</div>
                  )}
                </div>
              </>
            )}
          </div>
        </Portal>
      )}
    </div>
  );
}

// Format an ISO date (YYYY-MM-DD) as a localized dd.mm.yyyy — parsed from the
// string parts (no Date() so there's no timezone shift), shown everywhere a date
// is read instead of the raw ISO.
const fmtDate = (v: string) => formatDay(v, 'date') || v;

// A due date only says something once you can see urgency without doing the
// arithmetic — that is why a Trello board reads at a glance. Overdue red,
// today and tomorrow amber, everything else quiet.
function dateUrgency(v: string): '' | ' is-overdue' | ' is-soon' {
  const days = daysUntil(v);
  if (days === null) return '';
  if (days < 0) return ' is-overdue';
  if (days <= 1) return ' is-soon';
  return '';
}

// Format a computed (rollup/formula) value for display. The server sends either
// a number or a "⚠ <message>" error string.
function formatComputed(value: unknown): { text: string; error: boolean } {
  if (typeof value === 'string' && value.startsWith('⚠')) return { text: value, error: true };
  if (typeof value === 'number') {
    // Trim floating-point noise (13.000000001) without forcing decimals on ints.
    const rounded = Math.round(value * 1e6) / 1e6;
    return { text: formatNumber(rounded), error: false };
  }
  return { text: value != null ? String(value) : '', error: false };
}

// Renders a numeric value per its numberDisplay: plain text, a progress bar, or
// a ring. Used by number props and by number-valued rollup/formula props. Falls
// back to plain text when the value isn't a finite number.
function NumberDisplay({ value, def, compact }: { value: unknown; def: PropDef; compact?: boolean }) {
  const num = typeof value === 'number' ? value : parseFloat(String(value ?? ''));
  const display = def.numberDisplay ?? 'plain';
  const label = typeof value === 'number' ? String(Math.round(value * 1e6) / 1e6) : value != null ? String(value) : '';
  if (display === 'plain' || !isFinite(num)) {
    if (!label) return compact ? null : <span className="prop-empty" />;
    return <span className="prop-number">{label}</span>;
  }
  const max = def.numberMax && def.numberMax > 0 ? def.numberMax : 100;
  const pct = Math.max(0, Math.min(100, (num / max) * 100));
  if (display === 'ring') {
    const r = 7;
    const circ = 2 * Math.PI * r;
    return (
      <span className="prop-ring" title={`${label} / ${max}`}>
        <svg width="18" height="18" viewBox="0 0 18 18">
          <circle className="prop-ring-track" cx="9" cy="9" r={r} />
          <circle
            className="prop-ring-fill"
            cx="9"
            cy="9"
            r={r}
            strokeDasharray={circ}
            strokeDashoffset={circ * (1 - pct / 100)}
            transform="rotate(-90 9 9)"
          />
        </svg>
        <span className="prop-bar-label">{label}</span>
      </span>
    );
  }
  return (
    <span className="prop-bar" title={`${label} / ${max}`}>
      <span className="prop-bar-track">
        <span className="prop-bar-fill" style={{ width: pct + '%' }} />
      </span>
      <span className="prop-bar-label">{label}</span>
    </span>
  );
}

// ---- Checklist ----
// Sub-tasks with derived progress. Deliberately NOT a number with a stored
// percentage: two truths that drift apart, and the reason a Trello-style card
// reads as "half done" is that the ticks and the bar cannot disagree.

function checklistItems(value: unknown): ChecklistItem[] {
  if (!Array.isArray(value)) return [];
  // Tolerant like the select cell: an agent may write plain strings, and older
  // rows may carry items without an id.
  //
  // Empty items are KEPT. Dropping them here looked tidy and broke adding
  // entirely: a fresh sub-task starts empty, so it vanished on the very next
  // render and the + button appeared to do nothing. They are cleaned up when
  // the editor closes instead.
  return value.map((v, i) =>
    typeof v === 'string'
      ? { id: 'i' + i, text: v, done: false }
      : ({ id: String((v as ChecklistItem)?.id ?? 'i' + i), text: String((v as ChecklistItem)?.text ?? ''), done: (v as ChecklistItem)?.done === true } as ChecklistItem),
  );
}

/** The compact face of a checklist: a bar and "3/5". Used on cards and in
    table cells, where a full list would blow up the row. */
function ChecklistSummary({ items, compact }: { items: ChecklistItem[]; compact?: boolean }) {
  // A half-typed sub-task must not dilute the percentage, so the summary counts
  // only items that say something.
  const named = items.filter((i) => i.text.trim() !== '');
  const total = named.length;
  const done = named.filter((i) => i.done).length;
  if (!total) return compact ? null : <span className="prop-empty" />;
  const pct = Math.round((done / total) * 100);
  return (
    <span className="prop-bar prop-checklist-sum" title={`${done} / ${total}`}>
      <span className="prop-bar-track">
        <span className={'prop-bar-fill' + (done === total ? ' is-full' : '')} style={{ width: pct + '%' }} />
      </span>
      <span className="prop-bar-label">{pct}%</span>
    </span>
  );
}

function ChecklistValue({ value, onChange, readOnly, compact }: Props) {
  const items = checklistItems(value);
  const [open, setOpen] = useState(false);
  const ro = readOnly || !onChange;

  // Read-only cells and the collapsed state show the summary; the list itself
  // only appears once someone opens it, so a table row stays one line high.
  if (ro || !open) {
    const summary = <ChecklistSummary items={items} compact={compact} />;
    if (ro) return summary;
    return (
      <span className="prop-checklist-toggle" onClick={() => setOpen(true)}>
        {items.length ? summary : <span className="prop-empty">{t('+ Sub-task')}</span>}
      </span>
    );
  }

  const write = (next: ChecklistItem[]) => onChange!(next.length ? next : null);
  const toggle = (id: string) => write(items.map((i) => (i.id === id ? { ...i, done: !i.done } : i)));
  const setText = (id: string, text: string) => write(items.map((i) => (i.id === id ? { ...i, text } : i)));
  const remove = (id: string) => write(items.filter((i) => i.id !== id));
  const add = () =>
    write([...items, { id: 'i' + Date.now().toString(36) + items.length, text: '', done: false }]);

  return (
    <div className="prop-checklist">
      <ChecklistSummary items={items} />
      {items.map((it) => (
        <div key={it.id} className={'pcl-item' + (it.done ? ' is-done' : '')}>
          <input type="checkbox" checked={it.done} onChange={() => toggle(it.id)} />
          <input
            className="pcl-text"
            value={it.text}
            placeholder={t('Sub-task')}
            onChange={(e) => setText(it.id, e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                add();
              }
            }}
          />
          <button className="pcl-del" title={t('Delete')} onClick={() => remove(it.id)}>
            <Trash2 size={12} />
          </button>
        </div>
      ))}
      <div className="pcl-actions">
        <button className="pcl-add" onClick={add}>
          <Plus size={12} /> {t('Sub-task')}
        </button>
        <button
          className="pcl-add"
          onClick={() => {
            // Closing is where empty rows go — they exist so you can type in
            // them, not so they end up in the data.
            const named = items.filter((i) => i.text.trim() !== '');
            if (named.length !== items.length) write(named);
            setOpen(false);
          }}
        >
          {t('Done')}
        </button>
      </div>
    </div>
  );
}

// ---- Person ----
// Members of the workspaces this browser can see, by id AND by name, so a
// person value written as a name (by hand or by an agent) still finds its face.
// One request per workspace, shared by every cell — a table with 200 person
// cells must not make 200 calls.
type Member = { userId: string; name: string; color: string; avatar: string };
let memberCache: Promise<Member[]> | null = null;

function loadMembers(): Promise<Member[]> {
  if (!memberCache) {
    memberCache = api
      .listWorkspaces()
      .then((ws) => Promise.all(ws.map((w) => api.listMembers(w.id).catch(() => []))))
      .then((lists) => {
        const byID = new Map<string, Member>();
        for (const m of lists.flat()) {
          byID.set(m.userId, { userId: m.userId, name: m.name, color: m.color, avatar: m.avatar });
        }
        return [...byID.values()];
      })
      .catch(() => [] as Member[]);
  }
  return memberCache;
}

/** The chip: a face plus the name. Matches by id first, then by name, so a
    value an agent wrote as plain text still finds its person; an unknown value
    stays readable text rather than disappearing. */
function PersonChip({ raw, members }: { raw: string; members: Member[] }) {
  const lower = raw.toLowerCase();
  const hit = members.find((m) => m.userId === raw) ?? members.find((m) => m.name.toLowerCase() === lower);
  const name = hit?.name ?? raw;
  return (
    <span className="prop-person" title={name}>
      <span
        className="cp-avatar prop-person-av"
        style={{ background: hit?.avatar ? 'transparent' : hit?.color || nameColor(name) }}
      >
        {hit?.avatar ? <img src={hit.avatar} alt="" /> : initials(name)}
      </span>
      <span className="prop-person-name">{name}</span>
    </span>
  );
}

/** The people on a card, as overlapping faces in the top right corner (W126).
 *
 *  Cards used to print every person field as a full-name chip, one per line —
 *  so the same colleague appeared two or three times (once per field) and ate
 *  three rows before the first real fact. The stack dedupes by person, not by
 *  field: who is on this card is one question, however many fields answer it.
 *  The name lives in the tooltip; the face is enough to recognise. */
export function PersonStack({ values, max = 3 }: { values: string[]; max?: number }) {
  const [members, setMembers] = useState<Member[]>([]);
  useEffect(() => {
    let alive = true;
    void loadMembers().then((m) => alive && setMembers(m));
    return () => {
      alive = false;
    };
  }, []);

  const people: { key: string; name: string; color: string; avatar: string }[] = [];
  const seen = new Set<string>();
  for (const raw of values) {
    const v = raw.trim();
    if (!v) continue;
    const lower = v.toLowerCase();
    const hit = members.find((m) => m.userId === v) ?? members.find((m) => m.name.toLowerCase() === lower);
    const name = hit?.name ?? v;
    // Dedupe on the resolved person: the same human written once as an id and
    // once as a name is one face, not two.
    const key = hit?.userId ?? name.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    people.push({ key, name, color: hit?.color || nameColor(name), avatar: hit?.avatar ?? '' });
  }
  if (people.length === 0) return null;
  const shown = people.slice(0, max);
  const rest = people.slice(max);
  return (
    <span className="person-stack" title={people.map((p) => p.name).join(', ')}>
      {shown.map((p) => (
        <span
          key={p.key}
          className="cp-avatar person-stack-av"
          style={{ background: p.avatar ? 'transparent' : p.color }}
          title={p.name}
        >
          {p.avatar ? <img src={p.avatar} alt="" /> : initials(p.name)}
        </span>
      ))}
      {rest.length > 0 && (
        <span className="cp-avatar person-stack-av person-stack-more" title={rest.map((p) => p.name).join(', ')}>
          +{rest.length}
        </span>
      )}
    </span>
  );
}

/** A person cell: pick a colleague from a list, or type a name for somebody
    without an account.
 *
 *  The first version was a free-text field, and that was a dead end twice over:
 *  an empty cell rendered as a 0×0 span (nothing to click, so the whole column
 *  looked broken), and even once open it asked you to type a colleague's name
 *  exactly — with the roster sitting right there. Picking stores the USER ID, so
 *  the cell follows a rename; free text is kept as a fallback and stored as
 *  typed. */
function PersonValue({ value, onChange, readOnly, compact }: Props) {
  const raw = String(value ?? '').trim();
  const [members, setMembers] = useState<Member[]>([]);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const boxRef = useRef<HTMLDivElement>(null);
  const ro = readOnly || !onChange;

  useEffect(() => {
    let alive = true;
    void loadMembers().then((m) => alive && setMembers(m));
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onDown);
    return () => document.removeEventListener('pointerdown', onDown);
  }, [open]);

  if (ro || compact) return raw ? <PersonChip raw={raw} members={members} /> : null;

  const q = query.trim().toLowerCase();
  const filtered = members.filter((m) => m.name.toLowerCase().includes(q));
  const pick = (v: string) => {
    onChange!(v || null);
    setQuery('');
    setOpen(false);
  };

  return (
    <div className="relation-value" ref={boxRef}>
      {/* Always a real hit target: an empty cell says "＋ Person" instead of
          being an invisible nothing. */}
      <button type="button" className="relation-open" onClick={() => setOpen((v) => !v)}>
        {raw ? <PersonChip raw={raw} members={members} /> : <span className="prop-empty">{t('＋ Person')}</span>}
      </button>
      {open && (
        <div className="menu relation-menu">
          <input
            className="prop-input"
            autoFocus
            placeholder={t('Search or type a name…')}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              // Enter takes the single match, else what was typed — so somebody
              // without an account can be entered without leaving the keyboard.
              if (e.key !== 'Enter') return;
              e.preventDefault();
              if (filtered.length === 1) pick(filtered[0].userId);
              else if (query.trim()) pick(query.trim());
            }}
          />
          <div className="relation-options">
            {filtered.map((m) => (
              <button
                key={m.userId}
                type="button"
                className={'relation-option' + (m.userId === raw || m.name === raw ? ' on' : '')}
                onClick={() => pick(m.userId)}
              >
                <span className="relation-check">{m.userId === raw || m.name === raw ? '✓' : ''}</span>
                <PersonChip raw={m.userId} members={members} />
              </button>
            ))}
            {q && !filtered.some((m) => m.name.toLowerCase() === q) && (
              <button type="button" className="relation-option" onClick={() => pick(query.trim())}>
                <span className="relation-check" />
                {t('Use “{name}”', { name: query.trim() })}
              </button>
            )}
            {!members.length && !q && <div className="relation-empty">{t('No members')}</div>}
            {raw && (
              <button type="button" className="relation-option danger" onClick={() => pick('')}>
                <span className="relation-check" />
                {t('Remove')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// ---- Relation picker ----
// Rows of the target collection are fetched once per collection and shared
// across every relation cell via a module-level cache, so a table with many
// relation cells doesn't issue one request per cell.
export type RelOption = { id: string; title: string; icon: string };
const relCache = new Map<string, Promise<RelOption[]>>();

// Exported because the board groups by relation too, and it must show the same
// titles the cells show — loaded once through the same cache rather than
// fetched a second time per view.
export function loadRelationOptions(colId: string, force = false): Promise<RelOption[]> {
  if (force || !relCache.has(colId)) {
    const p = api
      .collectionRows(colId, { limit: 500 })
      .then((r) => r.rows.map((x) => ({ id: x.id, title: x.title, icon: x.icon })))
      .catch(() => [] as RelOption[]);
    relCache.set(colId, p);
  }
  return relCache.get(colId)!;
}

function RelationValue({ def, value, onChange, readOnly, compact }: Props) {
  const targetId = def.relationCollection;
  const ids = idList(value);
  const [options, setOptions] = useState<RelOption[]>([]);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const boxRef = useRef<HTMLDivElement>(null);
  const ro = readOnly || !onChange;

  useEffect(() => {
    if (!targetId) return;
    let alive = true;
    void loadRelationOptions(targetId).then((o) => alive && setOptions(o));
    return () => {
      alive = false;
    };
  }, [targetId]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onDown);
    return () => document.removeEventListener('pointerdown', onDown);
  }, [open]);

  const titleOf = (id: string) => options.find((o) => o.id === id)?.title || 'Untitled';
  const iconOf = (id: string) => options.find((o) => o.id === id)?.icon || '';

  if (!targetId) return <span className="prop-empty">{t('No target')}</span>;

  // A row's icon is any of the four kinds a page icon can be, so it goes
  // through PageIcon like everywhere else. Printed raw, a Lucide or MDI icon
  // arrived as the literal text "lucide:PhoneCall" — visible in the picker on
  // every row whose icon was not an emoji.
  const chips = (
    <span className="prop-multi">
      {ids.map((id) => (
        <span key={id} className="prop-chip relation-chip" style={{ background: '#3b6fb52e', color: '#3b6fb5' }}>
          {iconOf(id) && (
            <span className="relation-icon">
              <PageIcon icon={iconOf(id)} size={14} />
            </span>
          )}
          {titleOf(id)}
        </span>
      ))}
    </span>
  );

  if (ro) return ids.length ? chips : null;
  if (compact) return ids.length ? chips : null;

  const toggle = (id: string) => {
    onChange!(ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]);
  };
  const filtered = options.filter((o) =>
    (o.title || 'Untitled').toLowerCase().includes(query.toLowerCase()),
  );

  return (
    <div className="relation-value" ref={boxRef}>
      <button
        type="button"
        className="relation-open"
        onClick={() => {
          if (!open && targetId) void loadRelationOptions(targetId, true).then(setOptions);
          setOpen((v) => !v);
        }}
      >
        {ids.length ? chips : <span className="prop-empty">{t('＋ Link')}</span>}
      </button>
      {open && (
        <div className="menu relation-menu">
          <input
            className="prop-input"
            autoFocus
            placeholder={t('Search…')}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <div className="relation-options">
            {filtered.length === 0 && <div className="relation-empty">{t('No rows')}</div>}
            {filtered.map((o) => (
              <button
                key={o.id}
                type="button"
                className={'relation-option' + (ids.includes(o.id) ? ' on' : '')}
                onClick={() => toggle(o.id)}
              >
                <span className="relation-check">{ids.includes(o.id) ? '✓' : ''}</span>
                {o.icon && (
                  <span className="relation-icon">
                    <PageIcon icon={o.icon} size={16} />
                  </span>
                )}
                {o.title || 'Untitled'}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export default function PropertyValue({ def, value, onChange, onOptionsChange, readOnly, compact }: Props) {
  const [editing, setEditing] = useState(false);
  const ro = readOnly || !onChange;

  switch (def.type) {
    case 'relation':
      return <RelationValue def={def} value={value} onChange={onChange} readOnly={readOnly} compact={compact} />;
    // A backrelation IS a relation to read — same ids, same titles, same
    // chips. It is only computed rather than typed in, so it never takes an
    // onChange: editing happens on the side that owns the relation. Without
    // this case it fell through to the text renderer and showed raw ids.
    case 'backrelation':
      return (
        <RelationValue
          def={{ ...def, relationCollection: def.backrelationCollection ?? '' }}
          value={value}
          readOnly
          compact={compact}
        />
      );
    case 'lastActivity': {
      // Computed on read from the row's updated_at and the newest audit entry —
      // never stored, so it cannot go stale the way a field somebody has to
      // remember to set does. That is the whole point of the column: it moves by
      // itself when anyone, person or agent, touches the row.
      const v = (value ?? {}) as { at?: string; by?: string };
      if (!v.at) return compact ? null : <span className="prop-empty">—</span>;
      return (
        <span
          className="prop-computed"
          title={v.by ? t('Last change by {who}').replace('{who}', v.by) : undefined}
        >
          {formatMoment(v.at, 'datetime')}
          {v.by ? <span className="prop-activity-by"> · {v.by}</span> : null}
        </span>
      );
    }
    case 'rollup':
    case 'formula': {
      // Computed server-side; always read-only. Renders the number or an
      // inline "⚠ …" for a bad formula (division by zero, cycle, …).
      const { text, error } = formatComputed(value);
      if (!text) return compact ? null : <span className="prop-empty">—</span>;
      // A numeric computed value can display as a progress bar/ring too.
      if (!error && (def.numberDisplay === 'bar' || def.numberDisplay === 'ring') && isFinite(Number(value))) {
        return <NumberDisplay value={value} def={def} compact={compact} />;
      }
      return <span className={'prop-computed' + (error ? ' error' : '')}>{text}</span>;
    }
    case 'select': {
      const opt = def.options?.find((o) => o.id === value);
      if (ro) return value ? chip(opt, String(value)) : null;
      return (
        <SelectCell
          def={def}
          value={value}
          multi={false}
          onChange={onChange!}
          onOptionsChange={onOptionsChange}
        />
      );
    }
    case 'multiselect': {
      const vals = idList(value);
      if (compact || ro) {
        return (
          <span className="prop-multi">
            {vals.map((id) => chip(def.options?.find((o) => o.id === id), id))}
          </span>
        );
      }
      return (
        <SelectCell
          def={def}
          value={value}
          multi
          onChange={onChange!}
          onOptionsChange={onOptionsChange}
        />
      );
    }
    case 'checkbox':
      return (
        <input
          type="checkbox"
          checked={value === true}
          disabled={ro}
          onChange={(e) => onChange?.(e.target.checked)}
        />
      );
    case 'checklist':
      return <ChecklistValue def={def} value={value} onChange={onChange} readOnly={readOnly} compact={compact} />;
    case 'number': {
      const display = def.numberDisplay ?? 'plain';
      if (ro) return <NumberDisplay value={value} def={def} compact={compact} />;
      // A bar/ring number shows the visual until clicked, then edits the raw
      // value (like the text cell). A plain number stays an always-on input.
      if (display !== 'plain' && !editing) {
        return (
          <span className="prop-num-editable" onClick={() => setEditing(true)}>
            <NumberDisplay value={value} def={def} compact={compact} />
          </span>
        );
      }
      return (
        <input
          className="prop-input"
          type="number"
          autoFocus={display !== 'plain'}
          value={(value as number) ?? ''}
          onChange={(e) => onChange!(e.target.value === '' ? null : Number(e.target.value))}
          onBlur={display !== 'plain' ? () => setEditing(false) : undefined}
        />
      );
    }
    case 'date':
      if (ro)
        return value ? (
          <span className={'prop-date' + dateUrgency(String(value))}>{fmtDate(String(value))}</span>
        ) : compact ? null : (
          <span className="prop-empty" />
        );
      return (
        <input
          className="prop-input prop-input-date"
          type="date"
          value={(value as string) || ''}
          onChange={(e) => onChange!(e.target.value)}
        />
      );
    case 'url': {
      // Without a case of its own a URL landed in the text branch and sat on
      // the card as a full line of raw text ("https://trello.com/c/yksGXxLh")
      // — on a board that is pure noise. The host is what is shown, the full
      // address is what is opened.
      const href = String(value ?? '').trim();
      if (!href) return compact ? null : <span className="prop-empty">—</span>;
      let label = href;
      try {
        const u = new URL(href.includes('://') ? href : 'https://' + href);
        label = u.hostname.replace(/^www\./, '');
      } catch {
        /* not a valid URL — then leave it unshortened */
      }
      return (
        <a
          className="prop-url-chip"
          href={href.includes('://') ? href : 'https://' + href}
          target="_blank"
          rel="noopener noreferrer"
          title={href}
          onClick={(e) => e.stopPropagation()}
        >
          <LinkIcon size={11} />
          {label}
        </a>
      );
    }
    case 'person':
      return <PersonValue def={def} value={value} onChange={onChange} readOnly={readOnly} compact={compact} />;
    case 'text':
    default:
      if (compact) return value ? <span className="prop-text-chip">{String(value)}</span> : null;
      if (ro || !editing) {
        return (
          <span
            className="prop-text"
            onClick={ro ? undefined : () => setEditing(true)}
          >
            {value ? String(value) : <span className="prop-empty" />}
          </span>
        );
      }
      return (
        <input
          className="prop-input"
          autoFocus
          defaultValue={(value as string) || ''}
          onBlur={(e) => {
            setEditing(false);
            onChange!(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
          }}
        />
      );
  }
}
