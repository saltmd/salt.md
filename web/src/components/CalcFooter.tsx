import { useRef, useState } from 'react';
import { Check } from 'lucide-react';
import AnchoredMenu from './AnchoredMenu';
import { CALC_NONE, calculationLabel, calculationsFor, calculationText, columnCalculation } from '../calculations';
import { t } from '../i18n';
import { useMenuDismiss } from '../modal';
import type { PropDef } from '../types';

/** One cell of a table's calculation row: the value, and a menu to choose what
 *  is calculated. `prop` is null for the title column. */
export default function CalcCell({
  calcKey,
  prop,
  calcs,
  values,
  onChange,
  className,
  style,
}: {
  calcKey: string;
  prop: PropDef | null;
  calcs: Record<string, string> | undefined;
  values: Record<string, unknown>;
  onChange: (next: Record<string, string>) => void;
  className: string;
  style?: React.CSSProperties;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLTableCellElement>(null);
  useMenuDismiss(open, ref, () => setOpen(false));
  const current = columnCalculation(calcs, calcKey, prop);
  const chosen = calcs?.[calcKey] !== undefined;
  const text = current ? calculationText(current, values[calcKey], !chosen, prop === null) : '';
  const pick = (c: string) => {
    setOpen(false);
    onChange({ ...(calcs ?? {}), [calcKey]: c });
  };
  return (
    <td ref={ref} className={className} style={style}>
      <button className="db-calc-btn" onClick={() => setOpen((o) => !o)} title={t('Calculate')}>
        {text || <span className="db-calc-hint">{t('Calculate')}</span>}
      </button>
      {open && (
        <AnchoredMenu className="menu db-calc-menu">
          {[CALC_NONE, ...calculationsFor(prop)].map((c) => {
            const active = (current ?? CALC_NONE) === c;
            return (
              <button key={c} onClick={() => pick(c)} className={active ? 'active' : undefined}>
                <span className="db-calc-check">{active && <Check size={14} />}</span>
                {calculationLabel(c as Parameters<typeof calculationLabel>[0])}
              </button>
            );
          })}
        </AnchoredMenu>
      )}
    </td>
  );
}
