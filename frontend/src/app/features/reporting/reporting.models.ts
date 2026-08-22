/**
 * JSON shapes of the reporting endpoints (`backend/internal/httpapi/reporting.go`).
 *
 * Kept here rather than in `features/models.ts` because the vocabulary only makes sense
 * together with the components that render it: two different measures that must never be
 * added up or swapped, and a `recorded` flag that is not a count.
 */

/** The five derived outcomes, exactly as the service reports them. */
export type Outcome = 'validated' | 'live' | 'pending' | 'unqueried' | 'abandoned';

export interface UsageBucket {
  start: string;
  /** One published TXT value each. A wildcard certificate publishes two. */
  challengeAttempts: Record<Outcome, number>;
  /** Overlapping publications at one challenge name, grouped — one per certificate request. */
  issuanceAttempts: Record<Outcome, number>;
  challengeAttemptsTotal: number;
  issuanceAttemptsTotal: number;
}

export interface UsageResponse {
  projectId: number;
  from: string;
  to: string;
  bucket: 'day' | 'month';
  outcomes: Outcome[];
  measures: Record<string, string>;
  /**
   * False when this project has no recorded history AT ALL — not "none in this range".
   * The buckets are then zeroes that must not be shown as zeroes: history cannot be
   * backfilled, so a service deployed last week cannot substantiate a zero for last month,
   * and a fresh deployment reporting "0 renewals" looks like a broken one.
   */
  recorded: boolean;
  buckets: UsageBucket[];
}

export interface InventoryPoint {
  day: string;
  aliasCount: number;
  domainCount: number;
  loadBalancerCount: number;
}

export interface InventoryHistoryResponse {
  projectId: number;
  from: string;
  to: string;
  /** False when no daily snapshot has ever been written for this project. Not a zero. */
  recorded: boolean;
  days: InventoryPoint[];
  /** Read live, so a domain added a minute ago is already in it. */
  current: InventoryPoint;
  measures: Record<string, string>;
}
