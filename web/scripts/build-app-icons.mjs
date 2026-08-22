// Renders the home-screen icons from the ONE brand mark in public/logo.svg.
//
//   node scripts/build-app-icons.mjs
//   node scripts/build-app-icons.mjs --try   # variants into /tmp, ships nothing
//
// Deliberately NOT part of the build, for the same reason the wiki screenshots
// are not: it needs a browser to rasterise, and `npm ci` in the Docker image
// must not drag one in. Install it when you need it:
//
//   npm i --no-save playwright && npx playwright install chromium
//
// The output is committed, so a normal build and a normal checkout have the
// icons already. Run this again only when the mark or the palette changes.
//
// ---------------------------------------------------------------------------
// What the design is trying to satisfy
//
// Since iOS 26 the home screen re-renders every icon for Dark, Clear and Tinted
// appearances. Apple has published nothing about how a WEB app takes part; the
// only thing established from outside is that the treatment is driven by the
// `apple-touch-icon` link in the HTML head — icons declared only in the manifest
// keep the default look forever — and that a WHITE or light background is what
// their algorithm adapts best.
//
// So: white tile, dark mark, and the mark kept at the size it had before this
// script existed. A full-bleed coloured tile looked better standing alone and
// was the wrong thing to hand Apple.
//
// Three things that are still true regardless of the appearance question:
//
//   1. `purpose: "any maskable"` on ONE file cannot be right for both. Android
//      crops a maskable icon to a circle and expects the important part inside
//      the middle 80%; an icon drawn for that looks under-sized as a plain one,
//      and an icon drawn plain gets its edges shaved. Two files.
//   2. Transparency. iOS composites an alpha icon onto black on some versions
//      and white on others, so a mark that reads on one looks like a smudge on
//      the other. Every file here is opaque.
//   3. No corner rounding of our own — iOS applies its own mask, and a
//      pre-rounded icon gets rounded twice, which shows as a pale halo.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const pub = join(here, '..', 'public');

let chromium;
try {
  ({ chromium } = await import('playwright'));
} catch {
  console.error(
    '\n  playwright is not installed — it is deliberately not a dependency.\n' +
      '  npm i --no-save playwright && npx playwright install chromium\n',
  );
  process.exit(1);
}

const MARK = readFileSync(join(pub, 'logo.svg'), 'utf8');

// The shipped look. `ink` is the app's own text colour, the mark's colour
// everywhere else in the product.
const TILE = '#ffffff';
const INK = '#37352f';

// How much air is left around the mark. Chosen by rendering 0.17 / 0.12 / 0.08 /
// 0.04 side by side and looking, not by reasoning about it:
//
//   0.17 is what the icon had for a year. A thin dark line drawing at that size
//        reads as a small mark in a big empty square. It measured the same as
//        the coloured tile it replaced and still looked half the size, because
//        weight on screen is contrast, not geometry.
//   0.04 touches the edges, and iOS rounds the corners afterwards.
//   0.08 fills its tile the way an app icon does and survives the mask.
const INSET = 0.08;
// The maskable safe zone: everything that matters inside the middle 80 %, so a
// circular crop takes nothing but background.
const MASK_INSET = 0.3;

function page(size, inset, tile, ink) {
  const inner = Math.round(size * (1 - inset * 2));
  const svg = MARK.replace('<svg', '<svg style="width:100%;height:100%"').replace(
    'fill="currentColor"',
    `fill="${ink}"`,
  );
  return `<!doctype html><html><body style="margin:0">
    <div style="width:${size}px;height:${size}px;background:${tile};
                display:flex;align-items:center;justify-content:center;overflow:hidden">
      <div style="width:${inner}px;height:${inner}px;display:flex">${svg}</div>
    </div>
  </body></html>`;
}

const browser = await chromium.launch();

async function render(size, inset, tile, ink) {
  const p = await browser.newPage({ viewport: { width: size, height: size } });
  await p.setContent(page(size, inset, tile, ink));
  // omitBackground stays FALSE: these must be opaque. See the note above.
  const buf = await p.screenshot({ type: 'png' });
  await p.close();
  return buf;
}

if (process.argv.includes('--try')) {
  // Variants to look at on a real home screen. Only a device can answer how
  // Apple re-renders these, so the honest workflow is to put a few side by side
  // and look — not to argue about it here.
  const VARIANTS = [
    ['white-dark', '#ffffff', '#37352f'],
    ['white-green', '#ffffff', '#2f7d4f'],
    ['green-white', '#2f7d4f', '#ffffff'],
    ['sand-dark', '#f7f6f3', '#37352f'],
  ];
  for (const [name, tile, ink] of VARIANTS) {
    writeFileSync(`/tmp/salt-icon-${name}.png`, await render(512, INSET, tile, ink));
    console.log(`  /tmp/salt-icon-${name}.png`);
  }
  await browser.close();
  process.exit(0);
}

const SHOTS = [
  { file: 'apple-touch-icon.png', size: 180, inset: INSET },
  { file: 'icon-192.png', size: 192, inset: INSET },
  { file: 'icon-512.png', size: 512, inset: INSET },
  { file: 'icon-maskable-512.png', size: 512, inset: MASK_INSET },
];

mkdirSync(pub, { recursive: true });
for (const s of SHOTS) {
  writeFileSync(join(pub, s.file), await render(s.size, s.inset, TILE, INK));
  console.log(`  ok    ${s.file} (${s.size}×${s.size})`);
}
await browser.close();
console.log(`\n  ${SHOTS.length} icons written to web/public`);
