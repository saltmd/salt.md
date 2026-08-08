import type { Prefs } from './i18n';

export interface PageMeta {
  id: string;
  parentId: string | null;
  title: string;
  icon: string;
  cover: string;
  position: number;
  updatedAt: string;
  trashed: boolean;
  type: 'doc' | 'collection';
  props: Record<string, unknown>;
  workspaceId: string;
  ownerId: string;
  visibility: 'workspace' | 'private';
  isTemplate: boolean;
  tags: string[];
  description: string;
  snippet: string; // plain-text preview for the notes list (derived server-side)
  thumb: string; // first image URL, '' if none
}

export interface Workspace {
  id: string;
  name: string;
  role: 'admin' | 'member' | 'viewer';
  icon: string;
  image: string;
  /** This account's own area — it belongs to the account, not the instance. */
  personal?: boolean;
  /** Every newly created account joins automatically (only the owner sets this). */
  autoJoin?: boolean;
  /** Working conventions the admin wrote down — agents get them over MCP, members read them here. */
  rules?: string;
  /** A pending rules draft (usually from an agent) — inert until an admin applies it. */
  // Empty means the default in both cases — 'open' and 'split'. There is no
  // third state, so nothing has to handle one.
  agentAccess?: string;
  treeMode?: string;
  rulesProposal?: string;
  rulesProposalBy?: string;
  rulesProposalAt?: string;
}

/** One entry of the file index (W125): a file plus the page carrying it. */
export interface SaltFile {
  /** Stored name — the segment behind /files/. */
  name: string;
  displayName: string;
  ext: string;
  size: number;
  createdAt: string;
  pageId: string;
  pageTitle: string;
  workspaceId: string;
}

export interface AuditEntry {
  id: number;
  createdAt: string;
  actorType: 'human' | 'agent';
  actorName: string;
  action: string;
  pageId: string;
  detail: string;
}

export interface Page extends PageMeta {
  content: unknown[];
  createdAt: string;
}

export interface SearchResult {
  id: string;
  title: string;
  icon: string;
  snippet: string;
}

export interface Backlink {
  id: string;
  title: string;
  icon: string;
}

export interface User {
  id: string;
  email: string;
  name: string;
  color: string;
  avatar: string;
  isAdmin: boolean;
  /** Instanzrolle: owner betreibt die Instanz, admin verwaltet Menschen. */
  orgRole?: 'owner' | 'admin' | 'member';
  /** Deactivated: no sign-in, but everything stays attributable. */
  disabled?: boolean;
}

export interface Me {
  setupRequired: boolean;
  authenticated: boolean;
  user: User | null;
  version: string;
  // May non-admins create workspaces of their own? (instance setting, W97)
  allowUserWorkspaces?: boolean;
  /** Language and time preferences of this account (W112). Empty fields mean
   *  automatic — see server/prefs.go. */
  prefs?: Prefs;
}

/** An outbound webhook: salt.md calls this URL when something happens, so
 *  other tools do not have to keep asking. The secret is returned once, when
 *  it is created, and never again. */
export interface Webhook {
  id: string;
  url: string;
  events: string;
  active: boolean;
  createdAt: string;
  lastStatus: string;
  lastAt: string;
  secret?: string;
}

export interface Revision {
  id: string;
  createdAt: string;
  authorName: string;
  title: string;
}

export interface Comment {
  id: string;
  blockId: string;
  authorId: string;
  authorName: string;
  body: string;
  createdAt: string;
  authorColor?: string;
  authorAvatar?: string;
  resolvedAt: string | null;
}

// One entry of a page's raw trail. No resolvedAt, no editedAt and no author id
// to check against: nothing about it can change after it is written, which is
// the entire point (server/notelog.go).
export interface PageNote {
  id: string;
  body: string;
  author: string; // the account — verified
  agent?: string; // what an agent called itself — a claim
  label?: string;
  createdAt: string;
}

export interface ApiToken {
  id: string;
  name: string;
  scope: 'read' | 'write';
  workspaces: string[]; // empty = all the user's workspaces
  createdAt: string;
  lastUsedAt: string | null;
  lastUsedIp: string;
}

export type PropType =
  | 'text'
  | 'number'
  | 'select'
  | 'multiselect'
  | 'date'
  | 'checkbox'
  | 'checklist'
  | 'url'
  | 'person'
  | 'relation'
  | 'backrelation'
  | 'rollup'
  | 'formula';

/** One sub-task of a checklist property. Progress is derived from these — a
    stored percentage would be a second truth to keep in sync. */
export interface ChecklistItem {
  id: string;
  text: string;
  done: boolean;
}

export interface PropOption {
  id: string;
  name: string;
  color: string;
}

export interface PropDef {
  id: string;
  name: string;
  type: PropType;
  options?: PropOption[];
  // relation: which collection this links to
  relationCollection?: string;
  // rollup: aggregate a target property over a relation
  rollupRelation?: string;
  rollupTarget?: string;
  rollupAgg?: 'sum' | 'count' | 'avg' | 'min' | 'max' | 'percent';
  // rollup: count/sum only the related rows meeting this condition — the
  // difference between "how many tasks" and "how many are done".
  rollupWhereProp?: string;
  rollupWhereOp?: 'is' | 'is_not' | 'is_empty' | 'is_not_empty' | 'contains';
  rollupWhereValue?: string;
  // Several values, for is / is_not. "Open" means neither done NOR discarded,
  // and one comparison cannot say that. Empty falls back to rollupWhereValue,
  // so conditions written before this keep their meaning exactly.
  rollupWhereValues?: string[];
  // backrelation: the other side of a relation someone else declared. Computed
  // at read time, never stored — see backrelationIDs in derived.go.
  backrelationCollection?: string;
  backrelationProp?: string;
  // formula: expression over other props, {propId} references
  formula?: string;
  // number/rollup/formula: render a numeric value as a plain number (default),
  // a progress bar, or a ring. numberMax is the value that = 100% (default 100).
  numberDisplay?: 'plain' | 'bar' | 'ring';
  numberMax?: number;
}

export type FilterOp =
  | 'is'
  | 'is_not'
  | 'contains'
  | 'gt'
  | 'lt'
  | 'is_empty'
  | 'is_not_empty';

export interface Filter {
  property: string;
  op?: FilterOp; // default 'is'; legacy empty value = is_not_empty
  value: string;
}

export interface Sort {
  property: string;
  dir: 'asc' | 'desc';
}

export interface ViewDef {
  id: string;
  name: string;
  type: 'table' | 'board' | 'list' | 'gallery' | 'calendar' | 'form' | 'timeline';
  groupBy?: string;
  dateProp?: string; // calendar/timeline view: date property (timeline: start)
  endDateProp?: string; // timeline view: optional end-date property (else 1-day bar)
  hidden?: string[]; // property ids hidden in this view
  filters?: Filter[];
  sort?: Sort | null;
  formTitle?: string; // form view: heading above the form
  formDesc?: string; // form view: description under the heading
  formSubmit?: string; // form view: submit-button label
  subItemProp?: string; // table view: a self-relation prop whose value = child rows (renders a tree)
}

export interface CollectionConfig {
  schema: PropDef[];
  views: ViewDef[];
}

// Public form config served (unauthenticated) at /api/public/form/{token} —
// only the fillable field defs, never rows or the rest of the workspace.
export interface PublicFormConfig {
  title: string;
  icon: string;
  formTitle?: string;
  formDesc?: string;
  formSubmit?: string;
  schema: PropDef[];
}

// One item on the blueprint shelf (see server/library.go). Everything from
// `databases` down is READ OUT OF THE BLUEPRINT by the server, never written
// beside it — the preview built from this cannot promise something the
// blueprint does not contain.
export interface BlueprintEntry {
  id: string;
  title: string;
  tagline: string;
  icon: string;
  accent: string;
  tags: string[];
  price: string; // empty = free
  source: string; // 'built-in'
  databases: BlueprintDatabase[];
  rules: string;
}

export interface BlueprintDatabase {
  title: string;
  icon: string;
  description: string;
  props: BlueprintProp[];
  views: { name: string; type: string }[];
}

export interface BlueprintProp {
  name: string;
  type: string;
  options?: { name: string; color?: string }[];
}

// A connection an agent signed in for (server/oauth_provider.go). The counterpart
// to an API token, except it expires on its own and can be ended from here.
export interface OAuthGrant {
  id: string;
  clientName: string;
  scope: string;
  workspaces: string;
  createdAt: string;
  lastUsedAt: string | null;
  lastUsedIp: string;
}
