import { useEffect, useState } from 'react';
import { t } from '../i18n';

interface Toast {
  id: number;
  message: string;
}

let nextId = 1;

export default function Toaster() {
  const [toasts, setToasts] = useState<Toast[]>([]);

  useEffect(() => {
    const onToast = (e: Event) => {
      const message = (e as CustomEvent<string>).detail;
      const id = nextId++;
      setToasts((t) => [...t, { id, message }]);
      window.setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000);
    };
    window.addEventListener('salt:toast', onToast);
    return () => window.removeEventListener('salt:toast', onToast);
  }, []);

  // Toasts are the app's only failure feedback, so the region must announce to
  // screen readers. assertive: a failed save is worth interrupting for.
  if (toasts.length === 0) return null;
  return (
    <div className="toaster" role="region" aria-label={t('Notifications')} aria-live="assertive" aria-atomic="false">
      {toasts.map((t) => (
        <div key={t.id} className="toast">
          ⚠ {t.message}
        </div>
      ))}
    </div>
  );
}
