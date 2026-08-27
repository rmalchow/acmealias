import { domainStatus } from './domain-status';
import { CheckResult } from '@acme/shared';

const domain = { name: 'shop.example.com', challengeName: '_acme-challenge.shop.example.com' };

function result(over: Partial<CheckResult>): CheckResult {
  return { name: domain.name, type: 'A', expected: [], status: 'correct', ...over };
}

describe('domainStatus', () => {
  it('is "never" before any verification has run', () => {
    expect(domainStatus(domain, null)).toBe('never');
  });

  it('is "never" when verification ran but said nothing about this domain', () => {
    expect(domainStatus(domain, [result({ name: 'other.example.com' })])).toBe('never');
  });

  it('is "ok" when every relevant record is correct', () => {
    const results = [result({ type: 'A' }), result({ type: 'CNAME', name: domain.challengeName })];
    expect(domainStatus(domain, results)).toBe('ok');
  });

  it('is "problem" when a relevant record is missing or incorrect', () => {
    expect(domainStatus(domain, [result({ status: 'missing' })])).toBe('problem');
  });

  it('ignores CAA rows: an unrelated not-published CAA does not block "ok"', () => {
    const results = [
      result({ type: 'A' }),
      result({ name: 'example.com', type: 'CAA', status: 'not-published' }),
    ];
    expect(domainStatus(domain, results)).toBe('ok');
  });
});
