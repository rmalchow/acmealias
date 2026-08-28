import { CAAStatus, CertificateBand, DelegationStatus, Domain, DomainCheck, VerificationResult } from '@acme/shared';

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
 * What a DOT's tooltip says: the verdict, then the sentence behind it.
 *
 * A dot is labelled with the record TYPE and never with the verdict (design G3), so the word the
 * chip carries has nowhere else to go — and it is worth keeping, because "configured incorrectly"
 * ahead of the explanation is what makes a tooltip readable at a glance rather than a paragraph
 * to parse. The activation chip beside the dots still renders its label, which is why this is a
 * function here rather than a change to what the chips return.
 */
export function dotTitle(chip: StatusChip): string {
  return `${chip.label} — ${chip.tooltip}`;
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
 * The five delegation states (design D2), coloured by WHO IS IN THE MIDDLE OF WHAT (G4).
 *
 * `domain-setup-feedback` made `missing` and `incorrect` both red, on the grounds that both mean
 * no certificate. That is still true, and it is not what the colour is for. `missing` is
 * overwhelmingly somebody who added the domain five minutes ago and has not published the record
 * yet — mid-task, doing the right thing, and it is the state every new domain passes through, so
 * red tells them they have made a mistake they have not made. `incorrect` is a record that
 * exists, looks finished, and will never work: it does not self-heal, nobody is coming back to
 * it, and it costs a certificate six weeks later. That one is red.
 *
 * THE RISK IS STATED RATHER THAN MITIGATED: a domain sitting at yellow for three weeks is
 * genuinely broken and yellow understates it. What catches that is the activation chip beside
 * these dots, which reads "waiting for delegation · Not looked at yet" and does not soften with
 * time. Ageing yellow into red after some interval is a change to this one function, and is not
 * made now because the interval would be invented rather than measured.
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
        colour: 'orange',
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
 * issue — so it reads as an absence of restriction, never as an absence of configuration.
 * `blocking` is the only bad one, and it stops issuance no matter how perfect the delegation is.
 *
 * It was BLUE, and blue was the best a chip could do: a chip carries a word, and `correct` and
 * `not-published` are not the same fact, so the two could not share a colour without the label
 * lying about one of them. A DOT carries no word. The two states keep their own sentences in the
 * tooltip — "authorises us", "no restriction" — and share the green, because they are the same
 * INSTRUCTION, which is do nothing, and that is what a dot is for (G5). On a three-colour scale
 * blue means "unclear" to everybody who has not read the tooltip.
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
        colour: 'green',
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
 * The certificate dot, from the band the SERVICE computed (design G9).
 *
 * This switches on a string and computes nothing. The thresholds — 28 days and 7 — live in
 * `internal/certobserve` and reach the console banded, so that a console and an `adminctl` cannot
 * disagree about the same certificate because one of them was updated and the other was not.
 * NEVER band a `notAfter` here; there is no clock in this file for a reason.
 *
 * A FAILED HANDSHAKE IS GREY, NOT RED. It is the one place our own probe's failure meets a colour
 * scale, and red would claim the customer's node is broken when we cannot tell that apart from
 * our transit, their firewall dropping our source address, or a listener that is simply not on
 * 443 (G9). Grey says "we could not look", which is the only thing we know; the dialog carries
 * the address that failed, and the observation is recorded, so a run of failures shows up in the
 * history rather than as an absence.
 *
 * `uncovered` is red and is the most valuable red in this change: a certificate read perfectly
 * that does not cover the name we asked for is the "issued and never deployed" failure, the
 * "deployed to two nodes out of four" failure, and the "covers the names you had last month"
 * failure — every one of which the challenge history reports as a success.
 */
export function certificateChip(band: CertificateBand): StatusChip {
  switch (band) {
    case 'healthy':
      return {
        label: 'valid',
        colour: 'green',
        tooltip: 'Every address we reached served a certificate covering this name with more than 28 days left.',
      };
    // The upper edge is 28 days and not 30 because every ACME client renews at 30 by default: a
    // certificate with 29 days left has just been told to renew and has not yet, which is exactly
    // when this band starts being informative.
    case 'approaching':
      return {
        label: 'renewing soon',
        colour: 'orange',
        tooltip: 'A certificate has between 7 and 28 days left. Renewal is due and has not happened yet.',
      };
    case 'urgent':
      return {
        label: 'expires very soon',
        colour: 'red',
        tooltip: 'A certificate has less than 7 days left. Renewal should have happened by now.',
      };
    case 'expired':
      return {
        label: 'expired',
        colour: 'red',
        tooltip: 'A certificate we read has already expired. Visitors are seeing a browser warning.',
      };
    case 'uncovered':
      return {
        label: 'does not cover this name',
        colour: 'red',
        tooltip:
          'A certificate was served and does not include the name we asked for, so it is refused by every client whatever its remaining life.',
      };
    case 'unexamined':
      return {
        label: 'could not look',
        colour: 'default',
        // Never a verdict on their machine: we cannot tell a node that is down from our own
        // transit or a firewall that drops us (G9).
        tooltip:
          'We could not complete a handshake with at least one address. This says nothing about whether your node is working — open the dot for which address and how it failed.',
      };
    default:
      return {
        label: 'not observed yet',
        colour: 'default',
        tooltip: 'Nothing has been observed for this domain yet. The first pass runs within the observation interval.',
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
