// Dropping a file from the desktop into a document.
//
// BlockNote already accepts a drop that lands ON the text — it registers a
// ProseMirror drop handler and calls uploadFile. That part was never the
// problem. The problem is everywhere ELSE, and "everywhere else" is most of
// what a person aims at: the wide margins beside the column, the empty space
// below the last block (a document ends with about a fifth of a screen of it),
// the title, the sidebar.
//
// There, the browser's own default takes over — and its default for a dropped
// file is to NAVIGATE TO IT. The single-page app is replaced by a PDF viewer,
// and whatever was open is gone. So the worst outcome is also the most likely
// aim, which is a fair description of a feature that "does not work".
//
// Two things here, then:
//
//   carriesExternalFiles — whose drag is this. An internal block being moved
//                 looks identical to a container listener, and taking it breaks
//                 re-ordering with no error anywhere.
//   blockTypeFor  — what the file becomes once it is up.
//   guardDrops    — everywhere else in the app, a dropped file is swallowed.
//                 Not to be clever: purely so the browser does not throw the
//                 application away.
//
// Deliberately free of imports. That is what lets check-dropfiles.mjs bundle it
// and assert the two guesses directly — both of them fail silently when wrong,
// which is the worst way for a guess to fail.

/** Which block a file becomes. BlockNote ships all four; the file block is the
 *  fallback and renders a download row with the name. */
export function blockTypeFor(file: File): 'image' | 'video' | 'audio' | 'file' {
  const m = file.type.toLowerCase();
  if (m.startsWith('image/')) return 'image';
  if (m.startsWith('video/')) return 'video';
  if (m.startsWith('audio/')) return 'audio';
  return 'file';
}

/** True when this drag is carrying files from outside, rather than a block
 *  being moved around inside the editor. The two look identical to a container
 *  listener, and stealing an internal drag would break re-ordering. */
export function carriesExternalFiles(dt: DataTransfer | null): boolean {
  if (!dt) return false;
  if (dt.types.includes('blocknote/html')) return false;
  return dt.types.includes('Files');
}

/** Stop the browser from replacing the page with a dropped file.
 *
 *  Registered once for the whole application. It only ever cancels the default;
 *  anything that wants to ACT on a drop does so in its own handler, which runs
 *  first and can say so by calling preventDefault itself. */
export function guardDrops(): () => void {
  const over = (e: DragEvent) => {
    if (carriesExternalFiles(e.dataTransfer)) e.preventDefault();
  };
  const drop = (e: DragEvent) => {
    if (carriesExternalFiles(e.dataTransfer)) e.preventDefault();
  };
  window.addEventListener('dragover', over);
  window.addEventListener('drop', drop);
  return () => {
    window.removeEventListener('dragover', over);
    window.removeEventListener('drop', drop);
  };
}
