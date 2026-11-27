import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { t } from '../i18n';
import type { PropDef } from '../types';
import { formatComputed } from './PropertyValue';

/** How long typing pauses before the preview asks the server. */
const PREVIEW_DELAY_MS = 300;

/** The function reference under the expression, by group. Names are the
 *  language's own and are not translated. */
const FUNCTION_GROUPS = (): { label: string; names: string }[] => [
  { label: t('Logic'), names: 'if, ifs, empty, let, lets, and, or, not, ? :' },
  { label: t('Math'), names: 'round, floor, ceil, abs, sqrt, pow, mod, min, max, sum, mean, median, toNumber' },
  { label: t('Text'), names: 'concat, length, lower, upper, trim, contains, replace, replaceAll, test, substring, split, join, format' },
  { label: t('Dates'), names: 'now, today, dateAdd, dateSubtract, dateBetween, formatDate, parseDate, year, month, date, day, week, hour' },
  { label: t('Lists'), names: 'map, filter, find, some, every, first, last, at, sort, unique, includes' },
];

type Preview = { error?: string; rows: { id: string; title: string; value?: unknown; error?: string }[] };

/** The expression of a formula property, with a reference to the other
 *  properties and a live result for the first rows. `schema` is the dialog's
 *  unsaved draft. */
export default function FormulaEditor({
  collectionId,
  prop,
  schema,
  onChange,
}: {
  collectionId: string;
  prop: PropDef;
  schema: PropDef[];
  onChange: (formula: string) => void;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const formula = prop.formula ?? '';
  const schemaKey = JSON.stringify(schema);

  useEffect(() => {
    let live = true;
    const timer = window.setTimeout(() => {
      api
        .formulaPreview(collectionId, { formula, id: prop.id, schema })
        .then((p) => live && setPreview(p))
        .catch((e: Error) => live && setPreview({ error: e.message, rows: [] }));
    }, PREVIEW_DELAY_MS);
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collectionId, formula, prop.id, schemaKey]);

  // Puts text at the cursor, as if typed.
  const insert = (text: string) => {
    const el = ref.current;
    const at = el?.selectionStart ?? formula.length;
    const end = el?.selectionEnd ?? at;
    onChange(formula.slice(0, at) + text + formula.slice(end));
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(at + text.length, at + text.length);
    });
  };

  return (
    <>
      <label>{t('Expression')}</label>
      <textarea
        ref={ref}
        className="prop-input formula-input"
        placeholder={t('e.g. prop("Price") * prop("Qty")')}
        value={formula}
        rows={Math.min(8, Math.max(2, formula.split('\n').length))}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
      />
      <div className={'formula-status' + (preview?.error ? ' error' : '')} role="status">
        {preview?.error ? (
          '⚠ ' + preview.error
        ) : preview?.rows.length ? (
          <ul className="formula-preview">
            {preview.rows.map((r) => {
              const shown = formatComputed(r.value);
              return (
                <li key={r.id}>
                  <span className="formula-preview-title">{r.title || t('Untitled')}</span>
                  <span className={r.error ? 'formula-preview-value error' : 'formula-preview-value'}>
                    {r.error ? '⚠ ' + r.error : shown.text || '—'}
                  </span>
                </li>
              );
            })}
          </ul>
        ) : preview ? (
          t('No rows to try it on yet.')
        ) : null}
      </div>
      <div className="formula-hint">
        <div>{t('Click a property to insert it:')}</div>
        <div>
          {schema
            .filter((o) => o.id !== prop.id)
            .map((o) => (
              <button
                type="button"
                key={o.id}
                className="formula-token"
                title={'{' + o.id + '}'}
                onClick={() => insert(`prop(${JSON.stringify(o.name)})`)}
              >
                {o.name}
              </button>
            ))}
        </div>
        <details className="formula-functions">
          <summary>{t('Operators and functions')}</summary>
          <div>
            <code>+ - * / % ^ == != &lt; &lt;= &gt; &gt;=</code>
          </div>
          {FUNCTION_GROUPS().map((g) => (
            <div key={g.label}>
              <strong>{g.label}:</strong> <code>{g.names}</code>
            </div>
          ))}
        </details>
      </div>
    </>
  );
}
