import { AbstractControl, ValidationErrors, ValidatorFn } from '@angular/forms';

/**
 * The two facts about wildcards that surprise people (D4). The domain editor states both next
 * to the input, whether or not the current value is valid.
 */
export const WILDCARD_NOTES = [
  '*.example.com does not cover example.com itself.',
  '*.example.com does not cover a.b.example.com — a wildcard matches exactly one label.',
] as const;

const LABEL = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

/** lowercase, trim, drop the trailing root dot — mirrors `internal/dnsname`. */
export function normalizeDomain(name: string): string {
  return name.trim().toLowerCase().replace(/\.$/, '');
}

/** `*.example.com` -> `example.com`; anything else is returned unchanged. */
export function stripLeadingWildcard(name: string): string {
  return name.startsWith('*.') ? name.slice(2) : name;
}

/**
 * The wildcard for a name: `example.com` -> `*.example.com`, `shop.example.com` ->
 * `*.shop.example.com`. A name that is already a wildcard is returned without its star, which is
 * what makes it safe to call on whatever is in the field.
 *
 * It was called `counterpartDomain`, and "counterpart" is the reason: it is a word that means
 * nothing to somebody adding a domain, and it made the checkbox beside it unreadable. The concept
 * it named — "the other form, whichever way round you started" — is gone anyway, because the field
 * no longer holds a wildcard.
 *
 * Derivation only. The wildcard of a valid name can be invalid (`co.uk` -> `*.co.uk`), which is why
 * the server validates both and refuses the whole request if either fails.
 */
export function wildcardOf(name: string): string {
  const n = normalizeDomain(name);
  return n.startsWith('*.') ? n.slice(2) : '*.' + n;
}

export function challengeFqdn(name: string): string {
  return '_acme-challenge.' + stripLeadingWildcard(normalizeDomain(name));
}

/**
 * Returns the reason a domain name is unacceptable, or null if it is acceptable.
 *
 * ponytail: the one D4 rule not checked here is "the wildcard sits at a public suffix"
 * (`*.co.uk`). Deciding that needs the public suffix list, which is a ~30 kB payload and a
 * refresh obligation in the browser for a rule the server already enforces authoritatively with
 * `golang.org/x/net/publicsuffix`. A single-label base (`*.com`) is rejected here because it
 * needs no list; everything else falls to the server, whose reason is shown verbatim. Upgrade
 * path if that ever proves annoying: an endpoint that answers "is this a public suffix".
 */
export function domainNameProblem(raw: string): string | null {
  const name = normalizeDomain(raw);
  if (!name) return null;
  if (name.length > 253) return 'A domain name may not be longer than 253 characters.';

  const labels = name.split('.');
  const wildcards = labels.filter((l) => l === '*').length;
  if (wildcards > 1)
    return 'Only one wildcard label is permitted, and it must be the leftmost one.';
  if (wildcards === 1 && labels[0] !== '*') return 'A wildcard must be the leftmost label.';
  if (labels.some((l) => l !== '*' && l.includes('*'))) {
    return 'A wildcard label is exactly "*"; it cannot be combined with other characters.';
  }

  const base = labels[0] === '*' ? labels.slice(1) : labels;
  if (labels[0] === '*' && base.length < 2) {
    return 'A wildcard must sit below a registrable domain, so *.com or *. alone is not allowed.';
  }
  if (base.length < 2) return 'Enter a fully qualified domain name, for example example.com.';

  for (const label of base) {
    if (!label) return 'A domain name may not contain an empty label.';
    if (label.length > 63) return `The label "${label}" is longer than 63 characters.`;
    if (!LABEL.test(label)) {
      return `The label "${label}" is not valid: use a-z, 0-9 and hyphens, not starting or ending with a hyphen.`;
    }
  }
  return null;
}

/** Angular validator form of the above; the reason is in `errors['domainName']`. */
export const domainNameValidator: ValidatorFn = (
  control: AbstractControl,
): ValidationErrors | null => {
  const value = control.value;
  if (typeof value !== 'string') return null;
  const problem = domainNameProblem(value);
  return problem ? { domainName: problem } : null;
};
