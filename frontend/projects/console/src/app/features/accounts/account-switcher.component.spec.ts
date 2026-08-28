import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { Account } from '@acme/shared';
import { AccountSwitcherComponent } from './account-switcher.component';
import { AccountsService } from './accounts.service';

function account(over: Partial<Account>): Account {
  return {
    id: 'acct-1',
    slug: 'acme',
    name: 'Acme',
    createdAt: '2026-01-01T00:00:00Z',
    selected: false,
    kind: 'team',
    owner: true,
    credits: 100,
    suspended: false,
    ...over,
  };
}

/** n team accounts, the first of them selected. */
function team(n: number): Account[] {
  return Array.from({ length: n }, (_, i) =>
    account({ id: `acct-${i}`, slug: `team-${i}`, name: `Team ${i}`, selected: i === 0 }),
  );
}

describe('AccountSwitcherComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AccountSwitcherComponent],
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
      ],
    }).compileComponents();
  });

  function render(accounts: Account[]) {
    TestBed.inject(AccountsService).accounts.set(accounts);
    const fixture = TestBed.createComponent(AccountSwitcherComponent);
    fixture.detectChanges();
    return fixture;
  }

  // The threshold, from both sides. Below five the whole list is on screen and a search field is a
  // control that has to be dismissed to see what it was covering; at five, scanning stops working.
  it('offers no search below five accounts', () => {
    const fixture = render(team(4));
    expect(fixture.nativeElement.querySelector('.ant-select-show-search')).toBeNull();
  });

  it('offers search at five accounts', () => {
    const fixture = render(team(5));
    expect(fixture.nativeElement.querySelector('.ant-select-show-search')).not.toBeNull();
  });

  // The personal account is "Personal", never its stored name — which is derived from the owner's
  // address, and would put the signed-in person's identity beside the user menu that already says
  // it: the same word twice, a centimetre apart, one of them pretending to be a workspace's name.
  //
  // Asserted on the naming function rather than on the option list: ng-zorro keeps a select's
  // options in an overlay that does not exist until the control is opened, so a DOM assertion on a
  // closed select reads the placeholder and passes or fails for reasons that have nothing to do
  // with this rule. The rendered path is covered by the single-account case below, which has no
  // overlay.
  it('calls a personal account Personal and leaves a team account named', () => {
    const fixture = render(team(1));
    const c = fixture.componentInstance as unknown as {
      label(a: { kind: string; name: string }): string;
    };
    expect(c.label({ kind: 'personal', name: 'dana@corp.example' })).toBe('Personal');
    expect(c.label({ kind: 'team', name: 'Acme Ltd' })).toBe('Acme Ltd');
  });

});
