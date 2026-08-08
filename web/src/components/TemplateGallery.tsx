import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import type { PageMeta } from '../types';
import Portal from './Portal';
import { PageIcon } from '../pageIcon';
import { compare } from '../format';
import { useExclusiveModal } from '../modal';
import { tagColorClass } from '../tags';
import { plural, t } from '../i18n';
import { LayoutTemplate, Table2, Trash2 } from 'lucide-react';

// The template gallery: pick from what is prepared, and SEE it first.
//
// Until now templates were a collapsed sidebar section with a ＋ per row. That
// works once you know what "Onboarding" contains; it is useless the first time,
// which is exactly when a template is worth having. So: a list on the left, the
// template's own content on the right, and one button that says what happens.
//
// The preview is the page's Markdown export — the same text the export and the
// MCP get_page return, so nothing is rendered here that the rest of the
// instance would render differently. Databases come back as a table of their
// rows, which is precisely what you want to judge before copying one.
//
// This is also the shape a community catalogue needs later: a template is
// already a self-contained snapshot, and a workspace export is already a ZIP —
// so what is missing there is a place and a preview, and the preview is here.

/** Categories come from the template's tags — no second taxonomy to maintain. */
function categories(list: PageMeta[]): string[] {
  const seen = new Map<string, number>();
  for (const p of list) for (const tag of p.tags ?? []) seen.set(tag, (seen.get(tag) ?? 0) + 1);
  return [...seen.keys()].sort(compare);
}

export default function TemplateGallery({
  templates,
  onUse,
  onUnflag,
  onTrash,
  onClose,
}: {
  templates: PageMeta[];
  onUse: (id: string) => void;
  onUnflag: (id: string) => void;
  onTrash: (id: string) => void;
  onClose: () => void;
}) {
  useExclusiveModal(onClose);
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('');
  const [selected, setSelected] = useState<string | null>(templates[0]?.id ?? null);
  const [preview, setPreview] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const cats = useMemo(() => categories(templates), [templates]);

  const list = useMemo(() => {
    const q = query.trim().toLowerCase();
    return templates
      .filter((p) => !category || (p.tags ?? []).includes(category))
      .filter(
        (p) =>
          !q ||
          (p.title || '').toLowerCase().includes(q) ||
          (p.description || '').toLowerCase().includes(q),
      )
      .sort((a, b) => compare(a.title || '', b.title || ''));
  }, [templates, query, category]);

  // Keep the selection inside the filtered list, or the preview shows a
  // template the list no longer offers.
  useEffect(() => {
    if (!list.some((p) => p.id === selected)) setSelected(list[0]?.id ?? null);
  }, [list, selected]);

  useEffect(() => {
    if (!selected) {
      setPreview(null);
      return;
    }
    let alive = true;
    setPreview(null);
    void api
      .exportText(selected)
      .then((md) => alive && setPreview(md))
      .catch(() => alive && setPreview(''));
    return () => {
      alive = false;
    };
  }, [selected]);

  const current = list.find((p) => p.id === selected);

  const use = async () => {
    if (!current || busy) return;
    setBusy(true);
    try {
      await onUse(current.id);
      onClose();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog wide tpl-dialog" role="dialog" aria-modal="true" aria-label={t('Templates')}>
          <h2>
            <LayoutTemplate size={18} /> {t('Templates')}
          </h2>
          <p className="dialog-hint">
            {t(
              'A template is a snapshot: using one copies it, and from then on the two have nothing to do with each other.',
            )}
          </p>

          <div className="tpl-controls">
            <input
              className="prop-input"
              placeholder={t('Search templates…')}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label={t('Search templates…')}
            />
            {cats.length > 0 && (
              <div className="tpl-cats">
                <button
                  className={'tpl-cat' + (category === '' ? ' active' : '')}
                  onClick={() => setCategory('')}
                >
                  {t('All')}
                </button>
                {cats.map((c) => (
                  <button
                    key={c}
                    className={'tpl-cat' + (category === c ? ' active' : '')}
                    onClick={() => setCategory(c)}
                  >
                    {c}
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="tpl-body">
            <div className="tpl-list">
              {list.map((p) => (
                <button
                  key={p.id}
                  className={'tpl-item' + (p.id === selected ? ' active' : '')}
                  onClick={() => setSelected(p.id)}
                >
                  <span className="tpl-item-icon">
                    <PageIcon
                      icon={p.icon}
                      size={16}
                      fallback={p.type === 'collection' ? <Table2 size={16} /> : <LayoutTemplate size={16} />}
                    />
                  </span>
                  <span className="tpl-item-main">
                    <span className="tpl-item-title">{p.title || t('Untitled')}</span>
                    {p.description && <span className="tpl-item-desc">{p.description}</span>}
                    {(p.tags ?? []).length > 0 && (
                      <span className="tpl-item-tags">
                        {(p.tags ?? []).map((tag) => (
                          <span key={tag} className={'tag-chip ' + tagColorClass(tag, {})}>
                            #{tag}
                          </span>
                        ))}
                      </span>
                    )}
                  </span>
                </button>
              ))}
              {list.length === 0 && <div className="db-empty">{t('No templates yet.')}</div>}
            </div>

            <div className="tpl-preview">
              {!current ? (
                <div className="dialog-hint">
                  {t('Save any page as a template from its ⋯ menu — the page itself stays untouched.')}
                </div>
              ) : (
                <>
                  <div className="tpl-preview-head">
                    <strong>{current.title || t('Untitled')}</strong>
                    {current.type === 'collection' && (
                      <span className="index-badge" title={t('Collection')}>
                        <Table2 size={11} />
                      </span>
                    )}
                  </div>
                  {preview === null ? (
                    <div className="dialog-hint">{t('Loading…')}</div>
                  ) : preview === '' ? (
                    <div className="dialog-hint">{t('No preview available.')}</div>
                  ) : (
                    /* Plain text on purpose: a preview that renders blocks would
                       be a second editor to keep in step with the real one. */
                    <pre className="tpl-preview-text">{preview}</pre>
                  )}
                </>
              )}
            </div>
          </div>

          <div className="dialog-actions tpl-actions">
            <span className="tpl-count">{plural(templates.length, '{n} template', '{n} templates')}</span>
            {current && (
              <>
                <button className="btn-sm" onClick={() => onUnflag(current.id)}>
                  <LayoutTemplate size={13} /> {t('Remove template flag')}
                </button>
                <button className="btn-sm danger" onClick={() => onTrash(current.id)}>
                  <Trash2 size={13} /> {t('Delete')}
                </button>
              </>
            )}
            <button className="btn-sm" onClick={onClose}>
              {t('Close')}
            </button>
            <button className="btn primary" disabled={!current || busy} onClick={() => void use()}>
              {t('Use template')}
            </button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
