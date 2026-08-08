// What a person types, turned into an address.
//
// Its own module with no dependencies, so it can be asserted without starting
// Electron — and it is the one piece of this app that GUESSES. Everything else
// either works or shows an error; this decides what somebody meant.
//
// The guess that matters: a bare "salt.example.com" becomes **https**. An
// instance on the internet without TLS is a mistake, and one on localhost is
// typed with its port anyway, where the explicit http:// is natural.

/** Hosts that mean "this machine", where http is the honest default: a local
 *  salt.md serves plain HTTP unless somebody deliberately gave it a
 *  certificate, so defaulting to https here fails every first attempt. */
const LOCAL = /^(localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0)(:|$)/i;

/** Normalises typed input to an origin, or null when it cannot be one. */
function normalizeURL(input) {
  const raw = String(input ?? '').trim();
  if (!raw) return null;
  // A scheme that is neither http nor https is a refusal, not something to
  // prepend to. Without this, "ftp://x" becomes "https://ftp://x", whose host
  // parses as "ftp" — a nonsense input turned into a plausible address, which
  // is the worst way for a guess to be wrong.
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(raw) && !/^https?:\/\//i.test(raw)) return null;
  const withScheme = /^https?:\/\//i.test(raw)
    ? raw
    : (LOCAL.test(raw) ? 'http://' : 'https://') + raw;
  try {
    const u = new URL(withScheme);
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return null;
    if (!u.hostname) return null;
    // The origin drops any path, query and fragment somebody pasted along —
    // people copy the address of the page they are looking at, not the root.
    return u.origin;
  } catch {
    return null;
  }
}

module.exports = { normalizeURL };
