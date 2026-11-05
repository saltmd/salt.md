import { useEffect, useRef } from 'react';
import { FormattingToolbar, getFormattingToolbarItems, useComponentsContext, useEditorState } from '@blocknote/react';
import { MessageSquarePlus } from 'lucide-react';
import { Plugin, PluginKey } from 'prosemirror-state';
import { Decoration, DecorationSet } from 'prosemirror-view';
import { absolutePositionToRelativePosition, relativePositionToAbsolutePosition, ySyncPluginKey } from 'y-prosemirror';
import * as Y from 'yjs';
import type { SaltEditor } from '../pageLink';
import type { Comment, CommentAnchor } from '../types';
import type { SaltProvider } from '../collab';
import { t } from '../i18n';

const key = new PluginKey('salt-inline-comments');

function locate(editor: SaltEditor, provider: SaltProvider, anchor: CommentAnchor, state = editor.prosemirrorState) {
  if (anchor.gen !== provider.gen) return null;
  const binding = ySyncPluginKey.getState(state)?.binding;
  if (!binding) return null;
  try {
    const fragment = provider.doc.getXmlFragment('blocknote');
    const start = Y.createRelativePositionFromJSON(anchor.from);
    // If the first selected character is deleted, detach instead of
    // guessing whether replacement text belongs to the original comment.
    if (!start.item || Y.getItem(provider.doc.store, start.item).deleted) return null;
    const from = relativePositionToAbsolutePosition(provider.doc, fragment, start, binding.mapping);
    const to = relativePositionToAbsolutePosition(provider.doc, fragment, Y.createRelativePositionFromJSON(anchor.to), binding.mapping);
    if (from === null || to === null || from >= to || to > state.doc.content.size) return null;
    return { from, to };
  } catch {
    return null;
  }
}

export function InlineCommentToolbar({ editor, provider, onSelect }: {
  editor: SaltEditor;
  provider: SaltProvider;
  onSelect: (anchor: CommentAnchor, blockId: string) => void;
}) {
  const Components = useComponentsContext()!;
  const eligible = useEditorState({
    editor,
    selector: ({ editor: current }) => {
      const { selection, doc } = current.prosemirrorState;
      const quote = doc.textBetween(selection.from, selection.to);
      return current.isEditable && !selection.empty && selection.$from.sameParent(selection.$to)
        && selection.$from.parent.isTextblock && !!quote.trim() && [...quote].length <= 2000;
    },
  });
  const select = () => {
    const state = editor.prosemirrorState;
    const { from, to } = state.selection;
    const binding = ySyncPluginKey.getState(state)?.binding;
    const quote = state.doc.textBetween(from, to, ' ');
    if (!eligible || !binding || provider.gen === null || !provider.synced || [...quote].length > 2000) return;
    const fragment = provider.doc.getXmlFragment('blocknote');
    onSelect({
      gen: provider.gen,
      quote,
      from: Y.relativePositionToJSON(absolutePositionToRelativePosition(from, fragment, binding.mapping)),
      to: Y.relativePositionToJSON(absolutePositionToRelativePosition(to, fragment, binding.mapping)),
    }, editor.getTextCursorPosition().block.id);
  };
  return (
    <FormattingToolbar>
      {getFormattingToolbarItems()}
      <Components.FormattingToolbar.Button className="bn-button" label={t('Comment on selected text')}
        mainTooltip={t('Comment on selected text')} icon={<MessageSquarePlus size={17} />}
        isDisabled={!eligible || !provider.synced || provider.gen === null} onClick={select} />
    </FormattingToolbar>
  );
}

export function useInlineComments(editor: SaltEditor, provider: SaltProvider, comments: Comment[], onOpen: (id: string) => void, onStatuses: (statuses: Record<string, boolean>) => void) {
  const commentsRef = useRef(comments);
  const onOpenRef = useRef(onOpen);
  const onStatusesRef = useRef(onStatuses);
  commentsRef.current = comments;
  onOpenRef.current = onOpen;
  onStatusesRef.current = onStatuses;

  useEffect(() => {
    const plugin = new Plugin({
      key,
      props: {
        decorations(state) {
          const decorations = commentsRef.current.flatMap((comment) => {
            if (!comment.anchor || comment.resolvedAt || comment.deletedAt) return [];
            const range = locate(editor, provider, comment.anchor, state);
            return range ? [Decoration.inline(range.from, range.to, { class: 'salt-comment-highlight', 'data-comment-id': comment.id })] : [];
          });
          return DecorationSet.create(state.doc, decorations);
        },
        // ProseMirror routes rapid second/third clicks away from handleClick.
        // A passage should open its thread on every click, including on phones.
        handleDOMEvents: {
          click(_view, event) {
            const element = event.target as HTMLElement;
            const id = element.closest<HTMLElement>('[data-comment-id]')?.dataset.commentId;
            if (!id) return false;
            onOpenRef.current(id);
            return true;
          },
        },
      },
    });
    editor._tiptapEditor.registerPlugin(plugin);
    const jump = (event: Event) => {
      const id = (event as CustomEvent<string>).detail;
      const comment = commentsRef.current.find((item) => item.id === id);
      const range = comment?.anchor && locate(editor, provider, comment.anchor);
      if (!range) return;
      const { node } = editor.prosemirrorView.domAtPos(range.from);
      const element = node instanceof HTMLElement ? node : node.parentElement;
      element?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    };
    window.addEventListener('salt:comment-jump', jump);
    return () => {
      window.removeEventListener('salt:comment-jump', jump);
      editor._tiptapEditor.unregisterPlugin(key);
    };
  }, [editor, provider]);

  useEffect(() => {
    let active = true;
    const update = () => {
      queueMicrotask(() => {
        if (!active) return;
        // Local editor changes reach Yjs in the view update, after decorations.
        // Refresh once that binding has caught up, before the next paint.
        editor.prosemirrorView.dispatch(editor.prosemirrorState.tr.setMeta(key, true));
        const statuses: Record<string, boolean> = {};
        for (const comment of commentsRef.current) {
          if (comment.anchor) statuses[comment.id] = !!locate(editor, provider, comment.anchor);
        }
        onStatusesRef.current(statuses);
      });
    };
    update();
    const stop = editor.onChange(update);
    return () => { active = false; stop(); };
  }, [editor, provider, comments]);
}
