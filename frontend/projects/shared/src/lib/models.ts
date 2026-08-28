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
  /**
   * Activation, as two timestamps and nothing else (design domain-activation A1).
   *
   * There is no `active` flag beside them on purpose. Active IS `lastSuccessAt !== null`, and a
   * boolean sent alongside would be a second source of truth for one fact — the first bug being
   * the row where the two disagree.
   *
   * Three states are readable here, and the console has to tell them apart:
   *
   *   - `lastSuccessAt` null — we have never seen this domain's delegation. Challenges for it are
   *     refused. This is the normal state of a domain added five minutes ago, not a fault.
   *   - `lastCheckedAt` at or before `lastSuccessAt` — the last look found it. Everything is fine.
   *   - `lastCheckedAt` after `lastSuccessAt` — we looked and did not find it. The domain stays
   *     active regardless: activation is never reversed by a lookup (A3), so this is information
   *     about renewals that will fail, never a countdown to being switched off.
   *
   * `lastCheckedAt` null means we have not looked yet — including every domain that predates the
   * feature, which the migration activated without pretending to have checked.
   */
  lastCheckedAt: string | null;
  lastSuccessAt: string | null;
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

/**
 * The delegation's five states, on an axis of presence (design D2).
 *
 * `not-checked` is the one the checker cannot produce: it means the deployment configured no
 * resolver, or the answer has not arrived yet. We are the ones who did not look, so it is never
 * a finding about the customer's DNS (design D4).
 *
 * `missing` and `incorrect` stay apart because their fixes have nothing in common — one sends
 * somebody to add a record, the other to a record they already published and therefore believe
 * is done.
 */
export type DelegationStatus = 'not-checked' | 'missing' | 'incorrect' | 'correct' | 'undetermined';

/**
 * CAA's four states, on an axis of permission — plus `not-checked` for the same reason as above.
 *
 * This is deliberately NOT `DelegationStatus` (design D3). CAA answers "who may issue", not "is
 * something published", and `not-published` — by far the commonest state — is entirely correct:
 * an absent CAA record lets anyone issue. Sharing one union with the delegation is how
 * `not-published` would come to be rendered as "not configured", warning nearly every customer
 * and burying `blocking`, the one state here that actually stops issuance.
 */
export type CAAStatus = 'not-checked' | 'correct' | 'not-published' | 'blocking' | 'undetermined';

/** One record type's answer for one domain. `expected` is that domain's own value, not a sample. */
export interface RecordCheck<S> {
  name: string;
  status: S;
  expected?: string[];
  found?: string;
}

/**
 * One domain's answer, one field per record type.
 *
 * `caa` is absent, rather than carrying some status, when the plan holds no CAA row for this
 * domain: with no issuer configured there is nothing to authorise and so nothing to say.
 */
export interface DomainCheck {
  domainId: string;
  name: string;
  challengeName: string;
  aliasId: string;
  delegation: RecordCheck<DelegationStatus>;
  caa?: RecordCheck<CAAStatus>;
}

/**
 * `results` is the whole plan, one row per question asked of DNS — what the manual "Run
 * verification" control shows. `domains` is the same answers redistributed onto the rows a
 * customer reads, and the join is done by the server: a wildcard and its apex share one
 * challenge name and therefore one delegation, which no name match in the console could
 * reconstruct without reporting one of the two as absent.
 */
export interface VerificationResult {
  resolver: string;
  results: CheckResult[];
  domains: DomainCheck[];
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

/**
 * What a control says when the capability is not part of this KIND of account.
 *
 * A second sentence rather than a second use of NOT_PERMITTED, because the two
 * refusals have different remedies and the wrong one sends a person somewhere
 * there is nobody to ask (design C11). "Your permissions do not allow this" told
 * to the owner of a personal account sends them looking for whoever can grant
 * the permission — and in an account of one, that is themselves, and no grant
 * exists to be given.
 *
 * The navigation is where a missing capability is normally expressed at all
 * (C11): a screen a personal account does not have is not listed, and `Upgrade`
 * carries the whole message once. This string is for the places a control is
 * reached anyway — a deep link, or a control on a screen both kinds share.
 */
export const NOT_IN_PERSONAL_ACCOUNT =
  'A personal account does not have this. A team account does.';

/** The two kinds of account (design C1). Fixed at creation; neither becomes the other. */
export type AccountKind = 'personal' | 'team';

/**
 * One account this person may act in, as `GET /api/v1/accounts` reports it —
 * `accountView` in `backend/internal/httpapi/admin_accounts.go`.
 *
 * It lives here rather than beside the console's switcher because the staff
 * plane describes the same object: kind, owner and balance are what an account
 * IS now, not what the customer console happens to render.
 */
export interface Account {
  id: string;
  slug: string;
  name: string;
  createdAt: string;
  /** Which one this session is acting in, so the switcher needs no second request. */
  selected: boolean;
  kind: AccountKind;
  /** Whether the reader is the person this account answers to — the only person who may pay. */
  owner: boolean;
  /** Always zero for a personal account; being free is what it is (design C3). */
  credits: number;
  /** The negation of the service's one usable() predicate (C3, C6), not a second reading of it. */
  suspended: boolean;
  /**
   * Which suspension, in a sentence: only one of the two is lifted by paying
   * (design C6). Empty when the account is usable.
   */
  suspendedReason?: string;
}

/**
 * What an account is CALLED in the interface.
 *
 * A personal account is "Personal", never its stored name. That name is derived from its owner's
 * address, so rendering it puts the signed-in person's identity beside a user menu that already
 * says it — the same word twice, a centimetre apart, one of them pretending to be the name of a
 * workspace somebody chose. "Personal" is also what a person calls that account.
 *
 * It lives in `shared` rather than beside either screen because BOTH the picker and the switcher
 * render it, and the two would otherwise be one edit away from disagreeing about what an account is
 * called — which is the failure a person notices immediately, because those two controls sit on the
 * same page.
 *
 * A team account keeps its own name: somebody chose it, and it is how they tell several apart.
 */
export function accountLabel(a: Pick<Account, 'kind' | 'name'>): string {
  return a.kind === 'personal' ? 'Personal' : a.name;
}

/**
 * The icon that goes with `accountLabel`.
 *
 * A person for the account that IS one person's own, a GROUP for one that has other people in it.
 * That pairing is the whole distinction between the two kinds, drawn in one glyph: a team account
 * is not a bigger personal account, it is one with a roster.
 *
 * It was `cluster`, which draws connected nodes — infrastructure, not people. On a page whose other
 * icon is a person, it read as a different axis entirely.
 *
 * Paired with the label here so a screen cannot render one rule's word beside the other's glyph.
 * Any icon named here must also be REGISTERED in each console's app.config: an unregistered
 * `nz-icon` throws, and it surfaces as a broken screen rather than a missing glyph.
 */
export function accountIcon(a: Pick<Account, 'kind'>): string {
  return a.kind === 'personal' ? 'user' : 'team';
}

/**
 * The balance below which the console warns (task 6.10, design C4).
 *
 * The refusal at zero is soft — renewals stop and the certificate on the wire
 * keeps working for weeks — so the warning is what actually protects a customer,
 * and it has to arrive before the balance is gone rather than with it.
 *
 * It is the smallest amount a checkout offers, deliberately: "less than one
 * top-up left" is a threshold a person can act on, and it needs no second number
 * to be agreed anywhere. A real threshold belongs with the ledger that spends
 * the balance (`metering-and-credits`), which can say how long the remainder
 * lasts; until something spends credit, nothing here can.
 */
export const LOW_BALANCE = 25;
