import { useEffect, useMemo } from 'react';
import { api } from './api';
import { t } from './i18n';
import { useShortcut } from './keys';
import { modalOpen } from './modal';
import { toast } from './toast';

type Session = {
  ids: number[];
  pending: number;
  queue: Promise<void>;
  live: boolean;
};

// Multiple embedded boards may be mounted together. Only the one the reader
// last interacted with owns undo; mounting order must not choose for them.
let active: Session | null = null;

export function useBoardUndo(collectionId: string, enabled: boolean, refresh: () => Promise<void>) {
  const session = useMemo<Session>(
    () => ({ ids: [], pending: 0, queue: Promise.resolve(), live: true }),
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
    keys: ['mod+z'],
    scope: 'view',
    when: () => enabled && active === session && !modalOpen(),
    label: () => t('Undo board move'),
    group: () => t('Collection'),
    run: () => {
      if (!session.ids.length && !session.pending) return false;
      enqueue(async () => {
        const id = session.ids[session.ids.length - 1];
        if (!id) return;
        try {
          const result = await api.revertChange(id);
          session.ids.pop();
          if (!result.reverted.length && session.live) {
            toast(t('Nothing was taken back: every value has been changed since.'));
          }
        } catch {
          // Keep the id for a deliberate retry after a failed request.
          if (session.live) toast(t('That could not be taken back.'));
        }
        if (session.live) await refresh().catch(() => {});
      });
    },
  });

  return {
    activate: () => { active = enabled ? session : null; },
    move: (save: () => Promise<{ changeId?: number } | undefined>) => {
      active = session;
      enqueue(async () => {
        const saved = await save();
        if (saved?.changeId) session.ids.push(saved.changeId);
      });
    },
  };
}
