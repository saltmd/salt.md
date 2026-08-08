import { useEffect, useState } from 'react';
import { api } from '../api';
import type { Comment, Revision } from '../types';
import Portal from './Portal';
import { confirm } from '../dialog';
import { useExclusiveModal } from '../modal';
import { toast } from '../toast';
import { formatMoment } from '../format';
import { t } from '../i18n';

const when = (iso: string) => formatMoment(iso, 'full');

export function HistoryModal({
  pageId,
  onClose,
  onRestored,
}: {
  pageId: string;
  onClose: () => void;
  onRestored: () => void;
}) {
  const [revisions, setRevisions] = useState<Revision[]>([]);
  useExclusiveModal(onClose);
  const load = () => void api.listRevisions(pageId).then(setRevisions).catch(() => {});
  useEffect(load, [pageId]);

  const restore = async (rev: Revision) => {
    if (!(await confirm(t('Restore the version from {when}? The current state is saved as a version first.', { when: when(rev.createdAt) })))) return;
    try {
      await api.restoreRevision(pageId, rev.id);
      toast(t('Version restored'));
      onRestored();
      onClose();
    } catch (e) {
      toast((e as Error).message || t('Restoring failed'));
    }
  };

  return (
    <Portal>
    <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Version history')}>
        <h2>{t('Version history')}</h2>
        <p className="dialog-hint">{t('Snapshots are taken on save (at most every 2 minutes, the latest 50).')}</p>
        <div className="user-list">
          {revisions.map((r) => (
            <div key={r.id} className="user-row">
              <span className="user-row-name">🕘 {when(r.createdAt)}</span>
              <span className="user-row-email">{r.authorName || t('unknown')}</span>
              <button className="btn-sm" onClick={() => void restore(r)}>{t('Restore')}</button>
            </div>
          ))}
          {revisions.length === 0 && <div className="dialog-hint">{t('No versions yet.')}</div>}
        </div>
        <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
      </div>
    </div>
    </Portal>
  );
}

export function CommentsModal({
  pageId,
  myUserId,
  onClose,
}: {
  pageId: string;
  myUserId: string;
  onClose: () => void;
}) {
  const [comments, setComments] = useState<Comment[]>([]);
  const [body, setBody] = useState('');
  const [showResolved, setShowResolved] = useState(false);
  useExclusiveModal(onClose);
  const load = () => void api.listComments(pageId).then(setComments).catch(() => {});
  useEffect(load, [pageId]);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!body.trim()) return;
    try {
      await api.createComment(pageId, body.trim());
      setBody('');
      load();
    } catch (err) {
      toast((err as Error).message || t('Could not post the comment'));
    }
  };
  const toggleResolve = async (c: Comment) => {
    await api.resolveComment(c.id, !c.resolvedAt).catch(() => {});
    load();
  };
  const remove = async (c: Comment) => {
    await api.deleteComment(c.id).catch(() => {});
    load();
  };

  const visible = comments.filter((c) => showResolved || !c.resolvedAt);

  return (
    <Portal>
    <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Comments')}>
        <h2>{t('Comments')}</h2>
        <label className="check-label">
          <input type="checkbox" checked={showResolved} onChange={(e) => setShowResolved(e.target.checked)} />
          {t('Show resolved')}
        </label>
        <div className="comment-list">
          {visible.map((c) => (
            <div key={c.id} className={'comment' + (c.resolvedAt ? ' resolved' : '')}>
              <div className="comment-head">
                <strong>{c.authorName || t('unknown')}</strong>
                <span className="comment-time">{when(c.createdAt)}</span>
              </div>
              <div className="comment-body">{c.body}</div>
              <div className="comment-actions">
                <button className="btn-sm" onClick={() => void toggleResolve(c)}>
                  {c.resolvedAt ? t('Reopen') : t('✓ Resolved')}
                </button>
                {c.authorId === myUserId && (
                  <button className="btn-sm danger" onClick={() => void remove(c)}>{t('Delete')}</button>
                )}
              </div>
            </div>
          ))}
          {visible.length === 0 && <div className="dialog-hint">{t('No comments yet.')}</div>}
        </div>
        <form className="user-add" onSubmit={add}>
          <input value={body} placeholder={t('Write a comment…')} onChange={(e) => setBody(e.target.value)} />
          <button className="btn primary" type="submit">{t('Send')}</button>
        </form>
        <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
      </div>
    </div>
    </Portal>
  );
}
