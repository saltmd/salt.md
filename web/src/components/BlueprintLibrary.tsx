import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { BlueprintEntry, Workspace } from '../types';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { plural, t } from '../i18n';
import { ArrowLeft, Copy, FileText, Library, Plus } from 'lucide-react';

// The blueprint library — the shelf you land on when you make a workspace.
//
// A new workspace used to be a name prompt and an empty sheet, which answers the
// wrong question. Nobody wonders *what can this do*; they wonder *how do I
// start*. The shelf answers that before the first click, which is why it is the
// entrance now rather than a menu item somebody has to find.
//
// THE PREVIEW IS BUILT FROM THE BLUEPRINT, never from a screenshot. A picture is
// a promise that ages: three changes later it shows something you no longer get,
// and nobody notices, because an image does not break. Every column, colour and
// view below is read out of the file the server is about to import — it cannot
// show what is not in there.
//
// Own workspaces sit on the same shelf. "Like the one I already have" is the
// same question as "like a ready-made one", and splitting it across two screens
// is how people end up not finding either.

type Choice =
  | { kind: 'empty' }
  | { kind: 'blueprint'; entry: BlueprintEntry }
  | { kind: 'workspace'; ws: Workspace };

function propSummary(entry: BlueprintEntry) {
  const cols = entry.databases.reduce((n, d) => n + d.props.length, 0);
  const views = entry.databases.reduce((n, d) => n + d.views.length, 0);
  return { cols, views };
}

/** The first paragraph of the rules, as the shelf's honest one-liner about them.
 *
 * The markup is stripped rather than rendered: this is one line of plain text,
 * and rendering it would mean a second Markdown renderer here that has to keep
 * step with the real one. Showing the raw `**` is the third option and the worst.
 */
function rulesLead(rules: string) {
  for (const line of rules.split('\n')) {
    const s = line.trim();
    if (!s || s.startsWith('#')) continue;
    return s
      .replace(/\*\*(.+?)\*\*/g, '$1')
      .replace(/\*(.+?)\*/g, '$1')
      .replace(/`(.+?)`/g, '$1')
      .replace(/\[(.+?)\]\(.+?\)/g, '$1');
  }
  return '';
}

export default function BlueprintLibrary({
  workspaces,
  onCreated,
  onClose,
}: {
  workspaces: Workspace[];
  onCreated: (id: string) => void;
  onClose: () => void;
}) {
  useExclusiveModal(onClose);
  const [entries, setEntries] = useState<BlueprintEntry[] | null>(null);
  const [choice, setChoice] = useState<Choice | null>(null);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const bodyRef = useRef<HTMLDivElement>(null);

  // The shelf and the detail share one scroll box, so opening a card would
  // otherwise land you at whatever offset the grid was at — halfway down a page
  // you have not seen yet.
  useEffect(() => {
    bodyRef.current?.scrollTo({ top: 0 });
  }, [choice]);

  useEffect(() => {
    let alive = true;
    void api
      .library()
      .then((list) => alive && setEntries(list))
      .catch(() => alive && setEntries([]));
    return () => {
      alive = false;
    };
  }, []);

  // A personal workspace is not a blueprint anybody hands to a team, so it stays
  // off the shelf.
  const blueprintable = useMemo(() => workspaces.filter((w) => !w.personal), [workspaces]);

  const pick = (c: Choice) => {
    setChoice(c);
    setError('');
    setName(
      c.kind === 'blueprint' ? c.entry.title : c.kind === 'workspace' ? '' : '',
    );
  };

  const create = async () => {
    if (!choice || busy) return;
    const n = name.trim();
    if (!n) return;
    setBusy(true);
    setError('');
    try {
      if (choice.kind === 'blueprint') {
        const res = await api.useBlueprint(choice.entry.id, n);
        onCreated(res.workspaceId);
      } else if (choice.kind === 'workspace') {
        const ws = await api.createWorkspace(n, choice.ws.id);
        onCreated(ws.id);
      } else {
        const ws = await api.createWorkspace(n);
        onCreated(ws.id);
      }
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog wide lib-dialog" role="dialog" aria-modal="true" aria-label={t('New workspace')}>
          {!choice ? (
            <>
              <h2>
                <Library size={18} /> {t('Start with a ready-made workspace')}
              </h2>
              <p className="dialog-hint">
                {t('Each one brings its databases, views and house rules — and no data. You fill it.')}
              </p>

              <div className="lib-body" ref={bodyRef}>
                <div className="lib-grid">
                  <button className="lib-card lib-card--empty" onClick={() => pick({ kind: 'empty' })}>
                    <span className="lib-card-art" style={{ ['--accent' as string]: '#787774' }}>
                      <Plus size={26} />
                    </span>
                    <span className="lib-card-title">{t('Empty workspace')}</span>
                    <span className="lib-card-tagline">{t('Start from nothing and build it yourself.')}</span>
                  </button>

                  {entries === null && <div className="dialog-hint">{t('Loading…')}</div>}
                  {entries?.map((e) => {
                    const { cols, views } = propSummary(e);
                    return (
                      <button key={e.id} className="lib-card" onClick={() => pick({ kind: 'blueprint', entry: e })}>
                        <span className="lib-card-art" style={{ ['--accent' as string]: e.accent }}>
                          <span className="lib-card-emoji">{e.icon}</span>
                        </span>
                        <span className="lib-card-title">{t(e.title)}</span>
                        <span className="lib-card-tagline">{t(e.tagline)}</span>
                        <span className="lib-card-facts">
                          {plural(e.databases.length, '{n} database', '{n} databases')} ·{' '}
                          {plural(cols, '{n} column', '{n} columns')} ·{' '}
                          {plural(views, '{n} view', '{n} views')}
                          {e.rules ? ' · ' + t('house rules') : ''}
                        </span>
                      </button>
                    );
                  })}
                </div>

                {blueprintable.length > 0 && (
                  <>
                    <h3 className="lib-section">{t('Or like one you already have')}</h3>
                    <div className="lib-grid lib-grid--own">
                      {blueprintable.map((w) => (
                        <button key={w.id} className="lib-card lib-card--own" onClick={() => pick({ kind: 'workspace', ws: w })}>
                          <span className="lib-card-art" style={{ ['--accent' as string]: '#337ea9' }}>
                            <Copy size={22} />
                          </span>
                          <span className="lib-card-title">{w.name}</span>
                          <span className="lib-card-tagline">
                            {t('Its databases and rules, without the content.')}
                          </span>
                        </button>
                      ))}
                    </div>
                  </>
                )}
              </div>

              <div className="dialog-actions">
                <button className="btn-sm" onClick={onClose}>
                  {t('Cancel')}
                </button>
              </div>
            </>
          ) : (
            <>
              <h2>
                <button className="btn-icon lib-back" onClick={() => setChoice(null)} aria-label={t('Back')}>
                  <ArrowLeft size={16} />
                </button>
                {choice.kind === 'blueprint'
                  ? t(choice.entry.title)
                  : choice.kind === 'workspace'
                    ? choice.ws.name
                    : t('Empty workspace')}
              </h2>

              <div className="lib-body" ref={bodyRef}>
                {choice.kind === 'blueprint' && <BlueprintPreview entry={choice.entry} />}
                {choice.kind === 'workspace' && (
                  <p className="dialog-hint">
                    {t(
                      'Copies the databases with their columns, options and views, plus the workspace rules. Rows and documents stay where they are.',
                    )}
                  </p>
                )}
                {choice.kind === 'empty' && (
                  <p className="dialog-hint">{t('No databases, no rules — a blank workspace.')}</p>
                )}
              </div>

              {/* Outside the scroll box on purpose: it is the one field that has
                  to be filled in, and a long preview would push it under the
                  fold where nobody looks for it. */}
              <label className="lib-name">
                <span>{t('Name')}</span>
                <input
                  className="prop-input"
                  autoFocus
                  value={name}
                  placeholder={t('e.g. Team')}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void create();
                  }}
                />
              </label>
              {error && <div className="dialog-error">{error}</div>}

              <div className="dialog-actions">
                <button className="btn-sm" onClick={() => setChoice(null)}>
                  {t('Back')}
                </button>
                <button className="btn primary" disabled={!name.trim() || busy} onClick={() => void create()}>
                  {t('Create workspace')}
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </Portal>
  );
}

/** Everything here comes out of the blueprint the server is about to import. */
function BlueprintPreview({ entry }: { entry: BlueprintEntry }) {
  const lead = rulesLead(entry.rules);
  return (
    <div className="lib-preview">
      <p className="lib-preview-lead">{t(entry.tagline)}</p>

      {entry.databases.map((db) => (
        <div key={db.title} className="lib-db">
          <div className="lib-db-head">
            <span className="lib-db-icon">{db.icon}</span>
            <strong>{t(db.title)}</strong>
            <span className="lib-db-views">
              {db.views.map((v) => (
                <span key={v.name} className="lib-view-chip">
                  {t(v.name)}
                </span>
              ))}
            </span>
          </div>
          {db.description && <p className="lib-db-desc">{t(db.description)}</p>}
          <div className="lib-props">
            {db.props.map((p) => (
              <span key={p.name} className="lib-prop">
                <span className="lib-prop-name">{t(p.name)}</span>
                {p.options && p.options.length > 0 && (
                  <span className="lib-prop-opts">
                    {p.options.map((o) => (
                      /* The real option colour out of the blueprint — this is the
                         board you will actually get, not an illustration of one. */
                      <span
                        key={o.name}
                        className="lib-opt"
                        style={{ ['--opt' as string]: o.color || '#787774' }}
                      >
                        {t(o.name)}
                      </span>
                    ))}
                  </span>
                )}
              </span>
            ))}
          </div>
        </div>
      ))}

      {lead && (
        <div className="lib-rules">
          <div className="lib-rules-head">
            <FileText size={14} /> {t('House rules')}
          </div>
          <p>{t(lead)}</p>
        </div>
      )}
    </div>
  );
}
