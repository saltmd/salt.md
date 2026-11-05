import { useCallback, useEffect, useRef, useState } from 'react';
import { Inbox as InboxIcon, X, CheckCheck, Mail, MailOpen, ArrowUpRight, RefreshCw } from 'lucide-react';
import { api } from '../api';
import type { InboxPage, ReplyNotification } from '../types';
import { t, plural } from '../i18n';
import { formatRelative } from '../format';
import { useExclusiveModal } from '../modal';
import { initials, nameColor } from './CommentsPanel';
import Portal from './Portal';
const EMPTY: InboxPage = { items: [], unreadCount: 0, through: 0, nextCursor: 0 };
export default function Inbox({ onNavigate }: { onNavigate: (pageId: string, workspaceId: string) => void }) {
  const [open, setOpen] = useState(false);
  const [unreadOnly, setUnreadOnly] = useState(true);
  const [limit, setLimit] = useState(50);
  const [data, setData] = useState<InboxPage>(EMPTY);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [working, setWorking] = useState(false);
  const sequence = useRef(0);
  const trigger = useRef<HTMLButtonElement>(null);
  const refresh = useCallback(async () => {
    const seq = ++sequence.current;
    try {
      let result = await api.inbox(unreadOnly);
      while (result.nextCursor && result.items.length < limit) {
        const more = await api.inbox(unreadOnly, result.nextCursor);
        result = { ...more, items: [...result.items, ...more.items] };
      }
      if (sequence.current === seq) { setData(result); setError(''); }
    } catch (e) { if (sequence.current === seq) setError((e as Error).message || t('Could not load the inbox')); }
    finally { if (sequence.current === seq) setLoading(false); }
  }, [unreadOnly, limit]);
  useEffect(() => {
    void refresh();
    const load = () => { void refresh(); };
    const visible = () => { if (document.visibilityState === 'visible') load(); };
    for (const event of ['salt:notifications', 'focus']) window.addEventListener(event, load);
    document.addEventListener('visibilitychange', visible);
    return () => { sequence.current++; for (const event of ['salt:notifications', 'focus']) window.removeEventListener(event, load); document.removeEventListener('visibilitychange', visible); };
  }, [refresh]);
  const close = () => { setOpen(false); requestAnimationFrame(() => trigger.current?.focus()); };
  const changeRead = async (n: ReplyNotification) => {
    if (working) return; setWorking(true);
    try { await api.readNotification(n.id, !n.readAt); await refresh(); }
    catch (e) { setError((e as Error).message); } finally { setWorking(false); }
  };
  const markAll = async () => {
    if (working || !data.through) return; setWorking(true);
    try { await api.readAllNotifications(data.through); await refresh(); }
    catch (e) { setError((e as Error).message); } finally { setWorking(false); }
  };
  const visit = async (n: ReplyNotification) => {
    if (working) return; setWorking(true);
    try {
      const page = await api.getPage(n.pageId);
      setOpen(false); onNavigate(n.pageId, page.workspaceId);
      // The panel acknowledges read only after the addressed message renders.
      const fragment = new URLSearchParams({ comment: n.commentId, notification: String(n.id) });
      window.history.replaceState(window.history.state, '', window.location.pathname + window.location.search + '#' + fragment);
      window.dispatchEvent(new Event('hashchange'));
    } catch { setError(t('This conversation is no longer available.')); }
    finally { setWorking(false); }
  };
  return <>
    <button ref={trigger} type="button" className={'sidebar-search inbox-trigger' + (open ? ' active' : '')} onClick={() => { setOpen(true); void refresh(); }} aria-haspopup="dialog" aria-expanded={open} aria-label={t('Inbox')}>
      <span className="sidebar-item-label"><InboxIcon size={15} />{t('Inbox')}</span>
      {data.unreadCount > 0 && <span className="inbox-badge" aria-label={plural(data.unreadCount, '{n} unread notification', '{n} unread notifications')}>{data.unreadCount > 99 ? '99+' : data.unreadCount}</span>}
    </button>
    {open && <InboxDialog onClose={close}>
      <div className="inbox-heading"><div><h2 id="inbox-title"><InboxIcon size={21} />{t('Inbox')}</h2><p>{t('Replies to your comments')}</p></div><button type="button" className="icon-btn" title={t('Close')} onClick={close}><X size={19} /></button></div>
      <div className="inbox-tools"><div className="inbox-filters" role="group" aria-label={t('Filter notifications')}>
        <button type="button" className={unreadOnly ? 'active' : ''} onClick={() => { setUnreadOnly(true); setLimit(50); }}>{t('Unread')}<span>{data.unreadCount}</span></button>
        <button type="button" className={!unreadOnly ? 'active' : ''} onClick={() => { setUnreadOnly(false); setLimit(50); }}>{t('All')}</button>
      </div><button type="button" className="cp-act" disabled={working || !data.unreadCount} onClick={() => void markAll()}><CheckCheck size={15} />{t('Mark all as read')}</button></div>
      {error && <div className="inbox-error" role="alert"><span>{error}</span><button className="cp-act" type="button" onClick={() => void refresh()}><RefreshCw size={13} />{t('Retry')}</button></div>}
      <div className="inbox-list" aria-busy={loading || working}>
        {loading ? <div className="inbox-empty">{t('Loading…')}</div> : data.items.length === 0 ? <div className="inbox-empty"><InboxIcon size={34} strokeWidth={1.3} /><h3>{unreadOnly ? t('You’re all caught up') : t('No notifications yet')}</h3><p>{unreadOnly ? t('New replies will appear here.') : t('When someone replies to your comment, you’ll see it here.')}</p></div> : data.items.map((n) => <article className={'inbox-item' + (!n.readAt ? ' is-unread' : '')} key={n.id}>
          <button className="inbox-open" type="button" disabled={working} onClick={() => void visit(n)}>
            <span className="inbox-avatar" style={{ background: n.actorAvatar ? 'transparent' : n.actorColor || nameColor(n.actorName) }}>{n.actorAvatar ? <img src={n.actorAvatar} alt="" /> : initials(n.actorName)}</span>
            <span className="inbox-content"><span className="inbox-item-title"><strong>{n.actorName}</strong> {t('replied to your comment')}<ArrowUpRight size={13} /></span><span className="inbox-context">{n.workspaceName}<span aria-hidden="true"> / </span>{n.pageTitle || t('Untitled')}</span><span className="inbox-preview">{n.preview}</span><time>{formatRelative(n.createdAt)}</time></span>
            {!n.readAt && <span className="inbox-unread-dot" aria-label={t('Unread')} />}
          </button><button type="button" className="icon-btn inbox-read" disabled={working} title={n.readAt ? t('Mark as unread') : t('Mark as read')} onClick={() => void changeRead(n)}>{n.readAt ? <Mail size={15} /> : <MailOpen size={15} />}</button>
        </article>)}
        {!!data.nextCursor && <button type="button" className="btn inbox-more" disabled={working} onClick={() => setLimit((n) => n + 50)}>{t('Load more')}</button>}
      </div>
    </InboxDialog>}
  </>;
}
function InboxDialog({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  const panel = useRef<HTMLDivElement>(null);
  useExclusiveModal(onClose);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    panel.current?.querySelector<HTMLButtonElement>('button')?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.stopPropagation(); closeRef.current(); return; }
      if (e.key !== 'Tab') return;
      const buttons = [...(panel.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])];
      const first = buttons[0], last = buttons[buttons.length - 1];
      if (!panel.current?.contains(document.activeElement)) { e.preventDefault(); first?.focus(); }
      else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
    };
    document.addEventListener('keydown', key);
    return () => document.removeEventListener('keydown', key);
  }, []);

  return <Portal><div className="modal-overlay inbox-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}><div ref={panel} className="inbox-dialog" role="dialog" aria-modal="true" aria-labelledby="inbox-title">{children}</div></div></Portal>;
}
