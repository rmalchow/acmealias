import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { Account, LOW_BALANCE, PageHeaderComponent } from '@acme/shared';
import { AccountsService } from '../accounts/accounts.service';
import { openCheckout } from '../accounts/checkout.modal';
import { RolesComponent } from '../roles/roles.component';
import { SystemKeysPanelComponent } from '../apikeys/system-keys-panel.component';

/**
 * Settings: what administers the ACCOUNT, as opposed to the inventory it holds (task 6.5).
 *
 * Three things live here — the account itself, its balance, and who is in it with which role —
 * and they are together because they answer one question that Projects does not: what is this
 * account and who runs it. The roles and members screen used to be a top-level entry beside
 * Projects; it is a section here now (task 6.5a), which is why it renders no heading of its own.
 *
 * TEAM ACCOUNTS ONLY, and the guard on the route is what makes that true rather than an `@if`
 * here. A personal account has no members and no roles because there is nobody to give one to
 * (design C9), and the service refuses the roster routes there with a 409 saying so — this
 * screen would render two errors and a balance of zero. `Upgrade` carries that message once, in
 * the navigation, instead (C11).
 */
@Component({
  selector: 'app-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, NzButtonModule, NzIconModule, NzToolTipModule, PageHeaderComponent, RolesComponent, SystemKeysPanelComponent],
  template: `
    @if (account(); as a) {
      <app-page-header [crumbs]="[{ label: 'Settings' }]" title="Settings" [subtitle]="a.name" />

      <section class="cols">
        <div>
          <h2>Account</h2>
          <dl>
            <dt>Name</dt>
            <dd>{{ a.name }}</dd>
            <dt>Slug</dt>
            <dd><code>{{ a.slug }}</code></dd>
            <dt>Created</dt>
            <dd>{{ a.createdAt | date: 'medium' }}</dd>
            <dt>Owner</dt>
            <dd>{{ a.owner ? 'You' : 'Someone else in this account' }}</dd>
          </dl>
        </div>

        <div>
          <h2>Credits</h2>
          <p class="balance">
            <strong>{{ a.credits }}</strong> EUR
          </p>
          @if (low()) {
            <!--
              Warned before zero, not at it (task 6.10, design C4). The refusal when the balance
              runs out is soft — renewals stop and certificates already on the wire keep working
              for weeks — so the warning is the part that actually protects the customer.
            -->
            <p class="warn">
              Below {{ LOW_BALANCE }} EUR. When the balance reaches zero this account stops:
              members cannot open it and challenges are refused, so certificates stop renewing.
            </p>
          }
          <!--
            Disabled with a reason rather than hidden, and the reason is NOT "your permissions"
            (design C11): only the owner may pay, and that is a fact about the account rather than
            a grant anybody in it could be given.
          -->
          <button
            nz-button
            nzType="primary"
            [disabled]="!a.owner"
            nz-tooltip
            [nzTooltipTitle]="a.owner ? null : NOT_THE_OWNER"
            (click)="addCredit(a)"
          >
            <span nz-icon nzType="wallet"></span>
            Add credit
          </button>
        </div>
      </section>

      <!-- Its own two headings, Roles and Members, carry this part; a third above them would
           only name the pair. -->
      <app-roles />

      <!--
        And the members that are not people (design K8). It sits after the roster rather than with
        the inventory because that is what it is: a system key holds a role, so it belongs beside
        the roles it can hold. A key for one project's challenge answering is made on that
        project's page, dies with the project, and is deliberately not offered here.
      -->
      <section class="keys">
        <app-system-keys-panel />
      </section>
    }
  `,
  styles: [
    `
      section {
        margin-block-end: 2.5rem;
      }
      .cols {
        display: grid;
        gap: 2.5rem;
        grid-template-columns: repeat(auto-fit, minmax(18rem, 1fr));
      }
      dl {
        display: grid;
        grid-template-columns: 6rem 1fr;
        gap: 0.35rem 1rem;
        margin: 0;
      }
      dt {
        opacity: 0.6;
      }
      dd {
        margin: 0;
      }
      .balance {
        font-size: 1.5rem;
        margin-block: 0.5rem 1rem;
      }
      .warn {
        color: var(--ac-warn);
        max-width: 32rem;
      }
      .keys {
        margin-block-start: 2.5rem;
      }
    `,
  ],
})
export class SettingsComponent {
  private readonly accounts = inject(AccountsService);
  private readonly modal = inject(NzModalService);

  protected readonly account = computed(() => this.accounts.current());
  protected readonly low = computed(() => {
    const a = this.account();
    return !!a && a.kind === 'team' && a.credits > 0 && a.credits <= LOW_BALANCE;
  });

  protected readonly LOW_BALANCE = LOW_BALANCE;
  /**
   * A different sentence from the generic one, because the remedy is different: nobody in this
   * account can be granted the right to pay for it. Transferring ownership is what moves it.
   */
  protected readonly NOT_THE_OWNER = 'Only the account owner can add credit.';

  protected addCredit(account: Account): void {
    // The dialog does the POST and reloads the list, so the balance above is current by the time
    // it closes — there is nothing to do here with the account it hands back.
    openCheckout(this.modal, { mode: 'credit', account });
  }
}
