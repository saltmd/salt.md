import { useEffect, useState } from 'react';
import { Check, Send } from 'lucide-react';
import { api } from '../api';
import { formField } from './CollectionView';
import { PageIcon } from '../pageIcon';
import { PublicFormConfig, PropDef } from '../types';
import { toast } from '../toast';
import { t } from '../i18n';

// Standalone, unauthenticated form page served at /form/{token}. Reuses the
// exact same field renderer as the in-app form view, but posts to the public
// endpoint so anyone with the link can submit a row without an account.
export default function PublicForm({ token }: { token: string }) {
  const [cfg, setCfg] = useState<PublicFormConfig | null>(null);
  const [state, setState] = useState<'loading' | 'ready' | 'notfound'>('loading');
  const [title, setTitle] = useState('');
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  useEffect(() => {
    api
      .publicFormConfig(token)
      .then((c) => {
        setCfg(c);
        setState('ready');
      })
      .catch(() => setState('notfound'));
  }, [token]);

  const set = (id: string, v: unknown) => setValues((p) => ({ ...p, [id]: v }));

  const submit = async () => {
    if (!title.trim() || busy || !cfg) return;
    setBusy(true);
    try {
      const props: Record<string, unknown> = {};
      for (const f of cfg.schema) {
        const v = values[f.id];
        if (v === undefined || v === '' || v === null) continue;
        if (Array.isArray(v) && v.length === 0) continue;
        props[f.id] = v;
      }
      await api.publicFormSubmit(token, title.trim(), props);
      setDone(true);
    } catch {
      toast(t('Sending failed'));
    } finally {
      setBusy(false);
    }
  };

  if (state === 'loading') {
    return (
      <div className="public-form-page">
        <div className="form-card public-form-loading">{t('Loading…')}</div>
      </div>
    );
  }
  if (state === 'notfound' || !cfg) {
    return (
      <div className="public-form-page">
        <div className="form-card form-done">
          <h2>{t('Form not found')}</h2>
          <p>{t('This link is not valid or has been switched off.')}</p>
        </div>
      </div>
    );
  }

  if (done) {
    return (
      <div className="public-form-page">
        <div className="form-card form-done">
          <div className="form-done-ic">
            <Check size={30} />
          </div>
          <h2>{t('Thank you!')}</h2>
          <p>{t('Your answer has been submitted.')}</p>
          <button
            className="btn-sm primary"
            onClick={() => {
              setDone(false);
              setTitle('');
              setValues({});
            }}
          >
            {t('Send another answer')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="public-form-page">
      <div className="form-card">
        <div className="public-form-head">
          {cfg.icon && <PageIcon icon={cfg.icon} size={42} />}
          <h1 className="form-heading form-heading-static">{cfg.formTitle || cfg.title || t('Form')}</h1>
        </div>
        {cfg.formDesc && <p className="form-desc form-desc-static">{cfg.formDesc}</p>}
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
          {cfg.schema.map((f: PropDef) => (
            <label key={f.id} className={'form-field' + (f.type === 'checkbox' ? ' form-field--check' : '')}>
              <span className="form-label">{f.name}</span>
              {formField(f, values[f.id], (v) => set(f.id, v))}
            </label>
          ))}
        </div>
        <div className="form-actions">
          <button className="btn-sm primary form-submit" disabled={busy || !title.trim()} onClick={() => void submit()}>
            <Send size={14} /> {cfg.formSubmit?.trim() || 'Absenden'}
          </button>
        </div>
        <div className="public-form-footer">
          {t('Made with')} <b>salt.md</b>
        </div>
      </div>
    </div>
  );
}
