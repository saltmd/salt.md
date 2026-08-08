import { useCallback, useEffect, useRef, useState } from 'react';
import { ChevronDown, Eraser, StickyNote } from 'lucide-react';
import { api } from '../api';
import type { PageNote } from '../types';
import { formatMoment, formatRelative } from '../format';
import { AgentMark } from './AgentBadge';
import { confirm } from '../dialog';
import { toast } from '../toast';
import { t, plural } from '../i18n';

// The raw trail at the foot of a page — see server/notelog.go for what it is
// and why nothing here can edit an entry.
//
// The question that decides whether this is worth building is where it sits
// while you READ. A stream nobody finds without a second click does not get
// read; one that stands above the written-up version makes the page unreadable.
// So: one quiet line at the very bottom that says how many and over what
// stretch — "14 notes · 14:02 – 17:40" — and opens when asked. Visible enough to
// know it exists, quiet enough to be ignored.
//
// It renders NOTHING when the trail is empty and stays out of the way until
// somebody starts one. The way to start one is the same line, in its "add a
// note" shape: a page with no trail should not carry a heading for a thing that
// is not there.
//
// The order matters too. Comments come first — they are a conversation, aimed
// at people. The trail comes last, because it is evidence, and evidence belongs
// under the account it supports.

const timeOf = (iso: string) => formatMoment(iso, 'time');

export default function NoteTrail({ pageId, canWrite }: { pageId: string; canWrite: boolean }) {
  const [notes, setNotes] = useState<PageNote[]>([]);
  const [expanded, setExpanded] = useState(false);
  const [composing, setComposing] = useState(false);
  const [body, setBody] = useState('');
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const load = useCallback(() => {
    api
      .pageNotes(pageId)
      .then(setNotes)
      .catch(() => setNotes([]));
  }, [pageId]);

  useEffect(() => {
    setExpanded(false);
    setComposing(false);
    load();
  }, [pageId, load]);

  // Live, and narrowly: the event names one page, so a note landing elsewhere
  // does not make every open page refetch.
  useEffect(() => {
    const onNotes = (e: Event) => {
      if ((e as CustomEvent<string>).detail === pageId) load();
    };
    window.addEventListener('salt:notes', onNotes);
    return () => window.removeEventListener('salt:notes', onNotes);
  }, [pageId, load]);

  useEffect(() => {
    if (composing) inputRef.current?.focus();
  }, [composing]);

  const add = async () => {
    const text = body.trim();
    if (!text) return;
    // Clear first: the note is gone from the box the moment it is sent, which
    // is what makes a second one cost nothing. A failure puts it back.
    setBody('');
    try {
      await api.addNote(pageId, text);
      setExpanded(true);
      load();
    } catch (e) {
      setBody(text);
      toast((e as Error).message || t('Note not saved'));
    }
  };

  const clear = async () => {
    // The count goes in as an already-pluralised phrase rather than a bare
    // number, so the sentence stays grammatical at one as well as at fourteen —
    // in both languages, which a "{n} notes are lost" never manages.
    const ok = await confirm(
      t('Discard the whole trail of this page? This affects {notes} and cannot be undone.', {
        notes: plural(notes.length, '{n} note', '{n} notes'),
      }),
      { confirmText: t('Discard'), danger: true },
    );
    if (!ok) return;
    try {
      await api.clearNotes(pageId);
      load();
    } catch (e) {
      toast((e as Error).message || t('Not discarded'));
    }
  };

  // Nothing to show and nothing to write: stay out of the page entirely.
  if (notes.length === 0 && !canWrite) return null;

  const first = notes[0]?.createdAt;
  const last = notes[notes.length - 1]?.createdAt;
  const sameDay = first && last && first.slice(0, 10) === last.slice(0, 10);
  const span = !first
    ? ''
    : sameDay
      ? notes.length > 1
        ? `${timeOf(first)} – ${timeOf(last)}`
        : timeOf(first)
      : `${formatMoment(first)} – ${formatMoment(last!)}`;

  return (
    <section className={'trail' + (expanded ? ' is-open' : '')} aria-label={t('Raw trail')}>
      {notes.length === 0 ? (
        // The empty shape: not a heading for an absent thing, just the door in
        // — and the door goes away once it is open, or it stands there telling
        // you to do the thing you are already doing.
        !composing && (
          <button type="button" className="trail-start" onClick={() => setComposing(true)}>
            <StickyNote size={14} /> {t('Note something down')}
          </button>
        )
      ) : (
        <button
          type="button"
          className="trail-bar"
          aria-expanded={expanded}
          onClick={() => setExpanded((o) => !o)}
        >
          <StickyNote size={14} />
          <span>{plural(notes.length, '{n} note', '{n} notes')}</span>
          <span className="trail-span">{span}</span>
          <ChevronDown size={14} className="trail-chev" aria-hidden />
        </button>
      )}

      {(expanded || composing) && (
        <div className="trail-body">
          {expanded &&
            notes.map((n) => {
              const who = n.agent ? n.label || n.agent : n.author;
              return (
                <article key={n.id} className="trail-item">
                  <time className="trail-time" title={formatRelative(n.createdAt)}>
                    {timeOf(n.createdAt)}
                  </time>
                  <div className="trail-text">{n.body}</div>
                  <span className="trail-who">
                    {n.agent ? <AgentMark agent={n.agent} size={13} /> : null}
                    {who}
                    {/* An agent's name is a CLAIM (see presence.go); the account
                        it came through is the verified half, so both show. */}
                    {n.agent && n.author ? <span className="trail-via">{n.author}</span> : null}
                  </span>
                </article>
              );
            })}

          {canWrite && (
            <div className="trail-compose">
              <textarea
                ref={inputRef}
                rows={1}
                value={body}
                placeholder={t('What just happened, in one line…')}
                onChange={(e) => setBody(e.target.value)}
                onKeyDown={(e) => {
                  // Enter sends. A trail entry is one thought — the moment it
                  // wants paragraphs it wants to be a document instead, and
                  // needing a modifier is exactly the friction this avoids.
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    void add();
                  }
                }}
              />
              <button className="btn-sm" onClick={() => void add()} disabled={!body.trim()}>
                {t('Note')}
              </button>
            </div>
          )}

          {expanded && notes.length > 0 && (
            <div className="trail-foot">
              <span>{t('Notes cannot be edited or removed one by one — that is what makes them worth reading later.')}</span>
              {canWrite && (
                <button className="trail-clear" onClick={() => void clear()}>
                  <Eraser size={13} /> {t('Discard the whole trail')}
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </section>
  );
}
