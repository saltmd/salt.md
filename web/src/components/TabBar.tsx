import { FileText, Table2, X } from 'lucide-react';
import { PageIcon } from '../pageIcon';
import type { PageMeta } from '../types';
import { t } from '../i18n';

// Obsidian-style document tabs (Welle 26). One chip per open page; the active
// one is highlighted. Middle-click closes, matching browser tabs.
export default function TabBar({
  tabs,
  activeId,
  pagesById,
  onSelect,
  onClose,
}: {
  tabs: string[];
  activeId: string | null;
  pagesById: Map<string, PageMeta>;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
}) {
  if (tabs.length <= 1) return null; // a single doc needs no tab strip
  return (
    <div className="tab-bar" role="tablist">
      {tabs.map((id) => {
        const p = pagesById.get(id);
        const title = p?.title || t('Untitled');
        return (
          <div
            key={id}
            role="tab"
            aria-selected={id === activeId}
            className={'tab' + (id === activeId ? ' active' : '')}
            title={title}
            onClick={() => onSelect(id)}
            onAuxClick={(e) => {
              if (e.button === 1) {
                e.preventDefault();
                onClose(id);
              }
            }}
          >
            <span className="tab-icon">
              <PageIcon icon={p?.icon} size={13} fallback={p?.type === 'collection' ? <Table2 size={13} /> : <FileText size={13} />} />
            </span>
            <span className="tab-title">{title}</span>
            <button
              className="tab-close"
              aria-label={t('Close tab {title}', { title })}
              onClick={(e) => {
                e.stopPropagation();
                onClose(id);
              }}
            >
              <X size={13} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
