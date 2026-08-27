import { fakeAsync, TestBed, tick } from '@angular/core/testing';
import { ApiService, Interaction } from '@acme/shared';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { InteractionsPanelComponent } from './interactions-panel.component';
function interaction(over: Partial<Interaction> = {}): Interaction {
  return {
    id: '00000000-0000-7000-8000-000000000001',
    aliasLabel: 'alias-1',
    challengeName: '_acme-challenge.shop.example.com',
    domainNames: ['shop.example.com'],
    keyId: 'key-1',
    clientIp: '203.0.113.9',
    fqdnForm: 'alias',
    publishedAt: '2026-01-01T00:00:00Z',
    withdrawnAt: '2026-01-01T00:00:05Z',
    firstQueriedAt: '2026-01-01T00:00:02Z',
    lastQueriedAt: '2026-01-01T00:00:02Z',
    queryCount: 1,
    outcome: 'validated',
    ...over,
  };
}

describe('InteractionsPanelComponent', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [InteractionsPanelComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    // Every account-scoped call names its account in the path, so the client has to
    // have one before a component can make any.
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');
  });

  afterEach(() => http.verify());

  function create(loadBalancerId = 7) {
    const fixture = TestBed.createComponent(InteractionsPanelComponent);
    fixture.componentRef.setInput('loadBalancerId', loadBalancerId);
    fixture.detectChanges();
    return fixture;
  }

  it('requests limit/offset against the load balancer path and reads X-Total-Count', () => {
    const fixture = create(7);

    const req = http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions');
    expect(req.request.params.get('limit')).toBe('25');
    expect(req.request.params.get('offset')).toBe('0');
    req.flush([interaction()], { headers: { 'X-Total-Count': '3' } });

    expect(fixture.componentInstance.total()).toBe(3);
  });

  it('falls back to the row count when X-Total-Count is missing', () => {
    const fixture = create();
    http
      .expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions')
      .flush([interaction(), interaction({ id: '00000000-0000-7000-8000-000000000002' })]);
    expect(fixture.componentInstance.total()).toBe(2);
  });

  it('sends unqueried=true and resets to page one when the switch is toggled', () => {
    const fixture = create();
    http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions').flush([interaction()]);

    fixture.componentInstance.onQueryParams({ pageIndex: 2, pageSize: 25, sort: [], filter: [] });
    const paged = http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions');
    expect(paged.request.params.get('offset')).toBe('25');
    paged.flush([interaction()], { headers: { 'X-Total-Count': '100' } });

    fixture.componentInstance.onlyUnqueried = true;
    fixture.componentInstance.reload();
    const req = http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions');
    expect(req.request.params.get('unqueried')).toBe('true');
    expect(req.request.params.get('offset')).toBe('0');
    expect(fixture.componentInstance.pageIndex()).toBe(1);
    req.flush([]);
  });

  it('debounces typing instead of firing a request per keystroke', fakeAsync(() => {
    const fixture = create();
    http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions').flush([interaction()]);

    fixture.componentInstance.query = 's';
    fixture.componentInstance.onQueryChange();
    tick(100);
    fixture.componentInstance.query = 'shop';
    fixture.componentInstance.onQueryChange();
    tick(200);
    http.expectNone((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions');

    tick(50);
    const req = http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions');
    expect(req.request.params.get('q')).toBe('shop');
    req.flush([]);
  }));

  it('shows the "nothing published yet" empty state when unfiltered and empty', () => {
    const fixture = create();
    http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions').flush([]);
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('has not published a challenge yet');
    expect(fixture.nativeElement.textContent).not.toContain('Every publication was queried');
  });

  it('shows the "every publication was queried" empty state, distinct from the other one, under the unqueried filter', () => {
    const fixture = create();
    http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions').flush([interaction()]);
    fixture.detectChanges();

    fixture.componentInstance.onlyUnqueried = true;
    fixture.componentInstance.reload();
    http.expectOne((r) => r.url === '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/7/interactions').flush([]);
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Every publication was queried');
    expect(fixture.nativeElement.textContent).not.toContain('has not published a challenge yet');
  });
});
