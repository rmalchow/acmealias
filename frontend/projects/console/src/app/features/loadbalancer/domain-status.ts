import { CAAStatus, DelegationStatus, DomainCheck, VerificationResult } from '@acme/shared';

/**
 * What a domain's row says about its records.
 *
 * This used to reduce a domain to one of three verdicts, reverse-engineering which results
 * belonged to it by matching names. Two collapses lived in that: `missing` and `incorrect` both
 * read as "not verified", and `undetermined` — our lookup failing — was shown to the customer as
 * their fault. Both are undone here (design D2, D4), and the association is no longer guessed:
 * the server answers per domain.
 *
 * CAA used to be excluded from a domain's verdict altogether, and that exclusion was right for a
 * single verdict — CAA is shared across every host on a registrable name, and its "absent is
 * fine" reading is the opposite of a CNAME's. It is reported here rather than hidden, because it
 * is reported on its own axis: what the record set PERMITS, never whether it exists (design D3).
 */

/** A chip: what is true, coloured by what it means, with a sentence for the tooltip. */
export interface StatusChip {
  label: string;
  colour: string;
  tooltip: string;
}

/**
 * This domain's answer, or null when none has arrived.
 *
 * Keyed by domain id and not by name: a wildcard and its apex share one challenge name, so the
 * server sends each of them a row of its own carrying the state of the one delegation between
 * them. Matching on names here would put that answer on one row and leave the other reading
 * "absent" for a record that is published and correct.
 */
export function domainCheckFor(domainId: string, v: VerificationResult | null): DomainCheck | null {
  return v?.domains?.find((d) => d.domainId === domainId) ?? null;
}

/**
 * The five delegation states (design D2). `not configured` and `configured incorrectly` are both
 * red because both mean no certificate, but they never share a word: one is a record to add, the
 * other a record that exists and points at the wrong place.
 */
export function delegationChip(status: DelegationStatus, hasAlias = true): StatusChip {
  switch (status) {
    case 'correct':
      return {
        label: 'configured correctly',
        colour: 'green',
        tooltip: 'The CNAME is published and points at this domain’s alias. Nothing to do.',
      };
    case 'missing':
      return {
        label: 'not configured',
        colour: 'red',
        tooltip: 'Nothing resolves at the challenge name. The CNAME has not been published yet.',
      };
    case 'incorrect':
      return {
        label: 'configured incorrectly',
        colour: 'red',
        tooltip:
          'A record is published at the challenge name, but it points somewhere other than this domain’s alias. It will never validate.',
      };
    case 'undetermined':
      return {
        label: 'could not check',
        colour: 'default',
        // Never "you", never a fault: the lookup is ours (design D4).
        tooltip: 'The lookup failed on our side or the resolver’s. This says nothing about your DNS.',
      };
    default:
      return {
        label: 'not checked',
        colour: 'default',
        // A domain whose alias has gone missing has no target, so nothing is ever asked about it
        // and this chip would otherwise sit at "not checked" forever — the one state that can
        // stick permanently while reading as transient. Same label, because a sixth state would
        // break the five (design D2); a different sentence, because "we have not got round to it"
        // and "there is nothing to ask you for" are not the same claim, and the second one is ours.
        tooltip: hasAlias
          ? 'We have not looked yet. This is not a statement about your DNS.'
          : 'This domain has no alias yet, so there is nothing to ask DNS about. The alias is issued on our side, so this is ours to fix rather than yours.',
      };
  }
}

/**
 * CAA by what it permits (design D3).
 *
 * `not-published` is the commonest state and is entirely correct — absent CAA means anyone may
 * issue — so it is neutral blue and reads as an absence of restriction, never as an absence of
 * configuration. `blocking` is the only bad one, and it stops issuance no matter how perfect the
 * delegation is.
 */
export function caaChip(status: CAAStatus): StatusChip {
  switch (status) {
    case 'correct':
      return {
        label: 'authorises us',
        colour: 'green',
        // Not "allowed" or "permitted": those are the words the console uses for what a CALLER
        // may do (NOT_PERMITTED renders in the same row), and a customer reading both in one
        // table should never have to work out which of the two a chip is about.
        tooltip: 'A CAA record set exists and names our issuer among the authorities it authorises.',
      };
    case 'not-published':
      return {
        label: 'no restriction',
        colour: 'blue',
        tooltip:
          'No CAA record is published for this name, which is the default state of the internet: any authority may issue. Nothing to do.',
      };
    case 'blocking':
      return {
        label: 'blocks us',
        colour: 'red',
        tooltip:
          'A CAA record set exists and does not name our issuer, so issuance is refused however the delegation is set up.',
      };
    case 'undetermined':
      return {
        label: 'could not check',
        colour: 'default',
        tooltip: 'The CAA lookup failed on our side or the resolver’s. This says nothing about your DNS.',
      };
    default:
      return {
        label: 'not checked',
        colour: 'default',
        tooltip: 'We have not looked yet. This is not a statement about your DNS.',
      };
  }
}
