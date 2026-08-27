import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { apiErrorMessage } from '@acme/shared';
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
 * With two or more it is a searchable select rather than a dropdown menu, because a customer who
 * pays for several accounts will have several, and a list that has to be scrolled is a list that
 * gets the wrong entry picked.
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
          nzShowSearch
          nzBorderless
          [nzOptionOverflowSize]="12"
          [ngModel]="accounts.current()?.id ?? null"
          (ngModelChange)="choose($event)"
          [nzDisabled]="switching()"
          nzPlaceHolder="No account"
        >
          @for (a of accounts.accounts(); track a.id) {
            <nz-option nzCustomContent [nzValue]="a.id" [nzLabel]="a.name">
              {{ a.name }}
              @if (a.kind === 'personal') {
                <small class="tag">personal</small>
              }
              @if (a.suspended) {
                <small class="tag warn">suspended</small>
              }
            </nz-option>
          }
        </nz-select>
      } @else {
        <span class="pick inert" [attr.title]="accounts.current()?.slug ?? null">
          <span nz-icon nzType="cluster"></span>
          {{ accounts.current()?.name ?? 'No account' }}
        </span>
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
      .switcher {
        display: flex;
        align-items: center;
        gap: 0.25rem;
        margin-inline-start: auto;
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
