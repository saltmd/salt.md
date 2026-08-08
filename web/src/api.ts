import type { AgentWork } from './agentPresence';
import type {
  ApiToken,
  Backlink,
  BlueprintEntry,
  OAuthGrant,
  CollectionConfig,
  Me,
  Page,
  PageMeta,
  PublicFormConfig,
  SaltFile,
  SearchResult,
  User,
  Webhook,
  Workspace,
} from './types';
import { t } from './i18n';
import type { Prefs } from './i18n';
import { formatBytes } from './format';
import { serverMessage } from './serverErrors';

/** An error carrying an HTTP status and a machine-readable reason, so callers
 *  can branch on those rather than on the message text — the text changes with
 *  every rewording, and a branch on it breaks silently. */
export class ApiError extends Error {
  status: number;
  /** Reason from the response's `code` field, e.g. `2fa_required`. Empty when
   *  the endpoint sends none. */
  code: string;
  constructor(message: string, status: number, code = '') {
    super(message);
    this.status = status;
    this.code = code;
  }
}

/** Turn an error response into an ApiError — the same handling for JSON calls
 *  as for file uploads, which cannot send JSON.
 *
 *  The case that matters is "the response is not JSON at all": a proxy in front
 *  reports an oversized file as an HTML page. Calling `res.json()` blindly
 *  throws on that, and the screen then read `Unexpected token '<'` instead of
 *  anything about the file's size. */
async function toApiError(res: Response, fallback: string): Promise<ApiError> {
  let msg = fallback || res.statusText;
  let code = '';
  let data: Record<string, unknown> | undefined;
  try {
    const body = (await res.json()) as Record<string, unknown>;
    msg = (body.error as string) ?? msg;
    code = (body.code as string) ?? '';
    data = body;
  } catch {
    if (res.status === 413) msg = t('The file is too large for this instance.');
  }
  // Translating here rather than at each call site: every screen that shows
  // `err.message` gets the reader's language without knowing that server
  // messages are translatable at all. An unknown code keeps the server's
  // English, which is a correct sentence in the wrong language rather than a
  // broken one.
  return new ApiError(serverMessage(code, msg, data), res.status, code);
}

/** An expired session sends the user back to the sign-in screen — except on the
 *  sign-in endpoints themselves, where a 401 means a failed attempt and the
 *  reason is needed (2FA required vs. wrong password). */
function throwApiError(url: string, err: ApiError): never {
  if (err.status === 401 && !/^\/api\/(login|signup|setup)\b/.test(url)) {
    window.dispatchEvent(new Event('salt:unauthorized'));
    throw new ApiError('unauthorized', 401, 'session_expired');
  }
  throw err;
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  });
  if (!res.ok) throwApiError(url, await toApiError(res, ''));
  return res.json() as Promise<T>;
}

export const api = {
  me: () => req<Me>('/api/me'),
  // Webhooks: instance configuration, admin only. Creating one answers with the
  // secret — the only time it is ever readable.
  webhooks: () => req<Webhook[]>('/api/webhooks'),
  createWebhook: (url: string, events: string[]) =>
    req<Webhook>('/api/webhooks', { method: 'POST', body: JSON.stringify({ url, events }) }),
  deleteWebhook: (id: string) => req<{ ok: boolean }>(`/api/webhooks/${id}`, { method: 'DELETE' }),
  // Language and time preferences. Its own endpoint, not a field on the user
  // PATCH: that route lets an admin edit somebody else, and nobody should be
  // able to set another person's clock format (see server/prefs.go).
  putPrefs: (p: Prefs) => req<Prefs>('/api/me/prefs', { method: 'PUT', body: JSON.stringify(p) }),
  setup: (name: string, email: string, password: string) =>
    req<User>('/api/setup', { method: 'POST', body: JSON.stringify({ name, email, password }) }),
  login: (email: string, password: string, code?: string) =>
    req<User>('/api/login', { method: 'POST', body: JSON.stringify({ email, password, code }) }),
  logout: () => req<{ ok: boolean }>('/api/logout', { method: 'POST' }),
  signup: (name: string, email: string, password: string) =>
    req<User>('/api/signup', { method: 'POST', body: JSON.stringify({ name, email, password }) }),
  // No allowedDomains here: which sender domains an instance counts as its own
  // is nobody's business before they have signed in.
  signupPolicy: () => req<{ mode: string; instanceName: string; oauthGoogle: boolean; oauthMicrosoft: boolean }>('/api/signup-policy'),

  getSettings: () =>
    req<{
      instanceName: string;
      signupMode: string;
      allowedDomains: string;
      smtpHost: string;
      smtpPort: string;
      smtpUser: string;
      smtpFrom: string;
      smtpPassSet: boolean;
      publicBaseUrl: string;
      trustProxy: boolean;
      allowUserWorkspaces: boolean;
      maxUploadMb: number;
      trashDays: number;
      sessionDays: number;
      httpsDomain: string;
      httpsEnabled: boolean;
      googleClientId: string;
      googleSecretSet: boolean;
      msClientId: string;
      msSecretSet: boolean;
      mailProvider: string;
      mailAddress: string;
      mailFrom: string;
    }>('/api/settings'),
  mailTest: () => req<{ ok: boolean; to: string }>('/api/admin/mail-test', { method: 'POST', body: '{}' }),
  mailDisconnect: () => req<{ ok: boolean }>('/api/admin/mail-oauth/disconnect', { method: 'POST', body: '{}' }),
  adminInfo: () =>
    req<{
      version: string;
      goVersion: string;
      os: string;
      uptimeSec: number;
      users: number;
      workspaces: number;
      pages: number;
      trashed: number;
      dbSize: number;
      uploadsSize: number;
      dataDir: string;
      yourIp: string;
      trustProxy: boolean;
    }>('/api/admin/info'),
  publicAccess: () =>
    req<{
      status: string;
      mode: string;
      url: string;
      lastError: string;
      tokenSet: boolean;
      autostart: boolean;
      cloudflaredHere: boolean;
      httpsDomain: string;
      httpsEnabled: boolean;
      localUrl: string;
    }>('/api/admin/public-access'),
  tunnelAction: (action: string, token?: string) =>
    req<unknown>('/api/admin/tunnel', { method: 'POST', body: JSON.stringify({ action, token }) }),
  putSettings: (patch: Record<string, unknown>) =>
    req<unknown>('/api/settings', { method: 'PUT', body: JSON.stringify(patch) }),
  createInvite: (email: string, role: string, workspaceId: string) =>
    req<{ link: string; emailed: boolean }>('/api/invites', {
      method: 'POST',
      body: JSON.stringify({ email, role, workspaceId }),
    }),
  inviteInfo: (token: string) =>
    req<{ email: string; workspace: string }>(`/api/invites/${token}`),
  acceptInvite: (token: string, name: string, email: string, password: string, code = '') =>
    req<User>(`/api/invites/${token}/accept`, {
      method: 'POST',
      body: JSON.stringify({ name, email, password, code }),
    }),
  // scopes: the whole account, each workspace, and each collection that has a
  // date property at all (W120). url/webcal stay the unscoped pair.
  icsInfo: (rotate = false) =>
    req<{
      url: string;
      webcal: string;
      scopes: {
        id: string;
        kind: 'all' | 'workspace' | 'collection';
        name: string;
        links: { url: string; webcal: string };
      }[];
    }>(`/api/ics${rotate ? '?rotate=1' : ''}`),
  twoFAStatus: () => req<{ enabled: boolean }>('/api/2fa'),
  twoFASetup: () =>
    req<{ secret: string; otpauthUrl: string; qr: string }>('/api/2fa/setup', { method: 'POST' }),
  twoFAEnable: (code: string) =>
    req<{ enabled: boolean }>('/api/2fa/enable', { method: 'POST', body: JSON.stringify({ code }) }),
  twoFADisable: (code: string) =>
    req<{ enabled: boolean }>('/api/2fa/disable', { method: 'POST', body: JSON.stringify({ code }) }),

  listUsers: () => req<User[]>('/api/users'),
  accessOverview: () =>
    req<{ workspaces: { id: string; name: string }[]; memberships: { userId: string; workspaceId: string; role: string }[] }>('/api/admin/access'),
  setMembership: (userId: string, workspaceId: string, role: string) =>
    req<{ ok: boolean }>('/api/admin/membership', { method: 'PUT', body: JSON.stringify({ userId, workspaceId, role }) }),
  // Break-glass: a time-limited look into somebody else's workspace. Only the
  // owner may request it; the people responsible may view and end it too.
  breakGlass: (workspaceId: string, reason: string) =>
    req<{ ok: boolean; expiresAt: string; workspace: string }>(`/api/workspaces/${workspaceId}/break-glass`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }),
  listBreakGlass: (workspaceId: string) =>
    req<
      { id: string; user: string; reason: string; createdAt: string; expiresAt: string; revokedAt: string | null; active: boolean }[]
    >(`/api/workspaces/${workspaceId}/break-glass`),
  revokeBreakGlass: (workspaceId: string, grantId: string) =>
    req<{ ok: boolean }>(`/api/workspaces/${workspaceId}/break-glass/${grantId}`, { method: 'DELETE' }),
  createUser: (u: { name: string; email: string; password: string; isAdmin: boolean; workspaces?: { id: string; role: string }[] }) =>
    req<User>('/api/users', { method: 'POST', body: JSON.stringify(u) }),
  updateUser: (id: string, patch: Partial<{ name: string; email: string; color: string; avatar: string; password: string; currentPassword: string; isAdmin: boolean }>) =>
    req<User>(`/api/users/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  deleteUser: (id: string) => req<{ ok: boolean }>(`/api/users/${id}`, { method: 'DELETE' }),
  // Lifecycle (W105): consequences before deletion, deactivation as the normal
  // case, and the clean-up view for workspaces with nobody in charge.
  deletionImpact: (id: string) =>
    req<{
      userName: string;
      personal: { id: string; name: string; pages: number; members: number }[];
      orphaned: { id: string; name: string; pages: number; members: number; heir?: string }[];
      shared: { id: string; name: string; pages: number; members: number; heir?: string }[];
      pages: number;
    }>(`/api/users/${id}/deletion-impact`),
  // Hand the instance to another account. The only way to pass the owner role
  // on — before this it was a one-way street.
  transferOwner: (userId: string) =>
    req<{ ok: boolean; owner: string }>('/api/admin/transfer-owner', {
      method: 'POST',
      body: JSON.stringify({ userId }),
    }),
  setUserDisabled: (id: string, disabled: boolean) =>
    req<User>(`/api/users/${id}/disabled`, { method: 'PUT', body: JSON.stringify({ disabled }) }),
  strandedWorkspaces: () =>
    req<
      {
        id: string;
        name: string;
        owner: string;
        members: number;
        admins: number;
        pages: number;
        adoptable: boolean;
        deletable: boolean;
        personal: boolean;
      }[]
    >('/api/admin/stranded-workspaces'),
  adoptWorkspace: (id: string) =>
    req<{ ok: boolean; name: string }>(`/api/admin/stranded-workspaces/${id}/adopt`, { method: 'POST' }),
  deleteStrandedWorkspace: (id: string, confirm: string) =>
    req<{ ok: boolean }>(`/api/admin/stranded-workspaces/${id}`, {
      method: 'DELETE',
      body: JSON.stringify({ confirm }),
    }),

  listTokens: () => req<ApiToken[]>('/api/tokens'),
  createToken: (name: string, scope: 'read' | 'write' = 'write', workspaces: string[] = []) =>
    req<{ id: string; token: string; scope: string; workspaces: string[] }>('/api/tokens', {
      method: 'POST',
      body: JSON.stringify({ name, scope, workspaces }),
    }),
  deleteToken: (id: string) => req<{ ok: boolean }>(`/api/tokens/${id}`, { method: 'DELETE' }),

  listPages: () => req<PageMeta[]>('/api/pages'),
  createPage: (
    parentId: string | null,
    title = '',
    type: 'doc' | 'collection' = 'doc',
    props?: Record<string, unknown>,
    workspaceId?: string,
  ) =>
    req<Page>('/api/pages', {
      method: 'POST',
      body: JSON.stringify({ parentId, title, type, props, workspaceId }),
    }),
  getPage: (id: string) => req<Page>(`/api/pages/${id}`),
  updatePage: (
    id: string,
    patch: Partial<{
      title: string;
      icon: string;
      cover: string;
      content: unknown;
      props: Record<string, unknown>;
      propsPatch: Record<string, unknown>;
      parentId: string | null;
      position: number;
      visibility: 'workspace' | 'private';
      isTemplate: boolean;
      tags: string[];
      description: string;
      // Move to another workspace: takes the whole subtree along and puts the
      // page at the top level there.
      workspaceId: string;
    }>,
    opts?: { materialize?: boolean },
  ) =>
    req<Page>(`/api/pages/${id}${opts?.materialize ? '?materialize=1' : ''}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  trashPage: (id: string) => req<{ ok: boolean }>(`/api/pages/${id}`, { method: 'DELETE' }),
  // fromTemplate: instantiate a template (copy keeps the title).
  // asTemplate: save as template — the COPY becomes the template (snapshot),
  // the original stays a normal page.
  duplicatePage: (id: string, fromTemplate = false, asTemplate = false) =>
    req<{ id: string }>(
      `/api/pages/${id}/duplicate${fromTemplate ? '?fromTemplate=1' : asTemplate ? '?asTemplate=1' : ''}`,
      { method: 'POST' },
    ),
  importMarkdown: (parentId: string | null, title: string, markdown: string) =>
    req<{ id: string }>('/api/import', {
      method: 'POST',
      body: JSON.stringify({ parentId, title, markdown }),
    }),
  importZip: async (file: File): Promise<{ created: number; skipped: number }> => {
    const fd = new FormData();
    fd.append('file', file);
    // Raw fetch (FormData does not tolerate the JSON header), but the same
    // error handling as everywhere else: with a status, with a reason, and an
    // expired session lands on the sign-in screen rather than as text inside a
    // dialog.
    const res = await fetch('/api/import-zip', { method: 'POST', body: fd });
    if (!res.ok) throwApiError('/api/import-zip', await toApiError(res, t('Import failed')));
    return res.json() as Promise<{ created: number; skipped: number }>;
  },
  deleteForever: (id: string) =>
    req<{ ok: boolean }>(`/api/pages/${id}?permanent=1`, { method: 'DELETE' }),
  restorePage: (id: string) =>
    req<{ ok: boolean }>(`/api/pages/${id}/restore`, { method: 'POST' }),
  reindexSiblings: (parentId: string | null, workspaceId?: string) =>
    req<{ reindexed: number }>('/api/reindex-siblings', {
      method: 'POST',
      body: JSON.stringify({ parentId, workspaceId }),
    }),
  search: (q: string) => req<SearchResult[]>(`/api/search?q=${encodeURIComponent(q)}`),
  backlinks: (id: string) => req<Backlink[]>(`/api/pages/${id}/backlinks`),
  graph: () => req<{ edges: { source: string; target: string }[] }>('/api/graph'),

  getCollection: (pageId: string) => req<CollectionConfig>(`/api/collections/${pageId}`),
  // Who says they are working on what. Filtered per page on the server — the
  // live signal that triggers this carries no content for exactly that reason.
  presence: () =>
    req<{ working: AgentWork[]; freshSeconds: number }>('/api/presence'),
  collectionRows: (
    pageId: string,
    opts: {
      limit?: number;
      offset?: number;
      filters?: { property: string; op?: string; value: string }[];
      sort?: { property: string; dir: 'asc' | 'desc' } | null;
    } = {},
  ) => {
    const p = new URLSearchParams();
    if (opts.limit) p.set('limit', String(opts.limit));
    if (opts.offset) p.set('offset', String(opts.offset));
    for (const f of opts.filters ?? []) p.append('filter', `${f.property}:${f.op ?? ''}:${f.value}`);
    if (opts.sort) p.set('sort', `${opts.sort.property}:${opts.sort.dir}`);
    return req<{
      rows: {
        id: string;
        title: string;
        icon: string;
        cover: string;
        position: number;
        props: Record<string, unknown>;
        tags?: string[];
      }[];
      total: number;
      offset: number;
      limit: number;
    }>(`/api/collections/${pageId}/rows?${p.toString()}`);
  },
  putCollection: (pageId: string, config: CollectionConfig) =>
    req<CollectionConfig>(`/api/collections/${pageId}`, {
      method: 'PUT',
      body: JSON.stringify(config),
    }),

  audit: (before?: number) =>
    req<import('./types').AuditEntry[]>(`/api/audit${before ? `?before=${before}` : ''}`),

  listRevisions: (pageId: string) => req<import('./types').Revision[]>(`/api/pages/${pageId}/revisions`),
  getRevision: (pageId: string, revId: string) =>
    req<{ title: string; content: unknown[]; createdAt: string; authorName: string }>(
      `/api/pages/${pageId}/revisions/${revId}`,
    ),
  restoreRevision: (pageId: string, revId: string) =>
    req<{ ok: boolean }>(`/api/pages/${pageId}/revisions/${revId}/restore`, { method: 'POST' }),

  listComments: (pageId: string) => req<import('./types').Comment[]>(`/api/pages/${pageId}/comments`),
  // Open comments per page of a workspace, in one go — for the counters on
  // kanban cards. Deliberately not part of the page list (see
  // handleCommentCounts).
  commentCounts: (workspaceId: string) =>
    req<Record<string, number>>(`/api/comment-counts?workspaceId=${encodeURIComponent(workspaceId)}`),
  createComment: (pageId: string, body: string, blockId = '') =>
    req<{ id: string }>(`/api/pages/${pageId}/comments`, {
      method: 'POST',
      body: JSON.stringify({ body, blockId }),
    }),
  resolveComment: (id: string, resolved: boolean) =>
    req<{ ok: boolean }>(`/api/comments/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ resolved }),
    }),
  deleteComment: (id: string) => req<{ ok: boolean }>(`/api/comments/${id}`, { method: 'DELETE' }),

  // The raw trail. There is no edit and no single delete on purpose — see
  // server/notelog.go. clearNotes drops the whole of it and is the only removal
  // there is; the server refuses it to anything but a signed-in person.
  pageNotes: (pageId: string) => req<import('./types').PageNote[]>(`/api/pages/${pageId}/notes`),
  addNote: (pageId: string, body: string) =>
    req<{ id: string }>(`/api/pages/${pageId}/notes`, {
      method: 'POST',
      body: JSON.stringify({ body }),
    }),
  clearNotes: (pageId: string) =>
    req<{ ok: boolean; removed: number }>(`/api/pages/${pageId}/notes`, { method: 'DELETE' }),

  listTags: () => req<{ tag: string; count: number }[]>('/api/tags'),
  tagColors: (workspaceId: string) =>
    req<Record<string, string>>(`/api/tag-colors?workspace=${encodeURIComponent(workspaceId)}`),
  setTagColor: (workspaceId: string, tag: string, color: string) =>
    req<{ ok: boolean }>('/api/tag-colors', {
      method: 'PUT',
      body: JSON.stringify({ workspaceId, tag, color }),
    }),

  listWorkspaces: () => req<Workspace[]>('/api/workspaces'),
  // fromWorkspace copies an existing workspace's STRUCTURE into the new one:
  // its rules, databases, schemas and views, but no rows and no documents.
  createWorkspace: (name: string, fromWorkspace?: string) =>
    req<Workspace>('/api/workspaces', {
      method: 'POST',
      body: JSON.stringify(fromWorkspace ? { name, fromWorkspace } : { name }),
    }),
  // Signing an agent in (oauth_provider.go). Both need a browser session —
  // approving a grant with a token would be a key minting a better key.
  oauthRequestInfo: (clientId: string) =>
    req<{
      clientName: string;
      clientId: string;
      workspaces: { id: string; name: string }[];
      instanceName: string;
      host: string;
    }>(
      '/api/oauth/request?client_id=' + encodeURIComponent(clientId),
    ),
  oauthApprove: (body: {
    clientId: string;
    redirectUri: string;
    codeChallenge: string;
    codeChallengeMethod: string;
    scope: string;
    resource: string;
    allWorkspaces: boolean;
    workspaces: string[];
  }) => req<{ code: string }>('/api/oauth/approve', { method: 'POST', body: JSON.stringify(body) }),
  oauthGrants: () => req<OAuthGrant[]>('/api/oauth/grants'),
  revokeGrant: (id: string) => req<{ ok: boolean }>('/api/oauth/grants/' + id, { method: 'DELETE' }),

  // The blueprint library. The shelf ships in the binary, so this never fails
  // for want of a network.
  library: () => req<BlueprintEntry[]>('/api/library'),
  useBlueprint: (id: string, name: string) =>
    req<{ workspaceId: string; name: string }>(`/api/library/${id}`, {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),
  // Irreversible: the caller must echo the workspace name back as `confirm`.
  deleteWorkspace: (id: string, confirm: string) =>
    req<{ ok: boolean }>(`/api/workspaces/${id}`, {
      method: 'DELETE',
      body: JSON.stringify({ confirm }),
    }),
  updateWorkspace: (
    id: string,
    patch: Partial<{
      name: string;
      icon: string;
      image: string;
      autoJoin: boolean;
      agentAccess: string;
      treeMode: string;
    }>,
  ) =>
    req<{ ok: boolean }>(`/api/workspaces/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  // Rules go through their own endpoint: the server refuses API tokens on it
  // (sessionOnly) — agents follow the rules, they never write them. They may
  // PROPOSE a draft over MCP; applying (setWorkspaceRules settles it) or
  // dismissing it is the admin's browser-only act.
  setWorkspaceRules: (id: string, rules: string) =>
    req<{ ok: boolean }>(`/api/workspaces/${id}/rules`, { method: 'PUT', body: JSON.stringify({ rules }) }),
  dismissRulesProposal: (id: string) =>
    req<{ ok: boolean }>(`/api/workspaces/${id}/rules-proposal`, { method: 'DELETE' }),
  // The file index: a whole workspace, or everything below one page.
  listFiles: (scope: { workspace?: string; under?: string }) =>
    req<SaltFile[]>(
      '/api/files?' + new URLSearchParams(scope as Record<string, string>).toString(),
    ),
  addWorkspaceMember: (workspaceId: string, email: string, role: 'admin' | 'member' | 'viewer') =>
    req<{ ok: boolean }>(`/api/workspaces/${workspaceId}/members`, {
      method: 'POST',
      body: JSON.stringify({ email, role }),
    }),
  // color/avatar travel too (W116), so a person property can show the same face
  // as presence and comments.
  listMembers: (workspaceId: string) =>
    req<
      {
        userId: string;
        name: string;
        email: string;
        role: 'admin' | 'member' | 'viewer';
        color: string;
        avatar: string;
      }[]
    >(`/api/workspaces/${workspaceId}/members`),
  updateMember: (workspaceId: string, userId: string, role: 'admin' | 'member' | 'viewer') =>
    req<{ ok: boolean }>(`/api/workspaces/${workspaceId}/members/${userId}`, {
      method: 'PATCH',
      body: JSON.stringify({ role }),
    }),
  removeMember: (workspaceId: string, userId: string, confirmPrivate = false) =>
    req<{ ok: boolean }>(
      `/api/workspaces/${workspaceId}/members/${userId}${confirmPrivate ? '?confirmPrivate=1' : ''}`,
      { method: 'DELETE' },
    ),
  sharePage: (id: string, expiresInDays = 0, password = '') =>
    req<{ token: string; url: string }>(`/api/pages/${id}/share`, {
      method: 'POST',
      body: JSON.stringify({ expiresInDays, password }),
    }),
  unsharePage: (id: string) =>
    req<{ ok: boolean }>(`/api/pages/${id}/share`, { method: 'DELETE' }),

  // Resolved external base URL (public_base_url > HTTPS-Domain > Tunnel > Host).
  publicBase: () => req<{ base: string }>('/api/public-base'),

  // Public form sharing (owner side).
  formShareStatus: (collectionId: string) =>
    req<{ shared: boolean }>(`/api/collections/${collectionId}/form-share`),
  createFormShare: (collectionId: string) =>
    req<{ token: string; url: string }>(`/api/collections/${collectionId}/form-share`, { method: 'POST', body: '{}' }),
  deleteFormShare: (collectionId: string) =>
    req<{ ok: boolean }>(`/api/collections/${collectionId}/form-share`, { method: 'DELETE' }),
  // Public form (anonymous side).
  publicFormConfig: (token: string) =>
    req<PublicFormConfig>(`/api/public/form/${token}`),
  publicFormSubmit: (token: string, title: string, props: Record<string, unknown>) =>
    req<{ ok: boolean }>(`/api/public/form/${token}/submit`, {
      method: 'POST',
      body: JSON.stringify({ title, props }),
    }),

  listFavorites: () => req<string[]>('/api/favorites'),
  addFavorite: (pageId: string) =>
    req<{ ok: boolean }>(`/api/favorites/${pageId}`, { method: 'POST' }),
  removeFavorite: (pageId: string) =>
    req<{ ok: boolean }>(`/api/favorites/${pageId}`, { method: 'DELETE' }),

  // The Markdown a page exports, as text rather than as a download — the
  // template gallery shows it as a preview, so you can see what you are about
  // to copy instead of trusting its title.
  exportText: async (id: string) => {
    const res = await fetch(`/api/export/${id}`, { credentials: 'same-origin' });
    if (!res.ok) throw await toApiError(res, t('Could not be loaded'));
    return res.text();
  },

  // Anchor-based download: immune to popup blockers, unlike window.open.
  download: (url: string) => {
    const a = document.createElement('a');
    a.href = url;
    a.download = '';
    document.body.appendChild(a);
    a.click();
    a.remove();
  },

  // Max upload size — keep in sync with server/pages.go maxUploadSize (50 MiB).
  uploadMaxBytes: 50 * 1024 * 1024,

  // XHR-based so we can drive a global progress bar and give a precise
  // over-limit / server error message. Progress is broadcast on
  // "salt:upload-progress" (0-1) and "salt:upload-done".
  upload: (file: File, pageId?: string): Promise<string> =>
    new Promise((resolve, reject) => {
      if (file.size > api.uploadMaxBytes) {
        reject(
          new ApiError(
            t('File too large ({size}) — 50 MB max.', { size: formatBytes(file.size) }),
            413,
            'file_too_large',
          ),
        );
        return;
      }
      const fd = new FormData();
      fd.append('file', file);
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `/api/upload${pageId ? `?page=${pageId}` : ''}`);
      const emit = (p: number) => window.dispatchEvent(new CustomEvent('salt:upload-progress', { detail: p }));
      emit(0);
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) emit(e.loaded / e.total);
      };
      const finish = () => window.dispatchEvent(new Event('salt:upload-done'));
      xhr.onload = () => {
        finish();
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            resolve((JSON.parse(xhr.responseText) as { url: string }).url);
          } catch {
            reject(new ApiError(t('Upload failed'), xhr.status));
          }
          return;
        }
        // Status and reason here too — and an expired session leads to the
        // sign-in screen instead of landing as text in the editor while the
        // interface goes on pretending you are signed in.
        if (xhr.status === 401) {
          window.dispatchEvent(new Event('salt:unauthorized'));
          return reject(new ApiError('unauthorized', 401, 'session_expired'));
        }
        let msg = xhr.status === 413 ? t('The file is too large for this instance.') : t('Upload failed');
        let code = '';
        try {
          const body = JSON.parse(xhr.responseText) as { error?: string; code?: string };
          msg = body.error ?? msg;
          code = body.code ?? '';
        } catch {
          /* not a JSON response (a proxy's error page, say) */
        }
        reject(new ApiError(msg, xhr.status, code));
      };
      xhr.onerror = () => {
        finish();
        reject(new ApiError(t('Upload failed — no connection to the server.'), 0));
      };
      xhr.send(fd);
    }),
};
