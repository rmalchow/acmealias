import { caaChip, delegationChip, domainCheckFor } from './domain-status';
import { CAAStatus, DelegationStatus, DomainCheck, VerificationResult } from '@acme/shared';

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
  return { resolver: '10.0.0.1:53', results: [], domains };
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
    const all: CAAStatus[] = ['not-checked', 'correct', 'not-published', 'blocking', 'undetermined'];
    const tooltips = all.map((s) => caaChip(s).tooltip);
    expect(new Set(tooltips).size).toBe(5);
  });

  it('does not present an absent CAA record as a problem', () => {
    // The commonest state, and entirely correct: absence means anyone may issue. A warning
    // nearly every customer sees is one everybody learns to ignore (design D3).
    const chip = caaChip('not-published');
    expect(chip.colour).not.toBe('red');
    expect(chip.label).toBe('no restriction');
    expect(chip.label).not.toContain('not configured');
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
      results: [],
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
