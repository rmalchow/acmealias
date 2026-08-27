import { TestBed } from '@angular/core/testing';
import { ApiService } from '@acme/shared';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { NZ_MODAL_DATA, NzModalRef } from 'ng-zorro-antd/modal';
import { DomainEditorComponent } from './domain-editor.component';

describe('DomainEditorComponent', () => {
  let http: HttpTestingController;
  let closed: (boolean | undefined)[];

  beforeEach(async () => {
    closed = [];
    await TestBed.configureTestingModule({
      imports: [DomainEditorComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
        { provide: NZ_MODAL_DATA, useValue: { loadBalancerId: '00000000-0000-7000-8000-000000000007' } },
        {
          provide: NzModalRef,
          useValue: { close: (v?: boolean) => closed.push(v) },
        },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    // Every account-scoped call names its account in the path, so the client has to
    // have one before a component can make any.
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');
  });

  afterEach(() => http.verify());

  // The alias label is system configuration now, so the form has nothing to
  // say about it and the request must not carry one — the API refuses
  // `aliasLabel` as an unknown field.
  it('offers no alias input and submits only the name', () => {
    const fixture = TestBed.createComponent(DomainEditorComponent);
    fixture.detectChanges();

    const html: string = fixture.nativeElement.innerHTML;
    expect(html).not.toContain('aliasLabel');
    expect(html).not.toContain('Alias label');
    // One text field. (The include-wildcard checkbox is an input too, hence the selector.)
    expect(fixture.nativeElement.querySelectorAll('input[nz-input]').length).toBe(1);

    fixture.componentInstance.name = ' shop.example.com ';
    fixture.componentInstance.submit();

    const req = http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/00000000-0000-7000-8000-000000000007/domains');
    expect(req.request.body).toEqual({ name: 'shop.example.com' });
    req.flush({
      id: '00000000-0000-7000-8000-000000000001',
      name: 'shop.example.com',
      aliasLabel: 'k7m3q9x2r4tv8w1p',
      domains: [{ id: '00000000-0000-7000-8000-000000000001', name: 'shop.example.com' }],
      warnings: [],
    });

    // Nothing to read, so the modal closes reporting success.
    expect(closed).toEqual([true]);
  });

  // Unchanged behaviour, re-pinned here because removing the input touched
  // this template: a warning keeps the dialog open so it can be read.
  it('stays open to show a server warning', () => {
    const fixture = TestBed.createComponent(DomainEditorComponent);
    fixture.detectChanges();

    fixture.componentInstance.name = 'shop.example.com';
    fixture.componentInstance.submit();
    http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/00000000-0000-7000-8000-000000000007/domains').flush({
      id: '00000000-0000-7000-8000-000000000001',
      name: 'shop.example.com',
      aliasLabel: 'k7m3q9x2r4tv8w1p',
      warnings: ['already covered by the wildcard "*.example.com"'],
    });

    expect(closed).toEqual([]);
    expect(fixture.componentInstance.warnings().length).toBe(1);
  });

  // --- the include-wildcard control (design B2, B4) ---

  /** Renders with `name` typed in, so the computed label is the one the operator would see. */
  function withName(name: string) {
    const fixture = TestBed.createComponent(DomainEditorComponent);
    fixture.componentInstance.name = name;
    fixture.detectChanges();
    return fixture;
  }

  it('offers the counterpart control unticked, naming the wildcard it would add', () => {
    const fixture = withName('example.com');

    expect(fixture.componentInstance.includeWildcard).toBeFalse();
    expect(fixture.nativeElement.querySelector('input[type=checkbox]').checked).toBeFalse();
    expect(fixture.componentInstance.counterpartLabel()).toBe(
      'Also add the wildcard *.example.com',
    );
    expect(fixture.nativeElement.textContent).toContain('Also add the wildcard *.example.com');
  });

  it('mirrors the control for a wildcard, naming the apex it would add', () => {
    const fixture = withName('*.example.com');

    expect(fixture.componentInstance.counterpartLabel()).toBe('Also add the apex example.com');
    expect(fixture.nativeElement.textContent).toContain('Also add the apex example.com');
  });

  it('names the counterpart of a specific host, not the registrable domain', () => {
    expect(withName('shop.example.com').componentInstance.counterpartLabel()).toBe(
      'Also add the wildcard *.shop.example.com',
    );
  });

  // B4: the flag is absent unless it was asked for. The endpoint decodes strictly, so a body
  // is exactly what the operator chose.
  it('submits no includeWildcard when the control is untouched', () => {
    const fixture = withName('example.com');
    fixture.componentInstance.submit();

    const req = http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/00000000-0000-7000-8000-000000000007/domains');
    expect(req.request.body).toEqual({ name: 'example.com' });
    req.flush({
      id: '00000000-0000-7000-8000-000000000001',
      name: 'example.com',
      domains: [{ id: '00000000-0000-7000-8000-000000000001', name: 'example.com' }],
      warnings: [],
    });
    expect(closed).toEqual([true]);
  });

  it('submits includeWildcard when the control is ticked', () => {
    const fixture = withName('example.com');
    fixture.componentInstance.includeWildcard = true;
    fixture.componentInstance.submit();

    const req = http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/00000000-0000-7000-8000-000000000007/domains');
    expect(req.request.body).toEqual({
      name: 'example.com',
      includeWildcard: true,
    });
    req.flush({
      id: '00000000-0000-7000-8000-000000000001',
      name: 'example.com',
      warnings: [],
      domains: [
        { id: '00000000-0000-7000-8000-000000000001', name: 'example.com' },
        { id: '00000000-0000-7000-8000-000000000002', name: '*.example.com' },
      ],
    });
    expect(closed).toEqual([true]);
  });

  // B3 as the operator sees it: the counterpart was refused, so nothing was created, and the
  // dialog must not close as though something had been.
  it('keeps the dialog open and reports nothing created when the pair is refused', () => {
    const fixture = withName('co.uk');
    fixture.componentInstance.includeWildcard = true;
    fixture.componentInstance.submit();

    http.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/loadbalancers/00000000-0000-7000-8000-000000000007/domains').flush(
      {
        message:
          'cannot also add "*.co.uk": invalid dns name: "*.co.uk" would cover the whole public suffix "co.uk"',
      },
      { status: 400, statusText: 'Bad Request' },
    );

    expect(closed).toEqual([]);
    expect(fixture.componentInstance.done()).toBeFalse();
    expect(fixture.componentInstance.serverError()).toContain('*.co.uk');
  });
});
