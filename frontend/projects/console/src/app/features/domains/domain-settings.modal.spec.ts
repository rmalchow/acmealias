import { TestBed } from '@angular/core/testing';
import { ApiService, Domain } from '@acme/shared';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { NZ_MODAL_DATA, NzModalRef } from 'ng-zorro-antd/modal';
import { DomainSettingsModalComponent } from './domain-settings.modal';

function domain(over: Partial<Domain> = {}): Domain {
  return {
    id: 'd-1',
    projectId: '7',
    name: 'shop.example.com',
    challengeName: '_acme-challenge.shop.example.com',
    aliasId: 'a-1',
    aliasLabel: 'shop-example-com',
    createdAt: '2026-01-01T00:00:00Z',
    lastCheckedAt: null,
    lastSuccessAt: null,
    observeTls: false,
    tlsTestHost: null,
    cnameTarget: 'shop-example-com.svc.example.net',
    expectedCaa: null,
    ...over,
  };
}

describe('DomainSettingsModalComponent', () => {
  let http: HttpTestingController;
  let closed: (Domain | undefined)[];

  function build(d: Domain) {
    closed = [];
    return TestBed.configureTestingModule({
      imports: [DomainSettingsModalComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
        { provide: NZ_MODAL_DATA, useValue: { domain: d } },
        { provide: NzModalRef, useValue: { close: (v?: Domain) => closed.push(v) } },
      ],
    }).compileComponents();
  }

  afterEach(() => http?.verify());

  // A plain domain needs no hostname at all — the server name is the domain's own name — so
  // turning the switch on submits nothing but the flag.
  it('turns observation on for a plain domain with no hostname field', async () => {
    await build(domain({ observeTls: false }));
    http = TestBed.inject(HttpTestingController);
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');

    const fixture = TestBed.createComponent(DomainSettingsModalComponent);
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).not.toContain('Hostname to test');

    fixture.componentInstance.observeTls = true;
    fixture.componentInstance.submit();

    const req = http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/domains/d-1');
    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ observeTls: true });
    req.flush(domain({ observeTls: true }));

    expect(closed.length).toBe(1);
  });

  // G8/2.3: a wildcard requires a hostname, and it is never derived — the field starts on
  // whatever is already stored (here: nothing), not a guessed "www.".
  it('requires a covered hostname on a wildcard and refuses one outside it', async () => {
    await build(domain({ name: '*.example.com', observeTls: false, tlsTestHost: null }));
    http = TestBed.inject(HttpTestingController);
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');

    const fixture = TestBed.createComponent(DomainSettingsModalComponent);
    fixture.detectChanges();
    const c = fixture.componentInstance;
    c.observeTls = true;

    expect(c.tlsTestHost).toBe('');
    expect(c.clientProblem()).toContain('needs a hostname');

    c.tlsTestHost = 'a.b.example.com';
    expect(c.clientProblem()).toContain('not covered by');

    c.tlsTestHost = 'www.example.com';
    expect(c.clientProblem()).toBeNull();
  });

  // 9.3: turning the switch off omits tlsTestHost from the PATCH entirely, rather than sending
  // null — the store keeps whatever is already there (A/2.5) only when nothing overwrites it.
  it('omits tlsTestHost when turning observation off, keeping it in the field', async () => {
    await build(domain({ name: '*.example.com', observeTls: true, tlsTestHost: 'www.example.com' }));
    http = TestBed.inject(HttpTestingController);
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');

    const fixture = TestBed.createComponent(DomainSettingsModalComponent);
    fixture.detectChanges();
    const c = fixture.componentInstance;
    expect(c.tlsTestHost).toBe('www.example.com');

    c.observeTls = false;
    c.submit();

    const req = http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/domains/d-1');
    expect(req.request.body).toEqual({ observeTls: false });
    req.flush(domain({ observeTls: false, tlsTestHost: 'www.example.com' }));

    // The field itself is untouched by turning the switch off.
    expect(c.tlsTestHost).toBe('www.example.com');
  });
});
