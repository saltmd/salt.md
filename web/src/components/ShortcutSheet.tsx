import { useEffect, useState } from 'react';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { shortcutList, useShortcut } from '../keys';
import { t } from '../i18n';

// The list of shortcuts, read from the registry rather than written down.
//
// A hand-kept list is a second place to forget: the shortcut moves, the sheet
// still shows the old chord, and a reader who tries it concludes the feature is
// broken rather than the documentation. Reading the registry means the sheet
// cannot disagree with what is actually bound.
//
// It shows what is bound RIGHT NOW, which is why it is worth opening from the
// screen you are asking about: a shortcut registered by the editor is only in
// here while an editor is open. That is the honest answer to "what can I press
// here", and a flat list of everything the app can ever do is not.

export default function ShortcutSheet({ onClose }: { onClose: () => void }) {
  useExclusiveModal(onClose);

  // Escape closes it, through the registry rather than a listener of its own:
  // `modal` scope outranks everything underneath, so this wins over any Escape
  // the view behind it may want.
  useShortcut({
    id: 'help.close',
    keys: ['escape'],
    scope: 'modal',
    whileTyping: true,
    label: () => t('Close'),
    group: () => t('General'),
    run: () => onClose(),
  });

  // Read again AFTER mount so the sheet can list its own Escape: shortcuts
  // register in an effect, which has not run at first render — and "how do I
  // get out of this" is the one row a reader looks for here.
  const [all, setAll] = useState(shortcutList);
  useEffect(() => setAll(shortcutList()), []);

  const groups: { name: string; items: typeof all }[] = [];
  for (const s of all) {
    const name = s.group || t('General');
    const last = groups[groups.length - 1];
    if (last && last.name === name) last.items.push(s);
    else groups.push({ name, items: [s] });
  }

  return (
    <Portal>
      <div
        className="modal-overlay"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <div className="shortcut-sheet" role="dialog" aria-modal="true" aria-label={t('Keyboard shortcuts')}>
          <div className="shortcut-sheet-head">
            <h2>{t('Keyboard shortcuts')}</h2>
          </div>
          {groups.length === 0 && <p className="shortcut-sheet-empty">{t('Nothing is bound on this screen.')}</p>}
          {groups.map((g) => (
            <section key={g.name} className="shortcut-group">
              <h3>{g.name}</h3>
              {g.items.map((s) => (
                <div key={s.id} className="shortcut-row">
                  <span className="shortcut-label">{s.label}</span>
                  <kbd className="shortcut-keys">{s.keys}</kbd>
                </div>
              ))}
            </section>
          ))}
        </div>
      </div>
    </Portal>
  );
}
