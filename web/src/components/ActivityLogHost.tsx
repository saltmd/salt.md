import { useEffect, useState } from 'react';
import { ActivityModal } from './UserMenu';

/** Opens the activity log for one page, from anywhere.
 *
 *  A cell in a table has no way to reach the user menu's state, and threading a
 *  callback from the sidebar down through the collection view to a property
 *  renderer would touch a dozen components that have no business knowing about
 *  a dialog. So it goes the way confirm() and promptText() already do here: an
 *  event on the window, one host listening.
 */
export function showActivityFor(pageId: string, pageTitle?: string) {
  window.dispatchEvent(new CustomEvent('salt:activity', { detail: { pageId, pageTitle } }));
}

export default function ActivityLogHost() {
  const [req, setReq] = useState<{ pageId: string; pageTitle?: string } | null>(null);
  useEffect(() => {
    const onShow = (e: Event) => setReq((e as CustomEvent).detail);
    window.addEventListener('salt:activity', onShow);
    return () => window.removeEventListener('salt:activity', onShow);
  }, []);
  if (!req) return null;
  return <ActivityModal pageId={req.pageId} pageTitle={req.pageTitle} onClose={() => setReq(null)} />;
}
