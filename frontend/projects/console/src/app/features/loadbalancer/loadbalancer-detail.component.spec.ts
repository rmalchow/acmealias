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

    fixture.componentInstance.verify();
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
});
