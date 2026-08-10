// Mermaid is about a megabyte over the wire, so it is imported only when a page
// actually holds a diagram — the same arrangement mdiLoader makes for the icon
// set. A page without one never pays for it.
//
// The rendered SVG is stored back onto the block (see mermaidSpec in blocks.tsx)
// and that is not an optimisation. The print view is built by the SERVER and has
// no browser bundle to draw with: without a stored picture, every diagram would
// be missing from every PDF. It is the same lesson the page icons taught.

let mermaid: typeof import('mermaid').default | null = null;
let loading: Promise<void> | null = null;

/** Loads mermaid once and configures it. Repeat calls share the same promise. */
export function loadMermaid(): Promise<void> {
  if (mermaid) return Promise.resolve();
  if (!loading) {
    loading = import('mermaid')
      .then((m) => {
        mermaid = m.default;
        mermaid.initialize({
          startOnLoad: false,
          // The document decides its own colours; a diagram that follows the
          // interface theme would print white-on-white from a dark editor.
          theme: 'neutral',
          securityLevel: 'strict',
          fontFamily: 'inherit',
          // Labels as SVG <text>, not as HTML inside <foreignObject>. Two
          // reasons, and both were found the hard way: the export refuses
          // foreignObject outright (it can carry anything, including an
          // iframe), so every diagram was being dropped — and a foreignObject
          // is the part of an SVG that other renderers are least likely to
          // draw, which matters for a picture whose job is to end up in a PDF.
          flowchart: { htmlLabels: false },
          htmlLabels: false,
        });
      })
      .catch(() => {
        loading = null; // let a flaky network be retried
      });
  }
  return loading;
}

export interface MermaidResult {
  svg: string;
  error: string;
}

/** Renders diagram source to SVG. Never throws: a half-typed diagram is the
 *  normal state while somebody is writing one, so the error is a value. */
export async function renderMermaid(id: string, code: string): Promise<MermaidResult> {
  const text = code.trim();
  if (!text) return { svg: '', error: '' };
  await loadMermaid();
  if (!mermaid) return { svg: '', error: 'mermaid could not be loaded' };
  try {
    const out = await mermaid.render(id, text);
    return { svg: out.svg, error: '' };
  } catch (e) {
    // Mermaid leaves its failed attempt in the DOM under the id it was given.
    document.getElementById(id)?.remove();
    document.getElementById('d' + id)?.remove();
    return { svg: '', error: (e as Error).message || 'not a diagram' };
  }
}
