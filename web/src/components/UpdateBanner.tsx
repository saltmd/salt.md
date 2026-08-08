import React, { useEffect, useState } from 'react';
import { api } from '../api';
import { t } from '../i18n';
import type { UpdateInfo } from '../types';
import { ArrowUpCircle, X } from 'lucide-react';

/** "A newer release exists", for admins only.
 *
 *  Two things about it are deliberate.
 *
 *  It asks the server and renders nothing when the answer is 403. That is the
 *  whole permission check on this side: the endpoint is adminOnly, so a member's
 *  browser gets a refusal and shows nothing, and there is no isAdmin flag to
 *  thread through the tree and later get wrong.
 *
 *  It is NOT the "a new version is available, reload the page" toast. That one
 *  fires when the tab is older than the server it is talking to, and reloading
 *  fixes it. This one says the machine is running an old build, and reloading
 *  does nothing at all — somebody has to install it.
 */
export default function UpdateBanner() {
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    let alive = true;
    void api
      .update()
      .then((u) => alive && setInfo(u))
      // 403 for a member, 404 on an older server, offline: all the same answer.
      .catch(() => alive && setInfo(null));
    return () => {
      alive = false;
    };
  }, []);

  if (!info?.available || !info.latest) return null;

  // Dismissal is per version, so the next release speaks up again rather than
  // being swallowed by a click somebody made months earlier.
  const key = `salt-update-seen-${info.latest}`;
  let seen = false;
  try {
    seen = localStorage.getItem(key) === '1';
  } catch {
    /* private mode, an over-eager extension: not a reason to hide the banner */
  }
  if (seen || dismissed) return null;

  const hide = () => {
    try {
      localStorage.setItem(key, '1');
    } catch {
      /* the state above still hides it for this session */
    }
    setDismissed(true);
  };

  return (
    <div className="update-banner" role="status">
      <ArrowUpCircle size={14} />
      <span className="update-banner__text">
        {t('salt.md {version} is out').replace('{version}', info.latest)}
      </span>
      <a
        className="update-banner__link"
        href={info.url}
        target="_blank"
        rel="noopener noreferrer"
      >
        {t("What's new")}
      </a>
      <button className="update-banner__x" onClick={hide} title={t('Dismiss')} aria-label={t('Dismiss')}>
        <X size={13} />
      </button>
    </div>
  );
}
