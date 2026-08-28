import { CAAStatus, DelegationStatus, Domain, DomainCheck, VerificationResult } from '@acme/shared';

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

/**
 * Whether we will publish challenges for this domain at all, from the two timestamps the service
 * stores (design domain-activation A1).
 *
 * This is a different question from the delegation chip above, and the two sit side by side
 * because they are answered by different things at different moments. The delegation chip is a
 * LIVE lookup made when the screen opened: it says what DNS says right now. This one is the
 * service's own record of what it has seen out of band, and it is the thing `/present` actually
 * consults — a domain can read "configured correctly" and still be waiting, for the few minutes
 * between publishing the record and the poller noticing it. Showing only the live answer would
 * make that gap look like a bug in us.
 *
 * WAITING IS NOT BROKEN (task 4.1). A domain added five minutes ago has never been seen, and the
 * customer is at that moment in the middle of publishing the record. It is blue and phrased as an
 * expectation, never red and never the word "failed".
 *
 * Nothing here ever implies the domain will be switched off. It cannot be: activation is one-way
 * (A3), and a delegation that has stopped resolving costs renewals, not the domain.
 */
export function activationChip(d: Domain): StatusChip {
  if (!d.lastSuccessAt) {
    return {
      label: 'waiting for delegation',
      colour: 'blue',
      tooltip:
        'We have not seen this domain’s CNAME yet, so certificates for it cannot be issued. ' +
        'Publish the record and we will pick it up on our own — we keep looking and never give up.',
    };
  }
  // Parsed rather than compared as strings: the service sends RFC 3339, which admits a numeric
  // offset as well as `Z` and a variable number of fractional digits, so `>` on the text would
  // put a domain into the wrong state whenever the two stamps were written in different
  // representations. Two Date parses per row is not a cost worth being clever about.
  if (d.lastCheckedAt && Date.parse(d.lastCheckedAt) > Date.parse(d.lastSuccessAt)) {
    return {
      label: 'not resolving',
      // Orange, not red: this domain still works in every sense we control. Something in DNS
      // changed and its next renewal will fail, which is worth saying loudly and is not a failure
      // of the domain's setup here.
      colour: 'orange',
      tooltip:
        'This domain is active and stays active — we never switch one off. But the last time we ' +
        'looked, its CNAME did not resolve, so renewals will fail until it does again.',
    };
  }
  return {
    label: 'active',
    colour: 'green',
    tooltip: 'We have seen this domain’s delegation, so challenges for it are published.',
  };
}
