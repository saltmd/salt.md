import { useEffect, useRef, useState } from 'react';
import Portal from './components/Portal';
import { t } from './i18n';

// In-app confirm / prompt dialogs (replacing native window.confirm/prompt,
// which look out of place and can't be styled). Promise-based: call
// `await confirm(msg)` or `await promptText(msg)`; a single <DialogHost/>
// mounted at the app root renders the actual dialog and resolves the promise.

type ConfirmReq = {
  kind: 'confirm';
  message: string;
  confirmText?: string;
  danger?: boolean;
  resolve: (v: boolean) => void;
};
type PromptReq = {
  kind: 'prompt';
  message: string;
  placeholder?: string;
  defaultValue?: string;
  confirmText?: string;
  resolve: (v: string | null) => void;
};
type Req = ConfirmReq | PromptReq;

export function confirm(
  message: string,
  opts: { confirmText?: string; danger?: boolean } = {},
): Promise<boolean> {
  return new Promise((resolve) => {
    window.dispatchEvent(
      new CustomEvent('salt:dialog', { detail: { kind: 'confirm', message, ...opts, resolve } }),
    );
  });
}

export function promptText(
  message: string,
  opts: { placeholder?: string; defaultValue?: string; confirmText?: string } = {},
): Promise<string | null> {
  return new Promise((resolve) => {
    window.dispatchEvent(
      new CustomEvent('salt:dialog', { detail: { kind: 'prompt', message, ...opts, resolve } }),
    );
  });
}

export function DialogHost() {
  const [req, setReq] = useState<Req | null>(null);
  const [value, setValue] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onReq = (e: Event) => {
      const detail = (e as CustomEvent<Req>).detail;
      setReq(detail);
      setValue(detail.kind === 'prompt' ? detail.defaultValue ?? '' : '');
    };
    window.addEventListener('salt:dialog', onReq);
    return () => window.removeEventListener('salt:dialog', onReq);
  }, []);

  useEffect(() => {
    if (req?.kind === 'prompt') setTimeout(() => inputRef.current?.focus(), 0);
    if (!req) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') cancel();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [req]);

  if (!req) return null;

  const cancel = () => {
    if (req.kind === 'confirm') req.resolve(false);
    else req.resolve(null);
    setReq(null);
  };
  const ok = () => {
    if (req.kind === 'confirm') req.resolve(true);
    else req.resolve(value.trim() ? value.trim() : null);
    setReq(null);
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) cancel(); }}>
        <div className="dialog confirm-dialog" role="dialog" aria-modal="true">
          <p className="confirm-message">{req.message}</p>
          {req.kind === 'prompt' && (
            <input
              ref={inputRef}
              className="prop-input confirm-input"
              placeholder={req.placeholder}
              value={value}
              onChange={(e) => setValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') ok();
              }}
            />
          )}
          <div className="dialog-buttons">
            <button className="btn" onClick={cancel}>{t('Cancel')}</button>
            <button className={'btn ' + (req.kind === 'confirm' && req.danger ? 'danger' : 'primary')} onClick={ok}>
              {req.confirmText ?? (req.kind === 'prompt' ? t('OK') : t('Confirm'))}
            </button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
