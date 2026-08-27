import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { Router } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { Account } from '@acme/shared';
import { AccountsService } from './accounts.service';
import { openCheckout } from './checkout.modal';

/**
 * A suspended team account: the balance, and the way to fix it. Nothing else (tasks 6.6, 6.7,
 * design C5).
 *
 * Not the console in read-only. A read-only console has to answer, screen by screen, what
 * read-only means for every control — a second permission model running beside the real one, in
 * a state nobody exercises. And the screen has one job: an owner arriving here wants to know
 * what it costs to make the account work again, and everything else on the page is in the way of
 * that.
 *
 * A member sees a different sentence from the owner, and neither of them is "you do not have
 * permission" (task 6.7, design C11). The account is what is broken; nobody's rights are.
 * A member cannot normally get here at all — the service refuses to select a suspended account
 * for anyone but the owner — so this branch is what a stale tab renders after a balance runs out
 * underneath it.
 *
 * The two suspensions read differently on purpose (C6): paying lifts one of them and not the
 * other, so the reason comes from the service rather than being inferred from the balance.
 */
@Component({
  selector: 'app-suspended',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NzButtonModule, NzIconModule],
  template: `
    @if (account(); as a) {
      <div class="susp">
        <h1>{{ a.name }} is suspended</h1>
        <p class="reason">{{ a.suspendedReason || 'This account is not usable.' }}</p>

        <p class="balance">
          Balance <strong>{{ a.credits }}</strong> EUR
        </p>

        @if (a.owner) {
          <button nz-button nzType="primary" nzSize="large" (click)="addCredit(a)">
            <span nz-icon nzType="wallet"></span>
            Add credit
          </button>
          <p class="ac-hint">
            Credit takes effect immediately. Nothing in the account was deleted; it starts working
            again as soon as the balance is above zero.
          </p>
        } @else {
          <p class="ac-hint">
            This is not about your permissions. The account's owner can restore it by adding
            credit, and nobody else can — including an administrator of this account.
          </p>
        }
      </div>
    }
  `,
  styles: [
    `
      .susp {
        max-width: 34rem;
        margin: 4rem auto;
        text-align: center;
      }
      .reason {
        opacity: 0.8;
      }
      .balance {
        font-size: 1.25rem;
        margin-block: 2rem;
      }
    `,
  ],
})
export class SuspendedComponent {
  private readonly accounts = inject(AccountsService);
  private readonly modal = inject(NzModalService);
  private readonly router = inject(Router);

  protected readonly account = computed(() => this.accounts.current());

  protected addCredit(account: Account): void {
    openCheckout(this.modal, { mode: 'credit', account }).afterClose.subscribe((after?: Account) => {
      // Usable again without a new session (task 4.6): the list has been reloaded by the time
      // this fires, so the shell's navigation is already back and this screen has nothing left
      // to say.
      if (after && !after.suspended) void this.router.navigateByUrl('/projects');
    });
  }
}
