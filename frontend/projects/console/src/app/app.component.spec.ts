import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { Account, SessionService } from '@acme/shared';
import { AppComponent } from './app.component';
import { AccountsService } from './features/accounts/accounts.service';

function account(over: Partial<Account>): Account {
  return {
    id: 'acct-1',
    slug: 'acme',
    name: 'Acme',
    createdAt: '2026-01-01T00:00:00Z',
    selected: true,
    kind: 'team',
    owner: true,
    credits: 100,
    suspended: false,
    ...over,
  };
}

function signedIn(): void {
  TestBed.inject(SessionService).me.set({
    subject: 'idp|1',
    displayName: 'Reader',
    groups: [],
    roles: [],
    permissions: [],
    selectedAccountId: 'acct-1',
  });
}

describe('AppComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AppComponent],
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        // The expiry prompt is an nz-modal, and ng-zorro's overlays carry animation metadata:
        // without an animations provider Angular refuses to render them (NG05105).
        provideNoopAnimations(),
      ],
    }).compileComponents();
  });

  it('sends the mark to the account picker', () => {
    // The mark is the way OUT of wherever you are, and out of an account is the picker — the one
    // screen that answers "which account am I about to work in" before showing anything belonging
    // to one. NOT /projects: that is a shortcut to the current account's work, which is what the
    // navigation already is.
    //
    // Asserted on the rendered href rather than on the input, because what breaks this is somebody
    // changing the shell's default and the console forgetting to override it: the input would still
    // read the right value in one file while the anchor pointed elsewhere.
    signedIn();
    TestBed.inject(AccountsService).accounts.set([account({})]);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    const brand: HTMLAnchorElement = fixture.nativeElement.querySelector('a.brand');
    expect(brand).withContext('the bar has no brand link at all').toBeTruthy();
    expect(brand.getAttribute('href')).toBe('/accounts');
  });

  it('names the remedy once in a personal account', () => {
    // Not Projects with three disabled entries beside it: navigation is the map of the product,
    // and `Upgrade` is the positive form of everything a personal account is missing (C11).
    signedIn();
    TestBed.inject(AccountsService).accounts.set([account({ kind: 'personal', credits: 0 })]);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.nav().map((i) => i.label)).toEqual(['Projects', 'Upgrade']);
  });

  it('offers the account screens in a team account', () => {
    // Live challenges is deliberately absent from both — deferred, not dropped (task 6.2d).
    signedIn();
    TestBed.inject(AccountsService).accounts.set([account({})]);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.nav().map((i) => i.label)).toEqual(['Projects', 'Audit', 'Settings']);
  });

  it('changes the navigation when the account changes, without a reload', () => {
    // The switch is a signal write, not a page load (task 6.2c). If this ever needs a reload to
    // pass, the switcher has grown one back.
    signedIn();
    const accounts = TestBed.inject(AccountsService);
    accounts.accounts.set([account({ kind: 'personal' })]);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.nav().map((i) => i.label)).toContain('Upgrade');

    accounts.accounts.set([account({ kind: 'team' })]);
    fixture.detectChanges();
    expect(fixture.componentInstance.nav().map((i) => i.label)).toEqual(['Projects', 'Audit', 'Settings']);
  });

  it('offers nothing but the one screen a suspended account has', () => {
    // Not the console in read-only (design C5): the owner is here to see the balance and pay.
    signedIn();
    TestBed.inject(AccountsService).accounts.set([account({ credits: 0, suspended: true })]);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.nav()).toEqual([]);
  });

  it('warns before the balance reaches zero', () => {
    signedIn();
    TestBed.inject(AccountsService).accounts.set([account({ credits: 10 })]);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    expect(fixture.componentInstance.lowBalance()?.credits).toBe(10);
    expect(fixture.nativeElement.textContent).toContain('stop renewing');
  });

  it('prompts when the session has expired', () => {
    signedIn();
    TestBed.inject(AccountsService).accounts.set([account({})]);
    TestBed.inject(SessionService).expired.set(true);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    // The prompt is an nz-modal, and ng-zorro renders overlays into the CDK overlay container on
    // document.body — not inside the component's own element.
    expect(document.body.textContent).toContain('session has expired');
  });
});
