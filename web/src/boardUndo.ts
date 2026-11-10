import { useEffect, useMemo } from 'react';
import { ApiError, api } from './api';
import { t } from './i18n';
import { useShortcut } from './keys';
import { modalOpen } from './modal';
import { toast } from './toast';

// One board move. `id` is the audit entry once the save answers; `claimed`
// marks a move an undo press has already chosen, so a second press picks the
// move below it even while the first is still waiting in the queue.
type Entry = { id: number | null; claimed: boolean };

type Session = {
  entries: Entry[];
  queue: Promise<void>;
  live: boolean;
  // Advances when an undo fails and can be retried. Presses queued behind
  // it were meant for the moves below it; they are cancelled, since taking
  // an older move back first would skip or overwrite the newer one.
  epoch: number;
};

// Multiple embedded boards may be mounted together. Only the one the reader
// last interacted with owns undo; mounting order must not choose for them.
let active: Session | null = null;

// A refusal that a retry cannot change: the entry is not revertible (400), the
// reader may no longer write the card (403), or the card or entry is gone (404).
const permanent = (err: unknown) => err instanceof ApiError && [400, 403, 404].includes(err.status);

export function useBoardUndo(collectionId: string, enabled: boolean, refresh: () => Promise<void>) {
  const session = useMemo<Session>(
    () => ({ entries: [], queue: Promise.resolve(), live: true, epoch: 0 }),
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
    session.queue = session.queue.then(run);
  };
  const drop = (entry: Entry) => {
    const i = session.entries.indexOf(entry);
    if (i >= 0) session.entries.splice(i, 1);
  };

  useShortcut({
    id: 'board.undo',
    keys: ['mod+z'],
    scope: 'view',
    when: () => enabled && active === session && !modalOpen(),
    label: () => t('Undo board move'),
    group: () => t('Collection'),
    run: () => {
      // The newest move no earlier press has taken, saved or still saving.
      const entry = [...session.entries].reverse().find((e) => !e.claimed);
      if (!entry) return false;
      entry.claimed = true;
      const epoch = session.epoch;
      enqueue(async () => {
        if (epoch !== session.epoch) return;
        // The move this press was meant for did not save: there is nothing to
        // take back, and reverting the move before it was not what was asked.
        if (entry.id === null) return;
        try {
          const result = await api.revertChange(entry.id);
          drop(entry);
          if (!result.reverted.length && session.live) {
            toast(t('Nothing was taken back: every value has been changed since.'));
          }
        } catch (err) {
          // A permanent refusal would block every older move behind it.
          // Anything else keeps the move for a deliberate retry.
          if (permanent(err)) drop(entry);
          else {
            session.epoch++;
            for (const e of session.entries) e.claimed = false;
          }
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
      const entry: Entry = { id: null, claimed: false };
      session.entries.push(entry);
      enqueue(async () => {
        const saved = await save().catch(() => undefined);
        if (saved?.changeId) entry.id = saved.changeId;
        else drop(entry);
      });
    },
  };
}
