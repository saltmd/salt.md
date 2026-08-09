import React from 'react';
import ReactDOM from 'react-dom/client';
import '@blocknote/core/fonts/inter.css';
import '@blocknote/mantine/style.css';
// The fonts live in the binary, not at a CDN: a self-hosted instance should
// look complete with no outward network access, and a fetch from Google gives
// away every page view to a third party. The browser loads a font file only
// once it is actually used — so including them costs nothing as long as nobody
// has switched them on.
import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import './styles.css';
import App from './App';
import { installRingHover } from './ring';
import { initLocale, useLocale } from './i18n';
import ErrorBoundary from './components/ErrorBoundary';
import UpdateBanner from './components/UpdateBanner';
import ActivityLogHost from './components/ActivityLogHost';

installRingHover();

/** Remount the whole tree when the language changes.
 *
 *  The alternative is a useLocale() call in every component that translates
 *  anything — roughly forty of them, each one a chance to forget, and forgetting
 *  shows up as a pane that stays in the old language until you click something.
 *  Changing language is a once-in-a-blue-moon action, so paying for it with a
 *  remount is the right trade: one subscription instead of forty, and no way to
 *  get it wrong. */
function Root() {
  const locale = useLocale();
  return (
    <ErrorBoundary key={locale}>
      <App />
      {/* Outside App on purpose: app-wide chrome that fetches its own data, so
          it needs nothing from App's state — and App.tsx is what a wiki
          screenshot is stamped against, which this way stays untouched. */}
      <UpdateBanner />
      <ActivityLogHost />
    </ErrorBoundary>
  );
}

// Load the language before the first paint. Rendering first and translating
// afterwards would show every non-English user a flash of English on every
// load.
//
// A callback rather than top-level await on purpose: await here would force the
// build target up to ES2022 and drop Safari 14, which still runs the iOS
// home-screen app on older iPads. `finally` so a broken catalog costs the user
// English, never a blank page.
initLocale().finally(() => {
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <Root />
    </React.StrictMode>,
  );
});

// PWA app-shell caching. Service workers only run in secure contexts (HTTPS or
// localhost) — on a plain-HTTP LAN deployment this is a silent no-op, and the
// manifest/apple-touch-icon still give an installable home-screen app on iOS.
if ('serviceWorker' in navigator && window.isSecureContext) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      /* offline shell is a nice-to-have; never break the app over it */
    });
  });
}
