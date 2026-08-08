// Which children a sidebar section shows — the one rule the tree turns on.
//
// Its own module, with no imports, because this exact decision has now been got
// wrong twice in opposite directions and neither time did anything fail. A tree
// that quietly omits a page looks like a tree; you only find out when somebody
// goes looking for something they know is there.
//
//   Round one: a database filed under a document appeared nowhere, because
//   Documents dropped it and Collections only listed roots.
//   Round two (this file): the split-mode filter kept running in MIXED mode,
//   where the one section is the only section — so a database under a document
//   vanished again, while the page count went on including it.
//
// The rule in one sentence: **hide a child database only when there is another
// section that shows it.**

export type TreeSection = 'docs' | 'dbs';

export interface TreeChild {
  type?: string;
}

/** Whether the Collections section exists at all. In mixed mode it does not —
 *  one tree means one tree — which is precisely why the Documents section may
 *  not filter anything out there. */
export function hasSeparateDbSection(mixed: boolean): boolean {
  return !mixed;
}

/** The children a section renders, given the workspace's tree mode.
 *
 *  Split mode: Documents shows documents, Collections shows databases. A child
 *  database is hidden under its document because it has a home of its own.
 *
 *  Mixed mode: one tree, everything filed where you put it. Nothing may be
 *  hidden here, because nothing else would show it. */
export function childrenForSection<T extends TreeChild>(
  children: T[],
  section: TreeSection,
  mixed: boolean,
): T[] {
  if (section !== 'docs' || !hasSeparateDbSection(mixed)) return children;
  return children.filter((c) => c.type !== 'collection');
}

/** Top-level entries of the Documents/Pages section. Same rule, one level up:
 *  a top-level database is hidden only when Collections is there to show it. */
export function topLevelForDocs<T extends TreeChild>(topLevel: T[], mixed: boolean): T[] {
  return childrenForSection(topLevel, 'docs', mixed);
}
