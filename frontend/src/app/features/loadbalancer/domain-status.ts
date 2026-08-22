import { CheckResult } from '../models';

/**
 * A domain has never validated when no verification result touches its address or CNAME
 * records yet, or reads `ok` only when every one of them is `correct`. CAA is intentionally
 * excluded: it is shared across every domain on the registrable name, and its inverted
 * "absent is fine" reading belongs to the plan table as a whole, not to one domain's status.
 */
export function domainStatus(
  d: { name: string; challengeName: string },
  results: readonly CheckResult[] | null,
): 'ok' | 'problem' | 'never' {
  if (!results) return 'never';
  const relevant = results.filter((r) => r.name === d.name || r.name === d.challengeName);
  if (relevant.length === 0) return 'never';
  return relevant.every((r) => r.status === 'correct') ? 'ok' : 'problem';
}
