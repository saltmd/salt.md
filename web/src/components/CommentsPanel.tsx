import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { toast } from '../toast';
import { confirm } from '../dialog';
import type { Comment, CommentAnchor } from '../types';
import { Check, MessageSquareText, Reply, Trash2, X, Link2 } from 'lucide-react';
import { formatRelative } from '../format';
import { t, plural } from '../i18n';
import { AgentMark } from './AgentBadge';

export const COMMENTS_CHANGED = 'salt:comments-changed';
const PANEL_KEY = 'salt-comments-open';
export function commentsPanelOpen(): boolean { return localStorage.getItem(PANEL_KEY) === '1'; }
export function setCommentsPanelOpen(open: boolean): void {
  if (open) localStorage.setItem(PANEL_KEY, '1'); else localStorage.removeItem(PANEL_KEY);
}
export function nameColor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return `hsl(${[210, 145, 275, 25, 340, 190, 95, 55][h % 8]} 55% 45%)`;
}
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  return parts.length ? (parts[0][0] + (parts[1]?.[0] ?? '')).toUpperCase() : '?';
}
export type CommentDraft = { body: string; replyToId: string; requestId: string; anchor?: CommentAnchor; blockId?: string };
export function newCommentRequestId(): string { return [...crypto.getRandomValues(new Uint32Array(4))].map((n) => n.toString(16).padStart(8, '0')).join(''); }
export function newCommentDraft(): CommentDraft { return { body: '', replyToId: '', requestId: newCommentRequestId() }; }

export default function CommentsPanel({ pageId, myUserId, canWrite, open, onClose, draft, onDraftChange, activeComment, openRequest, anchorStatuses, narrow }: {
  pageId: string; myUserId: string; canWrite: boolean; open: boolean; onClose: () => void;
  draft: CommentDraft; onDraftChange: (draft: CommentDraft) => void;
  activeComment: string | null; openRequest: number;
  anchorStatuses: Record<string, boolean>; narrow: boolean;
}) {
  const [comments, setComments] = useState<Comment[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState('');
  const [loadError, setLoadError] = useState('');
  const [busy, setBusy] = useState(false);
  const [showResolved, setShowResolved] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const sendLock = useRef(false);
  const notified = useRef('');
  const roots = comments.filter((c) => !c.threadId && (!c.deletedAt || comments.some((r) => r.threadId === c.id && !r.deletedAt)));
  const openRoots = roots.filter((c) => !c.resolvedAt);
  const target = comments.find((c) => c.id === draft.replyToId);
  const targetRoot = target && (target.threadId ? comments.find((c) => c.id === target.threadId) : target);
  const active = comments.find((c) => c.id === activeComment);
  const activeRootId = active?.threadId || active?.id;
  const visible = roots.filter((c) => showResolved || !c.resolvedAt || c.id === activeRootId);

  useEffect(() => {
    let alive = true;
    const load = () => { void api.listComments(pageId).then((items) => {
      if (!alive) return;
      setComments(items); setLoaded(true); setLoadError('');
      window.dispatchEvent(new CustomEvent(COMMENTS_CHANGED));
    }).catch((e: Error) => { if (alive) setLoadError(e.message || t('Could not load comments')); }); };
    const live = (e: Event) => { if ((e as CustomEvent<string>).detail === pageId) load(); };
    load();
    window.addEventListener('salt:comments-live', live);
    window.addEventListener('focus', load);
    window.addEventListener('salt:comments-refresh', load);
    return () => { alive = false; window.removeEventListener('salt:comments-live', live); window.removeEventListener('focus', load); window.removeEventListener('salt:comments-refresh', load); };
  }, [pageId]);

  useEffect(() => {
    if (!open || !activeComment) return;
    const frame = requestAnimationFrame(() => {
      const el = listRef.current?.querySelector<HTMLElement>(`[data-panel-comment="${CSS.escape(activeComment)}"]`);
      const opened = pageId + ':' + activeComment + ':' + openRequest;
      if (!el || notified.current === opened) return;
      notified.current = opened;
      el.scrollIntoView({ block: 'nearest' }); el.focus({ preventScroll: true });
      const params = new URLSearchParams(window.location.hash.slice(1));
      const id = params.get('notification');
      if (id && params.get('comment') === activeComment) {
        void api.readNotification(Number(id), true).then(() => window.dispatchEvent(new Event('salt:notifications'))).catch(() => { notified.current = ''; });
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [open, activeComment, comments, showResolved, pageId, openRequest]);
  useEffect(() => {
    if (open && !activeComment && canWrite) requestAnimationFrame(() => boxRef.current?.focus());
  }, [open, activeComment, canWrite, draft.anchor, draft.replyToId]);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (sendLock.current || !draft.body.trim()) return;
    sendLock.current = true; setBusy(true); setError('');
    try {
      await api.createComment(pageId, draft.body.trim(), draft.blockId ?? '', { anchor: draft.anchor, replyToId: draft.replyToId, requestId: draft.requestId });
      onDraftChange(newCommentDraft());
      setComments(await api.listComments(pageId));
      window.dispatchEvent(new CustomEvent(COMMENTS_CHANGED));
      requestAnimationFrame(() => listRef.current?.scrollTo({ top: 1e6, behavior: 'smooth' }));
    } catch (e) { setError((e as Error).message || t('Could not post the comment')); }
    finally { sendLock.current = false; setBusy(false); }
  };
  const reply = async (c: Comment) => {
    if (draft.body.trim() && draft.replyToId !== c.id && !(await confirm(t('Use your current draft to reply to {name}?', { name: c.authorName })))) return;

    setError('');
    onDraftChange({ ...draft, anchor: undefined, blockId: undefined, replyToId: c.id, requestId: newCommentRequestId() });
    boxRef.current?.focus();
  };
  const resolve = async (c: Comment) => {
    try { await api.resolveComment(c.id, !c.resolvedAt); setError(''); setComments(await api.listComments(pageId)); window.dispatchEvent(new CustomEvent(COMMENTS_CHANGED)); }
    catch (e) { setError((e as Error).message); }
  };
  const remove = async (c: Comment) => {
    if (!(await confirm(t('Delete this comment? Other replies will be kept.')))) return;
    try { await api.deleteComment(c.id); setError(''); setComments(await api.listComments(pageId)); window.dispatchEvent(new CustomEvent(COMMENTS_CHANGED)); }
    catch (e) { setError((e as Error).message); }
  };
  const copyLink = async (c: Comment) => {
    try { await navigator.clipboard.writeText(`${window.location.origin}/p/${pageId}#comment=${c.id}`); toast(t('Link copied')); }
    catch { toast(t('Could not copy the link')); }
  };
  const message = (c: Comment, root: Comment) => {
    const name = c.authorName || t('unknown');
    const addressed = c.replyToId && c.replyToId !== root.id ? comments.find((x) => x.id === c.replyToId) : undefined;
    return <article key={c.id} tabIndex={-1} data-panel-comment={c.id} className={'cp-item' + (c.threadId ? ' cp-reply' : '') + (activeComment === c.id ? ' is-active' : '')}>
      <div className="cp-item-head">
        <span className="cp-avatar" style={{ background: c.authorAvatar ? 'transparent' : c.authorColor || nameColor(name) }}>{c.authorAvatar ? <img src={c.authorAvatar} alt="" /> : initials(name)}</span>
        <span className="cp-author">{name}</span>{c.agent && <span className="cp-agent" title={t('Written by an agent through this account')}><AgentMark agent={c.agent} size={12} />{c.agentLabel || t('Agent')}</span>}<time className="cp-time">{formatRelative(c.createdAt)}</time>
      </div>
      {addressed && !c.deletedAt && <div className="cp-addressed">{t('Replying to {name}', { name: addressed.deletedAt ? t('Deleted comment') : addressed.authorName })}</div>}

      {c.anchor && !c.deletedAt && <button type="button" className="cp-quote" onClick={() => { window.dispatchEvent(new CustomEvent('salt:comment-jump', { detail: c.id })); if (narrow || window.matchMedia('(max-width: 900px)').matches) onClose(); }} disabled={anchorStatuses[c.id] === false}>
        <span>{c.anchor.quote}</span>{anchorStatuses[c.id] === false && <small>{t('Original text no longer available')}</small>}
      </button>}
      <div className={'cp-body' + (c.deletedAt ? ' cp-deleted' : '')}>{c.deletedAt ? t('Deleted comment') : c.body}</div>
      <div className="cp-actions">
        {canWrite && !root.resolvedAt && !c.deletedAt && <button type="button" className="cp-act cp-reply-action" disabled={busy} onClick={() => void reply(c)}><Reply size={13} />{t('Reply')}</button>}
        <button type="button" className="cp-act" title={t('Copy link')} onClick={() => void copyLink(c)}><Link2 size={13} /></button>
        {canWrite && c.authorId === myUserId && !c.deletedAt && <button type="button" className="cp-act danger" title={t('Delete')} onClick={() => void remove(c)}><Trash2 size={13} /></button>}
      </div>
    </article>;
  };
  if (!open) return null;
  return <aside className="comments-panel" aria-label={t('Comments')}>
    <div className="cp-head"><span className="cp-head-title"><MessageSquareText size={15} />{t('Comments')}{openRoots.length > 0 && <span className="cp-count">{openRoots.length}</span>}</span><button type="button" className="icon-btn" title={t('Close')} onClick={onClose}><X size={15} /></button></div>
    {roots.some((c) => c.resolvedAt) && <label className="cp-toggle"><input type="checkbox" checked={showResolved} onChange={(e) => setShowResolved(e.target.checked)} />{t('Show resolved threads')}</label>}
    <div className="cp-list" ref={listRef}>
      {!loaded && !loadError && <p className="cp-empty">{t('Loading…')}</p>}
      {loaded && visible.length === 0 && <p className="cp-empty">{t('Nothing here yet. Write the first one.')}</p>}
      {loaded && activeComment && !active && <p className="cp-empty" role="status">{t('This comment is no longer available.')}</p>}
      {visible.map((root) => <section key={root.id} className={'cp-thread' + (root.resolvedAt ? ' is-resolved' : '')}>
        {message(root, root)}
        {comments.filter((c) => c.threadId === root.id).map((c) => message(c, root))}
        <div className="cp-thread-foot">
          <span>{plural(comments.filter((c) => c.threadId === root.id && !c.deletedAt).length, '{n} reply', '{n} replies')}</span>
          {canWrite && <button type="button" className="cp-act" onClick={() => void resolve(root)}><Check size={13} />{root.resolvedAt ? t('Reopen') : t('Resolve thread')}</button>}
        </div>
      </section>)}
    </div>
    {canWrite && <form className="cp-compose" onSubmit={(e) => void add(e)}>
      {draft.anchor && <div className="cp-pending"><span>{draft.anchor.quote}</span><button type="button" disabled={busy} onClick={() => onDraftChange({ ...draft, anchor: undefined, blockId: undefined, requestId: newCommentRequestId() })} aria-label={t('Remove selected text')}>×</button></div>}
      {draft.replyToId && <div className="cp-replying"><Reply size={14} /><span>{t('Replying to {name}', { name: target?.authorName || t('unknown') })}</span><button type="button" className="icon-btn" title={t('Cancel reply')} disabled={busy} onClick={() => onDraftChange({ ...draft, replyToId: '', requestId: newCommentRequestId() })}><X size={14} /></button></div>}

      {targetRoot?.resolvedAt && <p className="cp-draft-note">{t('Reopen this thread before replying. Your draft is preserved.')} <button type="button" className="cp-act" onClick={() => void resolve(targetRoot)}>{t('Reopen')}</button></p>}
      {target?.deletedAt && <p className="cp-draft-note">{t('This comment was deleted. Choose another reply target.')}</p>}
      <textarea ref={boxRef} value={draft.body} rows={3} maxLength={10000} disabled={busy} aria-label={draft.replyToId ? t('Write a reply…') : t('Write a comment…')} placeholder={draft.replyToId ? t('Write a reply…') : t('Write a comment…')} onChange={(e) => onDraftChange({ ...draft, body: e.target.value, requestId: newCommentRequestId() })} onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) void add(e); }} />
      <div className="cp-compose-foot"><span className="cp-hint">{t('⌘↵ to send')}</span><button className="btn primary btn-sm" type="submit" disabled={busy || !draft.body.trim() || !!targetRoot?.resolvedAt || !!target?.deletedAt}>{busy ? t('Sending…') : draft.replyToId ? t('Send reply') : t('Send')}</button></div>
    </form>}
    {(error || loadError) && <div className="cp-error" role="alert">{error || loadError}</div>}
  </aside>;
}
