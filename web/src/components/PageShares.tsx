import { useEffect, useState } from 'react';
import { api } from '../api';
import { toast } from '../toast';
import { t } from '../i18n';

type Access = 'view' | 'edit';
type Visibility = 'workspace' | 'private';
interface Share {
  userId: string;
  name: string;
  email: string;
  access: Access;
}
interface Member {
  userId: string;
  name: string;
  email: string;
}

// Who may see this page: everyone in the workspace, only its owner, or chosen
// members. It hangs off the lock button like the share menu — same place,
// same dismissal — so there is no second place where access is decided and no
// modal in between.
export default function PageShares({
  pageId,
  workspaceId,
  myUserId,
  visibility,
  onClose,
  onVisibilityChange,
}: {
  pageId: string;
  workspaceId: string;
  myUserId: string;
  visibility: Visibility;
  onClose: () => void;
  onVisibilityChange: (v: Visibility) => void;
}) {
  const [shares, setShares] = useState<Share[] | null>(null);
  const [denied, setDenied] = useState(false);
  const [members, setMembers] = useState<Member[]>([]);
  const [picked, setPicked] = useState('');
  const [pickedAccess, setPickedAccess] = useState<Access>('view');

  const load = () =>
    api
      .listPageShares(pageId)
      .then((l) => {
        setShares(l);
        setDenied(false);
      })
      .catch(() => {
        setShares([]);
        setDenied(true);
      });
  useEffect(() => {
    void load();
  }, [pageId]);
  useEffect(() => {
    void api.listMembers(workspaceId).then(setMembers).catch(() => {});
  }, [workspaceId]);

  const setMode = async (next: Visibility) => {
    try {
      await api.updatePage(pageId, { visibility: next });
      onVisibilityChange(next);
      // Going private hides the page from every non-admin, so the member
      // list stays open — the intermediate state must not sit unnoticed.
      if (next === 'private') load();
    } catch {
      toast(t('Visibility not saved'));
    }
  };

  const grant = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!picked) return;
    try {
      await api.grantPageShare(pageId, picked, pickedAccess);
      setPicked('');
      load();
    } catch (err) {
      toast((err as Error).message || t('Could not share the page'));
    }
  };

  const changeAccess = async (s: Share, access: Access) => {
    try {
      await api.grantPageShare(pageId, s.userId, access);
      load();
    } catch (err) {
      toast((err as Error).message || t('Could not change the access'));
    }
  };

  const revoke = async (s: Share) => {
    try {
      await api.revokePageShare(pageId, s.userId);
      load();
    } catch (err) {
      toast((err as Error).message || t('Could not remove the access'));
    }
  };

  const granted = new Set((shares ?? []).map((s) => s.userId));
  const candidates = members.filter((m) => !granted.has(m.userId));

  return (
    <div className="menu access-menu" role="dialog" aria-label={t('Page access')}>
      <h2 className="access-title">{t('Page access')}</h2>
          <div className="user-list">
            {(
              [
                { id: 'workspace', label: t('Everyone in the workspace') },
                { id: 'private', label: t('Only me and chosen members') },
              ] as { id: Visibility; label: string }[]
            ).map((m) => (
              <label key={m.id} className="user-row">
                <input
                  type="radio"
                  name="page-access"
                  value={m.id}
                  checked={visibility === m.id}
                  onChange={() => void setMode(m.id)}
                />
                <span className="user-row-name">{m.label}</span>
              </label>
            ))}
          </div>
          {visibility === 'private' &&
            (denied ? (
              <p>{t('Only workspace admins and the page owner can manage access.')}</p>
            ) : (
              <>
                <p className="prop-empty">
                  {t('Only the people below can see this page and its sub-pages. Workspace admins always can.')}
                </p>
                <div className="user-list">
                  {(shares ?? []).map((s) => (
                    <div key={s.userId} className="user-row">
                      <span className="user-row-name">
                        {s.name} {s.userId === myUserId && <span className="prop-empty">{t('(you)')}</span>}
                      </span>
                      <span className="user-row-email">{s.email}</span>
                      <span className="user-row-controls">
                        <select
                          className="prop-select"
                          value={s.access}
                          onChange={(e) => void changeAccess(s, e.target.value as Access)}
                        >
                          <option value="view">{t('Can view')}</option>
                          <option value="edit">{t('Can edit')}</option>
                        </select>
                        <button className="btn-sm danger" onClick={() => void revoke(s)}>
                          {t('Remove')}
                        </button>
                      </span>
                    </div>
                  ))}
                </div>
                {candidates.length > 0 && (
                  <form className="share-add" onSubmit={(e) => void grant(e)}>
                    <select
                      name="share-member"
                      value={picked}
                      onChange={(e) => setPicked(e.target.value)}
                    >
                      <option value="">{t('Add a member…')}</option>
                      {candidates.map((m) => (
                        <option key={m.userId} value={m.userId}>
                          {m.name} ({m.email})
                        </option>
                      ))}
                    </select>
                    <select
                      name="share-access"
                      value={pickedAccess}
                      onChange={(e) => setPickedAccess(e.target.value as Access)}
                    >
                      <option value="view">{t('Can view')}</option>
                      <option value="edit">{t('Can edit')}</option>
                    </select>
                    <button className="btn-sm" type="submit" disabled={!picked}>
                      {t('Share')}
                    </button>
                  </form>
                )}
              </>
            ))}
          <div className="share-actions share-actions-full">
            {shares?.length === 0 && (
              <p className="prop-empty">{t('Nobody yet. The page is visible to nobody but you and the workspace admins.')}</p>
            )}
            <button className="btn-sm" onClick={onClose}>
              {t('Done')}
            </button>
          </div>
    </div>
  );
}
