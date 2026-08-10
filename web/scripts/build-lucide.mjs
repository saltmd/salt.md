// Renders the curated Lucide set to plain SVG markup, once, at build time.
//
// The reason it has to exist: the icons are React components, so only the
// browser bundle can draw them. The PRINT view is built on the server and has
// no bundle — it was writing the raw string, and a document came out of the
// printer with "lucide:Rocket" where its icon should be.
//
// One JSON file rather than 380 SVGs: the server reads it once at startup, the
// way it already reads favicon.svg, and there is no per-request file access.
//
// Output goes to public/, which vite copies into dist/, which the binary
// embeds. It is generated, so it is gitignored — `npm run build` makes it, and
// the Docker image runs that too.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import * as lucide from 'lucide-react';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');

const src = readFileSync(join(web, 'src/lucideSet.ts'), 'utf8');
const block = src.match(/LUCIDE_SET[^=]*=\s*\{([^}]*)\}/s);
if (!block) throw new Error('LUCIDE_SET not found — did lucideSet.ts change shape?');
const names = block[1].split(',').map((n) => n.trim()).filter(Boolean);

const out = {};
const missing = [];
for (const name of names) {
  const Cmp = lucide[name];
  if (!Cmp) { missing.push(name); continue; }
  const svg = renderToStaticMarkup(
    React.createElement(Cmp, { size: 24, color: 'currentColor', fill: 'none', strokeWidth: 2 }),
  );
  // Only what is INSIDE the <svg>: the print view supplies its own wrapper, so
  // it controls the size and the colour without rewriting attributes by hand.
  const inner = svg.replace(/^<svg[^>]*>/, '').replace(/<\/svg>$/, '');
  out[name] = inner;
}

if (missing.length) {
  console.error(`  lucide: ${missing.length} name(s) not in lucide-react: ${missing.slice(0, 8).join(', ')}`);
  process.exit(1);
}

mkdirSync(join(web, 'public'), { recursive: true });
const path = join(web, 'public/lucide.json');
writeFileSync(path, JSON.stringify(out));
const kb = (JSON.stringify(out).length / 1024).toFixed(0);
console.log(`  lucide: ${Object.keys(out).length} icons → public/lucide.json (${kb} kB)`);
