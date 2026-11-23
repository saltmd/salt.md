import { useEffect, useReducer } from 'react';
import { api } from './api';
import { t } from './i18n';
import type { Comment } from './types';

// One copy of a page's comments for everything that shows them: the document's
// highlights, the comment panel and the header's count. Each used to fetch on
// its own, so one live event, focus or reconnect asked for the same list three
// times. A refresh requested while one is running runs once more after it, so
// a change made meanwhile is never answered with the older list.

type Entry = {
  comments: Comment[];
  loaded: boolean;
  error: string;
  running: Promise<void> | null;
  again: boolean;
  views: Set<() => void>;
};

const pages = new Map<string, Entry>();

export function refreshComments(pageId: string): Promise<void> {
  const entry = pages.get(pageId);
  if (!entry) return Promise.resolve();
  if (entry.running) {
    entry.again = true;
    return entry.running;
  }
  entry.running = (async () => {
    do {
      entry.again = false;
      try {
        entry.comments = await api.listComments(pageId);
        entry.loaded = true;
        entry.error = '';
      } catch (e) {
        entry.error = (e as Error).message || t('Could not load comments');
      }
      entry.views.forEach((render) => render());
    } while (entry.again);
    entry.running = null;
  })();
  return entry.running;
}

if (typeof window !== 'undefined') {
  // Live changes name their page; a reconnect or a return to the window
  // concerns every page on screen.
  window.addEventListener('salt:comments-live', (e) => void refreshComments((e as CustomEvent<string>).detail));
  window.addEventListener('salt:comments-refresh', () => { for (const id of pages.keys()) void refreshComments(id); });
}

export function usePageComments(pageId: string, enabled = true) {
  const [, render] = useReducer((n: number) => n + 1, 0);
  useEffect(() => {
    if (!enabled) return;
    let entry = pages.get(pageId);
    if (!entry) {
      entry = { comments: [], loaded: false, error: '', running: null, again: false, views: new Set() };
      pages.set(pageId, entry);
    }
    entry.views.add(render);
    if (!entry.loaded && !entry.running) void refreshComments(pageId);
    else render();
    const mine = entry;
    return () => {
      mine.views.delete(render);
      if (!mine.views.size && pages.get(pageId) === mine) pages.delete(pageId);
    };
  }, [pageId, enabled]);
  const entry = enabled ? pages.get(pageId) : undefined;
  return { comments: entry?.comments ?? [], loaded: entry?.loaded ?? false, error: entry?.error ?? '' };
}
