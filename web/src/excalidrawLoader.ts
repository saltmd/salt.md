// Draws a .excalidraw file, once.
//
// The library is about 1.4 MB over the wire — ten times mermaid — so it is
// imported only when a page actually holds a drawing that has no picture yet.
// Once the picture is stored on the block, nobody loads it again: every later
// reader, and the PDF, get the SVG.
//
// salt.md does not edit these. This is a reader, which is the whole point: the
// elaborate drawing is made somewhere else and looked at here.

export const EXCALIDRAW_REV = 1;

export interface DrawResult {
  svg: string;
  error: string;
}

let api: typeof import('@excalidraw/excalidraw') | null = null;
let loading: Promise<void> | null = null;

function load(): Promise<void> {
  if (api) return Promise.resolve();
  if (!loading) {
    loading = import('@excalidraw/excalidraw')
      .then((m) => {
        api = m;
      })
      .catch(() => {
        loading = null; // a flaky network may be retried
      });
  }
  return loading;
}

/** Fetch the file and draw it. Never throws: a file somebody uploaded may be
 *  anything at all, and the block has to say so rather than break the page. */
export async function renderExcalidraw(url: string): Promise<DrawResult> {
  if (!url) return { svg: '', error: '' };
  let scene: { elements?: unknown[]; appState?: Record<string, unknown>; files?: unknown };
  try {
    const res = await fetch(url);
    if (!res.ok) return { svg: '', error: `the file could not be read (${res.status})` };
    scene = await res.json();
  } catch {
    return { svg: '', error: 'the file is not readable Excalidraw JSON' };
  }
  if (!Array.isArray(scene?.elements)) {
    return { svg: '', error: 'this file holds no drawing' };
  }

  await load();
  if (!api) return { svg: '', error: 'the drawing library could not be loaded' };

  try {
    const el = await api.exportToSvg({
      elements: scene.elements as never,
      files: (scene.files ?? null) as never,
      appState: {
        ...(scene.appState ?? {}),
        // The document decides its own background. A drawing exported with a
        // dark canvas would print as a black rectangle.
        exportBackground: false,
        exportWithDarkMode: false,
      } as never,
    });
    // Same treatment as a mermaid drawing: the size is CSS's business, and the
    // viewBox is all the browser needs to scale it. Left in place, the library's
    // own width and height would beat any stylesheet.
    el.removeAttribute('width');
    el.removeAttribute('height');
    return { svg: el.outerHTML, error: '' };
  } catch (e) {
    return { svg: '', error: (e as Error).message || 'the drawing could not be rendered' };
  }
}
