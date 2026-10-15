import './inlineCheckbox.css';
import { BlockNoteSchema, defaultBlockSpecs, defaultInlineContentSpecs } from '@blocknote/core';
import { t } from './i18n';
import { createReactInlineContentSpec } from '@blocknote/react';
import { bookmarkSpec, databaseSpec, calloutSpec, tocSpec, subpagesSpec, columnsSpec, mermaidSpec } from './blocks';

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

// Inline content works in paragraphs and table cells. Updating the node through
// BlockNote keeps toggles in the same persistence, collaboration and undo flow.
export const inlineCheckboxSpec = createReactInlineContentSpec(
  {
    type: 'inlineCheckbox',
    propSchema: { checked: { default: false } },
    content: 'none',
  } as const,
  {
    render: ({ inlineContent, updateInlineContent, editor }) => (
      <span className="inline-checkbox" contentEditable={false}>
        <input
          type="checkbox"
          aria-label={t('Inline checkbox')}
          checked={inlineContent.props.checked}
          disabled={!editor.isEditable}
          onMouseDown={(event) => event.preventDefault()}
          onChange={(event) => {
            if (editor.isEditable) updateInlineContent({ type: 'inlineCheckbox', props: { checked: event.target.checked } });
          }}
        />
      </span>
    ),
    toExternalHTML: ({ inlineContent }) => (
      <span data-inline-checkbox={inlineContent.props.checked ? 'true' : 'false'}>
        {inlineContent.props.checked ? '☑' : '☐'}
      </span>
    ),
    parse: (element) => {
      const checked = element.getAttribute('data-inline-checkbox');
      return checked === null ? undefined : { checked: checked === 'true' };
    },
  },
);

// Account mentions are atomic inline nodes, so editing around a name cannot
// silently change its recipient. Labels are readable in exports without IDs.
export const userMentionSpec = createReactInlineContentSpec(
  {
    type: 'userMention',
    propSchema: {
      userId: { default: '' },
      label: { default: '' },
      mentionId: { default: '' },
      authorId: { default: '' },
      originPageId: { default: '' },
    },
    content: 'none',
  } as const,
  {
    render: ({ inlineContent }) => (
      <span className="person-mention page-person-mention" contentEditable={false} tabIndex={-1}
        data-person-mention-id={inlineContent.props.mentionId}>
        {'@' + inlineContent.props.label}
      </span>
    ),
  },
);

// Schema = the default blocks plus our own (callout, table of contents,
// sub-pages, bookmark/embed, embedded database, columns) and the pageLink inline content.
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
      subpages: subpagesSpec(),
      bookmark: bookmarkSpec(),
      database: databaseSpec(),
      columns: columnsSpec(),
      mermaid: mermaidSpec(),
    },
    inlineContentSpecs: {
      ...defaultInlineContentSpecs,
      pageLink: pageLinkSpec,
      userMention: userMentionSpec,
      inlineCheckbox: inlineCheckboxSpec,
    },
  });

export type SaltEditor = typeof saltSchema.BlockNoteEditor;
