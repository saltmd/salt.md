import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import Portal from './Portal';
import { confirm } from '../dialog';
import { useExclusiveModal } from '../modal';
import { toast } from '../toast';
import { t } from '../i18n';

type Role = 'admin' | 'member' | 'viewer';
interface Member {
  userId: string;
  name: string;
  email: string;
  role: Role;
}

const roleLabel = (r: Role): string =>
  ({
    admin: t('Admin'),
    member: t('Member (can edit)'),
    viewer: t('Viewer (read only)'),
  })[r];

export default function WorkspaceMembers({
  workspaceId,
  myUserId,
  myRole,
  onClose,
  onChanged,
}: {
  workspaceId: string;
  myUserId: string;
  myRole: Role;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [members, setMembers] = useState<Member[]>([]);
  const [email, setEmail] = useState('');
  const [newRole, setNewRole] = useState<Role>('member');
  const isAdmin = myRole === 'admin';
  useExclusiveModal(onClose);

  const load = () => void api.listMembers(workspaceId).then(setMembers).catch(() => {});
  useEffect(load, [workspaceId]);

  const [inviteLink, setInviteLink] = useState('');

  // Invite by link/email: creates an invitation the recipient accepts (they set
  // their own password), instead of an admin adding an already-existing user.
  const invite = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const r = await api.createInvite(email.trim(), newRole, workspaceId);
      setInviteLink(r.link);
      setEmail('');
      void navigator.clipboard?.writeText(r.link);
      toast(r.emailed ? t('Invitation sent by email') : t('Invitation link copied'));
      load();
    } catch (err) {
      toast((err as Error).message || t('Invitation failed'));
    }
  };

  const changeRole = async (m: Member, role: Role) => {
    try {
      await api.updateMember(workspaceId, m.userId, role);
      load();
    } catch (err) {
      toast((err as Error).message || t('Could not change the role'));
    }
  };

  const remove = async (m: Member, confirmPrivate = false) => {
    const self = m.userId === myUserId;
    if (
      !confirmPrivate &&
      !(await confirm(self ? t('Leave this workspace?') : t('Remove {name}?', { name: m.name }), {
        danger: true,
      }))
    )
      return;
    try {
      await api.removeMember(workspaceId, m.userId, confirmPrivate);
      if (self) {
        onChanged();
        onClose();
      } else {
        load();
      }
    } catch (err) {
      const msg = (err as Error).message || t('Could not remove the member');
      // 409 means: private pages are stored here, they will stay behind, and
      // afterwards only the workspace admins can see them. Detected by STATUS,
      // not by message text — that changes with every rewording, and removal
      // through the interface would silently stop working.
      if (!confirmPrivate && (err as ApiError).status === 409) {
        const question = self ? t('Leave anyway?') : t('Remove anyway?');
        if (
          await confirm(`${msg}\n\n${question}`, {
            danger: true,
            confirmText: self ? t('Leave') : t('Remove'),
          })
        ) {
          await remove(m, true);
        }
        return;
      }
      toast(msg);
    }
  };

  return (
    <Portal>
    <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Workspace members')}>
        <h2>{t('Workspace members')}</h2>
        <div className="user-list">
          {members.map((m) => (
            <div key={m.userId} className="user-row">
              <span className="user-row-name">
                {m.name} {m.userId === myUserId && <span className="prop-empty">{t('(you)')}</span>}
              </span>
              <span className="user-row-email">{m.email}</span>
              {isAdmin && m.userId !== myUserId ? (
                <select
                  className="prop-select"
                  value={m.role}
                  onChange={(e) => void changeRole(m, e.target.value as Role)}
                >
                  <option value="admin">{t('Admin')}</option>
                  <option value="member">{t('Member')}</option>
                  <option value="viewer">{t('Viewer')}</option>
                </select>
              ) : (
                <span className="token-scope write">{roleLabel(m.role)}</span>
              )}
              {(isAdmin || m.userId === myUserId) && (
                <button
                  className="icon-btn danger"
                  title={m.userId === myUserId ? t('Leave') : t('Remove')}
                  onClick={() => void remove(m)}
                >
                  ✕
                </button>
              )}
            </div>
          ))}
          {members.length === 0 && <div className="dialog-hint">{t('No members yet.')}</div>}
        </div>
        {isAdmin && (
          <>
            <form className="user-add" onSubmit={invite}>
              <input
                value={email}
                placeholder={t('Invite by email (blank = link only)')}
                onChange={(e) => setEmail(e.target.value)}
              />
              <select className="prop-select" value={newRole} onChange={(e) => setNewRole(e.target.value as Role)}>
                <option value="member">{t('Member')}</option>
                <option value="viewer">{t('Viewer')}</option>
                <option value="admin">{t('Admin')}</option>
              </select>
              <button className="btn primary" type="submit">{t('Invite')}</button>
            </form>
            {inviteLink && (
              <div className="invite-link">
                <span className="dialog-hint">{t('Invitation link (valid 14 days, copied):')}</span>
                <input className="prop-input" readOnly value={inviteLink} onFocus={(e) => e.currentTarget.select()} />
              </div>
            )}
          </>
        )}
        <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
      </div>
    </div>
    </Portal>
  );
}
