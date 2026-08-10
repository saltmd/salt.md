import { useEffect, useState } from 'react';
import { api } from '../api';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { toast } from '../toast';
import { formatBytes, formatMoment } from '../format';
import { plural, t } from '../i18n';
import type { Webhook } from '../types';

type Info = Awaited<ReturnType<typeof api.adminInfo>>;

const fmtBytes = (n: number) => formatBytes(n);

const fmtUptime = (sec: number) => {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`;
};

// A read-only config snippet with a copy button.
function ConfBlock({ title, text }: { title: string; text: string }) {
  return (
    <div className="conf-block">
      <div className="conf-head">
        <span>{title}</span>
        <button
          className="btn-sm"
          onClick={() => {
            void navigator.clipboard?.writeText(text);
            toast(t('Copied'));
          }}
        >
          {t('Copy')}
        </button>
      </div>
      <pre>
        <code>{text}</code>
      </pre>
    </div>
  );
}

// Instance-wide settings (admin only), grouped in tabs: general limits,
// registration, SMTP, reverse-proxy setup (Caddy / Cloudflare / nginx with
// generated configs) and maintenance (backup, instance info).
// The periods offered in the dropdown. Anything else lands in the number field
// beside it, so an admin who needs 45 days is not told to pick 30 or 60.
const AUDIT_PRESETS = ['0', '30', '60', '180', '365'];

export function AdminSettingsModal({ onClose }: { onClose: () => void }) {
  useExclusiveModal(onClose);
  const [s, setS] = useState<Record<string, string>>({});
  const [trustProxy, setTrustProxy] = useState(false);
  // How documents from this instance print. Held as one object because the five
  // travel together everywhere: loaded together, saved together, and the print
  // view reads them as a set.
  const [pdf, setPdf] = useState({
    cover: false, icon: true, footer: true, workspace: true,
    pageNums: true, comments: false, links: true, landscape: false,
  });
  const [allowUserWs, setAllowUserWs] = useState(true);
  const [passSet, setPassSet] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [tab, setTab] = useState<'general' | 'access' | 'email' | 'proxy' | 'webhooks' | 'documents' | 'maintenance'>('general');
  const [hooks, setHooks] = useState<Webhook[] | null>(null);
  const [hookURL, setHookURL] = useState('');
  const [hookEvents, setHookEvents] = useState<string[]>(['page.created', 'page.updated']);
  const [hookBusy, setHookBusy] = useState(false);
  const [hookErr, setHookErr] = useState('');
  // Shown once, right after creating: the receiver needs it and we cannot show
  // it again — the same bargain as an API token.
  const [freshSecret, setFreshSecret] = useState('');
  const [info, setInfo] = useState<Info | null>(null);
  const [upstream, setUpstream] = useState(window.location.host || '127.0.0.1:80');
  const [httpsEnabled, setHttpsEnabled] = useState(false);
  const [pa, setPa] = useState<Awaited<ReturnType<typeof api.publicAccess>> | null>(null);
  const [tunnelToken, setTunnelToken] = useState('');
  const [tunnelBusy, setTunnelBusy] = useState(false);
  const [oauthSet, setOauthSet] = useState({ google: false, ms: false });
  const [mail, setMail] = useState({ provider: '', address: '' });
  const [mailBusy, setMailBusy] = useState(false);

  useEffect(() => {
    void api
      .getSettings()
      .then((v) => {
        setS({
          instanceName: v.instanceName,
          signupMode: v.signupMode,
          allowedDomains: v.allowedDomains,
          smtpHost: v.smtpHost,
          smtpPort: v.smtpPort,
          smtpUser: v.smtpUser,
          smtpFrom: v.smtpFrom,
          publicBaseUrl: v.publicBaseUrl,
          smtpPass: '',
          maxUploadMb: String(v.maxUploadMb),
          trashDays: String(v.trashDays),
          auditDays: String(v.auditDays ?? 0),
          sessionDays: String(v.sessionDays),
          httpsDomain: v.httpsDomain,
          mailFrom: v.mailFrom,
          googleClientId: v.googleClientId,
          googleClientSecret: '',
          msClientId: v.msClientId,
          msClientSecret: '',
        });
        setTrustProxy(v.trustProxy);
        setPdf({
          cover: v.pdfCover, icon: v.pdfIcon, footer: v.pdfFooter,
          workspace: v.pdfWorkspace, pageNums: v.pdfPageNums,
          comments: v.pdfComments, links: v.pdfLinks, landscape: v.pdfLandscape,
        });
        setAllowUserWs(v.allowUserWorkspaces !== false);
        setHttpsEnabled(v.httpsEnabled);
        setOauthSet({ google: v.googleSecretSet, ms: v.msSecretSet });
        setMail({ provider: v.mailProvider, address: v.mailAddress });
        setPassSet(v.smtpPassSet);
        setLoaded(true);
      })
      .catch((e) => setLoadErr((e as Error).message || t('Loading failed')));
  }, []);

  // Instance info lazily when the maintenance tab opens.
  useEffect(() => {
    if (tab === 'maintenance' && !info) void api.adminInfo().then(setInfo).catch(() => {});
    if (tab === 'webhooks' && hooks === null) void api.webhooks().then(setHooks).catch(() => setHooks([]));
  }, [tab, info, hooks]);

  // Live tunnel status while the proxy or access tab is open (the OAuth cards
  // derive the public redirect URI from a running tunnel).
  useEffect(() => {
    if (tab !== 'proxy' && tab !== 'access') return;
    let alive = true;
    const load = () => void api.publicAccess().then((v) => alive && setPa(v)).catch(() => {});
    load();
    const iv = window.setInterval(load, 2500);
    return () => {
      alive = false;
      window.clearInterval(iv);
    };
  }, [tab]);

  const [pruning, setPruning] = useState(false);
  const [pruned, setPruned] = useState<number | null>(null);

  // Applying the period NOW rather than at the next nightly sweep. Without it
  // the setting looks like it did nothing: an admin shortens the period, the
  // log stays as long as it was, and there is no way to tell whether it worked
  // without coming back tomorrow.
  const prune = async () => {
    setPruning(true);
    try {
      const r = await api.auditPrune();
      // Reported beside the button, not through toast(): that channel is the
      // app's failure feedback and hard-wires a warning sign, so a successful
      // clean-up came up in red with a ⚠ in front of it.
      setPruned(r.removed);
    } catch (e) {
      toast((e as Error).message || t('Clean-up failed'));
    } finally {
      setPruning(false);
    }
  };

  const tunnel = async (action: string, token?: string) => {
    setTunnelBusy(true);
    try {
      await api.tunnelAction(action, token);
      setPa(await api.publicAccess());
      if (action === 'start-token') setTunnelToken('');
    } catch (e) {
      toast((e as Error).message || t('Tunnel action failed'));
    } finally {
      setTunnelBusy(false);
    }
  };

  const set = (k: string, v: string) => setS((p) => ({ ...p, [k]: v }));
  const mailTest = async () => {
    setMailBusy(true);
    try {
      const r = await api.mailTest();
      toast(t('Test mail sent to {address} ✓', { address: r.to }));
    } catch (e) {
      toast((e as Error).message || t('Test failed'));
    } finally {
      setMailBusy(false);
    }
  };
  const mailDisconnect = async () => {
    try {
      await api.mailDisconnect();
      setMail({ provider: '', address: '' });
      toast(t('Mail connection disconnected'));
    } catch (e) {
      toast((e as Error).message || t('Disconnecting failed'));
    }
  };
  const save = async () => {
    const num = (k: string, min: number, max: number) => {
      const n = parseInt(s[k], 10);
      return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : undefined;
    };
    try {
      await api.putSettings({
        instanceName: s.instanceName,
        signupMode: s.signupMode,
        allowedDomains: s.allowedDomains,
        smtpHost: s.smtpHost,
        smtpPort: s.smtpPort,
        smtpUser: s.smtpUser,
        smtpFrom: s.smtpFrom,
        smtpPass: s.smtpPass,
        publicBaseUrl: s.publicBaseUrl,
        trustProxy,
        pdfCover: pdf.cover,
        pdfIcon: pdf.icon,
        pdfFooter: pdf.footer,
        pdfWorkspace: pdf.workspace,
        pdfPageNums: pdf.pageNums,
        pdfComments: pdf.comments,
        pdfLinks: pdf.links,
        pdfLandscape: pdf.landscape,
        allowUserWorkspaces: allowUserWs,
        maxUploadMb: num('maxUploadMb', 1, 2048),
        trashDays: num('trashDays', 0, 3650),
        auditDays: num('auditDays', 0, 3650),
        sessionDays: num('sessionDays', 1, 365),
        httpsDomain: s.httpsDomain,
        httpsEnabled,
        mailFrom: s.mailFrom,
        googleClientId: s.googleClientId,
        googleClientSecret: s.googleClientSecret,
        msClientId: s.msClientId,
        msClientSecret: s.msClientSecret,
      });
      toast(t('Settings saved'));
      onClose();
    } catch (e) {
      toast((e as Error).message || t('Saving failed'));
    }
  };

  // Domain for the generated proxy configs, derived from the public base URL.
  const domain = (() => {
    try {
      return new URL(s.publicBaseUrl || '').host || 'salt.example.com';
    } catch {
      return 'salt.example.com';
    }
  })();

  const caddyConf = `${domain} {
	reverse_proxy ${upstream}
}`;

  // The comments inside the generated configs stay English regardless of the
  // interface language: this text is pasted into a server's config file, where
  // the next reader is as likely to be a colleague or a search engine as the
  // person who copied it.
  const cloudflaredConf = `# 1) Create the tunnel (once):
#    cloudflared tunnel login
#    cloudflared tunnel create salt
#    cloudflared tunnel route dns salt ${domain}
# 2) ~/.cloudflared/config.yml:
tunnel: salt
credentials-file: /root/.cloudflared/<TUNNEL-ID>.json
ingress:
  - hostname: ${domain}
    service: http://${upstream}
  - service: http_status:404
# 3) Run it / install as a service:
#    cloudflared tunnel run salt
#    (or: cloudflared service install)`;

  const nginxConf = `server {
	listen 443 ssl http2;
	server_name ${domain};
	client_max_body_size ${s.maxUploadMb || '50'}m;

	location / {
		proxy_pass http://${upstream};
		proxy_set_header Host $host;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
		# WebSockets (live collaboration):
		proxy_http_version 1.1;
		proxy_set_header Upgrade $http_upgrade;
		proxy_set_header Connection "upgrade";
		proxy_read_timeout 3600s;
	}
}`;

  const TABS: { id: typeof tab; label: string }[] = [
    { id: 'general', label: t('General') },
    { id: 'access', label: t('Access') },
    { id: 'email', label: t('Email') },
    { id: 'proxy', label: t('Domain & proxy') },
    { id: 'webhooks', label: t('Webhooks') },
    { id: 'documents', label: t('Documents') },
    { id: 'maintenance', label: t('Maintenance') },
  ];

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog wide settings-dialog" role="dialog" aria-modal="true" aria-label={t('Instance settings')}>
          <h2>{t('Instance settings')}</h2>
          {loadErr ? (
            <div className="login-error">{loadErr}</div>
          ) : !loaded ? (
            <div className="dialog-hint">{t('Loading…')}</div>
          ) : (
            <>
              <div className="settings-tabs">
                {/* `tb`, not `t` — the tab object would shadow the translate
                    function and every label below it would break. */}
                {TABS.map((tb) => (
                  <button key={tb.id} className={'view-tab' + (tab === tb.id ? ' active' : '')} onClick={() => setTab(tb.id)}>
                    {tb.label}
                  </button>
                ))}
              </div>
              <div className="settings-grid settings-body">
                {tab === 'general' && (
                  <>
                    <label>{t('Instance name (sign-in page & title)')}</label>
                    <input className="prop-input" placeholder={t('e.g. Acme Notes')} value={s.instanceName} onChange={(e) => set('instanceName', e.target.value)} />
                    <label>{t('Public base URL (for links, mail, calendars)')}</label>
                    <input className="prop-input" placeholder="https://notes.example.com" value={s.publicBaseUrl} onChange={(e) => set('publicBaseUrl', e.target.value)} />
                    <label>{t('Max. file size per upload (MB)')}</label>
                    <input className="prop-input" type="number" min={1} max={2048} value={s.maxUploadMb} onChange={(e) => set('maxUploadMb', e.target.value)} />
                    <label>{t('Empty the trash automatically after (days, 0 = never)')}</label>
                    <input className="prop-input" type="number" min={0} max={3650} value={s.trashDays} onChange={(e) => set('trashDays', e.target.value)} />
                    <label>{t('Sign-in session length (days)')}</label>
                    <input className="prop-input" type="number" min={1} max={365} value={s.sessionDays} onChange={(e) => set('sessionDays', e.target.value)} />
                  </>
                )}

                {tab === 'access' && (
                  <>
                    <label>{t('Who may register?')}</label>
                    <select className="prop-select" value={s.signupMode} onChange={(e) => set('signupMode', e.target.value)}>
                      <option value="invite">{t('By invitation only')}</option>
                      <option value="domain">{t('Email domain allowed')}</option>
                      <option value="open">{t('Open (anyone)')}</option>
                    </select>
                    {s.signupMode === 'domain' && (
                      <>
                        <label>{t('Allowed domains (comma separated)')}</label>
                        <input className="prop-input" placeholder="salt.md, example.com" value={s.allowedDomains} onChange={(e) => set('allowedDomains', e.target.value)} /> {/* i18n-ok: example domains, not prose */}
                      </>
                    )}
                    <p className="dialog-hint settings-hint">
                      {t(
                        'Invitations go out through “Members” in the workspace menu. For sending mail, configure SMTP on the “Email” tab.',
                      )}
                    </p>

                    <label className="check-label" style={{ marginTop: 10 }}>
                      <input
                        type="checkbox"
                        checked={allowUserWs}
                        onChange={(e) => setAllowUserWs(e.target.checked)}
                      />
                      {t('Users may create their own workspaces')}
                    </label>
                    <p className="dialog-hint settings-hint">
                      {t(
                        'Off: only admins create workspaces. On (default): anyone can create one of their own and becomes its admin — they then manage the members of THEIR workspace only, not the instance.',
                      )}
                    </p>

                    <h3>{t('Sign in with Google / Microsoft (OAuth)')}</h3>
                    <p className="dialog-hint settings-hint">
                      {t(
                        'Once client ID and secret are stored, the sign-in page shows the button automatically. New accounts follow the registration policy above (under “by invitation only”, OAuth signs in existing accounts and nothing else). Redirect URIs for the provider console:',
                      )}
                    </p>
                    {(() => {
                      const quickUrl = pa && pa.status === 'running' && pa.mode === 'quick' ? pa.url : '';
                      const base = (s.publicBaseUrl || quickUrl || window.location.origin).replace(/\/$/, '');
                      const insecure = base.startsWith('http://') && !/^http:\/\/(localhost|127\.)/.test(base);
                      return (
                        <>
                          <label>Google</label>
                          <input className="prop-input" readOnly value={base + '/api/oauth/google/callback'} onFocus={(e) => e.currentTarget.select()} />
                          <label>Microsoft</label>
                          <input className="prop-input" readOnly value={base + '/api/oauth/microsoft/callback'} onFocus={(e) => e.currentTarget.select()} />
                          {insecure && (
                            <p className="dialog-hint settings-hint pa-warn">
                              {t(
                                '⚠ Google and Microsoft accept HTTPS redirect URIs only (localhost aside). Start a tunnel (the “Domain & proxy” tab) or enter a public HTTPS base URL under “General” — it then appears here on its own.',
                              )}
                            </p>
                          )}
                          {!s.publicBaseUrl && quickUrl && (
                            <p className="dialog-hint settings-hint pa-warn">
                              {t(
                                '⚠ This is the URL of the running quick tunnel — it changes on every start. For OAuth that lasts, use a named tunnel or your own domain and enter it as the base URL.',
                              )}
                            </p>
                          )}
                        </>
                      );
                    })()}
                    <div className="pa-card">
                      <strong>Google</strong>
                      <p className="dialog-hint settings-hint">
                        {t(
                          'console.cloud.google.com → APIs & Services → Credentials → “OAuth client ID” (Web application) → enter the redirect URI above.',
                        )}
                      </p>
                      <label>{t('Client ID')}</label>
                      <input className="prop-input" placeholder="…apps.googleusercontent.com" value={s.googleClientId} onChange={(e) => set('googleClientId', e.target.value)} />
                      <label>{t('Client secret')}</label>
                      <input className="prop-input" type="password" placeholder={oauthSet.google ? t('•••••• (stored)') : 'GOCSPX-…'} value={s.googleClientSecret} onChange={(e) => set('googleClientSecret', e.target.value)} />
                    </div>
                    <div className="pa-card">
                      <strong>Microsoft</strong>
                      <p className="dialog-hint settings-hint">
                        {t(
                          'portal.azure.com → App registrations → New (supported account types: “Any org + personal accounts”) → Redirect URI (Web): as above but with',
                        )}{' '}
                        <code>/api/oauth/microsoft/callback</code>{' '}
                        {t('→ Certificates & secrets → client secret.')}
                      </p>
                      <label>{t('Client ID (application ID)')}</label>
                      <input className="prop-input" placeholder="00000000-0000-…" value={s.msClientId} onChange={(e) => set('msClientId', e.target.value)} />
                      <label>{t('Client secret')}</label>
                      <input className="prop-input" type="password" placeholder={oauthSet.ms ? t('•••••• (stored)') : t('secret value')} value={s.msClientSecret} onChange={(e) => set('msClientSecret', e.target.value)} />
                    </div>
                  </>
                )}

                {tab === 'email' && (
                  <>
                    <h3>{t('Sending through Google / Microsoft — no SMTP')}</h3>
                    {mail.provider ? (
                      <>
                        <div className="pa-status pa-running">
                          <span className="pa-dot" />
                          {t('Connected: sends as')}{' '}
                          <strong>{s.mailFrom || mail.address || mail.provider}</strong>
                          {' '}({mail.provider === 'google' ? 'Gmail' : 'Microsoft'})
                          <button className="btn-sm" disabled={mailBusy} onClick={() => void mailTest()}>{t('Send test mail')}</button>
                          <button className="btn-sm" onClick={() => void mailDisconnect()}>{t('Disconnect')}</button>
                        </div>
                        <label>{t('Override the sender address (optional, alias)')}</label>
                        <input
                          className="prop-input"
                          placeholder={mail.address || 'noreply@example.com'}
                          value={s.mailFrom}
                          onChange={(e) => set('mailFrom', e.target.value)}
                        />
                        <p className="dialog-hint settings-hint">
                          {t('Only needed if mail should not be sent as')} <code>{mail.address}</code>.{' '}
                          {t(
                            'The address has to be an alias of the connected mailbox (Gmail: verify it under “Send mail as” in the Gmail settings; Microsoft: alias or send-as permission). Want a different mailbox entirely? “Disconnect”, then pick the account you want in the sign-in dialog when reconnecting.',
                          )}
                        </p>
                      </>
                    ) : (
                      <div className="pa-card">
                        <p className="dialog-hint settings-hint" style={{ margin: 0 }}>
                          {t(
                            'Uses the OAuth apps from the Access tab: connect once and invitations go out through the chosen mailbox — no SMTP needed. Store and save the client ID and secret there first, then connect here. In the sign-in window you may pick any account at all — including a dedicated sending mailbox such as noreply@example.com; it does not have to be the account you sign in with.',
                          )}
                        </p>
                        <div className="settings-row">
                          <a
                            className={'btn' + (oauthSet.google ? '' : ' btn-disabled')}
                            href={oauthSet.google ? '/api/admin/mail-oauth/google/start' : undefined}
                            onClick={(e) => { if (!oauthSet.google) { e.preventDefault(); toast(t('Set up Google OAuth on the Access tab first')); } }}
                          >
                            {t('Connect with Google')}
                          </a>
                          <a
                            className={'btn' + (oauthSet.ms ? '' : ' btn-disabled')}
                            href={oauthSet.ms ? '/api/admin/mail-oauth/microsoft/start' : undefined}
                            onClick={(e) => { if (!oauthSet.ms) { e.preventDefault(); toast(t('Set up Microsoft OAuth on the Access tab first')); } }}
                          >
                            {t('Connect with Microsoft')}
                          </a>
                        </div>
                        <p className="dialog-hint settings-hint" style={{ margin: 0 }}>
                          {t(
                            'Google: in the Cloud Console also enable the Gmail API (APIs & Services → Library) and move the OAuth app to “In production”, or the connection expires after 7 days.',
                          )}
                        </p>
                      </div>
                    )}

                    <h3>{t('Or the classic way: SMTP')}</h3>
                    <div className="settings-row" style={{ justifyContent: 'flex-start' }}>
                      <button className="btn-sm" disabled={mailBusy} onClick={() => void mailTest()}>{t('Send test mail')}</button>
                    </div>
                    <label>{t('Host')}</label>
                    <input className="prop-input" placeholder="smtp.example.com" value={s.smtpHost} onChange={(e) => set('smtpHost', e.target.value)} />
                    <label>{t('Port')}</label>
                    <input className="prop-input" placeholder="587 / 465" value={s.smtpPort} onChange={(e) => set('smtpPort', e.target.value)} />
                    <label>{t('User')}</label>
                    <input className="prop-input" value={s.smtpUser} onChange={(e) => set('smtpUser', e.target.value)} />
                    <label>{t('Password')}</label>
                    <input className="prop-input" type="password" placeholder={passSet ? t('•••••• (unchanged)') : t('not set')} value={s.smtpPass} onChange={(e) => set('smtpPass', e.target.value)} />
                    <label>{t('Sender (From)')}</label>
                    <input className="prop-input" placeholder="salt@example.com" value={s.smtpFrom} onChange={(e) => set('smtpFrom', e.target.value)} />
                  </>
                )}

                {tab === 'proxy' && (
                  <>
                    <h3>{t('Public access — built in, no proxy of your own')}</h3>
                    {pa && (pa.status === 'running' || pa.status === 'starting') && (
                      <div className={'pa-status pa-' + pa.status}>
                        <span className="pa-dot" />
                        {pa.status === 'starting' && t('Tunnel starting…')}
                        {pa.status === 'running' && pa.mode === 'quick' && (
                          <>
                            {t('Publicly reachable:')}&nbsp;
                            <a href={pa.url} target="_blank" rel="noreferrer">{pa.url}</a>
                            <button className="btn-sm" onClick={() => { void navigator.clipboard?.writeText(pa.url); toast(t('Link copied')); }}>{t('Copy')}</button>
                          </>
                        )}
                        {pa.status === 'running' && pa.mode === 'token' && (
                          <>{t('Tunnel connected — reachable under the hostname set in the Cloudflare dashboard.')}</>
                        )}
                        <button className="btn-sm" disabled={tunnelBusy} onClick={() => void tunnel('stop')}>{t('Stop')}</button>
                      </div>
                    )}
                    {pa && pa.status === 'error' && (
                      <div className="pa-status pa-error">
                        <span className="pa-dot" />
                        {pa.lastError || t('Tunnel error')}
                        <button className="btn-sm" disabled={tunnelBusy} onClick={() => void tunnel('stop')}>{t('Reset')}</button>
                      </div>
                    )}

                    <div className="pa-card">
                      <strong>{t('1 · Try it right away (quick tunnel)')}</strong>
                      <p className="dialog-hint settings-hint">
                        {t('One click, no account: creates a temporary')} <code>trycloudflare.com</code>{' '}
                        {t(
                          'URL pointing at this instance. The URL changes on every start — ideal for trying things out and sharing quickly.',
                        )}{' '}
                        {!pa?.cloudflaredHere && t('On first start salt.md downloads the official cloudflared automatically.')}
                      </p>
                      <div className="settings-row">
                        <button
                          className="btn primary"
                          disabled={tunnelBusy || pa?.status === 'running' || pa?.status === 'starting'}
                          onClick={() => void tunnel('start-quick')}
                        >
                          {tunnelBusy ? t('Please wait…') : t('Start quick tunnel')}
                        </button>
                      </div>
                    </div>

                    <div className="pa-card">
                      <strong>{t('2 · Permanently, with your own domain (Cloudflare Tunnel)')}</strong>
                      <p className="dialog-hint settings-hint">
                        {t('A free Cloudflare account is required: dashboard →')}{' '}
                        <em>Zero Trust → Networks → Tunnels → Create tunnel</em>{' '}{/* i18n-ok: the Cloudflare dashboard is English whatever we render in */}
                        {t('→ copy the token and paste it here. You set the hostname (e.g.')}{' '}
                        <code>{domain}</code> → <code>http://localhost:80</code>){' '}
                        {t(
                          'in the dashboard. salt.md keeps the tunnel running, restarts included. No port forwarding needed.',
                        )}
                      </p>
                      <div className="settings-row">
                        <input
                          className="prop-input"
                          style={{ flex: 1 }}
                          type="password"
                          placeholder={pa?.tokenSet ? t('•••••• (token stored)') : t('eyJhIjoi… (tunnel token)')}
                          value={tunnelToken}
                          onChange={(e) => setTunnelToken(e.target.value)}
                        />
                        <button
                          className="btn primary"
                          disabled={tunnelBusy || pa?.status === 'running' || pa?.status === 'starting' || (!tunnelToken.trim() && !pa?.tokenSet)}
                          onClick={() => void tunnel('start-token', tunnelToken.trim() || undefined)}
                        >
                          {t('Connect')}
                        </button>
                      </div>
                    </div>

                    <div className="pa-card">
                      <strong>{t('3 · Straight to HTTPS (no Cloudflare, e.g. a VPS)')}</strong>
                      <p className="dialog-hint settings-hint">
                        {t(
                          'salt.md fetches its own Let’s Encrypt certificate and listens on 80/443 — no Caddy or nginx needed. Requirements: the domain’s DNS A record points at this server and ports 80 and 443 are reachable. Restart after saving.',
                        )}
                      </p>
                      <div className="settings-row">
                        <input
                          className="prop-input"
                          style={{ flex: 1 }}
                          placeholder="notes.example.com"
                          value={s.httpsDomain}
                          onChange={(e) => set('httpsDomain', e.target.value)}
                        />
                        <label className="settings-check" style={{ whiteSpace: 'nowrap' }}>
                          <input type="checkbox" checked={httpsEnabled} onChange={(e) => setHttpsEnabled(e.target.checked)} />
                          <span>{t('Active')}</span>
                        </label>
                      </div>
                    </div>

                    <h3>{t('Manual — your own reverse proxy')}</h3>
                    <label className="settings-check">
                      <input type="checkbox" checked={trustProxy} onChange={(e) => setTrustProxy(e.target.checked)} />
                      <span>
                        {t('Run behind a reverse proxy (trust')} <code>X-Forwarded-For</code>)
                      </span>
                    </label>
                    <p className="dialog-hint settings-hint">
                      {t(
                        'Only switch this on when salt.md runs behind Caddy, nginx or a Cloudflare tunnel — the instance then sees real client IPs (sign-in protection, audit log). Leave it off without a proxy, or an attacker could forge their IP.',
                      )}
                    </p>
                    <label>{t('Internal address of the instance (upstream)')}</label>
                    <input className="prop-input" value={upstream} onChange={(e) => setUpstream(e.target.value)} />
                    <p className="dialog-hint settings-hint">
                      {t('The domain in the examples comes from the public base URL (the “General” tab):')}{' '}
                      <strong>{domain}</strong>
                    </p>
                    <ConfBlock title={t('Caddy (automatic HTTPS)')} text={caddyConf} />
                    <ConfBlock title={t('Cloudflare Tunnel (no open port needed)')} text={cloudflaredConf} />
                    <ConfBlock title="nginx" text={nginxConf} />
                    <p className="dialog-hint settings-hint">
                      {t(
                        'Cloudflare: leave the DNS record “Proxied” (orange cloud); WebSockets are on by default. Caddy handles certificates and WebSockets by itself. Alternatively, direct TLS without a proxy via',
                      )}{' '}
                      <code>SALT_TLS_CERT</code>/<code>SALT_TLS_KEY</code>.
                    </p>
                  </>
                )}

                {tab === 'webhooks' && (
                  <>
                    <h3>{t('Tell other tools when something changes')}</h3>
                    <p className="dialog-hint settings-hint">
                      {t(
                        'Instead of other programs asking over and over whether anything is new, salt.md calls an address of your choosing when a page is created, changed or thrown away. That is what Zapier, Make and n8n need — and through them, everything else.',
                      )}
                    </p>
                    <p className="dialog-hint settings-hint">
                      {t(
                        'The message says WHICH page and what happened to it — never the content. So a URL entered by mistake cannot turn into a steady export of what people write.',
                      )}
                    </p>

                    <label>{t('Address to call')}</label>
                    <input
                      className="prop-input"
                      placeholder="https://hooks.example.com/salt"
                      value={hookURL}
                      onChange={(e) => setHookURL(e.target.value)}
                    />
                    <label>{t('When should we call?')}</label>
                    <div className="hook-events">
                      {(['page.created', 'page.updated', 'page.trashed'] as const).map((ev) => (
                        <label key={ev} className="hook-event">
                          <input
                            type="checkbox"
                            checked={hookEvents.includes(ev)}
                            onChange={(e) =>
                              setHookEvents((prev) =>
                                e.target.checked ? [...prev, ev] : prev.filter((x) => x !== ev),
                              )
                            }
                          />
                          <span>
                            {ev === 'page.created'
                              ? t('a page is created')
                              : ev === 'page.updated'
                                ? t('a page is changed')
                                : t('a page is thrown away')}
                          </span>
                          <code>{ev}</code>
                        </label>
                      ))}
                    </div>
                    {hookErr && <div className="login-error">{hookErr}</div>}
                    <button
                      className="btn primary"
                      disabled={hookBusy || !hookURL.trim() || hookEvents.length === 0}
                      onClick={() => {
                        setHookBusy(true);
                        setHookErr('');
                        void api
                          .createWebhook(hookURL.trim(), hookEvents)
                          .then((h) => {
                            setFreshSecret(h.secret ?? '');
                            setHookURL('');
                            return api.webhooks().then(setHooks);
                          })
                          .catch((e: unknown) => setHookErr(e instanceof Error ? e.message : String(e)))
                          .finally(() => setHookBusy(false));
                      }}
                    >
                      {hookBusy ? t('Saving…') : t('Add')}
                    </button>

                    {freshSecret && (
                      <div className="hook-secret">
                        <strong>{t('Copy this secret now — it is shown only once.')}</strong>
                        <p className="dialog-hint">
                          {t(
                            'Your receiver uses it to check that a message really came from us. We send it as a signature in the X-Salt-Signature header.',
                          )}
                        </p>
                        <code className="hook-secret-value">{freshSecret}</code>
                        <button className="btn-sm" onClick={() => setFreshSecret('')}>
                          {t('I have it')}
                        </button>
                      </div>
                    )}

                    <h3>{t('Configured')}</h3>
                    {hooks === null && <div className="dialog-hint">{t('Loading…')}</div>}
                    {hooks?.length === 0 && (
                      <div className="dialog-hint">{t('Nothing yet — nobody is being called.')}</div>
                    )}
                    {hooks?.map((h) => (
                      <div key={h.id} className="hook-row">
                        <div className="hook-row-main">
                          <code>{h.url}</code>
                          <span className="dialog-hint">{h.events.split(',').join(' · ')}</span>
                        </div>
                        <span className="dialog-hint hook-status">
                          {h.lastAt
                            ? `${t('last call')}: ${h.lastStatus} · ${formatMoment(h.lastAt)}`
                            : t('not called yet')}
                        </span>
                        <button
                          className="btn-sm danger"
                          onClick={() => {
                            void api.deleteWebhook(h.id).then(() => api.webhooks().then(setHooks));
                          }}
                        >
                          {t('Remove')}
                        </button>
                      </div>
                    ))}
                  </>
                )}
                {tab === 'documents' && (
                  <>
                    <label>{t('When a document is printed or saved as PDF')}</label>
                    <div className="settings-checks">
                      {([
                        ['cover', t('A title page of its own')],
                        ['icon', t("Show the document's icon")],
                        ['footer', t('Title and date at the foot of every page')],
                        ['pageNums', t('Page numbers')],
                        ['workspace', t('Name the workspace and the instance')],
                        ['comments', t('The comments, after the document')],
                        ['links', t('Links as links, not as plain text')],
                        ['landscape', t('Landscape, for documents made of wide tables')],
                      ] as const).map(([key, label]) => (
                        <label key={key} className="settings-check">
                          <input
                            type="checkbox"
                            checked={pdf[key]}
                            onChange={(e) => setPdf({ ...pdf, [key]: e.target.checked })}
                          />
                          {label}
                        </label>
                      ))}
                    </div>
                    <p className="dialog-hint settings-hint">
                      {t('salt.md lays the pages out itself and the browser only puts them on paper, so nothing of the browser gets printed along — no address, no date in the corner.')}
                    </p>
                    <p className="dialog-hint settings-hint">
                      {t('These are the defaults. The panel beside a print view can deviate for one document, and that choice travels in the link.')}
                    </p>
                  </>
                )}

                {tab === 'maintenance' && (
                  <>
                    <label>{t('Backup')}</label>
                    <div className="settings-row">
                      <button className="btn primary" onClick={() => api.download('/api/admin/backup')}>
                        {t('Download backup (.tar.gz)')}
                      </button>
                    </div>
                    <p className="dialog-hint settings-hint">
                      {t('Contains the whole database (a consistent snapshot) and every upload. To restore:')}{' '}
                      <code>./salt restore backup.tar.gz</code>.{' '}
                      {t('For automatic backups, run')} <code>./salt backup</code> {t('from cron.')}
                    </p>
                    <label>{t('Keep the activity log for')}</label>
                    <div className="settings-row">
                      <select
                        className="prop-select"
                        value={AUDIT_PRESETS.includes(s.auditDays) ? s.auditDays : 'custom'}
                        onChange={(e) => {
                          setPruned(null);
                          set('auditDays', e.target.value === 'custom' ? '90' : e.target.value);
                        }}
                      >
                        <option value="0">{t('Forever')}</option>
                        <option value="30">{plural(30, '{n} day', '{n} days')}</option>
                        <option value="60">{plural(60, '{n} day', '{n} days')}</option>
                        <option value="180">{plural(180, '{n} day', '{n} days')}</option>
                        <option value="365">{plural(365, '{n} day', '{n} days')}</option>
                        <option value="custom">{t('Custom period')}</option>
                      </select>
                      {!AUDIT_PRESETS.includes(s.auditDays) && (
                        <input
                          className="prop-input settings-days"
                          type="number"
                          min={1}
                          max={3650}
                          value={s.auditDays}
                          onChange={(e) => set('auditDays', e.target.value)}
                        />
                      )}
                      <button
                        className="btn"
                        disabled={s.auditDays === '0' || pruning}
                        onClick={() => void prune()}
                      >
                        {t('Clean up now')}
                      </button>
                    </div>
                    <p className="dialog-hint settings-hint">
                      {pruned !== null
                        ? plural(pruned, '{n} entry removed', '{n} entries removed')
                        : s.auditDays === '0'
                          ? t('Nothing is ever removed. Roughly 300 bytes per change.')
                          : t('Older entries are removed once a day. Taking a change back stops working once its entry is gone.')}
                    </p>
                    <label>{t('Open-source licences')}</label>
                    <p className="dialog-hint settings-hint">
                      {t('Everything salt.md is built on, with its licence in full.')}{' '}
                      <a href="/licenses" target="_blank" rel="noopener noreferrer">{t('Open')}</a>
                    </p>
                    <label>{t('Instance')}</label>
                    {!info ? (
                      <p className="dialog-hint">{t('Loading…')}</p>
                    ) : (
                      <div className="info-grid">
                        <span>{t('Version')}</span><strong>{info.version} · {info.goVersion} · {info.os}</strong>
                        <span>{t('Uptime')}</span><strong>{fmtUptime(info.uptimeSec)}</strong>
                        <span>{t('Users / workspaces')}</span><strong>{info.users} / {info.workspaces}</strong>
                        <span>{t('Pages (trashed)')}</span><strong>{info.pages} ({info.trashed})</strong>
                        <span>{t('Database')}</span><strong>{fmtBytes(info.dbSize)}</strong>
                        <span>{t('Uploads')}</span><strong>{fmtBytes(info.uploadsSize)}</strong>
                        <span>{t('Data directory')}</span><strong>{info.dataDir}</strong>
                        <span>{t('Your IP (as the server sees it)')}</span><strong>{info.yourIp}{info.trustProxy ? ' · ' + t('proxy headers active') : ''}</strong>
                      </div>
                    )}
                  </>
                )}
              </div>
            </>
          )}
          <div className="dialog-buttons">
            <button className="btn" onClick={onClose}>{t('Cancel')}</button>
            <button className="btn primary" onClick={() => void save()}>{t('Save')}</button>
          </div>
        </div>
      </div>
    </Portal>
  );
}

// Calendar subscription: a read-only ICS feed of every date property.
export function CalendarSubModal({ onClose }: { onClose: () => void }) {
  useExclusiveModal(onClose);
  type Info = Awaited<ReturnType<typeof api.icsInfo>>;
  const [info, setInfo] = useState<Info | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  // Which feed the links below refer to. '' is the whole account, which is what
  // the dialog offered before W120 and stays the default.
  const [pick, setPick] = useState('');
  useEffect(() => {
    void api.icsInfo().then(setInfo).catch((e) => setLoadErr((e as Error).message || t('Loading failed')));
  }, []);
  const rotate = async () => {
    setInfo(await api.icsInfo(true));
    toast(t('New calendar link created (the old one no longer works)'));
  };
  const scopes = info?.scopes ?? [];
  const key = (s: { kind: string; id: string }) => s.kind + ':' + s.id;
  const current = scopes.find((s) => key(s) === pick) ?? scopes[0];
  const workspaces = scopes.filter((s) => s.kind === 'workspace');
  const collections = scopes.filter((s) => s.kind === 'collection');
  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Subscribe to calendar')}>
          <h2>{t('Subscribe to calendar')}</h2>
          <p className="dialog-hint">
            {t(
              'Subscribe to every date property in your collections from Apple Calendar, Google Calendar or Outlook. The link is private — do not share it.',
            )}
          </p>
          {loadErr ? (
            <div className="login-error">{loadErr}</div>
          ) : !info ? (
            <div className="dialog-hint">{t('Loading…')}</div>
          ) : (
            <>
              {/* One feed for everything is rarely what a calendar app wants:
                  a separate subscription per workspace or per collection can be
                  switched off in the app without touching the others. */}
              <label className="dialog-hint">{t('What should the calendar contain?')}</label>
              <select
                className="prop-select"
                value={pick}
                onChange={(e) => setPick(e.target.value)}
                aria-label={t('What should the calendar contain?')}
              >
                <option value="">{t('Everything I can see')}</option>
                {workspaces.length > 0 && (
                  <optgroup label={t('Workspaces')}>
                    {workspaces.map((s) => (
                      <option key={key(s)} value={key(s)}>
                        {s.name}
                      </option>
                    ))}
                  </optgroup>
                )}
                {collections.length > 0 && (
                  <optgroup label={t('Collections')}>
                    {collections.map((s) => (
                      <option key={key(s)} value={key(s)}>
                        {s.name}
                      </option>
                    ))}
                  </optgroup>
                )}
              </select>
              {collections.length === 0 && (
                <p className="dialog-hint">
                  {t('A collection appears here once it has a date property.')}
                </p>
              )}
              <label className="dialog-hint">{t('Subscription link (webcal):')}</label>
              <input
                className="prop-input invite-input"
                readOnly
                value={current?.links.webcal ?? info.webcal}
                onFocus={(e) => e.currentTarget.select()}
              />
              <div className="dialog-buttons" style={{ justifyContent: 'flex-start', gap: 8 }}>
                <a className="btn primary" href={current?.links.webcal ?? info.webcal}>{t('Open in calendar')}</a>
                <button
                  className="btn"
                  onClick={() => void navigator.clipboard?.writeText(current?.links.url ?? info.url)}
                >
                  {t('Copy URL')}
                </button>
                {/* Rotating kills EVERY feed at once, because there is one token
                    behind all of them — say so where the button is. */}
                <button className="btn" onClick={() => void rotate()} title={t('Invalidates all calendar links')}>
                  {t('Reset the link')}
                </button>
              </div>
            </>
          )}
          <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
        </div>
      </div>
    </Portal>
  );
}

// Personal 2FA (TOTP) setup for the current user.
export function TwoFAModal({ onClose }: { onClose: () => void }) {
  useExclusiveModal(onClose);
  const [status, setStatus] = useState<boolean | null>(null);
  const [setup, setSetup] = useState<{ secret: string; otpauthUrl: string; qr?: string } | null>(null);
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void api.twoFAStatus().then((r) => setStatus(r.enabled));
  }, []);

  const begin = async () => {
    setError(null);
    try {
      setSetup(await api.twoFASetup());
    } catch (e) {
      setError((e as Error).message || t('2FA setup failed'));
    }
  };
  const enable = async () => {
    setError(null);
    try {
      await api.twoFAEnable(code);
      setStatus(true);
      setSetup(null);
      setCode('');
      toast(t('2FA enabled'));
    } catch {
      setError(t('Wrong code'));
    }
  };
  const disable = async () => {
    setError(null);
    try {
      await api.twoFADisable(code);
      setStatus(false);
      setCode('');
      toast(t('2FA disabled'));
    } catch {
      setError(t('Wrong code'));
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Two-factor authentication')}>
          <h2>{t('Two-factor authentication')}</h2>
          {status === null && <div className="dialog-hint">{t('Loading…')}</div>}

          {status === true && !setup && (
            <>
              <p className="dialog-hint">{t('2FA is active. Enter a current code to switch it off.')}</p>
              <input className="prop-input" inputMode="numeric" placeholder={t('6-digit code')} value={code} onChange={(e) => setCode(e.target.value)} />
              {error && <div className="login-error">{error}</div>}
              <button className="btn danger" onClick={() => void disable()}>{t('Disable 2FA')}</button>
            </>
          )}

          {status === false && !setup && (
            <>
              <p className="dialog-hint">{t('Protect your account with an authenticator app (Google Authenticator, 1Password, …).')}</p>
              <button className="btn primary" onClick={() => void begin()}>{t('Set up 2FA')}</button>
            </>
          )}

          {setup && (
            <>
              <p className="dialog-hint">
                {t(
                  'Scan the QR code with your authenticator app (Google Authenticator, 1Password, …) and confirm with a generated code. The QR code and the key are created on the instance and never leave it.',
                )}
              </p>
              {setup.qr && <img className="totp-qr" src={setup.qr} alt={t('QR code for the authenticator app')} />}
              <p className="dialog-hint totp-manual-hint">{t('No scanner? Type the key in by hand:')}</p>
              <code className="totp-secret" onClick={() => void navigator.clipboard?.writeText(setup.secret)}>
                {setup.secret.replace(/(.{4})/g, '$1 ').trim()}
              </code>
              <input className="prop-input" inputMode="numeric" placeholder={t('6-digit code')} value={code} onChange={(e) => setCode(e.target.value)} autoFocus />
              {error && <div className="login-error">{error}</div>}
              <button className="btn primary" onClick={() => void enable()}>{t('Enable')}</button>
            </>
          )}

          <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
        </div>
      </div>
    </Portal>
  );
}
