import { createContext, useContext } from 'react';
import type { PageMeta } from './types';

// Custom blocks render INSIDE the editor but have no access to its props. The
// database block needs exactly that: the page list (to pick a database and show
// its title), the tag colours and navigation. A context is the clean way to do
// it — module state would be coupled invisibly and would collide with two
// documents open at once.

export interface BlockCtx {
  pagesById: Map<string, PageMeta>;
  tagColors: Record<string, string>;
  onNavigate: (id: string | null) => void;
  onPagesChanged: () => void;
}

const empty: BlockCtx = {
  pagesById: new Map(),
  tagColors: {},
  onNavigate: () => {},
  onPagesChanged: () => {},
};

export const BlockContext = createContext<BlockCtx>(empty);
export const useBlockCtx = () => useContext(BlockContext);
