// Asserts what a typed address becomes (src/serverURL.js).
//
//   node scripts/check-serverurl.mjs
//
// This is the only place in the desktop app that guesses. Everything else
// either works or shows an error; this decides what somebody meant by
// "salt.example.com" — and it is the first thing anybody does with the app, so
// a wrong guess is a first impression of "it does not work".
//
// The two cases worth the file: a scheme that is neither http nor https must be
// REFUSED rather than prefixed (or "ftp://x" quietly becomes the address
// "https://ftp"), and localhost must default to http (or every local server is
// unreachable on the first try, for a reason nobody can see).

import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { normalizeURL } = createRequire(import.meta.url)(join(here, '../src/serverURL.js'));

const out = [];
const check = (input, want) => out.push({ input, got: normalizeURL(input), want });

// ---- what people type ----
check('salt.example.com', 'https://salt.example.com');
check('https://salt.example.com', 'https://salt.example.com');
check('http://salt.example.com', 'http://salt.example.com');
check('  salt.example.com  ', 'https://salt.example.com');
check('salt.example.com/', 'https://salt.example.com');
check('SALT.EXAMPLE.COM', 'https://salt.example.com');

// A pasted address carries the page somebody was looking at. Keep the origin.
check('https://salt.example.com/p/9fd2?tab=x#top', 'https://salt.example.com');

// ---- this machine: http, or the first attempt always fails ----
check('localhost:8420', 'http://localhost:8420');
check('localhost', 'http://localhost');
check('127.0.0.1:8420', 'http://127.0.0.1:8420');
check('0.0.0.0:8420', 'http://0.0.0.0:8420');
// An explicit scheme always wins over the guess.
check('https://localhost:8443', 'https://localhost:8443');

// ---- a LAN address is not local: it may well be behind TLS ----
check('192.0.2.10:8420', 'https://192.0.2.10:8420');

// ---- refusals ----
// The important one: a foreign scheme must not be prefixed into a plausible
// address. "https://" + "ftp://x" parses with the host "ftp".
check('ftp://x', null);
check('file:///etc/passwd', null);
check('javascript:alert(1)', null);
check('', null);
check('   ', null);
check(null, null);
check(undefined, null);
check('not a url', null);

let fail = 0;
for (const c of out) {
  if (c.got !== c.want) {
    fail++;
    console.log(`  FAIL ${JSON.stringify(c.input)}: got ${JSON.stringify(c.got)}, want ${JSON.stringify(c.want)}`);
  }
}
console.log(`\n  server address: ${out.length - fail} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);
