import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { User } from '../types';
import Logo from '../Logo';
import { t } from '../i18n';
import { serverMessage } from '../serverErrors';


// Where to go after signing in, when somebody arrived with a destination.
//
// Only a same-origin PATH is accepted — never an absolute URL, never a
// protocol-relative one. This value is followed straight after a successful
// sign-in, which is precisely the shape an open redirect is built from.
//
// A hard navigation rather than a route change: the destination is a
// server-rendered page (the desktop app's approval screen), not somewhere the
// single-page app knows how to go.
/** The same destination, as a query string to hang on the provider links. */
function oauthNextParam(): string {
  const next = nextDestination();
  return next ? '?next=' + encodeURIComponent(next) : '';
}

function nextDestination(): string {
  const raw = new URLSearchParams(window.location.search).get('next') ?? '';
  if (!raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\')) return '';
  return raw;
}

export default function Login({ onSuccess }: { onSuccess: (user: User) => void }) {
  const [mode, setMode] = useState<'login' | 'signup'>('login');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [needCode, setNeedCode] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [signupOpen, setSignupOpen] = useState(false);
  const [instanceName, setInstanceName] = useState('');
  const [oauth, setOauth] = useState<{ google: boolean; microsoft: boolean }>({ google: false, microsoft: false });

  useEffect(() => {
    void api
      .signupPolicy()
      .then((p) => {
        setSignupOpen(p.mode === 'open' || p.mode === 'domain');
        setInstanceName(p.instanceName || '');
        if (p.instanceName) document.title = p.instanceName;
        setOauth({ google: p.oauthGoogle, microsoft: p.oauthMicrosoft });
      })
      .catch(() => {});
    // Surface an error handed back by an aborted OAuth redirect. The server
    // sends a code plus its English sentence; the code is what gets
    // translated, the sentence is the fallback. `detail` is the provider's own
    // wording and stays as it came.
    const qs = new URLSearchParams(window.location.search);
    const code = qs.get('oauthError');
    if (code) {
      const text = qs.get('oauthErrorText') ?? code;
      const detail = qs.get('oauthErrorDetail');
      const msg = serverMessage(code, text);
      setError(detail ? `${msg} (${detail})` : msg);
      ['oauthError', 'oauthErrorText', 'oauthErrorDetail'].forEach((k) => qs.delete(k));
      const rest = qs.toString();
      window.history.replaceState({}, '', window.location.pathname + (rest ? '?' + rest : ''));
    }
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (mode === 'signup') {
        onSuccess(await api.signup(name, email, password));
        return;
      }
      const user = await api.login(email, password, needCode ? code : undefined);
      const next = nextDestination();
      if (next) {
        // Leaving the app entirely — do not hand control to onSuccess, which
        // would mount the workspace behind a page that is already navigating.
        window.location.assign(next);
        return;
      }
      onSuccess(user);
    } catch (err) {
      // Branch on the reason in the response, not on the message text: the
      // server sends `code`, and the message may be reworded or translated at
      // any time. This used to compare against the literal string "2fa
      // required" — a rewrite would have locked out every account with
      // two-factor sign-in.
      const e = err as ApiError;
      if (e.code === '2fa_required') setNeedCode(true);
      setError(e.message || t('Cannot reach the server'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login-wrap">
      <form className="login-card ring" onSubmit={submit}>
        <div className="login-logo"><Logo size={56} /></div>
        {/* With no instance name of its own this is the wordmark — small, mono,
            tightly tracked, as on the website and in the banner. When the
            instance carries its own name that is the company's mark, and it
            stays exactly as they wrote it. */}
        <h1 className={instanceName ? undefined : 'wordmark'}>{instanceName || 'salt.md'}</h1>
        <p>{mode === 'signup' ? t('Create an account.') : t('Sign in to your workspace.')}</p>
        {mode === 'signup' && (
          <input
            autoFocus
            value={name}
            placeholder={t('Name')}
            onChange={(e) => {
              setName(e.target.value);
              setError(null);
            }}
          />
        )}
        <input
          type="email"
          autoFocus={mode === 'login'}
          value={email}
          placeholder={t('Email')}
          onChange={(e) => {
            setEmail(e.target.value);
            setError(null);
          }}
        />
        <input
          type="password"
          value={password}
          placeholder={mode === 'signup' ? t('Password (min. 8 characters)') : t('Password')}
          onChange={(e) => {
            setPassword(e.target.value);
            setError(null);
          }}
        />
        {needCode && mode === 'login' && (
          <input
            inputMode="numeric"
            autoFocus
            value={code}
            placeholder={t('2FA code (6 digits)')}
            onChange={(e) => {
              setCode(e.target.value);
              setError(null);
            }}
          />
        )}
        {error && <div className="login-error">{error}</div>}
        <button className="btn primary" type="submit" disabled={busy}>
          {mode === 'signup' ? t('Create account') : t('Sign in')}
        </button>
        {(oauth.google || oauth.microsoft) && (
          <>
            <div className="login-divider"><span>{t('or')}</span></div>
            {oauth.google && (
              <a className="btn oauth-btn" href={'/api/oauth/google/start' + oauthNextParam()}>
                <svg width="17" height="17" viewBox="0 0 24 24" aria-hidden="true">
                  <path fill="#4285F4" d="M23.5 12.27c0-.85-.08-1.66-.22-2.45H12v4.64h6.45a5.52 5.52 0 0 1-2.39 3.62v3h3.86c2.26-2.09 3.58-5.17 3.58-8.81z" />
                  <path fill="#34A853" d="M12 24c3.24 0 5.96-1.07 7.94-2.91l-3.86-3c-1.07.72-2.44 1.14-4.08 1.14-3.13 0-5.78-2.11-6.73-4.96H1.29v3.1A12 12 0 0 0 12 24z" />
                  <path fill="#FBBC05" d="M5.27 14.27a7.2 7.2 0 0 1 0-4.54v-3.1H1.29a12 12 0 0 0 0 10.74l3.98-3.1z" />
                  <path fill="#EA4335" d="M12 4.77c1.76 0 3.35.61 4.6 1.8l3.42-3.42A11.97 11.97 0 0 0 12 0 12 12 0 0 0 1.29 6.63l3.98 3.1C6.22 6.88 8.87 4.77 12 4.77z" />
                </svg>
                {t('Sign in with Google')}
              </a>
            )}
            {oauth.microsoft && (
              <a className="btn oauth-btn" href={'/api/oauth/microsoft/start' + oauthNextParam()}>
                <svg width="17" height="17" viewBox="0 0 23 23" aria-hidden="true">
                  <rect x="1" y="1" width="10" height="10" fill="#F35325" />
                  <rect x="12" y="1" width="10" height="10" fill="#81BC06" />
                  <rect x="1" y="12" width="10" height="10" fill="#05A6F0" />
                  <rect x="12" y="12" width="10" height="10" fill="#FFBA08" />
                </svg>
                {t('Sign in with Microsoft')}
              </a>
            )}
          </>
        )}
        {signupOpen && (
          <button
            type="button"
            className="login-switch"
            onClick={() => {
              setMode((m) => (m === 'login' ? 'signup' : 'login'));
              setError(null);
              setNeedCode(false);
            }}
          >
            {mode === 'login' ? t('New here? Create an account') : t('Back to sign in')}
          </button>
        )}
      </form>
    </div>
  );
}
