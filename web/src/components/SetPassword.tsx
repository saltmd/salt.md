import { useEffect, useState } from 'react';
import { api } from '../api';
import Logo from '../Logo';
import { t } from '../i18n';

/** The path an emailed password link opens; the server builds the same one. */
export const SET_PASSWORD_PATH = /^\/set-password\/([a-f0-9]+)$/;

/** Query parameter that tells the sign-in screen a password was just set. */
export const PASSWORD_SET_PARAM = 'passwordSet';

// Rendered for /set-password/<token>: choose a password through an invitation
// or reset link, then sign in with it.
export default function SetPassword({ token }: { token: string }) {
  const [info, setInfo] = useState<{ email: string; name: string; purpose: 'invite' | 'reset' } | null>(null);
  const [invalid, setInvalid] = useState(false);
  const [password, setPassword] = useState('');
  const [confirmPw, setConfirmPw] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void api
      .passwordLinkInfo(token)
      .then(setInfo)
      .catch(() => setInvalid(true));
  }, [token]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password !== confirmPw) {
      setError(t('The two new passwords do not match'));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const res = await api.usePasswordLink(token, password);
      const qs = new URLSearchParams({ [PASSWORD_SET_PARAM]: res.email });
      window.location.assign('/?' + qs.toString());
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  if (invalid) {
    return (
      <div className="login-wrap">
        <div className="login-card ring">
          <div className="login-logo"><Logo size={56} /></div>
          <h1>{t('Link not valid')}</h1>
          <p>{t('This link is not valid any more. It may have been used already or expired.')}</p>
          <a className="btn" href="/">{t('To sign-in')}</a>
        </div>
      </div>
    );
  }

  return (
    <div className="login-wrap">
      <form className="login-card ring" onSubmit={submit}>
        <div className="login-logo"><Logo size={56} /></div>
        <h1>{info?.purpose === 'invite' ? t('Choose your password') : t('Choose a new password')}</h1>
        {info && (
          <p>
            {info.purpose === 'invite'
              ? t('Welcome, {name}. You sign in with {email}.', { name: info.name, email: info.email })
              : t('For the account {email}.', { email: info.email })}
          </p>
        )}
        <input
          autoFocus
          type="password"
          autoComplete="new-password"
          value={password}
          placeholder={t('Password (min. 8 characters)')}
          onChange={(e) => {
            setPassword(e.target.value);
            setError(null);
          }}
        />
        <input
          type="password"
          autoComplete="new-password"
          value={confirmPw}
          placeholder={t('Confirm new password')}
          onChange={(e) => {
            setConfirmPw(e.target.value);
            setError(null);
          }}
        />
        {error && <div className="login-error">{error}</div>}
        <button className="btn primary" type="submit" disabled={busy || !info}>
          {t('Save password')}
        </button>
      </form>
    </div>
  );
}
