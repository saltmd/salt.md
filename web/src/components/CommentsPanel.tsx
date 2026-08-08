import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { toast } from '../toast';
import type { Comment } from '../types';
import { Check, MessageSquareText, Trash2, X } from 'lucide-react';
import { formatRelative } from '../format';
import { t, plural } from '../i18n';

// Comments as a panel beside the document.
//
// This is the FOURTH shape, and the first one that came from watching people
// use it rather than from reasoning about it. His colleagues work in comments
// all day and reported the same two things: they hang at the very bottom, and
// there are two lines to write in.
//
// Both complaints are about the same mistake. The section at the foot of the
// document was modelled on how comments are READ — you finish the text, then
// you see the discussion. But these people are not reading, they are working:
// they write several, they answer, they go back and forth. For that the comment
// has to be visible AT THE SAME TIME as the passage it is about, and the box
// has to be big enough to hold a thought.
//
// So: a panel on the right, next to the text, that stays open. Six lines to
// write in, growing. And it can be closed, because somebody who is only reading
// should get the whole width.
//
// The count in the topbar does NOT come from here. Seeing that a page has three
// open comments is half the value and must work while the panel is closed, so
// the header fetches it itself and this panel only says when something changed
// (COMMENTS_CHANGED). Threading it through as a prop would have tied a number
// everybody needs to a component most people have shut.

/** Fired after anything about this page's comments changed, so the count in the
 *  topbar stays right without the panel owning it. */
export const COMMENTS_CHANGED = 'salt:comments-changed';

const PANEL_KEY = 'salt-comments-open';

export function commentsPanelOpen(): boolean {
  return localStorage.getItem(PANEL_KEY) === '1';
}

export function setCommentsPanelOpen(open: boolean): void {
  if (open) localStorage.setItem(PANEL_KEY, '1');
  else localStorage.removeItem(PANEL_KEY);
}

const when = (iso: string) => formatRelative(iso);

// Derive the colour from the name, so the same person always gets the same one.
export function nameColor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  const hues = [210, 145, 275, 25, 340, 190, 95, 55];
  return `hsl(${hues[h % hues.length]} 55% 45%)`;
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (!parts.length) return '?';
  return (parts[0][0] + (parts[1]?.[0] ?? '')).toUpperCase();
}

export default function CommentsPanel({
  pageId,
  myUserId,
  open,
  onClose,
}: {
  pageId: string;
  myUserId: string;
  open: boolean;
  onClose: () => void;
}) {
  const [comments, setComments] = useState<Comment[]>([]);
  const [body, setBody] = useState('');
  const [showResolved, setShowResolved] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLTextAreaElement>(null);

  const load = () =>
    void api
      .listComments(pageId)
      .then((list) => {
        setComments(list);
        window.dispatchEvent(new CustomEvent(COMMENTS_CHANGED));
      })
      .catch(() => {});
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(load, [pageId]);

  // The topbar button opens the panel and puts the cursor in the box: the
  // reason somebody presses it is almost always that they want to write.
  useEffect(() => {
    if (open) requestAnimationFrame(() => boxRef.current?.focus());
  }, [open, pageId]);

  const add = async (e?: React.FormEvent) => {
    e?.preventDefault();
    const text = body.trim();
    if (!text) return;
    setBody('');
    try {
      await api.createComment(pageId, text);
      load();
      // Your own contribution should be visible, not below the fold.
      requestAnimationFrame(() => listRef.current?.scrollTo({ top: 1e6, behavior: 'smooth' }));
    } catch (err) {
      setBody(text); // swallow nothing if sending fails
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

  const openOnes = comments.filter((c) => !c.resolvedAt);
  const resolved = comments.filter((c) => c.resolvedAt);
  const visible = showResolved ? comments : openOnes;

  // Mounted but invisible: the count above has to be right before anybody looks.
  if (!open) return null;

  return (
    <aside className="comments-panel" aria-label={t('Comments')}>
      <div className="cp-head">
        <span className="cp-head-title">
          <MessageSquareText size={15} />
          {t('Comments')}
          {openOnes.length > 0 && <span className="cp-count">{openOnes.length}</span>}
        </span>
        <button className="icon-btn" title={t('Close')} onClick={onClose}>
          <X size={15} />
        </button>
      </div>

      {resolved.length > 0 && (
        <label className="cp-toggle">
          <input
            type="checkbox"
            checked={showResolved}
            onChange={(e) => setShowResolved(e.target.checked)}
          />
          {t('Show {n} resolved', { n: resolved.length })}
        </label>
      )}

      <div className="cp-list" ref={listRef}>
        {visible.length === 0 ? (
          // Said once, quietly. The old bottom section showed "no comments yet"
          // as the most prominent thing on an empty page; here the box below is
          // already the invitation, so this only has to explain the silence.
          <p className="cp-empty">{t('Nothing here yet. Write the first one.')}</p>
        ) : (
          visible.map((c) => {
            const name = c.authorName || t('unknown');
            return (
              <article key={c.id} className={'cp-item' + (c.resolvedAt ? ' is-resolved' : '')}>
                <div className="cp-item-head">
                  <span
                    className="cp-avatar"
                    style={{ background: c.authorAvatar ? 'transparent' : c.authorColor || nameColor(name) }}
                  >
                    {c.authorAvatar ? <img src={c.authorAvatar} alt="" /> : initials(name)}
                  </span>
                  <span className="cp-author">{name}</span>
                  <time className="cp-time">{when(c.createdAt)}</time>
                </div>
                <div className="cp-body">{c.body}</div>
                <div className="cp-actions">
                  <button
                    className="cp-act"
                    title={c.resolvedAt ? t('Reopen') : t('Mark as resolved')}
                    onClick={() => void toggleResolve(c)}
                  >
                    <Check size={13} /> {c.resolvedAt ? t('Reopen') : t('Resolved')}
                  </button>
                  {c.authorId === myUserId && (
                    <button className="cp-act danger" title={t('Delete')} onClick={() => void remove(c)}>
                      <Trash2 size={13} />
                    </button>
                  )}
                </div>
              </article>
            );
          })
        )}
      </div>

      <form className="cp-compose" onSubmit={add}>
        <textarea
          ref={boxRef}
          value={body}
          rows={6}
          placeholder={t('Write a comment…')}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            // ⌘/Ctrl+Enter sends; Enter makes a paragraph. The other way round
            // would be faster for one-liners and would cost a half-written
            // thought every time somebody reaches for a new line — and this box
            // exists because people write more than one line here.
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void add(e);
          }}
        />
        <div className="cp-compose-foot">
          <span className="cp-hint">{t('⌘↵ to send')}</span>
          <button className="btn primary btn-sm" type="submit" disabled={!body.trim()}>
            {t('Send')}
          </button>
        </div>
      </form>

      {resolved.length > 0 && !showResolved && (
        <p className="cp-foot">
          {plural(resolved.length, '{n} resolved comment', '{n} resolved comments')}
        </p>
      )}
    </aside>
  );
}
