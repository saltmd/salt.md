// Online/offline signal for the UI. navigator.onLine reports the machine's
// network, not the server's health — a dead server still looks online. That
// narrower case stays with the collab watchdog and the API error paths; this
// hook covers the blunt case that makes the whole window useless.
import { useEffect, useState } from 'react';

export function useOnline(): boolean {
  const [online, setOnline] = useState(() =>
    typeof navigator === 'undefined' ? true : navigator.onLine !== false,
  );
  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    window.addEventListener('online', on);
    window.addEventListener('offline', off);
    return () => {
      window.removeEventListener('online', on);
      window.removeEventListener('offline', off);
    };
  }, []);
  return online;
}
