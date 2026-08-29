import { TestBed } from '@angular/core/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { Domain, DomainCheck } from '@acme/shared';
import { DnsRecordsPanelComponent } from './dns-records-panel.component';

function domain(over: Partial<Domain> = {}): Domain {
  return {
    id: 'd-1',
    projectId: '7',
    name: 'shop.example.com',
    challengeName: '_acme-challenge.shop.example.com',
    aliasId: 'a-1',
    aliasLabel: 'k7f2q',
    createdAt: '2026-01-01T00:00:00Z',
    lastCheckedAt: null,
    lastSuccessAt: null,
    observeTls: false,
    tlsTestHost: null,
    cnameTarget: 'k7f2q.edgedns.cloud',
    expectedCaa: [
      '0 issue "letsencrypt.org; accounturi=YOUR-ACME-ACCOUNT-URL"',
      '0 issuewild "letsencrypt.org; accounturi=YOUR-ACME-ACCOUNT-URL"',
    ],
    caaName: 'example.com',
    ...over,
  } as Domain;
}

function build(domains: Domain[], checks: DomainCheck[] | null = null): DnsRecordsPanelComponent {
  TestBed.configureTestingModule({
    imports: [DnsRecordsPanelComponent],
    providers: [provideNoopAnimations(), provideNzIconsTesting()],
  });
  const fixture = TestBed.createComponent(DnsRecordsPanelComponent);
  fixture.componentRef.setInput('domains', domains);
  fixture.componentRef.setInput('checks', checks);
  fixture.detectChanges();
  return fixture.componentInstance;
}

describe('DnsRecordsPanelComponent', () => {
  it('prints every record before anything has been asked of DNS', () => {
    // The half a customer setting a domain up actually needs, and it costs no lookup: what to
    // publish is a property of the domain. Waiting for a check to show it would be waiting on a
    // query to answer a question the query has nothing to do with.
    const c = build([domain()]);

    expect(c.delegations().length).toBe(1);
    expect(c.caa().length).toBe(1);
    expect(c.delegationChecked()).toBeFalse();
    expect(c.caaChecked()).toBeFalse();
    expect(c.delegations()[0].chip).toBeNull();
    expect(c.caa()[0].chip).toBeNull();
  });

  it('prints a whole zone-file line, with the trailing dot the target needs', () => {
    // Without the dot a provider that appends the zone turns the alias into
    // k7f2q.edgedns.cloud.shop.example.com, which is the commonest way this gets published wrong.
    const line = build([domain()]).delegations()[0].expected[0];
    expect(line).toBe('_acme-challenge.shop.example.com.  300  IN  CNAME  k7f2q.edgedns.cloud.');
  });

  it('puts the CAA record at the registrable name the service named, not at the host', () => {
    const row = build([domain()]).caa()[0];
    expect(row.name).toBe('example.com');
    expect(row.expected[0]).toContain('example.com.  300  IN  CAA  0 issue ');
    expect(row.expected[1]).toContain('issuewild');
    // The placeholder is the point: an account URI lives in the customer's ACME client and we
    // cannot see it. A plausible-looking URL here would lock somebody out of their own domain.
    expect(row.expected[0]).toContain('YOUR-ACME-ACCOUNT-URL');
  });

  it('gives a wildcard and its base one delegation row and one CAA row', () => {
    // They derive the same challenge name and sit under the same registrable name, so both are
    // one record. Two rows would be two places to publish it, one of them wrong.
    const c = build([
      domain({ id: 'd-1', name: 'example.com', challengeName: '_acme-challenge.example.com' }),
      domain({ id: 'd-2', name: '*.example.com', challengeName: '_acme-challenge.example.com' }),
    ]);

    expect(c.delegations().length).toBe(1);
    expect(c.delegations()[0].covers).toEqual(['example.com', '*.example.com']);
    expect(c.caa().length).toBe(1);
    expect(c.caa()[0].covers.length).toBe(2);
  });

  it('colours the rows once a check has answered, and prints what it found', () => {
    const checks: DomainCheck[] = [
      {
        domainId: 'd-1',
        name: 'shop.example.com',
        challengeName: '_acme-challenge.shop.example.com',
        aliasId: 'a-1',
        delegation: { name: '_acme-challenge.shop.example.com', status: 'correct', found: 'k7f2q.edgedns.cloud.' },
        caa: { name: 'example.com', status: 'unconstrained', found: 'letsencrypt.org' },
      },
    ];
    const c = build([domain()], checks);

    expect(c.delegationChecked()).toBeTrue();
    expect(c.caaChecked()).toBeTrue();
    expect(c.delegations()[0].chip?.colour).toBe('green');
    expect(c.delegations()[0].found).toBe('k7f2q.edgedns.cloud.');
    // Amber, not green: the authority is named and no account is, so every account there may
    // issue for this name — including this service's.
    expect(c.caa()[0].chip?.colour).toBe('orange');
    expect(c.caa()[0].found).toBe('letsencrypt.org');
  });

  it('reads a domain with no CAA published as red rather than as nothing to do', () => {
    const c = build([domain()], [
      {
        domainId: 'd-1',
        name: 'shop.example.com',
        challengeName: '_acme-challenge.shop.example.com',
        aliasId: 'a-1',
        delegation: { name: '_acme-challenge.shop.example.com', status: 'correct' },
        caa: { name: 'example.com', status: 'not-published' },
      },
    ]);
    expect(c.caa()[0].chip?.colour).toBe('red');
    // No rdata to print, so the column carries the chip's word instead — and the template renders
    // that in the body colour so it can never read as a published value.
    expect(c.caa()[0].found).toBe('');
  });

  it('reports each table as checked on its own, so one button does not label the other', () => {
    // The two buttons are two requests. A delegation-only answer leaves every CAA row without a
    // chip, and a panel-wide "have we checked" flag would have put "Check again" on a table nobody
    // had checked yet.
    const c = build([domain()], [
      {
        domainId: 'd-1',
        name: 'shop.example.com',
        challengeName: '_acme-challenge.shop.example.com',
        aliasId: 'a-1',
        delegation: { name: '_acme-challenge.shop.example.com', status: 'correct' },
        caa: { name: 'example.com', status: 'not-checked' },
      },
    ]);
    expect(c.delegationChecked()).toBeTrue();
    expect(c.caaChecked()).toBeFalse();
  });

  it('shows no CAA table where the deployment configured no issuer', () => {
    // There is then nothing to authorise, and a table telling somebody to publish a record naming
    // no authority would be worse than no table.
    const c = build([domain({ expectedCaa: null, caaName: '' })]);
    expect(c.caa().length).toBe(0);
    expect(c.delegations().length).toBe(1);
  });

  it('prints no delegation row for a domain whose alias has gone missing', () => {
    // An empty cnameTarget is our fault, not the customer's, and "publish a CNAME to nothing" is
    // an instruction nobody can follow.
    const c = build([domain({ cnameTarget: '' })]);
    expect(c.delegations().length).toBe(0);
  });
});
