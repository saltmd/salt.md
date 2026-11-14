import { useEffect, useMemo } from 'react';
import { api, ApiError } from './api';
import { t } from './i18n';
import { normalize, useShortcut } from './keys';
import { modalOpen } from './modal';
import { toast } from './toast';

type Session = {
  actions: (() => Promise<void>)[];
  pending: number;
  queue: Promise<void>;
  live: boolean;
};

// Multiple embedded boards may be mounted together. Only the one the reader
// last interacted with owns undo; mounting order must not choose for them.
let active: Session | null = null;

export function useBoardUndo(collectionId: string, enabled: boolean, refresh: () => Promise<void>) {
  const session = useMemo<Session>(
    () => ({ actions: [], pending: 0, queue: Promise.resolve(), live: true }),
    [collectionId],
  );
  useEffect(() => {
    session.live = true;
    return () => {
      session.live = false;
      if (active === session) active = null;
    };
  }, [session]);

  // Serialize moves and undo, including a key pressed before a save answers.
  // Each queued operation handles its own request failure so the next can run.
  const enqueue = (run: () => Promise<void>) => {
    session.pending++;
    session.queue = session.queue.then(run).finally(() => { session.pending--; });
  };

  useShortcut({
    id: 'board.undo',
    keys: normalize('mod+z') === 'ctrl+z' ? ['mod+z'] : ['mod+z', 'ctrl+z'],
    scope: 'view',
    when: () => enabled && active === session && !modalOpen(),
    label: () => t('Undo board move'),
    group: () => t('Collection'),
    run: () => {
      if (!session.actions.length && !session.pending) return false;
      enqueue(async () => {
        const undo = session.actions[session.actions.length - 1];
        if (!undo) return;
        try {
          await undo();
          session.actions.pop();
        } catch (error) {
          // A newer move has replaced this one. Let the next undo reach older moves.
          if (error instanceof ApiError && error.code === 'collection_move_stale') {
            session.actions.pop();
            if (session.live) toast(error.message);
          } else if (session.live) {
            toast(t('That could not be taken back.'));
          }
        }
        if (session.live) await refresh().catch(() => {});
      });
    },
  });

  return {
    activate: () => { active = enabled ? session : null; },
    move: (save: () => Promise<(() => Promise<void>) | undefined>) => {
      active = session;
      enqueue(async () => {
        const undo = await save();
        if (undo) session.actions.push(undo);
      });
    },
    undoProperties: (changeId: number) => async () => {
      const result = await api.revertChange(changeId);
      if (!result.reverted.length && session.live) {
        toast(t('Nothing was taken back: every value has been changed since.'));
      }
    },
  };
}
