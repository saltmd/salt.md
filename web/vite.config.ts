import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const backend = process.env.SALT_PROXY ?? 'http://localhost:8420';

// The version the frontend compares against the server's, from ONE source.
//
// It used to be a constant in App.tsx with a comment saying "kept in sync with
// server.Version". It was not: the frontend said 1.2.0 while the server said
// 1.3.1, and since the two are compared for equality, the "new version —
// reload" banner fired on every single page load and never stopped. A warning
// that is always on is a warning nobody reads, which costs exactly the case it
// exists for. Hand-syncing two numbers was never going to hold; SALT_VERSION
// comes from the build (see Dockerfile's ldflags) and 'dev' locally, where a
// mismatch is meaningless anyway.
const version = process.env.SALT_VERSION ?? 'dev';

export default defineConfig({
  define: {
    __SALT_VERSION__: JSON.stringify(version),
    // False here, true in the website's demo build (demo-app/vite.demo.config.ts).
    // Three places behave differently when this application is framed on the
    // marketing site rather than serving its own origin. Those three used to be
    // patches applied to a COPY of this source over there — which meant they
    // were silently lost the first time somebody refreshed the copy, and the
    // demo then handed visitors an MCP address that speaks no MCP. Guarded here
    // instead: constant-false, so the branches are dead code in this build.
    __SALT_DEMO__: false,
  },
  plugins: [react()],
  server: {
    proxy: {
      '/api': backend,
      '/files': backend,
      // The Yjs relay lives outside /api; without ws:true the dev editor
      // never leaves its loading state (the socket dies at the vite server).
      '/collab': { target: backend, ws: true },
    },
  },
});
