import type { PropDef, PropOption } from '../types';
import PropertyValue from './PropertyValue';
import { tagColorClass } from '../tags';
import { PageIcon } from '../pageIcon';

interface Row {
  id: string;
  title: string;
  icon: string;
  cover: string;
  props: Record<string, unknown>;
  tags?: string[];
}

// A Gallery view renders rows as a responsive grid of cards, each showing the
// row's cover (image or gradient), its tags, plus its properties. Select /
// multi-select properties are editable inline (Notion-style colour popover).
export default function GalleryView({
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
  return (
    <div className="gallery">
      {rows.map((r) => (
        <div key={r.id} className="gallery-card" onClick={() => onNavigate(r.id)}>
          <div className="gallery-cover" style={coverStyle(r.cover)}>
            {!r.cover && <span className="gallery-cover-icon"><PageIcon icon={r.icon} size={40} fallback="📄" /></span>}
          </div>
          <div className="gallery-body">
            <div className="gallery-title">
              {r.icon && r.cover ? <span className="inline-icon"><PageIcon icon={r.icon} size={14} /> </span> : null}
              {r.title || 'Untitled'}
            </div>
            {!!r.tags?.length && (
              <div className="db-row-tags card-tags">
                {r.tags.map((t) => (
                  <span key={t} className={'row-tag ' + tagColorClass(t, tagColors)}>
                    #{t}
                  </span>
                ))}
              </div>
            )}
            <div className="gallery-props">
              {schema.map((p) => {
                const v = r.props[p.id];
                const editable = p.type === 'select' || p.type === 'multiselect';
                if (!editable && (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)))
                  return null;
                if (editable) {
                  return (
                    <div key={p.id} className="card-prop-edit" onClick={(e) => e.stopPropagation()}>
                      <PropertyValue
                        def={p}
                        value={v}
                        onChange={(nv) => onSetProp(r.id, p.id, nv)}
                        onOptionsChange={(opts) => onSetOptions(p.id, opts)}
                      />
                    </div>
                  );
                }
                return <PropertyValue key={p.id} def={p} value={v} readOnly compact />;
              })}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// A cover is either an uploaded image URL (/files/...) or a "gradient:<css>".
function coverStyle(cover: string): React.CSSProperties {
  if (!cover) return {};
  if (cover.startsWith('gradient:')) return { background: cover.slice('gradient:'.length) };
  return { backgroundImage: `url(${cover})`, backgroundSize: 'cover', backgroundPosition: 'center' };
}

export type { Row as GalleryRow };
