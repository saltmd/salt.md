import { BlockNoteSchema, defaultBlockSpecs, defaultInlineContentSpecs } from '@blocknote/core';
import { createReactInlineContentSpec } from '@blocknote/react';
import { bookmarkSpec, databaseSpec, calloutSpec, tocSpec, columnsSpec, mermaidSpec, excalidrawSpec } from './blocks';

// A "pageLink" is an inline mention of another salt.md page. It stores the target
// page id and a display label. Clicking it dispatches a navigation event that
// App listens for (keeps BlockNote's render decoupled from React routing).
export const pageLinkSpec = createReactInlineContentSpec(
  {
    type: 'pageLink',
    propSchema: {
      pageId: { default: '' },
      label: { default: '' },
    },
    content: 'none',
  } as const,
  {
    render: (props) => (
      <button
        type="button"
        className="page-link"
        contentEditable={false}
        onClick={() =>
          window.dispatchEvent(
            new CustomEvent('salt:navigate', { detail: props.inlineContent.props.pageId }),
          )
        }
      >
        <span className="page-link-icon">🔗</span>
        {props.inlineContent.props.label || 'Untitled'}
      </button>
    ),
  },
);

// Schema = the default blocks plus our own (callout, table of contents,
// bookmark/embed, embedded database, columns) and the pageLink inline content.
// createReactBlockSpec returns a factory in 0.51 — call it.
//
// Columns used to come from @blocknote/xl-multi-column, which is BlockNote's
// PAID tier ("GPL-3.0 OR PROPRIETARY"). Nobody used it — 0 of 1410 pages — and
// keeping it would have forced a licence decision the day any closed part
// exists. Ours is below in blocks.tsx.
export const saltSchema =
  BlockNoteSchema.create({
    blockSpecs: {
      ...defaultBlockSpecs,
      callout: calloutSpec(),
      toc: tocSpec(),
      bookmark: bookmarkSpec(),
      database: databaseSpec(),
      columns: columnsSpec(),
      mermaid: mermaidSpec(),
      excalidraw: excalidrawSpec(),
    },
    inlineContentSpecs: {
      ...defaultInlineContentSpecs,
      pageLink: pageLinkSpec,
    },
  });

export type SaltEditor = typeof saltSchema.BlockNoteEditor;
