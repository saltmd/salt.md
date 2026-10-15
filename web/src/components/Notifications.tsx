import { useEffect, useRef, useState } from 'react';
import { Bell, X } from 'lucide-react';
import { api } from '../api';
import { t } from '../i18n';
import { formatRelative } from '../format';
import { openComment, openPageMention, hasPageMention } from '../commentMentions';
import type { MentionNotification } from '../types';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';

export default function Notifications() {
  const [open, setOpen] = useState(false);
  const [data, setData] = useState<{ items: MentionNotification[]; unread: number }>({ items: [], unread: 0 });
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(true);
  const button = useRef<HTMLButtonElement>(null);
  const load = async () => {
    try { setData(await api.notifications()); setError(false); }
    catch { setError(true); setData({ items: [], unread: 0 }); }
    finally { setLoading(false); }
  };
  useEffect(() => {
    let active = true;
    let busy = false;
    const refresh = async () => {
      if (document.hidden || busy) return;
      busy = true;
      try { const result = await api.notifications(); if (active) { setData(result); setError(false); } }
      catch { if (active) { setData({ items: [], unread: 0 }); setError(true); } }
      finally { busy = false; if (active) setLoading(false); }
    };
    void refresh();
    const interval = window.setInterval(() => void refresh(), 15000);
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
    return () => { active = false; clearInterval(interval); window.removeEventListener('focus', refresh); document.removeEventListener('visibilitychange', refresh); };
  }, []);
  const close = () => { setOpen(false); requestAnimationFrame(() => button.current?.focus()); };
  return <>
    <button ref={button} className="sidebar-search" onClick={() => { setOpen(true); setLoading(true); void load(); }}>
      <span className="sidebar-item-label"><Bell size={15} /> {t('Notifications')}</span>
      {data.unread > 0 && <span className="cp-count">{data.unread}</span>}
    </button>
    {open && <NotificationDialog items={data.items} loading={loading} error={error} onClose={close} onRetry={load} onOpen={async n => {
      try {
        // Fetch again before navigating, so a revoked or deleted page does not
        // open from a stale item. The API also checks access to the comment.
        if (n.mentionId) {
          const page = await api.getPage(n.pageId);
          if (!hasPageMention(page.content, n.mentionId)) { await load(); return; }
        } else {
          const comments = await api.listComments(n.pageId);
          if (!comments.some(c => c.id === n.commentId)) { await load(); return; }
        }
        await api.readNotification(n.id);
        setOpen(false);
        if (n.mentionId) openPageMention(n.pageId, n.mentionId);
        else openComment(n.pageId, n.commentId);
        void load();
      } catch { setError(true); setData({ items: [], unread: 0 }); }
    }} />}
  </>;
}

function NotificationDialog({ items, loading, error, onClose, onRetry, onOpen }: {
  items: MentionNotification[]; loading: boolean; error: boolean;
  onClose: () => void; onRetry: () => void; onOpen: (n: MentionNotification) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useExclusiveModal(onClose);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <Portal>
    <dialog ref={dialog} className="mention-notifications" aria-modal="true" aria-label={t('Notifications')}
      onCancel={e => { e.preventDefault(); onClose(); }}>
      <div className="cp-head"><h2>{t('Notifications')}</h2><button className="icon-btn" title={t('Close')} onClick={onClose}><X size={16} /></button></div>
      <div className="notification-list">
        {error ? <p role="alert">{t('Could not load notifications.')} <button className="btn btn-sm" onClick={onRetry}>{t('Retry')}</button></p>
          : loading ? <p>{t('Loading…')}</p>
          : !items.length ? <p>{t('No mentions yet.')}</p>
          : items.map(n => <button className={'notification-item' + (n.read ? '' : ' is-unread')} key={n.id} onClick={() => onOpen(n)}>
            <strong>{t('{name} mentioned you', { name: n.authorName })}</strong>
            <span>{n.pageTitle || t('Untitled')}</span>
            <span className="notification-excerpt">{n.body}</span>
            <small>{!n.read && <span>{t('Unread')} · </span>}{formatRelative(n.createdAt)}</small>
          </button>)}
      </div>
    </dialog>
  </Portal>;
}
