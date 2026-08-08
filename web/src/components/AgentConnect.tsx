import { useEffect, useState } from 'react';
import { api } from '../api';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { toast } from '../toast';
import type { Workspace } from '../types';
import { Bot, Check, Copy, Download, ShieldCheck, KeyRound } from 'lucide-react';
import { t } from '../i18n';

/** Injected by the build; false everywhere except the website's framed demo. */
declare const __SALT_DEMO__: boolean;

// "Connect an agent" (wave 44): salt.md is AI-native — every agent talks to the
// workspace through the built-in MCP server. This modal turns connecting into a
// one-minute job: create a token with one click, pick an agent from the
// gallery, copy a ready-made config snippet.

interface AgentDef {
  id: string;
  name: string;
  logo: React.ReactNode;
  snippet: (url: string, token: string) => string;
}

const TOKEN_PH = '<YOUR-TOKEN>';

// TWO WAYS IN, and the same snippet shape for both — the only difference is
// whether the address carries a token.
//
//   …/mcp            the client signs in: it finds the authorization server on
//                    its own (the 401 says where), you approve once in the
//                    browser, and nothing secret is ever in the address.
//   …/mcp/<token>    the token IS the address. For clients with nothing but a
//                    URL field and no sign-in support.
//
// An empty token means the first. Deliberately one function rather than two, so
// no snippet can drift into a shape the other never got.
const mcpURL = (url: string, token: string) => (token ? `${url}/mcp/${token}` : `${url}/mcp`);

const mcpJSON = (url: string, token: string) =>
  JSON.stringify({ mcpServers: { salt: { url: mcpURL(url, token) } } }, null, 2);

// Real logos from selfh.st/icons, bundled locally (web/public/agents/).
// mono = black logo → inverted in dark mode.
const img = (file: string, mono = false) => (
  // Not an absolute path from the site root: that is right only while this
  // application owns its origin, and wrong the moment it is served under a
  // sub-path — where every one of these logos 404s. BASE_URL is '/' here.
  <img
    className={'agent-img' + (mono ? ' agent-img--mono' : '')}
    src={import.meta.env.BASE_URL + 'agents/' + file}
    alt=""
  />
);

// Hints live apart from the agent table so that t() runs at render time —
// a module-level constant would freeze whatever language was active at import.
const agentHint = (id: string): string =>
  ({
    'claude-app': t('Settings → Connectors → “Add custom connector” — paste the URL, nothing else.'),
    'claude-code': t('One command in the terminal and you are done.'),
    chatgpt: t('Settings → Connectors (Developer Mode) — paste the URL.'),
    codex: t('Add it to ~/.codex/config.toml.'),
    cursor: t('In .cursor/mcp.json (project) or ~/.cursor/mcp.json (global).'),
    openclaw: t('Register it as an MCP server in the OpenClaw configuration.'),
    hermes: t('A standard mcpServers entry in the agent configuration.'),
    gemini: t('Add it to ~/.gemini/settings.json.'),
    other: t('Any MCP-capable client (streamable HTTP).'),
  })[id] ?? '';

const AGENTS: AgentDef[] = [
  {
    id: 'claude-app',
    name: 'Claude (App & Web)',
    logo: img('claude.svg'),
    snippet: (url, token) => mcpURL(url, token),
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    logo: img('claude.svg'),
    snippet: (url, token) => `claude mcp add --transport http salt ${mcpURL(url, token)}`,
  },
  {
    id: 'chatgpt',
    name: 'ChatGPT',
    logo: img('chatgpt.svg'),
    snippet: (url, token) => mcpURL(url, token),
  },
  {
    id: 'codex',
    name: 'OpenAI Codex',
    logo: img('openai.svg', true),
    snippet: (url, token) => `[mcp_servers.salt]
url = "${mcpURL(url, token)}"`,
  },
  {
    id: 'cursor',
    name: 'Cursor',
    logo: (
      // No selfh.st icon for this one — a neutral cube in currentColor.
      <svg viewBox="0 0 24 24" width="26" height="26" aria-hidden="true">
        <path fill="currentColor" d="M12 2l9 5v10l-9 5-9-5V7z" opacity="0.9" />
        <path fill="var(--bg)" d="M12 6.2L17.5 9 12 11.8 6.5 9z" opacity="0.85" />
      </svg>
    ),
    snippet: (url, token) => mcpJSON(url, token),
  },
  {
    id: 'openclaw',
    name: 'OpenClaw',
    logo: img('openclaw.svg'),
    snippet: (url, token) => mcpJSON(url, token),
  },
  {
    id: 'hermes',
    name: 'Hermes Agent',
    logo: img('hermes-agent.png'),
    snippet: (url, token) => mcpJSON(url, token),
  },
  {
    id: 'gemini',
    name: 'Gemini CLI',
    logo: img('google-gemini.svg'),
    snippet: (url, token) =>
      JSON.stringify({ mcpServers: { salt: { httpUrl: mcpURL(url, token) } } }, null, 2),
  },
  {
    id: 'other',
    name: 'Other agent',
    logo: <Bot size={26} />,
    snippet: (url, token) => `MCP URL (token included):  ${mcpURL(url, token)}

Or the classic way, with a header:
  Endpoint:  ${url}/mcp
  Header:    Authorization: Bearer ${token}

REST API:  ${url}/api  (same bearer token)`,
  },
];

export default function AgentConnectModal({
  workspaces,
  currentWs,
  onClose,
}: {
  workspaces: Workspace[];
  currentWs: string;
  onClose: () => void;
}) {
  useExclusiveModal(onClose);
  const [token, setToken] = useState('');
  const [manual, setManual] = useState('');
  const [scope, setScope] = useState<'write' | 'read'>('write');
  const [wsScope, setWsScope] = useState<'current' | 'all'>('current');
  const [busy, setBusy] = useState(false);
  const [agent, setAgent] = useState<AgentDef>(AGENTS[0]);
  const [copied, setCopied] = useState(false);
  // Which of the two ways the snippet shows. Sign-in first, because it is the
  // one that puts nothing secret into an address — but the token way stays a
  // click away rather than being argued for, since plenty of clients still
  // cannot do anything else.
  const [how, setHow] = useState<'signin' | 'token'>('signin');

  // Prefer the configured public address (Domain/Tunnel) over whatever address
  // this browser happens to use — cloud agents must reach the URL from outside.
  // Framed on the marketing site, this origin is salt.md — an address that
  // speaks no MCP and that a visitor might really paste into their client. An
  // obviously invented host says "put your own instance here" instead.
  const [url, setUrl] = useState(
    __SALT_DEMO__ ? 'https://salt.example.com' : window.location.origin,
  );
  useEffect(() => {
    api
      .publicBase()
      .then((r) => r.base && setUrl(r.base.replace(/\/$/, '')))
      .catch(() => {});
  }, []);
  const effToken = token || manual.trim() || TOKEN_PH;
  const wsName = workspaces.find((w) => w.id === currentWs)?.name ?? t('this workspace');

  const createToken = async () => {
    setBusy(true);
    try {
      const chosen = wsScope === 'current' && currentWs ? [currentWs] : [];
      const res = await api.createToken('agent', scope, chosen);
      setToken(res.token);
      toast(t('Token created — shown only once'));
    } catch (e) {
      toast((e as Error).message || t('The token could not be created'));
    } finally {
      setBusy(false);
    }
  };

  const snippet = agent.snippet(url, how === 'signin' ? '' : effToken);
  const copy = () => {
    void navigator.clipboard?.writeText(snippet);
    setCopied(true);
    toast(t('Setup copied'));
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog wide agent-dialog" role="dialog" aria-modal="true" aria-label={t('Connect an agent')}>
          <h2>
            <Bot size={22} style={{ verticalAlign: '-4px' }} /> {t('Connect an agent')}
          </h2>
          <p className="dialog-hint">
            {t(
              'salt.md is AI-native: the built-in MCP server lets any agent read, write and search pages and maintain collections. There are two ways in — signing in, or a token that lives in the address.',
            )}
          </p>

          {/* WHICH way in. Deliberately not a per-client capability table: which
              agent speaks OAuth changes month to month, and a table that is
              wrong is worse than none. What is true regardless: a client that
              can sign in discovers it from the plain address by itself, and one
              that cannot will ask for a token instead. So the advice is "try
              this one first", which is also self-correcting. */}
          <div className="agent-how">
            <button
              className={'agent-how-opt' + (how === 'signin' ? ' active' : '')}
              onClick={() => setHow('signin')}
            >
              <ShieldCheck size={15} />
              <span>
                {t('Sign in')}
                <span className="agent-how-sub">{t('Nothing secret in the address. Expires and can be ended.')}</span>
              </span>
            </button>
            <button
              className={'agent-how-opt' + (how === 'token' ? ' active' : '')}
              onClick={() => setHow('token')}
            >
              <KeyRound size={15} />
              <span>
                {t('Token in the address')}
                <span className="agent-how-sub">{t('For clients that only have a URL field. Treat it like a password.')}</span>
              </span>
            </button>
          </div>

          {/* Only for the token way. With sign-in, the scope and the workspaces
              are chosen on the consent screen instead — two places offering the
              same decision would leave people wondering which one counts. */}
          {how === 'token' && (
          <div className="agent-token">
            {token ? (
              <div className="agent-token-fresh">
                <code onClick={() => { void navigator.clipboard?.writeText(token); toast(t('Token copied')); }}>{token}</code>
                <span className="dialog-hint">{t('Visible only now — already filled into the snippet below.')}</span>
              </div>
            ) : (
              <>
                <div className="agent-token-row">
                  <select className="prop-select" value={scope} onChange={(e) => setScope(e.target.value as 'write' | 'read')}>
                    <option value="write">{t('Read & write')}</option>
                    <option value="read">{t('Read only')}</option>
                  </select>
                  <select className="prop-select" value={wsScope} onChange={(e) => setWsScope(e.target.value as 'current' | 'all')}>
                    <option value="current">{t('Only “{name}”', { name: wsName })}</option>
                    <option value="all">{t('All workspaces')}</option>
                  </select>
                  <button className="btn primary" disabled={busy} onClick={() => void createToken()}>
                    {t('Create token')}
                  </button>
                </div>
                <input
                  className="prop-input"
                  placeholder={t('… or paste an existing token here')}
                  value={manual}
                  onChange={(e) => setManual(e.target.value)}
                />
              </>
            )}
          </div>
          )}

          <div className="agent-grid">
            {AGENTS.map((a) => (
              <button
                key={a.id}
                className={'agent-card' + (agent.id === a.id ? ' active' : '')}
                onClick={() => setAgent(a)}
              >
                <span className="agent-logo">{a.logo}</span>
                <span className="agent-name">{a.name}</span>
              </button>
            ))}
          </div>

          <div className="conf-block agent-snippet">
            <div className="conf-head">
              <span>{agent.name} — {agentHint(agent.id)}</span>
              <button className="btn-sm" onClick={copy}>
                {copied ? <Check size={13} /> : <Copy size={13} />} {t('Copy')}
              </button>
            </div>
            <pre>
              <code>{snippet}</code>
            </pre>
          </div>

          {how === 'signin' ? (
            <p className="dialog-hint settings-hint">
              {t('Paste this and the client sends you here to approve it — you pick the workspaces then. If it asks for a token instead, it cannot sign in yet: use the other way.')}
            </p>
          ) : (
            effToken === TOKEN_PH && (
              <p className="dialog-hint settings-hint">
                {t('Create a token above (or paste one) — it is filled into the snippet automatically.')}
              </p>
            )
          )}
          {url.startsWith('http://') && !/^http:\/\/(localhost|127\.)/.test(url) && (
            <p className="dialog-hint settings-hint pa-warn">
              {t('⚠ Cloud agents (claude.ai, say) cannot reach')} <code>{url}</code>{' '}
              {t(
                '— make the instance public for that (Instance settings → Domain & proxy) and connect through the public URL. Local CLIs on the same network work directly.',
              )}
            </p>
          )}

          {/* Connecting is only half of it. An agent that is connected still
              does not know how this team works, and being told in a chat means
              being told again in the next one — which is the actual complaint.
              The skill is generated per instance, so the address, the workspace
              ids and the rules in it are this instance's own; its first
              instruction is to write a short block into the repository's
              CLAUDE.md / AGENTS.md, which is the file that survives. */}
          <div className="agent-skill">
            <div className="agent-skill-main">
              <span className="agent-skill-title">{t('Teach the agent how you work here')}</span>
              <span className="agent-skill-sub">
                {t(
                  'A skill with the playbook, this workspace and its rules. It installs a short block into the repository so the next session knows it too — without being told again.',
                )}
              </span>
            </div>
            <button
              className="btn-sm"
              onClick={() => api.download(`/api/skill?workspace=${encodeURIComponent(currentWs)}`)}
            >
              <Download size={13} /> {t('Download skill')}
            </button>
          </div>

          <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
        </div>
      </div>
    </Portal>
  );
}
