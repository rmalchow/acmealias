import { Component, computed, inject } from '@angular/core';
import { NavigationEnd, Router, RouterLink, RouterOutlet } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { filter, map, startWith } from 'rxjs';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { AccountSwitcherComponent } from './features/accounts/account-switcher.component';
import { AccountsService } from './features/accounts/accounts.service';
import { LOW_BALANCE, NavItem, SessionService, ShellComponent, SignedOutComponent } from '@acme/shared';

/**
 * A personal account's navigation, and a team account's (design C11, tasks 6.2 / 6.2a).
 *
 * THE NAVIGATION IS THE ONE PLACE THINGS ARE LEFT OUT, and it is worth saying why that does not
 * contradict the rule right beside it. Inside a screen a control the caller lacks a grant for
 * stays visible and disabled with a reason (`NOT_PERMITTED`), because an absent button teaches
 * nothing. Navigation is not a set of controls, though — it is the map of the product, and a map
 * with three greyed-out destinations does not read as "you could go here", it reads as clutter
 * on every screen forever, for a customer who may never want any of them.
 *
 * What the disabled-control rule is really protecting is the REMEDY: someone must be able to
 * learn that the capability exists and what would unlock it. One entry that names the remedy
 * does that better than three that describe the lack, so `Upgrade` is the whole message in a
 * personal account — the positive form of the same sentence, pointing at what to do rather than
 * at what is missing.
 *
 * `Live challenges` is in neither list and its route still exists: deferred, not dropped (6.2d).
 */
export const PERSONAL_NAV: NavItem[] = [
  { path: '/projects', label: 'Projects' },
  { path: '/upgrade', label: 'Upgrade' },
];

export const TEAM_NAV: NavItem[] = [
  { path: '/projects', label: 'Projects' },
  { path: '/audit', label: 'Audit' },
  { path: '/settings', label: 'Settings' },
];

@Component({
  selector: 'app-root',
  imports: [
    SignedOutComponent,
    RouterOutlet,
    RouterLink,
    ShellComponent,
    NzButtonModule,
    NzDropDownModule,
    NzIconModule,
    NzMenuModule,
    NzModalModule,
    AccountSwitcherComponent,
  ],
  templateUrl: './app.component.html',
  styleUrl: './app.component.css',
})
export class AppComponent {
  readonly session = inject(SessionService);
  readonly accounts = inject(AccountsService);
  private readonly router = inject(Router);

  /**
   * The navigation of the SELECTED ACCOUNT, recomputed when the selection changes — which is
   * what task 6.2c asks for and what the switcher's `location.reload()` used to prevent. The
   * account list is loaded before the first render (app.config's initializer), so this is right
   * the first time rather than flickering from one shape into the other.
   *
   * Nothing while no account is selected: the picker is the only screen reachable from that
   * state, and a row of links that all redirect back to it is worse than no row at all — which
   * is what an empty list gets, the shell rendering no navigation rather than an empty rule
   * across the page. Nothing either while the account is suspended: that account has exactly one
   * screen (design C5).
   *
   * And nothing ON the picker, even when an account IS selected. That screen's question is which
   * account you want to be in; wearing the current one's navigation above it answers a different
   * question and invites a click that would leave the screen you came to use. The picker is
   * reachable with a selection — the mark leads there — so this is a real state and not a
   * theoretical one.
   */
  readonly nav = computed<NavItem[]>(() => {
    if (this.onPicker()) return [];
    const account = this.accounts.current();
    if (!account || account.suspended) return [];
    return account.kind === 'personal' ? PERSONAL_NAV : TEAM_NAV;
  });

  /**
   * Whether the picker is the screen on show.
   *
   * From the router rather than from the selection, because the two are independent: a session
   * with an account selected can be standing on the picker, which is exactly the case this
   * exists for.
   *
   * `startWith` seeds it from the router's current URL, so the first render is right rather than
   * rendering the navigation and then withdrawing it once a navigation event arrives.
   */
  private readonly onPicker = toSignal(
    this.router.events.pipe(
      filter((e): e is NavigationEnd => e instanceof NavigationEnd),
      map((e) => e.urlAfterRedirects.startsWith('/accounts')),
      startWith(this.router.url.startsWith('/accounts')),
    ),
    { initialValue: this.router.url.startsWith('/accounts') },
  );

  /**
   * Warned before zero, not at it (task 6.10, design C4). It sits in the shell rather than on
   * one screen because the point is that it is seen — the consequence of an empty balance is a
   * clock the customer will not feel for weeks, so the warning is the part that protects them.
   */
  readonly lowBalance = computed(() => {
    const account = this.accounts.current();
    if (!account || account.kind !== 'team' || account.suspended) return null;
    return account.credits <= LOW_BALANCE ? account : null;
  });

  /**
   * What the routed screen belongs to. The outlet is keyed on it (see the template), so a switch
   * between two accounts re-mounts the screen even when the URL does not change — which is the
   * other half of dropping the reload: every screen fetches for one account, and the one it
   * fetched for has just gone.
   */
  readonly accountKey = computed(() => this.accounts.current()?.id ?? 'none');

  /**
   * Leaving is not signing out. "No account selected" is a state the service understands and
   * answers distinctly, so someone who has finished with one account gets back to the picker
   * without ending their session. It moved into this menu when the switcher became a select,
   * which has options but no place to hang a verb.
   */
  async leave(): Promise<void> {
    await this.accounts.leave();
    await this.router.navigateByUrl('/accounts');
  }
}
