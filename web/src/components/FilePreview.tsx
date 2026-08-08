import { useEffect } from 'react';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { t } from '../i18n';

// Files that open in a viewer instead of the download folder. The browser
// renders PDF itself, so this costs no library and no server-side conversion.
// Office formats (.docx/.xlsx/.pptx) are NOT here: no browser reads them
// natively, and the two ways to change that — shipping LibreOffice in the
// image, or handing the file to Microsoft's online viewer — cost either the
// single-binary install or the promise that a self-hosted Salt keeps its
// documents to itself.
const PREVIEWABLE = /\.pdf$/i;

// Only same-origin uploads. A file block's URL can also be a foreign address
// (the block's "Embed" tab takes any URL), and those must keep opening the
// normal way: /files/ is the one path that carries the sandbox CSP, so it is
// also the only one whose contents we are willing to frame.
export function isPreviewable(url: string): boolean {
  return url.startsWith('/files/') && PREVIEWABLE.test(url);
}

export function FilePreview({
  name,
  url,
  onClose,
}: {
  name: string;
  url: string;
  onClose: () => void;
}) {
  useExclusiveModal(onClose);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <Portal>
      <div
        className="modal-overlay file-preview-overlay"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <div className="file-preview" role="dialog" aria-modal="true" aria-label={name}>
          <div className="file-preview-bar">
            <span className="file-preview-name" title={name}>
              {name}
            </span>
            {/* A plain link, not fetch+blob: the browser already sends the
                session cookie, and download= keeps the readable name instead
                of the random id the file is stored under. */}
            <a className="btn-sm" href={url} download={name}>
              {t('Download')}
            </a>
            <button className="btn-sm" onClick={onClose}>
              {t('Close')}
            </button>
          </div>
          <iframe className="file-preview-frame" src={url} title={name} />
        </div>
      </div>
    </Portal>
  );
}
