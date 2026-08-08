import { useEffect, useReducer } from 'react';

// The full Material Design Icons set is ~7.4k path strings (2.6 MB raw). Putting
// that in the main bundle would dwarf the app, so mdiSet.ts is imported
// dynamically the first time an MDI icon actually needs to render or the picker
// opens its MDI tab. Components subscribe via useMdi() and re-render once the
// chunk lands — a page with MDI icons shows its fallback for a moment, then the
// real icon, and every later render is synchronous.

let MDI: Record<string, string> | null = null;
let loading: Promise<void> | null = null;
const subscribers = new Set<() => void>();

export function loadMdi(): Promise<void> {
  if (MDI) return Promise.resolve();
  if (!loading) {
    loading = import('./mdiSet')
      .then((m) => {
        MDI = m.MDI_SET;
        subscribers.forEach((f) => f());
      })
      .catch(() => {
        loading = null; // allow a retry (e.g. after a flaky network)
      });
  }
  return loading;
}

export function mdiPath(name: string): string | undefined {
  return MDI?.[name];
}

export function mdiNames(): string[] {
  return MDI ? Object.keys(MDI) : [];
}

/** Subscribe to the MDI set, triggering the load only when `want` is true. */
export function useMdi(want: boolean): boolean {
  const [, force] = useReducer((x: number) => x + 1, 0);
  useEffect(() => {
    if (!want) return;
    subscribers.add(force);
    void loadMdi();
    return () => {
      subscribers.delete(force);
    };
  }, [want]);
  return MDI !== null;
}
