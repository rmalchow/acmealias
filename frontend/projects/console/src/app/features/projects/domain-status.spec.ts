import { caaChip, certificateChip, delegationChip, domainCheckFor } from './domain-status';
import { CAAStatus, CertificateBand, DelegationStatus, DomainCheck, VerificationResult } from '@acme/shared';

const apex = { id: 'd-apex', challengeName: '_acme-challenge.example.com' };
const wildcard = { id: 'd-star', challengeName: '_acme-challenge.example.com' };

function check(over: Partial<DomainCheck>): DomainCheck {
  return {
    domainId: apex.id,
    name: 'example.com',
    challengeName: apex.challengeName,
    aliasId: 'a-1',
    delegation: { name: apex.challengeName, status: 'correct', expected: ['example-com.svc.example.net'] },
    ...over,
  };
}

function verification(domains: DomainCheck[]): VerificationResult {
  return { resolver: '10.0.0.1:53', domains };
}

describe('domainCheckFor', () => {
  it('gives a wildcard and its apex the one delegation they share', () => {
    // One alias is issued per challenge name, so these two domains have one CNAME between them.
    // Both rows must read it; neither may report a published record as absent.
    const v = verification([
      check({ domainId: apex.id, name: 'example.com' }),
      check({ domainId: wildcard.id, name: '*.example.com' }),
    ]);
    expect(domainCheckFor(apex.id, v)?.delegation.status).toBe('correct');
    expect(domainCheckFor(wildcard.id, v)?.delegation.status).toBe('correct');
  });

  it('is null before any verification has arrived', () => {
    expect(domainCheckFor(apex.id, null)).toBeNull();
  });

  it('is null for a domain the answer says nothing about', () => {
    expect(domainCheckFor('d-other', verification([check({})]))).toBeNull();
  });
});

describe('delegationChip', () => {
  it('keeps an absent record and a wrong one apart', () => {
    // Their fixes have nothing in common: one is a record to add, the other a record that
    // already exists and points at the wrong target (design D2).
    expect(delegationChip('missing').label).not.toBe(delegationChip('incorrect').label);
    expect(delegationChip('missing').label).toBe('not configured');
    expect(delegationChip('incorrect').label).toBe('configured incorrectly');
  });

  it('is yellow for an absent record and red for a wrong one', () => {
    // The inversion of what domain-setup-feedback shipped, and the reason is not severity
    // (design G4). `missing` is the state every new domain passes through while somebody is
    // mid-task; `incorrect` is a record that looks finished and will never work.
    expect(delegationChip('missing').colour).toBe('orange');
    expect(delegationChip('missing').colour).not.toBe('red');
    expect(delegationChip('incorrect').colour).toBe('red');
    expect(delegationChip('correct').colour).toBe('green');
  });

  it('renders all five states, none of them as another', () => {
    const all: DelegationStatus[] = ['not-checked', 'missing', 'incorrect', 'correct', 'undetermined'];
    const labels = all.map((s) => delegationChip(s).label);
    expect(new Set(labels).size).toBe(5);
  });

  it('says a failed lookup is ours, not the customer’s fault', () => {
    const chip = delegationChip('undetermined');
    expect(chip.label).toBe('could not check');
    expect(chip.colour).not.toBe('red');
    expect(chip.tooltip).toContain('says nothing about your DNS');
  });

  it('does not alarm on a state nobody has looked at', () => {
    const chip = delegationChip('not-checked');
    expect(chip.colour).toBe('default');
    expect(chip.tooltip).toContain('not a statement about your DNS');
  });

  it('says why, when a domain with no alias will never be checked at all', () => {
    // Nothing is ever asked about a domain whose alias has gone missing, so this is the one
    // state that can sit still forever while reading as transient. Same label — a sixth state
    // would break the five — and a sentence that puts the omission on us (design D2, D4).
    const chip = delegationChip('not-checked', false);
    expect(chip.label).toBe('not checked');
    expect(chip.tooltip).toContain('no alias yet');
    expect(chip.tooltip).toContain('ours to fix');
  });
});

describe('caaChip', () => {
  it('gives every outcome its own sentence', () => {
    const all: CAAStatus[] = ['not-checked', 'correct', 'unconstrained', 'not-published', 'blocking', 'undetermined'];
    const tooltips = all.map((s) => caaChip(s).tooltip);
    expect(new Set(tooltips).size).toBe(6);
  });

  it('reads an absent CAA record as the least bounded state, not the best one', () => {
    // THIS REVERSES the chip's previous verdict, which was green on the grounds that absence lets
    // anyone issue and therefore lets us. That is the wrong question for a customer who has
    // delegated their challenge name to this service: "anyone may issue" then includes us, and
    // saying "no restriction — nothing to do" was telling them the opposite of what was true.
    const chip = caaChip('not-published');
    expect(chip.colour).toBe('red');
    expect(chip.label).toBe('no CAA published');
  });

  it('separates a record that authorises everybody from one bound to an account', () => {
    // The state most customers who have thought about CAA at all are in, and a real finding
    // rather than a nag: the fix is one parameter on a line they already publish.
    expect(caaChip('unconstrained').colour).toBe('orange');
    expect(caaChip('correct').colour).toBe('green');
    expect(caaChip('unconstrained').tooltip).toContain('accounturi');
    expect(caaChip('undetermined').colour).toBe('default');
  });

  it('never claims the pinned account is the customer\'s', () => {
    // Nothing in DNS says whose an account URI is. Green means "bound to one account", and a
    // chip asserting whose would be asserting something it cannot see.
    const chip = caaChip('correct');
    expect(chip.label).toBe('bound to one account');
    expect(chip.tooltip).not.toContain('your account');
  });

  it('presents a CAA set that excludes us as blocking', () => {
    const chip = caaChip('blocking');
    expect(chip.colour).toBe('red');
    expect(chip.label).toBe('blocks us');
  });

  it('never borrows the console’s word for what a CALLER may do', () => {
    // "permitted" and "allowed" belong to the permission model, whose refusal renders in the
    // same table row. A chip about who may ISSUE must not be read as a chip about who may act.
    for (const s of ['correct', 'not-published', 'blocking', 'undetermined'] as CAAStatus[]) {
      const chip = caaChip(s);
      expect(`${chip.label} ${chip.tooltip}`.toLowerCase()).not.toContain('permitted');
      expect(`${chip.label} ${chip.tooltip}`.toLowerCase()).not.toContain('allowed');
    }
  });

  it('never borrows the delegation’s vocabulary', () => {
    const caa: CAAStatus[] = ['correct', 'not-published', 'blocking'];
    const delegation: DelegationStatus[] = ['correct', 'missing', 'incorrect'];
    const spoken = new Set(delegation.map((s) => delegationChip(s).label));
    for (const s of caa) expect(spoken.has(caaChip(s).label)).toBeFalse();
  });
});

describe('an unconfigured resolver', () => {
  it('leaves every row not checked, with no failure anywhere', () => {
    // The route answers 200 with an empty resolver rather than 503, so nothing here is an error
    // (design D4). The chips say we did not look; they do not say the customer is broken.
    const v: VerificationResult = {
      resolver: '',
      domains: [
        check({ domainId: apex.id, delegation: { name: apex.challengeName, status: 'not-checked' } }),
        check({
          domainId: wildcard.id,
          delegation: { name: wildcard.challengeName, status: 'not-checked' },
          caa: { name: 'example.com', status: 'not-checked' },
        }),
      ],
    };
    for (const id of [apex.id, wildcard.id]) {
      const chip = delegationChip(domainCheckFor(id, v)!.delegation.status);
      expect(chip.label).toBe('not checked');
      expect(chip.colour).toBe('default');
    }
    expect(caaChip(domainCheckFor(wildcard.id, v)!.caa!.status).colour).toBe('default');
  });
});

describe('certificateChip', () => {
  it('takes the band from the service and computes no threshold', () => {
    // The edges — 28 days and 7 — live in internal/certobserve and reach us banded (design G9).
    // A console applying its own would disagree with adminctl the first time one was updated.
    expect(certificateChip('healthy').colour).toBe('green');
    expect(certificateChip('approaching').colour).toBe('orange');
    expect(certificateChip('urgent').colour).toBe('red');
    expect(certificateChip('expired').colour).toBe('red');
  });

  it('is red for a certificate that does not cover the name asked for', () => {
    // The most valuable red in the change: "issued and never deployed", "deployed to two nodes
    // out of four", "covers the names you had last month" — all reported as success everywhere
    // else in the product.
    expect(certificateChip('uncovered').colour).toBe('red');
  });

  it('is GREY for a handshake that failed, never red (task 7.7)', () => {
    // The one place our own probe's failure meets a colour scale. Red would claim the customer's
    // node is broken when we cannot tell that apart from our transit or their firewall.
    const chip = certificateChip('unexamined');
    expect(chip.colour).toBe('default');
    expect(chip.colour).not.toBe('red');
    expect(chip.tooltip).toContain('says nothing about whether your node is working');
  });

  it('says nothing has been observed yet without implying a fault', () => {
    const chip = certificateChip('unobserved');
    expect(chip.colour).toBe('default');
    expect(chip.tooltip).not.toContain('failed');
  });

  it('gives every band its own sentence', () => {
    const all: CertificateBand[] = [
      'healthy',
      'approaching',
      'urgent',
      'expired',
      'uncovered',
      'unexamined',
      'unobserved',
    ];
    expect(new Set(all.map((b) => certificateChip(b).tooltip)).size).toBe(7);
  });
});
