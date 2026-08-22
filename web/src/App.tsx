import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from './api';
import { serverMessage } from './serverErrors';
import { WifiOff } from 'lucide-react';
import type { Me, PageMeta, User, Workspace } from './types';
import Sidebar from './components/Sidebar';
import Editor from './components/Editor';
import TabBar from './components/TabBar';
import SearchModal from './components/SearchModal';
import IndexView from './components/IndexView';
import NotesList from './components/NotesList';
import Login from './components/Login';
import Setup from './components/Setup';
import InviteAccept from './components/InviteAccept';
import PublicForm from './components/PublicForm';
import OAuthConsent from './components/OAuthConsent';
import { UploadBar, ImageLightbox } from './components/Overlays';
import Toaster from './components/Toaster';
import { DialogHost, confirm, promptText } from './dialog';
import { announceModal } from './modal';
import { toast } from './toast';
import { onRefresh } from './pwa';
import PullToRefresh from './components/PullToRefresh';
import Logo from './Logo';
import ThemeSwitch, { type ThemePref } from './ThemeSwitch';
import { applyPrefs, plural, t } from './i18n';
import { guardDrops } from './dropFiles';

/** Injected by the build; false everywhere except the website's framed demo. */
declare const __SALT_DEMO__: boolean;

/** Schriftwahl: 'system' laesst alles wie bisher, 'brand' schaltet die
 *  mitgelieferten Inter- und JetBrains-Mono-Schriften ein. */
export type FontPref = 'system' | 'brand';

// Feedback from the mail OAuth consent redirect (/?mailOauth=ok|<code>).
//
// The server hands back a code plus its English sentence, never a finished
// German one — otherwise this toast would be the one German string left in an
// English interface. Provider text arrives separately in `detail`, because
// nobody can translate what Google or Microsoft wrote.
const mailOauthMsg = (() => {
  const qs = new URLSearchParams(window.location.search);
  const code = qs.get('mailOauth');
  const text = qs.get('mailOauthText');
  const detail = qs.get('mailOauthDetail');
  if (code) {
    ['mailOauth', 'mailOauthText', 'mailOauthDetail'].forEach((k) => qs.delete(k));
    const rest = qs.toString();
    window.history.replaceState({}, '', window.location.pathname + (rest ? '?' + rest : ''));
  }
  return code ? { code, text: text ?? code, detail } : null;
})();
if (mailOauthMsg) {
  setTimeout(() => {
    if (mailOauthMsg.code === 'ok') {
      toast(t('Mail sending connected ✓'));
      return;
    }
    const msg = serverMessage(mailOauthMsg.code, mailOauthMsg.text);
    toast(t('Mail connection: {detail}', {
      detail: mailOauthMsg.detail ? `${msg} (${mailOauthMsg.detail})` : msg,
    }));
  }, 400);
}

// Injected at build time from the same value the server is built with (see
// vite.config.ts). A stale open tab after a deploy sees a different server
// version (via /api/me and the SSE hello) and is told to reload.
//
// Never hardcode this again: as two hand-kept numbers they drifted, and the
// reload banner then fired on every load forever.
declare const __SALT_VERSION__: string;
const BUILD_VERSION = __SALT_VERSION__;

function pageIdFromLocation(): string | null {
  const m = window.location.pathname.match(/^\/p\/([0-9a-f]+)$/);
  return m ? m[1] : null;
}

type Theme = 'light' | 'dark';

export default function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [pages, setPages] = useState<PageMeta[] | null>(null);
  const [favorites, setFavorites] = useState<string[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  // The last opened workspace is remembered. Without it every reload dropped
  // you back into the first one — anybody who mostly works in a second had to
  // pick it again on every page load.
  const [currentWs, setCurrentWs] = useState<string>(() => localStorage.getItem('salt-ws') ?? '');
  const [loadError, setLoadError] = useState(false);
  // Bear-style notes mode (middle notes column) — an explicit per-user setting
  // in the UserMenu, DEFAULT OFF so the first impression stays the classic
  // tree layout (user feedback: three parallel content areas felt chaotic).
  const [notesMode, setNotesMode] = useState(() => localStorage.getItem('salt-notes-mode') === '1');
  // Tag selected in the sidebar while in notes mode — filters the notes list.
  const [notesTag, setNotesTag] = useState<string | null>(null);
  // The notes list only exists ≥900px; below that the sidebar must keep its
  // document tree or mobile loses all navigation.
  const [isDesktop, setIsDesktop] = useState(() => window.matchMedia('(min-width: 900px)').matches);
  useEffect(() => {
    const mq = window.matchMedia('(min-width: 900px)');
    const onChange = () => setIsDesktop(mq.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  const notesActive = notesMode && isDesktop;
  const toggleNotesMode = useCallback(() => {
    setNotesMode((cur) => {
      const next = !cur;
      localStorage.setItem('salt-notes-mode', next ? '1' : '0');
      if (!next) setNotesTag(null);
      return next;
    });
  }, []);
  // Ref mirror so the []-deps ⌥N handler always calls the current createPage.
  const createPageRef = useRef<((parentId: string | null) => Promise<void>) | null>(null);
  const [currentId, setCurrentId] = useState<string | null>(pageIdFromLocation());
  // Open document tabs (Obsidian-style): an ordered list of page ids; the active
  // one is `currentId`. Seeded from the last session and the URL.
  const [openTabs, setOpenTabs] = useState<string[]>(() => {
    let seed: string[] = [];
    try {
      const s = JSON.parse(localStorage.getItem('salt-tabs') ?? '[]');
      if (Array.isArray(s)) seed = s.filter((x): x is string => typeof x === 'string');
    } catch {
      /* localStorage unavailable — tabs fall back to a single view */
    }
    const fromUrl = pageIdFromLocation();
    if (fromUrl && !seed.includes(fromUrl)) seed = [...seed, fromUrl];
    return seed;
  });
  // Refs mirror the latest values so the stable useCallback handlers below can
  // read them without being re-created on every navigation.
  const activeRef = useRef<string | null>(currentId);
  const tabsRef = useRef<string[]>(openTabs);
  useEffect(() => {
    activeRef.current = currentId;
  }, [currentId]);
  useEffect(() => {
    tabsRef.current = openTabs;
    try {
      localStorage.setItem('salt-tabs', JSON.stringify(openTabs));
    } catch {
      /* best-effort persistence */
    }
  }, [openTabs]);
  const [searchOpen, setSearchOpen] = useState(false);
  const [indexOpen, setIndexOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  // Desktop-only: collapse the sidebar entirely (mobile uses the drawer). The
  // editor's hamburger reopens it. Persisted so it stays collapsed across loads.
  const [sidebarCollapsed, setSidebarCollapsed] = useState(
    () => localStorage.getItem('salt-sidebar-collapsed') === '1',
  );
  useEffect(() => {
    try {
      localStorage.setItem('salt-sidebar-collapsed', sidebarCollapsed ? '1' : '0');
    } catch {
      /* best-effort */
    }
  }, [sidebarCollapsed]);
  // After clicking "collapse" the pointer is still over the sidebar, so the
  // hover-reveal would instantly show it again ("the click did nothing"). Lock
  // the reveal until the pointer has actually left the sidebar area once.
  const [hoverLock, setHoverLock] = useState(false);
  useEffect(() => {
    if (!hoverLock) return;
    const onMove = (e: MouseEvent) => {
      if (e.clientX > 300) setHoverLock(false);
    };
    window.addEventListener('mousemove', onMove);
    return () => window.removeEventListener('mousemove', onMove);
  }, [hoverLock]);
  // The hamburger both opens the mobile drawer and un-collapses on desktop.
  const openSidebar = () => {
    setSidebarOpen(true);
    setSidebarCollapsed(false);
  };
  // What is stored is the CHOICE ('auto' included); what is applied is the
  // theme derived from it. Anyone who had already stored 'light'/'dark' before
  // this change keeps it — that was a deliberate setting, not something to
  // overwrite quietly. 'auto' is the new default.
  const [themePref, setThemePref] = useState<ThemePref>(() => {
    const saved = localStorage.getItem('salt-theme');
    return saved === 'light' || saved === 'dark' || saved === 'auto' ? saved : 'auto';
  });
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia('(prefers-color-scheme: dark)').matches,
  );
  // Under 'auto' a change to the system setting has to take effect AT ONCE, or
  // "automatic" would only ever be a snapshot taken at load time.
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => setSystemDark(mq.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  const theme: Theme = themePref === 'auto' ? (systemDark ? 'dark' : 'light') : themePref;


  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem('salt-theme', themePref);
  }, [theme, themePref]);

  // Typeface: the same mechanism as the theme — the choice is kept locally,
  // the result goes onto <html> as an attribute so CSS alone decides. Off
  // until somebody switches it on: the browser loads the bundled font files
  // only once they are actually used.
  // Defaults to 'brand'. Anybody who explicitly chose 'system' keeps it —
  // only a MISSING key means "not decided yet" and therefore falls to the
  // bundled fonts.
  const [fontPref, setFontPref] = useState<FontPref>(() =>
    localStorage.getItem('salt-font') === 'system' ? 'system' : 'brand',
  );
  useEffect(() => {
    document.documentElement.dataset.font = fontPref;
    localStorage.setItem('salt-font', fontPref);
  }, [fontPref]);

  const loadFavorites = useCallback(async () => {
    try {
      setFavorites(await api.listFavorites());
    } catch {
      /* keep current favorites on transient failure */
    }
  }, []);

  // Pages reload on SSE events; favorites are per-user and reloaded only on
  // login/mount, so a page-change broadcast can't clobber an in-flight
  // optimistic favorite toggle.
  const loadPages = useCallback(async () => {
    try {
      setPages(await api.listPages());
      setLoadError(false);
    } catch (e) {
      if ((e as Error).message !== 'unauthorized') setLoadError(true);
    }
  }, []);

  const loadWorkspaces = useCallback(async () => {
    try {
      const ws = await api.listWorkspaces();
      setWorkspaces(ws);
      // The remembered workspace only counts while it still exists and you are
      // still a member — otherwise fall back to the first reachable one.
      setCurrentWs((cur) => (cur && ws.some((w) => w.id === cur) ? cur : ws[0]?.id ?? ''));
    } catch {
      /* keep current */
    }
  }, []);

  // Tag colour overrides for the current workspace (lower-case tag → colour).
  const [tagColors, setTagColors] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!currentWs) return;
    try {
      localStorage.setItem('salt-ws', currentWs);
    } catch {
      /* private mode */
    }
    let alive = true;
    void api.tagColors(currentWs).then((c) => alive && setTagColors(c)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [currentWs]);
  const setTagColor = useCallback(
    async (tag: string, color: string) => {
      const key = tag.toLowerCase();
      setTagColors((prev) => {
        const next = { ...prev };
        if (!color || color === 'default') delete next[key];
        else next[key] = color;
        return next;
      });
      try {
        await api.setTagColor(currentWs, tag, color);
      } catch {
        void api.tagColors(currentWs).then(setTagColors).catch(() => {});
      }
    },
    [currentWs],
  );

  const loadAll = useCallback(async () => {
    await Promise.all([loadPages(), loadFavorites(), loadWorkspaces()]);
  }, [loadPages, loadFavorites, loadWorkspaces]);

  const toggleFavorite = useCallback(
    async (id: string) => {
      const willAdd = !favorites.includes(id);
      setFavorites((prev) =>
        willAdd ? [...prev, id] : prev.filter((f) => f !== id),
      );
      try {
        if (willAdd) await api.addFavorite(id);
        else await api.removeFavorite(id);
      } catch {
        void loadFavorites(); // reconcile on failure
      }
    },
    [favorites, loadFavorites],
  );

  useEffect(() => {
    api
      .me()
      .then((m) => {
        setMe(m);
        // The account decides the language and time settings; initLocale only
        // had the localStorage cache to go on, which is a copy and may be
        // stale — somebody who changed the language on their phone gets it
        // here too, one frame later (W112).
        if (m.authenticated && m.prefs) void applyPrefs(m.prefs);
        if (m.version && m.version !== BUILD_VERSION) {
          toast(t('A new version is available — reload the page'));
        }
        if (m.authenticated) void loadAll();
      })
      .catch(() => setLoadError(true));
  }, [loadAll]);

  useEffect(() => {
    const onUnauthorized = () =>
      setMe((prev) => ({
        setupRequired: prev?.setupRequired ?? false,
        authenticated: false,
        user: null,
        version: prev?.version ?? BUILD_VERSION,
      }));
    // Back/forward: restore the exact tab set from the history entry's state
    // (set by pushTabHistory). Falls back to the URL id for entries with no
    // snapshot (e.g. the very first load), reopening a tab only then.
    const onPop = (e: PopStateEvent) => {
      const st = e.state as { tabs?: string[]; active?: string | null } | null;
      if (st && Array.isArray(st.tabs)) {
        setOpenTabs(st.tabs);
        setCurrentId(st.active ?? null);
        return;
      }
      const id = pageIdFromLocation();
      setCurrentId(id);
      if (id) setOpenTabs((prev) => (prev.includes(id) ? prev : [...prev, id]));
    };
    // Clicking an inline @-mention (rendered by BlockNote) dispatches this.
    // Inlined (not via `navigate`) so the listener needs no deps and can't
    // hit a temporal-dead-zone on the later useCallback; navigates the active
    // tab in place, matching a normal link click.
    const onLinkNav = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      if (!id) return;
      history.pushState(null, '', `/p/${id}`);
      setCurrentId(id);
      setSidebarOpen(false);
      setOpenTabs((prev) => {
        if (prev.includes(id)) return prev;
        const active = activeRef.current;
        if (active && prev.includes(active)) return prev.map((t) => (t === active ? id : t));
        return [...prev, id];
      });
    };
    window.addEventListener('salt:unauthorized', onUnauthorized);
    window.addEventListener('popstate', onPop);
    window.addEventListener('salt:navigate', onLinkNav);
    return () => {
      window.removeEventListener('salt:unauthorized', onUnauthorized);
      window.removeEventListener('popstate', onPop);
      window.removeEventListener('salt:navigate', onLinkNav);
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setSearchOpen((v) => !v);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // ⌥N = new note (⌘N is reserved by browsers and can't be intercepted).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey && !e.metaKey && !e.ctrlKey && e.code === 'KeyN') {
        e.preventDefault();
        void createPageRef.current?.(null);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // A file dropped anywhere the application does not handle itself would be
  // NAVIGATED TO by the browser — the whole app replaced by a PDF viewer, with
  // whatever was open gone. Missing a drop is a shrug; losing the page for
  // aiming two centimetres wide is not. See dropFiles.ts.
  useEffect(() => guardDrops(), []);

  // Live updates: whenever anyone (or an agent via the API) changes the page
  // tree, the server broadcasts an SSE event and we refetch.
  const reloadTimer = useRef<number | undefined>(undefined);
  // True while the live connection is down. Only used to offer a way out — the
  // recovery below is automatic and this is what shows when it has not happened
  // yet, so nobody is left looking at a stale tree with no explanation.
  const [liveDown, setLiveDown] = useState(false);
  // Bumped to force a fresh EventSource. The watchdog below uses it when the
  // stream has gone silent — a stalled connection never errors, so closing and
  // reopening is the only way back.
  const [liveNonce, setLiveNonce] = useState(0);

  // The home-screen shortcut (long-press the icon) starts the app at
  // /?action=search. Wired here rather than left in the manifest as decoration:
  // a shortcut that opens the app and then does nothing is worse than no
  // shortcut. The parameter is cleared straight away so a reload does not
  // re-open the search box.
  useEffect(() => {
    if (new URLSearchParams(window.location.search).get('action') !== 'search') return;
    setSearchOpen(true);
    const url = window.location.pathname + window.location.hash;
    window.history.replaceState(window.history.state, '', url || '/');
  }, []);

  // Pull-to-refresh and the sync button land here. Both halves matter: fetch
  // the tree again, AND throw the live stream away and open a new one. After a
  // phone has slept the stream is often gone without having errored, so a
  // refresh that only refetched once would leave the app looking current and
  // then quietly stop updating — which is the state people describe as "it
  // does not sync".
  useEffect(
    () =>
      onRefresh(() => {
        void loadPages();
        setLiveNonce((n) => n + 1);
      }),
    [loadPages],
  );

  useEffect(() => {
    if (!me?.authenticated) return;
    const es = new EventSource('/api/events');
    let warnedVersion = false;

    // The watchdog. The server sends {"type":"ping"} every 25 seconds, so a
    // stream that has said nothing for 60 is not a quiet workspace — it is a
    // dead connection, and a dead SSE connection does NOT fire onerror. It just
    // stops, which is why waiting for an error found nothing during a real
    // outage. Silence is the only signal there is, so silence is what is
    // measured.
    let watchdog: number | undefined;
    const heard = () => {
      window.clearTimeout(watchdog);
      watchdog = window.setTimeout(() => {
        setLiveDown(true);
        setLiveNonce((n) => n + 1); // tear it down and build a new one
      }, 60000);
    };

    // THE FIX for "changes are not there until I restart".
    //
    // EventSource reconnects on its own when a connection drops — a sleeping
    // laptop, a switched network, a proxy timing out. What it does not do is
    // replay: every event that fired during the gap is simply gone, so the tree
    // stayed as it was until somebody happened to change something else. The
    // only visible cure was closing the window, because opening it refetches.
    //
    // onopen fires on the FIRST connect and on every reconnect, so refetching
    // here closes the gap by construction, whatever caused it.
    es.onopen = () => {
      setLiveDown(false);
      heard();
      void loadPages();
    };
    // CLOSED means the browser has given up retrying, which is worth saying at
    // once. CONNECTING is the ordinary state during a two-second hiccup and is
    // left to the watchdog.
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) setLiveDown(true);
    };
    es.onmessage = (e) => {
      heard();
      try {
        const msg = JSON.parse(e.data) as {
          type: string;
          version?: string;
          collection?: string;
          id?: string;
        };
        if (msg.type === 'hello' && msg.version && msg.version !== BUILD_VERSION && !warnedVersion) {
          warnedVersion = true;
          toast(t('A new version is available — reload the page'));
        }
        if (msg.type === 'pages') {
          window.clearTimeout(reloadTimer.current);
          reloadTimer.current = window.setTimeout(() => void loadPages(), 250);
        }
        // A database's rows moved. Passed on as a DOM event rather than through
        // props: only the open CollectionView cares, and only when it is the
        // database named here. Reloading every open view on every event is what
        // this used to do, and a database with 50k rows re-crawled itself
        // whenever anybody renamed anything.
        if (msg.type === 'rows' && msg.collection) {
          window.dispatchEvent(new CustomEvent('salt:rows', { detail: msg.collection }));
        }
        // An agent checked in or out. Content-free on purpose: the list is
        // fetched through a route that checks permissions per page.
        if (msg.type === 'presence') {
          window.dispatchEvent(new CustomEvent('salt:presence'));
        }
        // A note landed on a page's trail. Names the page and nothing more —
        // the text would reach every browser on the instance.
        if (msg.type === 'notes' && msg.id) {
          window.dispatchEvent(new CustomEvent('salt:notes', { detail: msg.id }));
        }
      } catch {
        /* ignore malformed events */
      }
    };
    // The second half of the same problem. A machine that slept does not always
    // notice the stream died — it can sit in CONNECTING for a while, or the tab
    // was hidden and throttled. Coming back to the window is the moment a person
    // expects to see current state, so that is when it is fetched, regardless of
    // what the stream thinks.
    const onVisible = () => {
      if (document.visibilityState === 'visible') void loadPages();
    };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    window.addEventListener('online', onVisible);

    return () => {
      window.clearTimeout(reloadTimer.current);
      window.clearTimeout(watchdog);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
      window.removeEventListener('online', onVisible);
      es.close();
    };
  }, [me?.authenticated, loadPages, liveNonce]);

  // Any modal opening collapses the sidebar drawer (mobile) — a popup and the
  // menu should never be visible at once.
  useEffect(() => {
    const onModal = () => setSidebarOpen(false);
    window.addEventListener('salt:modal', onModal);
    return () => window.removeEventListener('salt:modal', onModal);
  }, []);

  const rememberRecent = (id: string) => {
    try {
      const cur: string[] = JSON.parse(localStorage.getItem('salt-recents') ?? '[]');
      const next = [id, ...cur.filter((x) => x !== id)].slice(0, 8);
      localStorage.setItem('salt-recents', JSON.stringify(next));
    } catch {
      /* localStorage unavailable — recents are a nice-to-have */
    }
  };

  // Each history entry carries a snapshot of the tab set + active id in its
  // state, so back/forward restore the EXACT prior tabs instead of the URL id
  // being re-appended as a phantom tab (which happens with in-place navigation).
  const pushTabHistory = (tabs: string[], id: string | null, replace: boolean) => {
    const url = id ? `/p/${id}` : '/';
    const state = { tabs, active: id };
    if (replace) history.replaceState(state, '', url);
    else history.pushState(state, '', url);
  };

  // navigate activates `id`. Like a browser tab, it reuses an already-open tab
  // if `id` is open, otherwise it navigates the *active* tab in place (or opens
  // the first tab if none is active). Use openInNewTab to add a background tab.
  const navigate = useCallback((id: string | null, replace = false) => {
    setIndexOpen(false); // any navigation leaves the full-screen index overlay
    let nextTabs = tabsRef.current;
    if (id && !tabsRef.current.includes(id)) {
      const active = activeRef.current;
      nextTabs =
        active && tabsRef.current.includes(active)
          ? tabsRef.current.map((t) => (t === active ? id : t)) // navigate active tab
          : [...tabsRef.current, id];
    }
    pushTabHistory(nextTabs, id, replace);
    setOpenTabs(nextTabs);
    setCurrentId(id);
    setSidebarOpen(false); // close the mobile drawer after picking a page
    if (id && !replace) rememberRecent(id);
  }, []);

  // openInNewTab adds `id` as a new tab right after the active one and focuses it.
  const openInNewTab = useCallback((id: string) => {
    setIndexOpen(false);
    const prev = tabsRef.current;
    let nextTabs = prev;
    if (!prev.includes(id)) {
      const i = activeRef.current ? prev.indexOf(activeRef.current) : -1;
      nextTabs = i < 0 ? [...prev, id] : [...prev.slice(0, i + 1), id, ...prev.slice(i + 1)];
    }
    pushTabHistory(nextTabs, id, false);
    setOpenTabs(nextTabs);
    setCurrentId(id);
    setSidebarOpen(false);
    rememberRecent(id);
  }, []);

  // closeTab removes a tab; if it was active, the neighbour that slides into its
  // slot (else the previous one, else nothing) becomes active.
  const closeTab = useCallback((id: string) => {
    const prev = tabsRef.current;
    const i = prev.indexOf(id);
    if (i < 0) return;
    const next = prev.filter((x) => x !== id);
    setOpenTabs(next);
    if (activeRef.current === id) {
      const neighbour = next[i] ?? next[i - 1] ?? null;
      pushTabHistory(next, neighbour, true);
      setCurrentId(neighbour);
    }
  }, []);

  // Ctrl+Alt+←/→ cycles open tabs. metaKey is intentionally excluded: Cmd+Alt+←/→
  // is the macOS browser tab-switch shortcut. Ignored while typing.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && !e.metaKey && e.altKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
        const el = document.activeElement as HTMLElement | null;
        if (el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))) return;
        const tabs = tabsRef.current;
        if (tabs.length < 2) return;
        e.preventDefault();
        const i = activeRef.current ? tabs.indexOf(activeRef.current) : -1;
        const d = e.key === 'ArrowRight' ? 1 : -1;
        navigate(tabs[(i + d + tabs.length) % tabs.length]);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate]);

  // Pick a landing page when nothing is selected, and bounce away from a page
  // that was trashed. IMPORTANT: a selected id that is simply absent from the
  // tree list is still valid — database rows are children of a collection and
  // are deliberately excluded from /api/pages, so clicking one must NOT be
  // treated as an invalid selection (that caused a jump back to the home page).
  // Genuinely-gone pages are handled by the editor's onMissing callback.
  // Keep a tab only if its page is live in the tree, or it is the active page.
  // This drops trashed pages and stale ids left in localStorage (e.g. pages
  // deleted in another session), so no "Untitled" ghost tabs accumulate. The
  // trade-off: a database row (absent from /api/pages) survives only while it
  // is the active tab — an accepted minor limitation, not data loss.
  useEffect(() => {
    if (!pages) return;
    const alive = new Set(pages.filter((p) => !p.trashed).map((p) => p.id));
    setOpenTabs((prev) => {
      const next = prev.filter((id) => alive.has(id) || id === currentId);
      return next.length === prev.length && next.every((v, i) => v === prev[i]) ? prev : next;
    });
  }, [pages, currentId]);

  useEffect(() => {
    if (!pages) return;
    // Don't hijack the /invite/<token> route: an authenticated invitee must stay
    // on the invite screen long enough to accept, not get replaced with a page.
    if (/^\/invite\/[a-f0-9]+$/.test(window.location.pathname)) return;
    // Nor the consent screen. It renders only once `me` has loaded, and by then
    // this effect had already moved the browser to the last page you had open —
    // the agent's sign-in vanished into the app before anyone could answer it.
    if (window.location.pathname === '/oauth/consent') return;
    if (currentId) {
      const cur = pages.find((p) => p.id === currentId);
      if (!cur || !cur.trashed) return; // in-tree-and-live, OR a row not in the tree → keep
      // else: the current page is trashed → fall through and pick another
    }
    // Prefer a still-open tab over jumping into the tree, so closing/​trashing the
    // active page lands on a neighbouring tab rather than the first page.
    const openAlive = tabsRef.current.find((id) => {
      const p = pages.find((pp) => pp.id === id);
      return p && !p.trashed && id !== currentId;
    });
    if (openAlive) {
      navigate(openAlive, true);
      return;
    }
    const first =
      pages.find((p) => !p.trashed && !p.parentId) ?? pages.find((p) => !p.trashed);
    navigate(first ? first.id : null, true);
  }, [pages, currentId, navigate]);

  const createPage = useCallback(
    async (parentId: string | null, type: 'doc' | 'collection' = 'doc') => {
      // Root pages land in the selected workspace; children inherit the parent's.
      const p = await api.createPage(parentId, '', type, undefined, parentId ? undefined : currentWs);
      setPages((prev) => (prev ? [...prev, p] : [p]));
      navigate(p.id);
    },
    [navigate, currentWs],
  );
  createPageRef.current = createPage;

  const updateMeta = useCallback((id: string, patch: Partial<PageMeta>) => {
    setPages((prev) => prev?.map((p) => (p.id === id ? { ...p, ...patch } : p)) ?? prev);
  }, []);

  // Import for the empty-state (no pages yet). The primary import entry point is
  // the page ⋯-menu; this keeps a Notion-style zip/markdown import reachable
  // before any page exists.
  const emptyImportRef = useRef<HTMLInputElement>(null);
  const onEmptyImport = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      e.target.value = '';
      if (!file) return;
      try {
        if (file.name.toLowerCase().endsWith('.zip')) {
          const r = await api.importZip(file);
          toast(
            t('Imported {pages}', { pages: plural(r.created, '{n} page', '{n} pages') }) +
              (r.skipped ? ', ' + t('{n} skipped', { n: r.skipped }) : ''),
          );
          void loadPages();
        } else {
          const text = await file.text();
          const r = await api.importMarkdown(null, '', text);
          toast(t('Page imported'));
          void loadPages();
          navigate(r.id);
        }
      } catch (err) {
        toast((err as Error).message || t('Import failed'));
      }
    },
    [loadPages, navigate],
  );

  const trashPage = useCallback(
    async (id: string) => {
      await api.trashPage(id);
      // Leaving you on the page you just threw away was harmless while this was
      // reachable from the sidebar only — there you are usually looking at
      // something else. The page's own menu can do it now, so it has to move.
      if (id === currentId) navigate(null);
      await loadPages();
    },
    [loadPages, currentId, navigate],
  );

  const duplicatePage = useCallback(
    async (id: string) => {
      try {
        const r = await api.duplicatePage(id);
        await loadPages();
        navigate(r.id);
      } catch {
        toast(t('Could not be duplicated'));
      }
    },
    [loadPages, navigate],
  );

  const restorePage = useCallback(
    async (id: string) => {
      await api.restorePage(id);
      await loadPages();
    },
    [loadPages],
  );

  const deleteForever = useCallback(
    async (id: string) => {
      if (
        !(await confirm(t('Delete this page and all its sub-pages forever?'), {
          danger: true,
          confirmText: t('Delete'),
        }))
      )
        return;
      await api.deleteForever(id);
      await loadPages();
    },
    [loadPages],
  );

  const movePage = useCallback(
    async (id: string, parentId: string | null, position: number) => {
      await api.updatePage(id, { parentId, position });
      const fresh = await api.listPages();
      setPages(fresh);
      // Self-heal float precision: if two siblings ended up closer than this,
      // renumber them to clean integers so midpoints can't exhaust f64.
      const siblings = fresh
        .filter((p) => !p.trashed && (p.parentId ?? null) === parentId)
        .map((p) => p.position)
        .sort((a, b) => a - b);
      const tooDense = siblings.some((v, i) => i > 0 && v - siblings[i - 1] < 1e-6);
      if (tooDense) {
        // At the top level the server needs the workspace: otherwise it has to
        // guess which root pages are meant — and it used to take every one in
        // the whole instance.
        await api.reindexSiblings(parentId, parentId ? undefined : currentWs).catch(() => {});
        setPages(await api.listPages());
      }
    },
    [currentWs],
  );

  const handleMissing = useCallback(
    (id: string) => {
      setPages((prev) => prev?.filter((p) => p.id !== id) ?? prev);
      closeTab(id); // a genuinely-gone page closes its tab (rows stay — they 200)
      void loadPages();
    },
    [loadPages, closeTab],
  );

  const pagesById = useMemo(
    () => new Map((pages ?? []).map((p) => [p.id, p])),
    [pages],
  );

  // A viewer (read-only workspace role) may not edit; the doc editor renders
  // read-only so a viewer isn't teased with an editable-looking page whose
  // writes the server would only reject.
  const canEditCurrent = useMemo(() => {
    if (!currentId) return true;
    const page = pagesById.get(currentId);
    if (!page) return true;
    const role = workspaces.find((w) => w.id === page.workspaceId)?.role;
    return role !== 'viewer';
  }, [currentId, pagesById, workspaces]);

  const onAuthed = useCallback(
    (user: User) => {
      setMe({ setupRequired: false, authenticated: true, user, version: BUILD_VERSION });
      setSearchOpen(false);
      void loadAll();
    },
    [loadAll],
  );

  if (loadError && !pages) {
    return (
      <div className="empty-state">
        <div className="empty-emoji">🍂</div>
        <h2>{t('Cannot reach the server')}</h2>
        <p>{t('salt.md could not load your workspace.')}</p>
        <button className="btn primary" onClick={() => window.location.reload()}>
          {t('Retry')}
        </button>
      </div>
    );
  }

  // Public form: /form/<token>. Fully public — renders before any auth/me gate
  // so anyone with the link can submit without an account (or even while `me`
  // is still loading).
  const formMatch = window.location.pathname.match(/^\/form\/([a-f0-9]+)$/);
  if (formMatch) return <PublicForm token={formMatch[1]} />;

  // Invite-accept flow: /invite/<token>. A signed-out visitor sets up (or signs
  // into) an account and joins; a signed-in visitor joins as their current
  // account with one click. Handling both stops an already-logged-in invitee
  // from being silently bounced to the landing page without ever joining.
  // The consent screen for an agent signing in. It needs a real session — the
  // server bounces an unauthenticated visitor to /login and back here, so by the
  // time this renders there is somebody to ask.
  if (window.location.pathname === '/oauth/consent' && me?.authenticated) {
    return <OAuthConsent />;
  }

  const inviteMatch = window.location.pathname.match(/^\/invite\/([a-f0-9]+)$/);
  if (inviteMatch && me) {
    if (!me.authenticated) {
      return <InviteAccept token={inviteMatch[1]} onSuccess={onAuthed} />;
    }
    if (me.user) {
      return (
        <InviteAccept token={inviteMatch[1]} currentUser={me.user} onSuccess={onAuthed} />
      );
    }
  }
  // The sign-in screens have no sidebar yet, so the switch floats free in the
  // corner. Anyone arriving at the login page at night should not be blasted
  // with white and left unable to do anything about it.
  const authThemeSwitch = (
    <div className="auth-theme-switch">
      <ThemeSwitch value={themePref} onChange={setThemePref} />
    </div>
  );
  if (me?.setupRequired)
    return (
      <>
        {authThemeSwitch}
        <Setup onSuccess={onAuthed} />
      </>
    );
  if (me && !me.authenticated)
    return (
      <>
        {authThemeSwitch}
        <Login onSuccess={onAuthed} />
      </>
    );
  if (!pages || !me?.user) return <div className="app-loading"><Logo size={40} /></div>;

  const toaster = <Toaster />;

  return (
    <div className={'app' + (sidebarCollapsed ? ' sidebar-collapsed' : '') + (hoverLock ? ' hover-lock' : '')}>
      {/* Collapsed-sidebar hover zone (desktop): hovering the left edge slides
          the sidebar in as an overlay without permanently un-collapsing it. */}
      {sidebarCollapsed && <div className="sidebar-hotzone" />}
      {sidebarOpen && (
        <div className="sidebar-backdrop" onClick={() => setSidebarOpen(false)} />
      )}
      <Sidebar
        onUserChanged={(u) => setMe((prev) => (prev ? { ...prev, user: u } : prev))}
        canCreateWorkspace={!!me?.user?.isAdmin || me?.allowUserWorkspaces !== false}
        pages={pages}
        favorites={favorites}
        workspaces={workspaces}
        currentWs={currentWs}
        tagColors={tagColors}
        onSwitchWorkspace={setCurrentWs}
        onWorkspacesChanged={loadWorkspaces}
        user={me.user}
        currentId={currentId}
        open={sidebarOpen}
        onCollapse={() => {
          // One button for both worlds: on mobile the sidebar is a drawer, and
          // "collapse" there simply means closed. On the desktop it becomes a
          // hover overlay — the collapsed state applies only there, or it would
          // linger after a phone tap as an invisible side effect.
          setSidebarOpen(false);
          if (window.matchMedia('(min-width: 769px)').matches) {
            setSidebarCollapsed(true);
            setHoverLock(true);
          }
        }}
        collapsed={sidebarCollapsed}
        onExpand={() => {
          setSidebarCollapsed(false);
          setHoverLock(false);
        }}
        onNavigate={navigate}
        onOpenInNewTab={openInNewTab}
        onCreate={createPage}
        onTrash={trashPage}
        onDuplicate={duplicatePage}
        onRestore={restorePage}
        onDeleteForever={deleteForever}
        onMove={movePage}
        onToggleFavorite={toggleFavorite}
        onOpenSearch={() => setSearchOpen(true)}
        onOpenIndex={() => {
          announceModal(); // close any open modal + collapse the sidebar
          setIndexOpen(true);
        }}
        theme={theme}
        themePref={themePref}
        onSetTheme={setThemePref}
        onLogout={async () => {
          await api.logout();
          // Inside the marketing site's demo frame, '/' is the marketing home
          // page — loaded into the frame, with no way back to the application.
          // There the session IS the in-memory store, so a reload is the
          // sign-out: everything resets and the demo starts over.
          if (__SALT_DEMO__) window.location.reload();
          else window.location.href = '/';
        }}
        notesMode={notesActive}
        activeTag={notesTag}
        onSelectTag={setNotesTag}
        notesModeSetting={notesMode}
        onToggleNotesMode={toggleNotesMode}
        fontPref={fontPref}
        onSetFont={setFontPref}
      />
      {notesActive && (
        <NotesList
          pages={pagesById}
          currentWs={currentWs}
          activeId={currentId}
          tagColors={tagColors}
          tagFilter={notesTag}
          onClearTag={() => setNotesTag(null)}
          onNavigate={navigate}
          onCreate={() => void createPage(null)}
        />
      )}
      <main className="main">
        {indexOpen ? (
          <IndexView
            pages={pages}
            favorites={favorites}
            workspaces={workspaces}
            currentWs={currentWs}
            onNavigate={(id) => {
              setIndexOpen(false);
              navigate(id);
            }}
            onClose={() => setIndexOpen(false)}
          />
        ) : currentId ? (
          <>
            <TabBar
              tabs={openTabs}
              activeId={currentId}
              pagesById={pagesById}
              onSelect={navigate}
              onClose={closeTab}
            />
            <Editor
              key={currentId}
              pageId={currentId}
              pagesById={pagesById}
              user={me.user}
              theme={theme}
              canEdit={canEditCurrent}
              favorite={favorites.includes(currentId)}
              tagColors={tagColors}
              onSetTagColor={setTagColor}
              onMenu={openSidebar}
              onToggleFavorite={toggleFavorite}
              onMetaChange={updateMeta}
              onMissing={handleMissing}
              onNavigate={navigate}
              onCreatePage={createPage}
              onTrash={trashPage}
              onPagesChanged={loadPages}
            />
          </>
        ) : workspaces.length === 0 ? (
          // With no workspace at all this used to be a blank area: the app said
          // "no pages" and every button led nowhere, because pages need a
          // workspace. Since W102 every account gets a space of its own — but
          // if someone is left without one anyway (assignment revoked, creation
          // failed), at least say what is going on.
          <div className="empty-state">
            <button className="menu-btn empty-menu-btn" onClick={openSidebar}>
              ☰
            </button>
            <div className="empty-emoji"><Logo size={52} /></div>
            <h2>{t('No workspace')}</h2>
            <p>
              {t(
                'Your account currently belongs to no workspace. Ask an admin for access — or create one of your own, if this instance allows it.',
              )}
            </p>
            {me?.allowUserWorkspaces && (
              <div className="empty-actions">
                <button
                  className="btn primary"
                  onClick={() => {
                    void (async () => {
                      const name = await promptText(t('Name for the new workspace?'), {
                        placeholder: t('e.g. Personal'),
                      });
                      if (!name?.trim()) return;
                      try {
                        const ws = await api.createWorkspace(name.trim());
                        await loadWorkspaces();
                        setCurrentWs(ws.id);
                      } catch (e) {
                        toast((e as Error).message || t('Could not be created'));
                      }
                    })();
                  }}
                >
                  {t('Create workspace')}
                </button>
              </div>
            )}
          </div>
        ) : (
          <div className="empty-state">
            <button className="menu-btn empty-menu-btn" onClick={openSidebar}>
              ☰
            </button>
            <div className="empty-emoji"><Logo size={52} /></div>
            <h2>{t('No pages yet')}</h2>
            <p>{t('Create your first page — or import from Notion (.zip) / Markdown (.md).')}</p>
            <div className="empty-actions">
              <button className="btn primary" onClick={() => void createPage(null)}>
                {t('New page')}
              </button>
              <button className="btn" onClick={() => emptyImportRef.current?.click()}>
                {t('Import (.md / .zip)')}
              </button>
            </div>
            <input
              ref={emptyImportRef}
              type="file"
              accept=".md,.markdown,.zip"
              style={{ display: 'none' }}
              onChange={(e) => void onEmptyImport(e)}
            />
          </div>
        )}
      </main>
      {searchOpen && (
        <SearchModal
          recent={(() => {
            try {
              const ids: string[] = JSON.parse(localStorage.getItem('salt-recents') ?? '[]');
              return ids
                .map((id) => pagesById.get(id))
                .filter((p): p is PageMeta => !!p && !p.trashed)
                .map((p) => ({ id: p.id, title: p.title, icon: p.icon }));
            } catch {
              return [];
            }
          })()}
          onClose={() => setSearchOpen(false)}
          onNavigate={(id) => {
            setSearchOpen(false);
            navigate(id);
          }}
        />
      )}
      {/* Shown only while the live connection is genuinely down. Everything
          above recovers by itself; this is for the case where it has not, and
          its job is to say WHY the page looks stale rather than leave somebody
          restarting the application to find out. Refetching is enough — a full
          page reload would throw away open tabs and unsaved editor state. */}
      {liveDown && (
        <button
          type="button"
          className="live-down"
          onClick={() => {
            void loadPages();
            window.location.reload();
          }}
        >
          <WifiOff size={13} />
          {t('Not receiving live updates — reconnect')}
        </button>
      )}
      {toaster}
      <DialogHost />
      <UploadBar />
      <ImageLightbox />
      <PullToRefresh />
    </div>
  );
}
