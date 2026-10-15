import type { Page } from './types';

type Baseline = Pick<Page, 'createdAt' | 'title' | 'props' | 'parentId' | 'workspaceId' | 'visibility'>;
const KEY = 'salt:untouched-new-pages';
function readDrafts(): Record<string, Baseline> {
  try {
    const value = JSON.parse(sessionStorage.getItem(KEY) || '{}');
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
  } catch { return {}; }
}
const drafts = readDrafts();
let active: string | null = null;
function persist() {
  try { sessionStorage.setItem(KEY, JSON.stringify(drafts)); } catch { /* Keep the in-memory guard. */ }
}

/** Only explicit UI New actions opt in. Imports, templates, forms and old
 * Untitled pages are never inferred to be disposable from their title. */
export function rememberNewPage(page: Page) {
  if (page.type !== 'doc' || (page.title !== '' && page.title !== 'Untitled')) return;
  drafts[page.id] = { createdAt: page.createdAt, title: page.title, props: page.props,
    parentId: page.parentId, workspaceId: page.workspaceId, visibility: page.visibility };
  persist();
}

/** Called before debounce/network writes, so navigating immediately after an
 * edit cannot race a discard. Once edited, a page is an ordinary saved page. */
export function keepNewPage(id: string) {
  if (!(id in drafts)) return;
  delete drafts[id];
  persist();
}

/** No inactivity timeout. The short retry only drains the outgoing editor's
 * connection after navigation; a page still open is never a discard candidate. */
export function activateNewPage(id: string | null, onDiscard: (id: string) => void) {
  const previous = active;
  active = id;
  if (!previous || previous === id || !drafts[previous]) return;
  const baseline = drafts[previous];
  void (async () => {
    for (let attempt = 0; attempt < 8; attempt++) {
      await new Promise(resolve => window.setTimeout(resolve, 250));
      if (active === previous || drafts[previous] !== baseline) return;
      try {
        const response = await fetch(`/api/pages/${previous}/discard-empty`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(baseline),
        });
        if (!response.ok) return; // Preserve on error, permission change or offline.
        const result = await response.json() as { discarded: boolean; busy?: boolean };
        if (result.busy) continue;
        keepNewPage(previous);
        if (result.discarded) onDiscard(previous);
        return;
      } catch { return; }
    }
  })();
}
