import type { PropDef, PropOption } from '../types';
import PropertyValue from './PropertyValue';
import { tagColorClass } from '../tags';
import { PageIcon } from '../pageIcon';
import { t } from '../i18n';

interface Row {
  id: string;
  title: string;
  icon: string;
  props: Record<string, unknown>;
  tags?: string[];
}

// A list view is deliberately NOT a narrow table: no column grid, no header
// row, no horizontal scrolling. The title leads and the properties trail
// quietly behind it. This is the calm view for notes — anyone who wants to
// compare values or read down a column takes the table instead.
export default function ListView({
  rows,
  schema,
  emptyLabel,
  tagColors,
  onNavigate,
  onSetProp,
  onSetOptions,
}: {
  rows: Row[];
  schema: PropDef[];
  emptyLabel: string;
  tagColors: Record<string, string>;
  onNavigate: (id: string) => void;
  onSetProp: (rowId: string, propId: string, value: unknown) => void;
  onSetOptions: (propId: string, options: PropOption[]) => void;
}) {
  if (rows.length === 0) {
    return <div className="db-empty">{emptyLabel}</div>;
  }
  // A list lives on restraint: only the first few properties accompany the
  // title, or the row turns back into a table.
  const inline = schema.slice(0, 3);

  return (
    <div className="list-view">
      {rows.map((r) => (
        <div key={r.id} className="list-row" onClick={() => onNavigate(r.id)}>
          <span className="list-row-icon">
            <PageIcon icon={r.icon} size={17} fallback="📄" />
          </span>
          <span className="list-row-title">{r.title || t('Untitled')}</span>
          {r.tags && r.tags.length > 0 && (
            <span className="list-row-tags">
              {r.tags.map((t) => (
                <span key={t} className={'tag-chip ' + tagColorClass(t, tagColors)}>
                  #{t}
                </span>
              ))}
            </span>
          )}
          {inline.length > 0 && (
            // The cells stay editable (same select popover as everywhere else),
            // so the click must not fall through to the row.
            <span className="list-row-props" onClick={(e) => e.stopPropagation()}>
              {inline.map((p) => (
                <PropertyValue
                  key={p.id}
                  def={p}
                  value={r.props?.[p.id]}
                  compact
                  onChange={(v) => onSetProp(r.id, p.id, v)}
                  onOptionsChange={(opts) => onSetOptions(p.id, opts)}
                />
              ))}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}
