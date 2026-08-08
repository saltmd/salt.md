import { BlockNoteSchema, defaultBlockSpecs, defaultInlineContentSpecs } from '@blocknote/core';
import { createReactInlineContentSpec } from '@blocknote/react';
import { withMultiColumn } from '@blocknote/xl-multi-column';
import { bookmarkSpec, databaseSpec, calloutSpec, tocSpec } from './blocks';

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

// Schema = the default blocks plus our custom blocks (callout, table of
// contents, bookmark/embed), column layout (xl-multi-column), and the pageLink
// inline content. createReactBlockSpec returns a factory in 0.51 — call it.
export const saltSchema = withMultiColumn(
  BlockNoteSchema.create({
    blockSpecs: {
      ...defaultBlockSpecs,
      callout: calloutSpec(),
      toc: tocSpec(),
      bookmark: bookmarkSpec(),
      database: databaseSpec(),
    },
    inlineContentSpecs: {
      ...defaultInlineContentSpecs,
      pageLink: pageLinkSpec,
    },
  }),
);

export type SaltEditor = typeof saltSchema.BlockNoteEditor;
