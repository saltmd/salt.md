import { useEffect, useState } from 'react';

// Who else is on this page.
//
// The information arises in CollabEditor (Yjs awareness) and is shown one level
// above it in the header. Rather than passing it through several components it
// lives in this small store — the same role blockContext plays for the database
// block, only without a context, because nobody in between needs to know about
// it.
//
// Until W90 presence was sent but displayed NOWHERE: two people could type in
// the same document without noticing each other.

export interface Peer {
  name: string;
  color: string;
  avatar?: string;
}

const peersByPage = new Map<string, Peer[]>();
const listeners = new Set<() => void>();

export function setPeers(pageId: string, peers: Peer[]) {
  const prev = peersByPage.get(pageId);
  // Awareness fires on every cursor movement. Without this comparison the
  // header would repaint on every keystroke of everybody else.
  if (
    prev &&
    prev.length === peers.length &&
    prev.every((p, i) => p.name === peers[i].name && p.avatar === peers[i].avatar)
  ) {
    return;
  }
  peersByPage.set(pageId, peers);
  listeners.forEach((fn) => fn());
}

export function clearPeers(pageId: string) {
  if (peersByPage.delete(pageId)) listeners.forEach((fn) => fn());
}

export function usePeers(pageId: string): Peer[] {
  const [, bump] = useState(0);
  useEffect(() => {
    const fn = () => bump((n) => n + 1);
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  }, []);
  return peersByPage.get(pageId) ?? [];
}
