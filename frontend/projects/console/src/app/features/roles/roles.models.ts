/** JSON shapes for roles, grants and membership (`backend/internal/httpapi/admin_accounts.go`). */

/**
 * One grant a role or key holds: [type, identifier, action] plus the inheritance flag.
 *
 * The identifier has exactly TWO forms — a wildcard (`objectId` absent) or one object id. A third,
 * a typed predicate, was carried as an always-null slot for a while after the DNS product it was
 * built for went; migration 000003 dropped the column, so nothing can produce one.
 */
export interface Grant {
  type: string;
  objectId?: string | null;
  action: string;
  inheritable: boolean;
  /** The grant written the way the design writes them, so a listing needs no assembly. */
  summary?: string;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  /**
   * The account's fixed role: every action on every object of that account. Uneditable and
   * undeletable, and always with at least one member — it is the way back into an account, so
   * nothing may take it away one edit at a time.
   */
  unrestricted: boolean;
  grants: Grant[];
}

export interface Member {
  id: string;
  userId: string;
  subject: string;
  displayName: string;
  roleId: string;
  roleName: string;
}

/** The wildcard, spelled once. */
export const ANY = '*';

/** The object types a grant can name. */
export const OBJECT_TYPES = [
  { value: ANY, label: 'Anything in this account' },
  { value: 'project', label: 'Project' },
  { value: 'load_balancer', label: 'Load balancer' },
  { value: 'lb_node', label: 'Node' },
  { value: 'domain', label: 'Domain' },
  { value: 'alias', label: 'Alias' },
  { value: 'api_key', label: 'API key' },
];

/** The actions a grant can confer, in the vocabulary the service authorizes against. */
export const ACTIONS = [
  { value: ANY, label: 'Everything' },
  { value: 'inventory.view', label: 'Read inventory' },
  { value: 'inventory.write', label: 'Write inventory' },
  { value: 'dns.plan', label: 'Read DNS plans and verification' },
  { value: 'key.write', label: 'Create and revoke API keys' },
  { value: 'key.secret.view', label: 'See a key secret once, at creation' },
  { value: 'challenge.publish', label: 'Publish challenge values' },
  { value: 'challenge.withdraw', label: 'Withdraw challenge values' },
];
