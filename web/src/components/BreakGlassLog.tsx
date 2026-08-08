import { useEffect, useState } from 'react';
import { api } from '../api';
import Portal from './Portal';
import { toast } from '../toast';
import { formatMoment } from '../format';
import { t } from '../i18n';

// Emergency access to a workspace — for the people in charge of it.
//
// Access you only hear about by email is a notification with no handle on it.
// This is where it says who looked in, when, and with what reason — and a
// running access can be ended on the spot.

interface Grant {
  id: string;
  user: string;
  reason: string;
  createdAt: string;
  expiresAt: string;
  revokedAt: string | null;
  active: boolean;
}

const when = (iso: string) => formatMoment(iso, 'datetime');

export default function BreakGlassLog({
  workspaceId,
  workspaceName,
  onClose,
}: {
  workspaceId: string;
  workspaceName: string;
  onClose: () => void;
}) {
  const [grants, setGrants] = useState<Grant[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    api
      .listBreakGlass(workspaceId)
      .then(setGrants)
      .catch((e: Error) => {
        setError(e.message);
        setGrants([]);
      });
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(load, [workspaceId]);

  const revoke = async (g: Grant) => {
    try {
      await api.revokeBreakGlass(workspaceId, g.id);
      toast(`Zugriff von ${g.user} beendet.`);
      load();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Emergency access log')}>
          <h2>{t('Emergency access log')}</h2>
          <p className="dialog-hint">
            {t(
              'A look inside “{name}” by the instance owner. Emergency access allows reading only, expires after two hours, and can be ended early at any time.',
              { name: workspaceName },
            )}
          </p>
          {error && <div className="login-error">{error}</div>}
          {grants === null && <div className="dialog-hint">{t('Loading…')}</div>}
          {grants?.length === 0 && (
            <div className="dialog-hint">{t('There has been no emergency access to this workspace so far.')}</div>
          )}
          {grants && grants.length > 0 && (
            <div className="bg-list">
              {grants.map((g) => (
                <div key={g.id} className={'bg-row' + (g.active ? ' active' : '')}>
                  <div className="bg-row-main">
                    <strong>{g.user}</strong>
                    <span className="bg-when">
                      {when(g.createdAt)}
                      {g.active
                        ? ' · ' + t('runs until {time}', { time: when(g.expiresAt) })
                        : g.revokedAt
                          ? ' · ' + t('ended early')
                          : ' · ' + t('expired')}
                    </span>
                    <span className="bg-reason">{g.reason}</span>
                  </div>
                  {g.active && (
                    <button className="btn-sm danger" onClick={() => void revoke(g)}>
                      {t('End it now')}
                    </button>
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
