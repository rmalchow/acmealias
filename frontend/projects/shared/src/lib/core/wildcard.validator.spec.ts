import { FormControl } from '@angular/forms';
import { challengeFqdn, domainNameProblem, domainNameValidator } from './wildcard.validator';

describe('domainNameProblem', () => {
  const accepted = ['example.com', '*.example.com', 'shop.example.com', 'EXAMPLE.com.', 'a-b.example.co.uk'];
  for (const name of accepted) {
    it(`accepts ${name}`, () => expect(domainNameProblem(name)).toBeNull());
  }

  const rejected = ['*.*.example.com', 'foo.*.example.com', '*', '*.com', 'example', 'foo..example.com', '-bad.example.com', 'a*.example.com', 'x'.repeat(64) + '.example.com'];
  for (const name of rejected) {
    it(`rejects ${name}`, () => expect(domainNameProblem(name)).toBeTruthy());
  }

  it('says nothing about an empty value, which is `required`\'s job', () => {
    expect(domainNameProblem('  ')).toBeNull();
  });
});

describe('domainNameValidator', () => {
  it('puts the reason in the error', () => {
    const errors = domainNameValidator(new FormControl('*.*.example.com'));
    expect(errors?.['domainName']).toContain('leftmost');
  });

  it('passes a valid wildcard', () => {
    expect(domainNameValidator(new FormControl('*.example.com'))).toBeNull();
  });
});

describe('challengeFqdn', () => {
  it('gives a wildcard and its apex the same challenge name', () => {
    expect(challengeFqdn('*.example.com')).toBe('_acme-challenge.example.com');
    expect(challengeFqdn('example.com.')).toBe('_acme-challenge.example.com');
  });
});
