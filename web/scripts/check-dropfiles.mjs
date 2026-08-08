// Asserts what a dropped file becomes, and when a drag is ours to take
// (src/dropFiles.ts).
//
//   node scripts/check-dropfiles.mjs
//
// Same shape as check-cardlayout.mjs: esbuild is here as part of vite, so the
// module is bundled once and imported. No framework.
//
// Why this exists: both functions are guesses about values that arrive from
// outside the application, and both fail SILENTLY when they are wrong.
//
//   blockTypeFor          — a wrong answer renders a photo as a download row.
//   carriesExternalFiles  — a wrong TRUE steals the drag that re-orders blocks
//                           inside the editor, and re-ordering breaks with no
//                           error anywhere. A wrong FALSE means the browser
//                           navigates to the dropped file and throws the whole
//                           application away.
//
// The second one is why this file is worth its length: the internal-drag case
// looks exactly like the external one to a container listener, and the only
// thing telling them apart is a MIME type in dataTransfer.types.

import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');
const tmp = mkdtempSync(join(tmpdir(), 'salt-drop-'));
const bundle = join(tmp, 'dropFiles.mjs');

try {
  try {
    execFileSync(
      join(web, 'node_modules/esbuild/bin/esbuild'),
      [join(web, 'src/dropFiles.ts'), '--bundle', '--format=esm', '--outfile=' + bundle],
      { stdio: ['ignore', 'ignore', 'pipe'] },
    );
  } catch (e) {
    console.error(String(e.stderr ?? e));
    process.exit(1);
  }

  const df = await import(bundle);
  const out = [];
  const check = (name, got, want) => out.push({ name, got: String(got), want: String(want) });
  const file = (name, type) => ({ name, type });
  const dt = (...types) => ({ types });

  // ---- What a file becomes ----
  check('a png is an image', df.blockTypeFor(file('a.png', 'image/png')), 'image');
  check('a heic is an image', df.blockTypeFor(file('a.heic', 'image/heic')), 'image');
  check('an mp4 is a video', df.blockTypeFor(file('a.mp4', 'video/mp4')), 'video');
  check('an mp3 is audio', df.blockTypeFor(file('a.mp3', 'audio/mpeg')), 'audio');
  check('a pdf is a file', df.blockTypeFor(file('a.pdf', 'application/pdf')), 'file');
  check('a docx is a file', df.blockTypeFor(file('a.docx',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document')), 'file');
  // Some systems hand over an upper-case type, and plenty hand over none at
  // all — a file with no type must still land as SOMETHING rather than vanish.
  check('an upper-case type still matches', df.blockTypeFor(file('a.PNG', 'IMAGE/PNG')), 'image');
  check('no type at all is a file', df.blockTypeFor(file('unknown-thing', '')), 'file');

  // ---- Whose drag is this ----
  check('files from outside are ours', df.carriesExternalFiles(dt('Files')), true);
  check('files plus a name are ours', df.carriesExternalFiles(dt('Files', 'text/plain')), true);
  // Dragging a block by its handle inside the editor. BlockNote puts its own
  // type on the transfer, and taking that drag breaks re-ordering silently.
  check('a block being moved is not ours', df.carriesExternalFiles(dt('blocknote/html', 'text/html')), false);
  // Chrome adds "Files" to an internal block drag as well, so the blocknote
  // marker has to WIN rather than merely be present. This is the assertion the
  // whole file is here for.
  check('a block drag wins even with Files on it',
    df.carriesExternalFiles(dt('blocknote/html', 'Files')), false);
  check('selected text is not ours', df.carriesExternalFiles(dt('text/plain')), false);
  check('a dragged link is not ours', df.carriesExternalFiles(dt('text/uri-list', 'text/plain')), false);
  check('nothing at all is not ours', df.carriesExternalFiles(dt()), false);
  check('a missing transfer is not ours', df.carriesExternalFiles(null), false);

  let fail = 0;
  for (const c of out) {
    if (c.got !== c.want) {
      fail++;
      console.log(`  FAIL ${c.name}: got ${JSON.stringify(c.got)}, want ${JSON.stringify(c.want)}`);
    }
  }
  console.log(`\n  dropped files: ${out.length - fail} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
