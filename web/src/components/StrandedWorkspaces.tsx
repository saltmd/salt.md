import { useEffect, useState } from 'react';
import { api } from '../api';
import Portal from './Portal';
import { toast } from '../toast';
import { promptText } from '../dialog';
import { plural, t } from '../i18n';

// Workspaces with nobody in charge — the owner's clean-up view.
//
// Before W105 this could happen quietly: delete a workspace's last member and
// it stayed behind with zero of them. Gone from every sidebar, but still
// holding its pages, files and search index entries. New orphans can no longer
// appear; the ones already there still need a way out.

interface Stranded {
  id: string;
  name: string;
  owner: string;
  members: number;
  admins: number;
  pages: number;
  /** Truly nobody left — only then can it be adopted or deleted. */
  adoptable: boolean;
  deletable: boolean;
  personal: boolean;
}

export default function StrandedWorkspaces({ onClose, onChanged }: { onClose: () => void; onChanged: () => void }) {
  const [list, setList] = useState<Stranded[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    api
      .strandedWorkspaces()
      .then(setList)
      .catch((e: Error) => {
        // list stays null — otherwise the reassuring "all clear" would sit
        // right below the red error line, claiming the opposite of it.
        setError(e.message);
      });
  };
  useEffect(load, []);

  const adopt = async (w: Stranded) => {
    try {
      await api.adoptWorkspace(w.id);
      toast(t('Adopted “{name}” — it is in your list now.', { name: w.name }));
      load();
      onChanged();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const remove = async (w: Stranded) => {
    const typed = await promptText(
      t('Permanently delete “{name}” and its {pages}? Type the name to confirm.', {
        name: w.name,
        pages: plural(w.pages, '{n} page', '{n} pages'),
      }),
      { placeholder: w.name },
    );
    if (typed?.trim() !== w.name) return;
    try {
      await api.deleteStrandedWorkspace(w.id, w.name);
      toast(t('“{name}” deleted.', { name: w.name }));
      load();
      onChanged();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Workspaces with nobody in charge')}>
          <h2>{t('Workspaces with nobody in charge')}</h2>
          <p className="dialog-hint">
            {t(
              'These are workspaces nobody can look after any more. With no members left at all you can adopt or delete them. Where members remain, only someone in charge is missing — appoint one of them in user management.',
            )}
          </p>
          {error && <div className="login-error">{error}</div>}
          {list === null && <div className="dialog-hint">{t('Loading…')}</div>}
          {list?.length === 0 && (
            <div className="dialog-hint">{t('All clear — every workspace has someone in charge.')}</div>
          )}
          {list && list.length > 0 && (
            <div className="bg-list">
              {list.map((w) => (
                <div key={w.id} className="bg-row active">
                  <div className="bg-row-main">
                    <strong>{w.name}</strong>
                    <span className="bg-when">
                      {plural(w.pages, '{n} page', '{n} pages')} ·{' '}
                      {plural(w.members, '{n} member', '{n} members')} ·{' '}
                      {plural(w.admins, '{n} admin', '{n} admins')}
                      {w.owner ? ' · ' + t('last {name}', { name: w.owner }) : ''}
                    </span>
                  </div>
                  {w.adoptable && (
                    <button className="btn-sm" onClick={() => void adopt(w)}>{t('Adopt')}</button>
                  )}
                  {w.deletable && (
                    <button className="btn-sm danger" onClick={() => void remove(w)}>{t('Delete')}</button>
                  )}
                  {!w.deletable && (
                    <span className="dialog-hint">{t('Still has members: make one of them an admin.')}</span>
                  )}
                  {w.deletable && w.personal && (
                    <span className="dialog-hint">{t('Orphaned personal space — clean up only, do not open.')}</span>
                  )}
                </div>
              ))}
            </div>
          )}
          <div className="dialog-actions">
            <button className="btn" onClick={onClose}>{t('Close')}</button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
