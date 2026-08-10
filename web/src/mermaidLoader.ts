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
          // 'base' is the theme meant to be customised — with any other, the
          // variables below are ignored and everything stays mermaid grey.
          theme: 'base',
          securityLevel: 'strict',
          // Named outright, never 'inherit'. The drawing is shown as an IMAGE,
          // and an image has no page around it to inherit from — it fell back
          // to the browser default, which is a serif nobody chose.
          fontFamily:
            "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif",
          // Labels as SVG <text>, not as HTML inside <foreignObject>. Two
          // reasons, and both were found the hard way: the export refuses
          // foreignObject outright (it can carry anything, including an
          // iframe), so every diagram was being dropped — and a foreignObject
          // is the part of an SVG that other renderers are least likely to
          // draw, which matters for a picture whose job is to end up in a PDF.
          flowchart: { htmlLabels: false, padding: 12, nodeSpacing: 40, rankSpacing: 46 },
          htmlLabels: false,
          // salt.md's own colours rather than mermaid's grey. A diagram in a
          // document should look like it belongs to the document.
          themeVariables: {
            primaryColor: '#eef4f0',
            primaryBorderColor: '#8fb9a2',
            primaryTextColor: '#1f1f1d',
            lineColor: '#8a8a85',
            secondaryColor: '#f5f5f4',
            tertiaryColor: '#faf9f7',
            fontSize: '15px',
          },
        });
      })
      .catch(() => {
        loading = null; // let a flaky network be retried
      });
  }
  return loading;
}

// Bump this whenever the way a diagram LOOKS changes — colours, spacing, the
// attributes stripped below. The picture on a block records the rev it was
// drawn with, and a block drawn by an older one is redrawn the next time it is
// opened. Without it a change to the renderer reaches only diagrams somebody
// happens to retype, which is how a themed renderer shipped and every existing
// diagram stayed grey.
//
// Same idea as ftsVersion and filesVersion on the server: a derived thing needs
// a number that says which version derived it.
export const MERMAID_REV = 3;

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
    // Mermaid writes its natural size into the element's own style attribute,
    // and an inline style beats any stylesheet — so "show this one at full size
    // and scroll" could never take effect. The viewBox stays, which is all the
    // browser needs to scale it; the sizing is CSS's business from here.
    const svg = out.svg
      .replace(/\sstyle="[^"]*max-width:[^"]*"/i, '')
      .replace(/^(<svg[^>]*?)\swidth="[^"]*"/i, '$1')
      .replace(/^(<svg[^>]*?)\sheight="[^"]*"/i, '$1');
    return { svg, error: '' };
  } catch (e) {
    // Mermaid leaves its failed attempt in the DOM under the id it was given.
    document.getElementById(id)?.remove();
    document.getElementById('d' + id)?.remove();
    return { svg: '', error: (e as Error).message || 'not a diagram' };
  }
}
