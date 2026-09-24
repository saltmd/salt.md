import type { PropOption } from './types';

// Keep persisted selections (including multi-select arrays) and filter values
// valid: renaming changes the label, never the id or the colour.
export function renameSelectOption(
  options: PropOption[],
  id: string,
  draft: string,
): PropOption[] | null {
  const name = draft.trim();
  if (
    !name ||
    !options.some((option) => option.id === id) ||
    options.some((option) => option.id !== id && option.name.toLowerCase() === name.toLowerCase())
  ) return null;
  return options.map((option) => option.id === id ? { ...option, name } : option);
}

// Reorder definitions without changing the ids stored in rows or filters.
export function moveSelectOption(options: PropOption[], id: string, direction: -1 | 1): PropOption[] {
  const index = options.findIndex((option) => option.id === id);
  const target = index + direction;
  if (index < 0 || target < 0 || target >= options.length) return options;
  const next = [...options];
  [next[index], next[target]] = [next[target], next[index]];
  return next;
}

// Insert before/after a target, even when the source is several positions away.
export function reorderSelectOption(options: PropOption[], id: string, targetId: string, after: boolean): PropOption[] {
  const source = options.find((option) => option.id === id);
  if (!source || id === targetId || !options.some((option) => option.id === targetId)) return options;
  const next = options.filter((option) => option.id !== id);
  const target = next.findIndex((option) => option.id === targetId);
  next.splice(target + (after ? 1 : 0), 0, source);
  return next;
}
