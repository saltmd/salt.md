import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Trash2, Upload as UploadIcon } from 'lucide-react';
import Portal from './Portal';
import { EMOJI_GROUPS } from '../emojiData';
import { LUCIDE_SET, LUCIDE_NAMES } from '../lucideSet';
import { api } from '../api';
import { toast } from '../toast';
import { makeLucide, makeMdi } from '../pageIcon';
import { mdiNames, mdiPath, useMdi } from '../mdiLoader';
import { t } from '../i18n';

// Notion-style icon colours; '' = default (adapts to light/dark theme).
const ICON_COLORS = [
  { name: 'Standard', hex: '' },
  { name: 'Grau', hex: '#787774' },
  { name: 'Rot', hex: '#e03131' },
  { name: 'Orange', hex: '#e8590c' },
  { name: 'Gelb', hex: '#f2b100' },
  { name: 'Green', hex: '#2f9e44' },
  { name: 'Teal', hex: '#0c8599' },
  { name: 'Blau', hex: '#1971c2' },
  { name: 'Lila', hex: '#7048e8' },
  { name: 'Pink', hex: '#c2255c' },
];

interface Props {
  onPick: (icon: string) => void;
  onRemove: () => void;
  onClose: () => void;
  pageId?: string;
  /** The element the picker hangs off. With it, the picker leaves its container
   *  through a Portal and positions itself against the viewport — necessary
   *  wherever an ancestor scrolls or clips: inside the Workspace-image dialog
   *  (.dialog is `overflow-y: auto`) the list was cut off after a single row of
   *  smileys. Without it the picker stays where it always was, absolutely
   *  positioned under its wrapper. */
  anchor?: React.RefObject<HTMLElement | null>;
}

export default function IconPicker({ onPick, onRemove, onClose, pageId, anchor }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  // Same shape as SelectCell's popover: clamp to the viewport, flip upward when
  // there is more room above, and never exceed the space actually available.
  const [pos, setPos] = useState<{
    left: number;
    top?: number;
    bottom?: number;
    maxHeight: number;
  } | null>(null);

  useLayoutEffect(() => {
    const el = anchor?.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const width = Math.min(348, vw - 16);
    const spaceBelow = vh - r.bottom - 12;
    const spaceAbove = r.top - 12;
    const below = spaceBelow >= 320 || spaceBelow >= spaceAbove;
    setPos({
      left: Math.max(8, Math.min(r.left, vw - width - 8)),
      top: below ? r.bottom + 6 : undefined,
      bottom: below ? undefined : vh - r.top + 6,
      maxHeight: Math.max(240, below ? spaceBelow : spaceAbove),
    });
  }, [anchor]);
  const [tab, setTab] = useState<'emoji' | 'icon' | 'upload'>('emoji');
  const [q, setQ] = useState('');
  const [color, setColor] = useState('');
  // Two icon libraries: Lucide (outline, bundled) and the full Material Design
  // Icons set (~7.4k, lazily fetched the first time this tab is used).
  const [lib, setLib] = useState<'lucide' | 'mdi'>('lucide');
  const mdiReady = useMdi(tab === 'icon' && lib === 'mdi');
  const fileRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      const t = e.target as Element;
      if (t.closest?.('.icon-trigger')) return; // its own onClick toggles us
      if (ref.current && !ref.current.contains(t)) onClose();
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [onClose]);

  const emojiResults = useMemo(() => {
    const query = q.trim().toLowerCase();
    if (!query) return EMOJI_GROUPS;
    return EMOJI_GROUPS.map((g) => ({ cat: g.cat, items: g.items.filter(([, n]) => n.includes(query)) })).filter(
      (g) => g.items.length,
    );
  }, [q]);

  const iconResults = useMemo(() => {
    const query = q.trim().toLowerCase().replace(/[-\s]/g, '');
    const all = lib === 'mdi' ? (mdiReady ? mdiNames() : []) : LUCIDE_NAMES;
    if (!query) return lib === 'mdi' ? all.slice(0, 600) : all;
    const hits = all.filter((n) => n.toLowerCase().includes(query));
    // MDI is huge — cap the rendered grid so typing stays responsive.
    return lib === 'mdi' ? hits.slice(0, 600) : hits;
  }, [q, lib, mdiReady]);

  const doUpload = async (file?: File | null) => {
    if (!file) return;
    setUploading(true);
    try {
      const url = await api.upload(file, pageId);
      onPick(url);
    } catch {
      toast(t('Upload failed'));
    } finally {
      setUploading(false);
    }
  };

  const body = (
    <div
      className={'icon-picker' + (anchor ? ' is-anchored' : '')}
      ref={ref}
      style={anchor && pos ? { left: pos.left, top: pos.top, bottom: pos.bottom, maxHeight: pos.maxHeight } : undefined}
    >
      <div className="icon-picker-tabs">
        <button type="button" className={tab === 'emoji' ? 'on' : ''} onClick={() => { setTab('emoji'); setQ(''); }}>
          {t('Emoji')}
        </button>
        <button type="button" className={tab === 'icon' ? 'on' : ''} onClick={() => { setTab('icon'); setQ(''); }}>
          {t('Icons')}
        </button>
        <button type="button" className={tab === 'upload' ? 'on' : ''} onClick={() => { setTab('upload'); setQ(''); }}>
          {t('Upload')}
        </button>
        <button type="button" className="icon-picker-remove" onClick={onRemove} title={t('Remove icon')}>
          <Trash2 size={15} />
        </button>
      </div>

      {tab !== 'upload' && (
        <input
          className="icon-search"
          autoFocus
          placeholder={tab === 'emoji' ? t('Search emoji…') : t('Search icons…')}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      )}

      {tab === 'icon' && (
        <div className="icon-controls">
          <div className="icon-colors">
            {ICON_COLORS.map((c) => (
              <button
                key={c.name}
                type="button"
                className={'icon-color' + (color === c.hex ? ' on' : '')}
                title={c.name}
                onClick={() => setColor(c.hex)}
              >
                <span className="icon-color-dot" style={{ background: c.hex || 'var(--fg)' }} />
              </button>
            ))}
          </div>
          <div className="icon-style-toggle">
            <button type="button" className={lib === 'lucide' ? 'on' : ''} onClick={() => setLib('lucide')}>
              {t('Lucide')}
            </button>
            <button type="button" className={lib === 'mdi' ? 'on' : ''} onClick={() => setLib('mdi')}>
              {t('Material')}
            </button>
          </div>
        </div>
      )}

      <div className="icon-picker-body">
        {tab === 'emoji' &&
          emojiResults.map((g) => (
            <div key={g.cat} className="icon-cat">
              <div className="icon-cat-label">{g.cat}</div>
              <div className="icon-grid emoji-grid">
                {g.items.map(([e, n]) => (
                  <button key={e} type="button" title={n} onClick={() => onPick(e)}>
                    {e}
                  </button>
                ))}
              </div>
            </div>
          ))}
        {tab === 'emoji' && emojiResults.length === 0 && <div className="icon-empty">{t('No matches')}</div>}

        {tab === 'icon' && (
          <div className="icon-grid lucide-grid">
            {iconResults.map((name) => {
              const c = color || 'currentColor';
              if (lib === 'mdi') {
                const d = mdiPath(name);
                if (!d) return null;
                return (
                  <button
                    key={name}
                    type="button"
                    title={name}
                    onClick={() => onPick(makeMdi(name, color || undefined))}
                  >
                    <svg width={20} height={20} viewBox="0 0 24 24" aria-hidden="true">
                      <path d={d} fill={c} />
                    </svg>
                  </button>
                );
              }
              const Cmp = LUCIDE_SET[name];
              return (
                <button
                  key={name}
                  type="button"
                  title={name}
                  onClick={() => onPick(makeLucide(name, color || undefined))}
                >
                  <Cmp size={20} color={c} fill="none" strokeWidth={2} />
                </button>
              );
            })}
          </div>
        )}
        {tab === 'icon' && lib === 'mdi' && !mdiReady && (
          <div className="icon-empty">{t('Loading Material icons…')}</div>
        )}
        {tab === 'icon' && (lib !== 'mdi' || mdiReady) && iconResults.length === 0 && (
          <div className="icon-empty">{t('No matches')}</div>
        )}

        {tab === 'upload' && (
          <div className="icon-upload">
            <input
              ref={fileRef}
              type="file"
              accept="image/*"
              style={{ display: 'none' }}
              onChange={(e) => doUpload(e.target.files?.[0])}
            />
            <button type="button" className="icon-upload-btn" disabled={uploading} onClick={() => fileRef.current?.click()}>
              <UploadIcon size={16} /> {uploading ? t('Loading…') : t('Upload picture')}
            </button>
            <p className="icon-upload-hint">{t('PNG, JPG, GIF or SVG — shown square, as an icon.')}</p>
          </div>
        )}
      </div>
    </div>
  );

  // Anchored means "out of the box": the picker goes to <body>, so no scrolling
  // or clipping ancestor can cut it off.
  return anchor ? <Portal>{body}</Portal> : body;
}
