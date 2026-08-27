/**
 * Identifiers are UUID strings, not numbers (design P17). They used to be integers and the
 * declarations here said so long after the API stopped returning them, which cost nothing at
 * runtime — JSON hands over whatever it hands over — and would have cost the first person who
 * did arithmetic on one.
 *
 * JSON shapes returned by the management API (`backend/internal/httpapi/admin_*.go`).
 * Kept here, shared across the feature screens that all read pieces of the same inventory —
 * see admin.go's comment on `projectView`/`loadBalancerView`/etc for why these are hand-written
 * views rather than 1:1 with the Go models: camelCase, and only what the console needs.
 */

export interface LBNode {
  id: string;
  loadBalancerId: string;
  label: string;
  ipv4: string | null;
  ipv6: string | null;
}

/**
 * One publication seen through to its end: `GET /loadbalancers/:id/interactions`.
 *
 * `outcome` is derived by the service, not stored, so it is correct for a row that is still open
 * while being read: validated · unqueried · live · pending.
 */
export interface Interaction {
  id: string;
  aliasLabel: string;
  challengeName: string;
  domainNames: string[];
  keyId: string | null;
  clientIp: string | null;
  fqdnForm: 'alias' | 'challenge';
  publishedAt: string;
  withdrawnAt: string | null;
  firstQueriedAt: string | null;
  lastQueriedAt: string | null;
  queryCount: number;
  outcome: 'validated' | 'unqueried' | 'live' | 'pending';
}

export interface Domain {
  id: string;
  loadBalancerId: string;
  name: string;
  challengeName: string;
  aliasId: string;
  aliasLabel: string;
  createdAt: string;
}

/** Body of `POST /loadbalancers/:id/domains`. */
export interface DomainCreateRequest {
  name: string;
  /**
   * Also create the name's counterpart — the wildcard of a plain name, the base name of a
   * wildcard. Omitted means false: the pair is never created unasked (design B4), and the
   * endpoint decodes strictly, so nothing else may be sent.
   */
  includeWildcard?: boolean;
}

/**
 * Response of `POST /loadbalancers/:id/domains` — the submitted domain plus what the create
 * call learned. `domains` is every domain the request created: the submitted name alone, or it
 * followed by its counterpart when `includeWildcard` was set. Both share one alias.
 */
export interface DomainCreated extends Domain {
  domains: Domain[];
  aliasCreated: boolean;
  warnings: string[];
}

export interface LoadBalancer {
  id: string;
  projectId: string;
  slug: string;
  name: string;
  createdAt: string;
  /**
   * What this caller may do to THIS object, answered by the service per row.
   *
   * Controls are rendered from it instead of from the caller's role name, which said nothing
   * about which objects. It is not the enforcement point — every request is checked again when
   * it arrives — so a stale or tampered value costs a refusal, not access.
   */
  permittedActions: string[];
  nodes: LBNode[];
  domains?: Domain[];
}

export interface Project {
  id: string;
  slug: string;
  name: string;
  createdAt: string;
  /** What this caller may do to this project; see LoadBalancer.permittedActions. */
  permittedActions: string[];
  loadBalancers?: LoadBalancer[];
}

export interface ApiKey {
  id: string;
  /**
   * What the key may do, and to what. A key used to belong to one load balancer and its authority
   * WAS that foreign key; it is a subject holding grants now, anchored to the account, so the only
   * honest way to show what it can reach is to show them (task 9.5).
   */
  grants: KeyGrant[];
  identifier: string;
  displayPrefix: string;
  note: string;
  networks: string[];
  createdAt: string;
  expiresAt: string | null;
  lastUsedAt: string | null;
  disabledAt: string | null;
  active: boolean;
}

/** One grant a key holds, as the service renders it. */
export interface KeyGrant {
  type: string;
  objectId?: string | null;
  action: string;
  inheritable: boolean;
  /** The grant written the way the design writes them, so a listing needs no assembly. */
  summary: string;
}

/** Response of create/rotate — the one and only time `secret` is ever sent. */
export interface ApiKeyCreated {
  key: ApiKey;
  secret: string;
  notice: string;
}

export interface PlanRecord {
  name: string;
  ttl: number;
  type: 'A' | 'AAAA' | 'CNAME' | 'CAA';
  value: string;
}

export interface DNSPlan {
  projectSlug: string;
  projectName: string;
  loadBalancerSlug: string;
  loadBalancerName: string;
  records: PlanRecord[];
  zone: string;
}

/**
 * `status` is one of four, not two (design D5 / D11): `correct`, `missing`, `incorrect` (with
 * `found` set to what was actually there), or `undetermined`. CAA additionally uses
 * `not-published`, which is the *good* state for CAA — absence blocks nothing — and must not be
 * rendered as an error.
 */
export type CheckStatus = 'correct' | 'missing' | 'incorrect' | 'undetermined' | 'not-published';

export interface CheckResult {
  name: string;
  type: string;
  expected: string[];
  status: CheckStatus;
  found?: string;
}

export interface VerificationResult {
  resolver: string;
  results: CheckResult[];
}

export interface ChallengeEntry {
  id: string;
  aliasLabel: string;
  challengeName: string;
  value: string;
  publishedBy: string | null;
  createdAt: string;
  expiresAt: string;
  ageSeconds: number;
}

export interface AuditEntry {
  id: string;
  occurredAt: string;
  actorSubject: string | null;
  actorName: string | null;
  actorGroups: string[];
  actorRoles: string[];
  actorSource: string;
  requestId: string | null;
  clientIp: string | null;
  action: string;
  objectType: string;
  objectId: string | null;
  objectLabel: string;
  projectId: string | null;
  before?: unknown;
  after?: unknown;
}

/** The actions the service reports per object. Strings, because the vocabulary is the service's. */
export const CAN_VIEW = 'inventory.view';
export const CAN_WRITE = 'inventory.write';
export const CAN_ADMIN_PROJECT = 'project.admin';
export const CAN_WRITE_KEYS = 'key.write';
export const CAN_READ_PLAN = 'dns.plan';

/** Whether an object's reported actions include one. */
export function may(o: { permittedActions?: string[] } | null | undefined, action: string): boolean {
  return o?.permittedActions?.includes(action) ?? false;
}

/**
 * What a control says when the caller may not use it.
 *
 * A PERMISSION DISABLES A CONTROL; IT DOES NOT HIDE ONE. Hiding is the tempting
 * default and it is worse in both directions. Someone who cannot do a thing
 * learns nothing from its absence — they cannot tell an unavailable feature from
 * one they lack the rights for, so the question that reaches support is "where is
 * the button" rather than "who can give me this". And someone comparing two
 * accounts sees two different-shaped screens with no way to know why.
 *
 * A disabled control with a reason answers both: the capability exists, here is
 * where it lives, and you need a grant for it.
 *
 * This is presentation only. The server refuses the action regardless of what the
 * console renders — a disabled button is a courtesy, never the enforcement.
 */
export const NOT_PERMITTED = 'Your permissions do not allow this.';
