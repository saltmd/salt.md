import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { t, plural } from '../i18n';
import { formatBytes, formatMoment } from '../format';
import type { SaltFile } from '../types';
import { FileText } from 'lucide-react';

// "Show me every document for this customer." Until now a file existed only as
// a block on a page, so that question meant clicking through a dossier or
// trusting full-text search. The list reads the file index (W125) and links
// each file back to the page carrying it.
export default function FileList({
  workspaceId,
  under,
  underTitle,
  onOpenPage,
  onClose,
}: {
  workspaceId: string;
  under?: string;
  underTitle?: string;
  onOpenPage: (id: string) => void;
  onClose: () => void;
}) {
  const [files, setFiles] = useState<SaltFile[] | null>(null);
  const [filter, setFilter] = useState('');
  const [kind, setKind] = useState('');
  useExclusiveModal(onClose);

  useEffect(() => {
    let alive = true;
    void api
      .listFiles(under ? { under } : { workspace: workspaceId })
      .then((f) => alive && setFiles(f))
      .catch(() => alive && setFiles([]));
    return () => {
      alive = false;
    };
  }, [workspaceId, under]);

  const kinds = useMemo(() => {
    const set = new Set((files ?? []).map((f) => f.ext).filter(Boolean));
    return [...set].sort();
  }, [files]);

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return (files ?? []).filter(
      (f) =>
        (!kind || f.ext === kind) &&
        (!q ||
          f.displayName.toLowerCase().includes(q) ||
          f.pageTitle.toLowerCase().includes(q)),
    );
  }, [files, filter, kind]);

  const totalSize = shown.reduce((n, f) => n + f.size, 0);

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog dialog-wide" role="dialog" aria-modal="true" aria-label={t('Files')}>
          <h2>{underTitle ? t('Files below “{name}”', { name: underTitle }) : t('Files')}</h2>
          {files === null ? (
            <div className="dialog-hint">{t('Loading…')}</div>
          ) : (
            <>
              <div className="file-list-tools">
                <input
                  className="prop-input"
                  value={filter}
                  placeholder={t('Filter by file or page…')}
                  onChange={(e) => setFilter(e.target.value)}
                />
                <select className="prop-select" value={kind} onChange={(e) => setKind(e.target.value)}>
                  <option value="">{t('All types')}</option>
                  {kinds.map((k) => (
                    <option key={k} value={k}>
                      {k}
                    </option>
                  ))}
                </select>
              </div>
              <div className="dialog-hint">
                {plural(shown.length, '{n} file', '{n} files')} · {formatBytes(totalSize)}
              </div>
              <div className="file-list">
                {shown.map((f) => (
                  <div key={f.name} className="file-row">
                    <span className="file-row-icon"><FileText size={15} /></span>
                    <a className="file-row-name" href={'/files/' + f.name} target="_blank" rel="noreferrer">
                      {f.displayName}
                    </a>
                    <span className="file-row-meta">{formatBytes(f.size)}</span>
                    <span className="file-row-meta">{f.createdAt ? formatMoment(f.createdAt, 'date') : ''}</span>
                    {f.pageId ? (
                      <button
                        className="file-row-page"
                        title={t('Open the page carrying this file')}
                        onClick={() => {
                          onOpenPage(f.pageId);
                          onClose();
                        }}
                      >
                        {f.pageTitle || t('Untitled')}
                      </button>
                    ) : (
                      <span className="file-row-meta">{t('no page')}</span>
                    )}
                  </div>
                ))}
                {shown.length === 0 && <div className="dialog-hint">{t('No files here yet.')}</div>}
              </div>
            </>
          )}
          <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
        </div>
      </div>
    </Portal>
  );
}
