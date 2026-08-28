import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { provideRouter } from '@angular/router';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzMessageService } from 'ng-zorro-antd/message';
import { ApiService, Domain, LoadBalancer, VerificationResult } from '@acme/shared';
import { LoadBalancerDetailComponent } from './loadbalancer-detail.component';

const ACCOUNT = '00000000-0000-7000-8000-0000000000a1';
const BASE = `/api/v1/accounts/${ACCOUNT}`;

function domain(over: Partial<Domain> = {}): Domain {
  return {
    id: 'd-1',
    loadBalancerId: '7',
    name: 'shop.example.com',
    challengeName: '_acme-challenge.shop.example.com',
    aliasId: 'a-1',
    aliasLabel: 'shop-example-com',
    createdAt: '2026-01-01T00:00:00Z',
    // Activated, which is what a domain in a working deployment looks like. The activation chip
    // reads these two directly, so a fixture that left them out would test the "waiting" state
    // on every row without meaning to.
    lastCheckedAt: '2026-01-02T00:00:00Z',
    lastSuccessAt: '2026-01-02T00:00:00Z',
    ...over,
  };
}

function loadBalancer(): LoadBalancer {
  return {
    id: '7',
    projectId: 'p-1',
    slug: 'lb',
    name: 'Load balancer',
    createdAt: '2026-01-01T00:00:00Z',
    permittedActions: [],
    nodes: [],
    domains: [domain()],
  } as unknown as LoadBalancer;
}

function verification(over: Partial<VerificationResult> = {}): VerificationResult {
  return {
    resolver: '10.0.0.1:53',
    results: [],
    domains: [
      {
        domainId: 'd-1',
        name: 'shop.example.com',
        challengeName: '_acme-challenge.shop.example.com',
        aliasId: 'a-1',
        delegation: { name: '_acme-challenge.shop.example.com', status: 'correct' },
        caa: { name: 'example.com', status: 'blocking', found: 'someone.else' },
      },
    ],
    ...over,
  };
}

describe('LoadBalancerDetailComponent', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [LoadBalancerDetailComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
        // The header renders breadcrumb links, so the component needs a router even though this
        // spec never navigates.
        provideRouter([]),
        // The component holds both for controls this spec never opens; ng-zorro provides them
        // through modules the test bed does not import.
        NzModalService,
        NzMessageService,
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    TestBed.inject(ApiService).setAccount(ACCOUNT);
  });

  afterEach(() => http.verify());

  /** Opens the screen and answers everything but the verification, which the caller drives. */
  function open() {
    const fixture = TestBed.createComponent(LoadBalancerDetailComponent);
    fixture.componentRef.setInput('id', '7');
    fixture.detectChanges();
    http.expectOne(`${BASE}/loadbalancers/7`).flush(loadBalancer());
    http.expectOne(`${BASE}/projects/p-1`).flush({ id: 'p-1', slug: 'proj', name: 'Project' });
    http.expectOne(`${BASE}/loadbalancers/7/plan`).flush({ records: [], zone: 'svc.example.net' });
    return fixture;
  }

  it('asks without being asked, and says nothing about the button while it does', () => {
    // The check runs on load (design D1). The manual control is a different question and must
    // not look pressed: a spinner nobody started reads as work somebody set going.
    const fixture = open();
    const req = http.expectOne(`${BASE}/loadbalancers/7/verification`);
    expect(fixture.componentInstance.verifying()).toBeFalse();
    req.flush(verification());
    expect(fixture.componentInstance.verifying()).toBeFalse();
  });

  it('shows no banner when the automatic check fails, only a degraded row', () => {
    // Task 3.3: a check nobody asked for that fails is not an error on the page. It degrades the
    // rows it concerns and never the screen (design D1, D4).
    const fixture = open();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush('boom', { status: 500, statusText: 'Server Error' });

    expect(fixture.componentInstance.verifyError()).toBeNull();
    const chips = fixture.componentInstance.checks(domain());
    expect(chips.delegation.label).toBe('could not check');
  });

  it('banners a failure somebody pressed for', () => {
    const fixture = open();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush(verification());

    fixture.componentInstance.checkNow();
    expect(fixture.componentInstance.verifying()).toBeTrue();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush('boom', { status: 500, statusText: 'Server Error' });
    expect(fixture.componentInstance.verifyError()).not.toBeNull();
    expect(fixture.componentInstance.verifying()).toBeFalse();
  });

  it('degrades both chips on a failed re-check rather than dropping CAA or keeping a stale verdict', () => {
    // A row that said "blocks us" must not lose its CAA line because a later request failed, and
    // must not go on asserting the old answer either: the banner and the chips would then
    // disagree about whether anything is known.
    const fixture = open();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush(verification());
    expect(fixture.componentInstance.checks(domain()).caa?.label).toBe('blocks us');

    fixture.componentInstance.verify();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush('boom', { status: 503, statusText: 'Unavailable' });

    const chips = fixture.componentInstance.checks(domain());
    expect(chips.caa?.label).toBe('could not check');
    expect(chips.delegation.label).toBe('could not check');
  });

  it('never prints a resolver error into the plan table', () => {
    // `found` on an undetermined row is our own error text, addressed to an operator. The table
    // is painted on every screen open now, so it would reach a customer unprompted (design D4).
    const fixture = open();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush(
      verification({
        results: [
          {
            name: '_acme-challenge.shop.example.com',
            type: 'CNAME',
            expected: ['shop-example-com.svc.example.net'],
            status: 'undetermined',
            found: 'query CNAME _acme-challenge.shop.example.com: dial udp 10.0.0.1:53: i/o timeout',
          },
        ],
      }),
    );
    expect(fixture.componentInstance.foundFor('_acme-challenge.shop.example.com', 'CNAME')).toBe('—');
  });

  // Check now (design domain-activation task 4.3): the same lookup, against the route that
  // RECORDS what it saw, followed by a reload — because the stamps on screen are what just
  // changed and the person pressing it is watching for exactly that.
  it('records what a check-now saw and reloads the rows it changed', () => {
    const fixture = open();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush(verification());

    fixture.componentInstance.checkNow();
    const req = http.expectOne(`${BASE}/loadbalancers/7/verification`);
    expect(req.request.method).toBe('POST');
    req.flush(verification());

    // The reload, and its breadcrumb follow-up. Without it the row would still read "waiting for
    // delegation" for a domain the service has just activated.
    http.expectOne(`${BASE}/loadbalancers/7`).flush(loadBalancer());
    http.expectOne(`${BASE}/projects/p-1`).flush({ id: 'p-1', slug: 'proj', name: 'Project' });
  });

  // The automatic pass must stay a GET. It runs on every screen open, and a page that activated
  // domains as a side effect of being looked at is a page nobody can safely open.
  it('asks the automatic check as a read', () => {
    const fixture = open();
    const req = http.expectOne(`${BASE}/loadbalancers/7/verification`);
    expect(req.request.method).toBe('GET');
    req.flush(verification());
    expect(fixture.componentInstance.verifyError()).toBeNull();
  });

  // Waiting is not broken (task 4.1). A domain added five minutes ago has never been seen, and
  // the chip has to say so as an expectation rather than as a fault.
  it('reads a never-seen delegation as waiting, and one that stopped resolving as still active', () => {
    const fixture = open();
    http.expectOne(`${BASE}/loadbalancers/7/verification`).flush(verification());
    const c = fixture.componentInstance;

    expect(c.activation(domain({ lastCheckedAt: null, lastSuccessAt: null })).label).toBe('waiting for delegation');
    expect(c.activation(domain({ lastCheckedAt: '2026-02-01T00:00:00Z', lastSuccessAt: null })).colour).toBe('blue');
    expect(c.activation(domain()).label).toBe('active');

    // Checked after it was last seen: we looked and it was gone. It stays active — activation is
    // one-way (A3) — and the row says since when rather than counting down to anything.
    const broken = domain({ lastSuccessAt: '2026-02-01T00:00:00Z', lastCheckedAt: '2026-02-03T00:00:00Z' });
    expect(c.activation(broken).label).toBe('not resolving');
    expect(c.stoppedResolving(broken)).toBeTrue();
    expect(c.stoppedResolving(domain())).toBeFalse();

    // Check now is offered in both states a check can change (task 4.3). The second one is the
    // person who has just REPAIRED a delegation: gating on "never seen" alone left exactly them
    // with no way to ask, waiting a full settled interval for the chip to go green.
    expect(c.canCheckNow(domain({ lastCheckedAt: null, lastSuccessAt: null }))).toBeTrue();
    expect(c.canCheckNow(broken)).toBeTrue();
    expect(c.canCheckNow(domain())).toBeFalse();
  });
});
