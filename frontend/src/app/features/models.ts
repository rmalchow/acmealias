/**
 * JSON shapes returned by the management API (`backend/internal/httpapi/admin_*.go`).
 * Kept here, shared across the feature screens that all read pieces of the same inventory —
 * see admin.go's comment on `projectView`/`loadBalancerView`/etc for why these are hand-written
 * views rather than 1:1 with the Go models: camelCase, and only what the console needs.
 */

export interface LBNode {
  id: number;
  loadBalancerId: number;
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
  id: number;
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
  id: number;
  loadBalancerId: number;
  name: string;
  challengeName: string;
  aliasId: number;
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
  id: number;
  projectId: number;
  slug: string;
  name: string;
  createdAt: string;
  nodes: LBNode[];
  domains?: Domain[];
}

export interface Project {
  id: number;
  slug: string;
  name: string;
  createdAt: string;
  loadBalancers?: LoadBalancer[];
}

export interface ApiKey {
  id: number;
  loadBalancerId: number;
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
  id: number;
  aliasLabel: string;
  challengeName: string;
  value: string;
  publishedBy: string | null;
  createdAt: string;
  expiresAt: string;
  ageSeconds: number;
}

export interface AuditEntry {
  id: number;
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
  objectId: number | null;
  objectLabel: string;
  projectId: number | null;
  before?: unknown;
  after?: unknown;
}
