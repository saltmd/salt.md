import { useEffect, useRef, useState } from 'react';
import Portal from './Portal';
import { t } from '../i18n';

// A slim top progress bar shown while any file uploads (driven by the
// salt:upload-progress / salt:upload-done events from api.upload).
export function UploadBar() {
  const [progress, setProgress] = useState<number | null>(null);
  const hideTimer = useRef<number | undefined>(undefined);
  useEffect(() => {
    const onProg = (e: Event) => {
      window.clearTimeout(hideTimer.current);
      setProgress((e as CustomEvent<number>).detail);
    };
    const onDone = () => {
      setProgress(1);
      hideTimer.current = window.setTimeout(() => setProgress(null), 400);
    };
    window.addEventListener('salt:upload-progress', onProg);
    window.addEventListener('salt:upload-done', onDone);
    return () => {
      window.removeEventListener('salt:upload-progress', onProg);
      window.removeEventListener('salt:upload-done', onDone);
    };
  }, []);
  if (progress === null) return null;
  return (
    <Portal>
      <div className="upload-bar" role="progressbar" aria-label={t('File upload')}>
        <div className="upload-bar-fill" style={{ width: `${Math.max(4, progress * 100)}%` }} />
      </div>
    </Portal>
  );
}

// Click any content image to open it fullscreen. Delegated at the document
// level so it works for BlockNote images too, without touching the editor.
export function ImageLightbox() {
  const [src, setSrc] = useState<string | null>(null);
  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      const t = e.target as HTMLElement;
      if (t.tagName !== 'IMG') return;
      const img = t as HTMLImageElement;
      // Only static content images: skip avatars, icons, cover thumbnails, and —
      // crucially — images inside the editor or wrapped in a link, so editing
      // gestures and link navigation keep their native behavior.
      if (img.closest('.avatar, .page-cover, .bn-bookmark, button')) return;
      if (img.closest('[contenteditable="true"], .bn-editor, a')) return;
      if (img.naturalWidth < 80) return;
      e.preventDefault();
      setSrc(img.currentSrc || img.src);
    };
    document.addEventListener('click', onClick);
    return () => document.removeEventListener('click', onClick);
  }, []);
  useEffect(() => {
    if (!src) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setSrc(null);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [src]);
  if (!src) return null;
  return (
    <Portal>
      <div className="lightbox" onClick={() => setSrc(null)}>
        <img src={src} alt="" onClick={(e) => e.stopPropagation()} />
      </div>
    </Portal>
  );
}
