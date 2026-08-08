import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { User } from '../types';
import Logo from '../Logo';
import { t } from '../i18n';

// Rendered when the URL is /invite/<token>.
//  - Signed-out visitor → a small sign-up/sign-in form that joins the workspace.
//    If the email already belongs to an account, the password (and, when set, a
//    2FA code) is required — the server authenticates before joining, so a
//    leaked link can never take over an existing account.
//  - Signed-in visitor → a one-click "join as <me>" prompt; the session proves
//    identity, so no credentials are re-collected.
export default function InviteAccept({
  token,
  currentUser,
  onSuccess,
}: {
  token: string;
  currentUser?: User | null;
  onSuccess: (user: User) => void;
}) {
  const [info, setInfo] = useState<{ email: string; workspace: string } | null>(null);
  const [invalid, setInvalid] = useState(false);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [needCode, setNeedCode] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void api
      .inviteInfo(token)
      .then((i) => {
        setInfo(i);
        if (i.email) setEmail(i.email);
      })
      .catch(() => setInvalid(true));
  }, [token]);

  const finish = (user: User) => {
    history.replaceState(null, '', '/'); // drop the invite path so a reload lands in the app
    onSuccess(user);
  };

  // Signed-in: join the workspace as the current account (no credentials).
  const joinAsCurrent = async () => {
    setBusy(true);
    setError(null);
    try {
      finish(await api.acceptInvite(token, '', '', ''));
    } catch (err) {
      setError((err as Error).message || t('Joining failed'));
    } finally {
      setBusy(false);
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      finish(await api.acceptInvite(token, name, email, password, code));
    } catch (err) {
      // As with signing in: go by the reason in the answer, not the message text.
      const e = err as ApiError;
      if (e.code === '2fa_required') setNeedCode(true);
      setError(e.message || t('Joining failed'));
    } finally {
      setBusy(false);
    }
  };

  if (invalid) {
    return (
      <div className="login-wrap">
        <div className="login-card ring">
          <div className="login-logo"><Logo size={56} /></div>
          <h1>{t('Invitation not valid')}</h1>
          <p>{t('This invitation link is not valid or has expired.')}</p>
          <a className="btn" href="/">{t('To sign-in')}</a>
        </div>
      </div>
    );
  }

  // Signed-in join view.
  if (currentUser) {
    const mismatch =
      !!info?.email && info.email.toLowerCase() !== currentUser.email.toLowerCase();
    return (
      <div className="login-wrap">
        <div className="login-card ring">
          <div className="login-logo"><Logo size={56} /></div>
          <h1>{t('Join the workspace')}</h1>
          <p>
            {info?.workspace
              ? `Du wurdest zum Workspace „${info.workspace}" eingeladen.`
              : 'Du wurdest eingeladen.'}
          </p>
          {mismatch ? (
            <>
              <p>
                {t('This invitation is for')} <strong>{info?.email}</strong>, du bist aber als{' '}
                <strong>{currentUser.email}</strong> angemeldet.
              </p>
              <a className="btn" href="/api/logout">Abmelden &amp; neu anmelden</a>
            </>
          ) : (
            <>
              <p>
                {t('Signed in as')} <strong>{currentUser.email}</strong>.
              </p>
              {error && <div className="login-error">{error}</div>}
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="btn primary" onClick={joinAsCurrent} disabled={busy}>
                  {t('Join')}
                </button>
                <a className="btn" href="/">{t('Cancel')}</a>
              </div>
            </>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="login-wrap">
      <form className="login-card ring" onSubmit={submit}>
        <div className="login-logo"><Logo size={56} /></div>
        <h1>{t('Join')}</h1>
        <p>
          {info?.workspace
            ? `Du wurdest zum Workspace „${info.workspace}" eingeladen.`
            : 'Du wurdest eingeladen.'}
        </p>
        <input
          autoFocus
          value={name}
          placeholder={t('Your name')}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          type="email"
          value={email}
          placeholder={t('Email')}
          readOnly={!!info?.email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <input
          type="password"
          value={password}
          placeholder={t('Password (min. 8 characters)')}
          onChange={(e) => setPassword(e.target.value)}
        />
        {needCode && (
          <input
            value={code}
            placeholder={t('2FA code')}
            inputMode="numeric"
            autoComplete="one-time-code"
            onChange={(e) => setCode(e.target.value)}
          />
        )}
        {error && <div className="login-error">{error}</div>}
        <button className="btn primary" type="submit" disabled={busy}>
          {t('Join')}
        </button>
      </form>
    </div>
  );
}
