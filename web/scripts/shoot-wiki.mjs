// Takes every screenshot the wiki uses, from wiki/screenshots.json.
//
//   node scripts/shoot-wiki.mjs                 # all of them
//   node scripts/shoot-wiki.mjs admin-mail      # one, or a prefix
//
// A picture in documentation is a claim like any other sentence, and it is the
// one nobody re-reads. It also cannot be checked for truth — so the next best
// thing is to make retaking it MECHANICAL, and to know exactly which pictures a
// change has made stale. That is what the manifest is for: every shot records
// the source files it shows, and check-wiki.mjs fails when any of them has
// changed since the shot was taken. The fix is to run this script again.
//
// Deliberately NOT a dependency of the build:
//
//   - `playwright` is not in package.json, so `npm ci` in the Docker image does
//     not fetch it and `npm run build` cannot break on it. That has happened
//     once already, with check-wiki.mjs reading files the image had not copied.
//   - Nothing here runs in the gate. The gate only compares dates.
//
// The frontend the instance serves must already contain the change you are
// photographing — and `npm run build` will NOT get you there, because it runs
// the gate first and the gate is what is failing. Use vite directly for this
// one step:
//
//   npx vite build && (restart the instance) && node scripts/shoot-wiki.mjs <id>
//
// That is the one place bypassing the gate is right: the gate's own remedy is
// on the other side of it.
//
// Before the first run:
//
//   npm i --no-save playwright && npx playwright install chromium
//
// It needs a running instance to photograph — never a real one. Point it at a
// throwaway with invented content:
//
//   SALT_SHOOT_URL=http://127.0.0.1:8421 \
//   SALT_SHOOT_EMAIL=… SALT_SHOOT_PASSWORD=… \
//   SALT_SHOOT_FRESH_URL=http://127.0.0.1:8422 \
//   node scripts/shoot-wiki.mjs
//
// The credentials come from the environment and never from the manifest: the
// manifest is committed, and a password in a repository is a password that has
// leaked.

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '../..');
const manifestPath = join(repo, 'wiki/screenshots.json');
const imgDir = join(repo, 'wiki/img');

const BASE = process.env.SALT_SHOOT_URL ?? 'http://127.0.0.1:8421';
const FRESH = process.env.SALT_SHOOT_FRESH_URL ?? '';
const EMAIL = process.env.SALT_SHOOT_EMAIL ?? '';
const PASSWORD = process.env.SALT_SHOOT_PASSWORD ?? '';

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

const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
const only = process.argv.slice(2);
const wanted = manifest.shots.filter((s) => !only.length || only.some((o) => s.id.startsWith(o)));
if (!wanted.length) {
  console.error(`  nothing matches ${only.join(', ')}`);
  process.exit(1);
}

mkdirSync(imgDir, { recursive: true });

/**
 * What a shot is stamped with: the CONTENT of every file it shows, not the
 * commit it was taken at.
 *
 * The commit was the obvious choice and it is wrong in the ordinary case. You
 * change a component, retake the picture, then commit — and now the commit that
 * changed the component is newer than the stamp, so the freshly taken picture
 * reports itself stale. Content cannot lie about that, needs no history, and
 * works in the Docker image where there is no .git at all.
 */
export function fingerprint(shows) {
  const out = {};
  for (const f of shows ?? []) {
    try {
      out[f] = createHash('sha256').update(readFileSync(join(repo, f))).digest('hex').slice(0, 12);
    } catch {
      out[f] = 'missing';
    }
  }
  return out;
}

const browser = await chromium.launch();

/** One signed-in context, reused: signing in for every shot is slow and the
 *  session is the normal state anyway. Fresh-instance shots get their own. */
async function contextFor(shot) {
  // Every shot is taken twice, light and dark. The wiki is published on a dark
  // site and read on GitHub, which is either — so the page offers both and lets
  // the reader's own setting choose. Two files, one recipe: the alternative is
  // a light screenshot burning a white hole in a dark page.
  const ctx = await browser.newContext({
    viewport: manifest.viewport ?? { width: 1280, height: 800 },
    deviceScaleFactor: 2, // a screenshot is read on a retina display
    colorScheme: shot.__theme,
    locale: 'en-GB',
    timezoneId: 'Europe/Berlin',
  });
  // 'fresh' — an instance with no account yet, for the welcome screen.
  // 'anon'  — the seeded instance, not signed in, for the sign-in screen and
  //           for anything a visitor sees.
  if (shot.instance === 'fresh') return { ctx, base: FRESH };
  if (shot.instance === 'anon') return { ctx, base: BASE };
  const page = await ctx.newPage();
  await page.goto(BASE, { waitUntil: 'load' });
  await page.waitForSelector('input[type=password], .sidebar', { timeout: 20000 });
  if (await page.locator('input[type=password]').count()) {
    await page.fill('input[type=email]', EMAIL);
    await page.fill('input[type=password]', PASSWORD);
    await page.click('button[type=submit]');
    await page.waitForSelector('.sidebar', { timeout: 15000 });
  }
  await page.close();
  return { ctx, base: BASE };
}

/** The step vocabulary. Small on purpose: a recipe somebody cannot read at a
 *  glance is a recipe nobody will re-run. */
async function run(page, step) {
  // 'load', never 'networkidle': the app holds an event stream open for as
  // long as it is on screen, so networkidle never arrives and every navigation
  // burns its full timeout instead. What a shot actually needs is a waitFor on
  // something it is about to photograph.
  if (step.goto !== undefined) return page.goto(page.__base + step.goto, { waitUntil: 'load' });
  if (step.click !== undefined) return page.click(step.click, { timeout: 10000 });
  if (step.hover !== undefined) return page.hover(step.hover, { timeout: 10000 });
  if (step.fill !== undefined) return page.fill(step.fill[0], step.fill[1]);
  if (step.press !== undefined) return page.keyboard.press(step.press);
  if (step.wait !== undefined) return page.waitForTimeout(step.wait);
  if (step.waitFor !== undefined) return page.waitForSelector(step.waitFor, { timeout: 15000 });
  // Hide anything that would make two runs of the same shot differ — a blinking
  // caret, a relative timestamp. A picture that changes on every run is a
  // picture whose staleness check means nothing.
  if (step.hide !== undefined)
    return page.addStyleTag({ content: `${step.hide} { visibility: hidden !important; }` });
  throw new Error(`unknown step: ${JSON.stringify(step)}`);
}

let taken = 0;
const failed = [];

for (const shot of wanted) {
  if (shot.instance === 'fresh' && !FRESH) {
    console.log(`  skip  ${shot.id} — needs SALT_SHOOT_FRESH_URL (an instance with no account yet)`);
    continue;
  }
  for (const theme of ['light', 'dark']) {
  shot.__theme = theme;
  const suffix = theme === 'dark' ? '-dark' : '';
  const { ctx, base } = await contextFor(shot);
  const page = await ctx.newPage();
  page.__base = base;
  try {
    for (const step of shot.steps) await run(page, step);
    // Transient chrome never belongs in documentation. A toast is whatever
    // happened to fire while the shot was taken — the reload banner walked into
    // a board screenshot exactly this way — and the caret blinks, which would
    // make two runs of the same shot differ for no reason.
    await run(page, { hide: '.toaster, .toast' });
    const target = shot.element ? page.locator(shot.element).first() : page;
    await target.screenshot({ path: join(imgDir, `${shot.id}${suffix}.png`), animations: 'disabled' });
    shot.takenAt = fingerprint(shot.shows);
    taken++;
    console.log(`  ok    ${shot.id}${suffix}`);
  } catch (err) {
    failed.push(`${shot.id}${suffix}: ${err.message.split('\n')[0]}`);
    console.log(`  FAIL  ${shot.id}${suffix} — ${err.message.split('\n')[0]}`);
  } finally {
    await ctx.close();
  }
  }
  delete shot.__theme;
}

await browser.close();
writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + '\n');
console.log(`\n  ${taken} of ${wanted.length * 2} taken`);
if (failed.length) {
  console.log(`  ${failed.length} failed:\n${failed.map((f) => '    ' + f).join('\n')}\n`);
  process.exit(1);
}
console.log('');
