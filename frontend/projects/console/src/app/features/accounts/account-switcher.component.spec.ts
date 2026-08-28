import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { Account, accountLabel } from '@acme/shared';
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

  // The naming rule itself lives in shared (accountLabel) and is asserted with it. What this pins
  // is that the switcher USES it — the failure being a component that grows its own copy back and
  // then disagrees with the picker sitting on the same page.
  it('names accounts through the shared rule', () => {
    const fixture = render(team(1));
    const c = fixture.componentInstance as unknown as { label: typeof accountLabel };
    expect(c.label).toBe(accountLabel);
    expect(c.label({ kind: 'personal', name: 'dana@corp.example' } as Account)).toBe('Personal');
    expect(c.label({ kind: 'team', name: 'Acme Ltd' } as Account)).toBe('Acme Ltd');
  });

});
