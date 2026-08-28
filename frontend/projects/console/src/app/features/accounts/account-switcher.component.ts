import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { accountIcon, accountLabel, apiErrorMessage } from '@acme/shared';
import { AccountsService } from './accounts.service';

/**
 * The account this session is acting in, and a way to change it (design C12).
 *
 * It is ALWAYS rendered, including for the user who has only their personal account. It used to
 * be absent below two accounts, on the grounds that there was no choice to offer; the cost of
 * that is a navbar that changes shape the moment a second account is bought, which is the one
 * moment a person is least sure what they just did. So with one account it is inert — the name,
 * in the place the control lives, with nothing to open. Inert rather than disabled: a disabled
 * control says something is wrong, and nothing is (task 6.1a).
 *
 * With two or more it is a select. The SEARCH box appears at five, not at two: below that the
 * whole list is on screen and a search field is a control that costs a keystroke to dismiss and
 * finds nothing you could not already see. Five is where a list stops being scannable at a
 * glance, and it is where scrolling to the wrong entry starts being possible.
 *
 * The PERSONAL account is shown as a profile icon and the word "Personal", not by its name. Its
 * name is derived from its owner's address, so rendering it here puts the signed-in person's
 * identity next to the user menu that already says it — the same word twice, a centimetre apart,
 * one of them pretending to be the name of a thing. "Personal" is also what the person calls it.
 *
 * A SUSPENDED account is listed and marked, never omitted (task 6.1b): its owner is the only
 * person who can pay for it, and the picker is how they reach it. Selecting one as a member is
 * refused by the service with a reason that says suspended rather than forbidden, and that
 * sentence is shown here verbatim (task 6.7).
 */
@Component({
  selector: 'app-account-switcher',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, RouterLink, NzButtonModule, NzIconModule, NzSelectModule, NzToolTipModule],
  template: `
    <div class="switcher">
      @if (accounts.hasChoice()) {
        <nz-select
          class="pick"
          [nzShowSearch]="searchable()"
          nzBorderless
          [nzOptionOverflowSize]="12"
          [ngModel]="accounts.current()?.id ?? null"
          (ngModelChange)="choose($event)"
          [nzDisabled]="switching()"
          nzPlaceHolder="No account"
        >
          @for (a of accounts.accounts(); track a.id) {
            <!--
              nzLabel is what the CLOSED control shows and what a search matches on, so it carries
              the same word the row does. A personal account whose label stayed its derived name
              would read "Personal" in the list and something else once picked.
            -->
            <nz-option nzCustomContent [nzValue]="a.id" [nzLabel]="label(a)">
              @if (a.kind === 'personal') {
                <span nz-icon nzType="user" class="kind"></span>
              }
              {{ label(a) }}
              @if (a.suspended) {
                <small class="tag warn">suspended</small>
              }
            </nz-option>
          }
        </nz-select>
      } @else {
        <!--
          The one-account case, and in practice that account is almost always the personal one —
          which is exactly where showing its derived name would repeat the user menu beside it.
          The icon follows the same rule as the list so the control does not change appearance
          the day a second account arrives.
        -->
        <!--
          A control-flow guard, NOT a non-null assertion. This branch renders whenever there is no
          CHOICE,
          which includes having no account selected at all — during the picker, and on the first
          paint before the list has loaded. A current() non-null assertion there is a lie the template
          tells the compiler, and it costs a TypeError that takes the whole screen out rather than
          leaving one control blank.
        -->
        @if (accounts.current(); as a) {
          <span class="pick inert" [attr.title]="a.slug">
            <span nz-icon [nzType]="icon(a)"></span>
            {{ label(a) }}
          </span>
        } @else {
          <span class="pick inert">No account</span>
        }
      }

      <!--
        Add account is the buy flow (C1, C12) and leads exactly where Upgrade in a personal
        account's navigation leads. Two entrances, one room: this one is found by someone
        looking at which account they are in.
      -->
      <button
        nz-button
        nzType="text"
        routerLink="/upgrade"
        nz-tooltip
        nzTooltipTitle="Add a team account"
        aria-label="Add a team account"
      >
        <span nz-icon nzType="plus"></span>
      </button>
    </div>
  `,
  styles: [
    `
      /* No 'margin-inline-start: auto' any more. This used to be what pushed the whole
       * right-hand group of the bar over, which made the bar's layout depend on a margin set by
       * the first component that happened to sit in it — remove the switcher and the user menu
       * drifted to the middle. The shared shell lays its bar out with 'space-between', so the
       * group is at the window's edge whatever is in it. */
      .switcher {
        display: flex;
        align-items: center;
        gap: 0.25rem;
      }
      /* The select and the inert label occupy the same box, so the bar does not move when a
       * second account arrives. */
      .pick {
        min-width: 12rem;
      }
      .inert {
        display: inline-flex;
        align-items: center;
        gap: 0.4rem;
        padding-inline: 0.5rem;
        color: rgb(255 255 255 / 65%);
      }
      .tag {
        margin-inline-start: 0.5rem;
        opacity: 0.6;
      }
      /* Sits with the word rather than in a column of its own: only the personal row has one, so
       * a reserved gutter would indent every other row against nothing. */
      .kind {
        margin-inline-end: 0.4rem;
        opacity: 0.75;
      }
      .tag.warn {
        color: var(--ac-warn);
        opacity: 1;
      }
    `,
  ],
})
export class AccountSwitcherComponent {
  protected readonly accounts = inject(AccountsService);
  private readonly message = inject(NzMessageService);
  private readonly router = inject(Router);

  protected readonly switching = signal(false);

  /**
   * The search box appears at five accounts.
   *
   * Below that the whole list is visible at once, and a search field is then a control that has
   * to be dismissed to see what it was covering. Five is where scanning stops working.
   */
  protected readonly searchable = computed(() => this.accounts.accounts().length >= 5);

  /**
   * What an account is called and shown as.
   *
   * From `shared`, because the PICKER renders the same pair and the two are on screen together
   * once an account is selected — a rule kept in two places is one that will be edited in one.
   */
  protected readonly label = accountLabel;
  protected readonly icon = accountIcon;

  /**
   * There is no ngOnInit here any more. The list is loaded before the first render (app.config's
   * initializer) because the navigation is computed from the selected account's kind, and a list
   * that arrives afterwards means a navbar that changes shape after the page has settled.
   */
  protected async choose(id: string): Promise<void> {
    if (this.switching() || id === this.accounts.current()?.id) return;
    this.switching.set(true);
    try {
      await this.accounts.select(id);
      // No location.reload(). The switch changes what the navigation contains (task 6.2c), and a
      // reload would throw away the very thing that has to re-render. Everything below the shell
      // belongs to the account that just changed, so this navigates to that account's front
      // screen; the shell re-mounts the outlet on the selection itself, which is what deals with
      // switching while already standing on it.
      await this.router.navigateByUrl('/projects');
    } catch (err) {
      // A refused switch — a suspended account picked by somebody who is not its owner — leaves
      // the session where it was, and says why in the service's own words.
      this.message.error(apiErrorMessage(err));
    } finally {
      this.switching.set(false);
    }
  }
}
