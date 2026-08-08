import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { SearchResult } from '../types';
import Portal from './Portal';
import { PageIcon } from '../pageIcon';
import { useExclusiveModal } from '../modal';
import { t } from '../i18n';

function escapeHtml(s: string) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// The server wraps matches in \x01…\x02 so we can highlight them safely.
function snippetHtml(s: string) {
  return escapeHtml(s).replaceAll('\u0001', '<mark>').replaceAll('\u0002', '</mark>');
}

interface Props {
  recent?: { id: string; title: string; icon: string }[];
  onClose: () => void;
  onNavigate: (id: string) => void;
}

export default function SearchModal({ recent, onClose, onNavigate }: Props) {
  const [q, setQ] = useState('');
  const [results, setResults] = useState<SearchResult[]>([]);
  const [sel, setSel] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const seq = useRef(0);
  useExclusiveModal(onClose);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    const cur = ++seq.current;
    const t = window.setTimeout(async () => {
      const res = q.trim() ? await api.search(q) : [];
      if (seq.current === cur) {
        setResults(res);
        setSel(0);
      }
    }, 150);
    return () => window.clearTimeout(t);
  }, [q]);

  const onKey = (e: React.KeyboardEvent) => {
    if (e.nativeEvent.isComposing) return; // don't hijack IME composition keys
    if (e.key === 'Escape') onClose();
    else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSel((s) => Math.min(s + 1, Math.max(results.length - 1, 0)));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSel((s) => Math.max(s - 1, 0));
    } else if (e.key === 'Enter' && results[sel]) {
      onNavigate(results[sel].id);
    }
  };

  return (
    <Portal>
    <div
      className="modal-overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="search-modal" onKeyDown={onKey}>
        <input
          ref={inputRef}
          className="search-input"
          placeholder={t('Search all pages…')}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <div className="search-results">
          {results.map((r, i) => (
            <button
              key={r.id}
              className={'search-result' + (i === sel ? ' selected' : '')}
              onMouseEnter={() => setSel(i)}
              onClick={() => onNavigate(r.id)}
            >
              <span className="result-icon"><PageIcon icon={r.icon} size={16} fallback="📄" /></span>
              <span className="result-body">
                <span className="result-title">{r.title || t('Untitled')}</span>
                {r.snippet && (
                  <span
                    className="result-snippet"
                    dangerouslySetInnerHTML={{ __html: snippetHtml(r.snippet) }}
                  />
                )}
              </span>
            </button>
          ))}
          {q.trim() !== '' && results.length === 0 && (
            <div className="search-empty">{t('No results')}</div>
          )}
          {q.trim() === '' && (recent?.length ?? 0) > 0 && (
            <>
              <div className="search-section-label">{t('Recently opened')}</div>
              {recent!.map((r) => (
                <button key={r.id} className="search-result" onClick={() => onNavigate(r.id)}>
                  <span className="result-icon"><PageIcon icon={r.icon} size={16} fallback="📄" /></span>
                  <span className="result-body">
                    <span className="result-title">{r.title || t('Untitled')}</span>
                  </span>
                </button>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
    </Portal>
  );
}
