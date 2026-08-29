import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { Router } from '@angular/router';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { Account, InfoBoxComponent, NOT_IN_PERSONAL_ACCOUNT, PageHeaderComponent, SessionService } from '@acme/shared';
import { openCheckout } from './checkout.modal';

/**
 * What a team account is, and a way to buy one (tasks 6.1c, 6.2b, design C12).
 *
 * ONE room with two doors. `Upgrade` in a personal account's navigation and `Add account` beside
 * the switcher both arrive here, and that is deliberate rather than duplicated: one is found by
 * a person looking at what this account cannot do, the other by a person looking at which
 * account they are in, and neither would look where the other does.
 *
 * It is outside the account guard. Buying an account needs no account selected — it is one of
 * the things reachable from the picker (design P19), and it is how a user with only their
 * personal account gets a second one.
 *
 * It explains before it sells because a team account is not a bigger personal account: it is a
 * different object with people in it (C1), and the one thing that is genuinely irreversible here
 * is that a personal account never becomes one. Someone about to pay should know that first.
 */
@Component({
  selector: 'app-upgrade',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NzAlertModule, NzButtonModule, NzIconModule, PageHeaderComponent, InfoBoxComponent],
  template: `
    <app-page-header
      [crumbs]="[{ label: 'Team account' }]"
      title="Add a team account"
      subtitle="Colleagues, roles, and no project limit."
    />

    <div class="up">
      @if (from()) {
        <!--
          Somebody followed a link to a screen a personal account does not have. The sentence is
          NOT the permissions one (design C11, task 6.4): being told your permissions are short
          would send the owner of an account of one looking for whoever can widen them, and there
          is nobody — the remedy is a different kind of account, which is the page they are on.
        -->
        <app-info-box key="team-only" [heading]="NOT_IN_PERSONAL_ACCOUNT" [body]="asked" />
        <ng-template #asked>
          <code>{{ from() }}</code> belongs to a team account.
        </ng-template>
      }

      <p class="lead">
        Your personal account is yours alone and always free. A team account is a separate place
        to work, with other people in it — you decide who, and what each of them may do.
      </p>

      <dl>
        <dt>People</dt>
        <dd>Members you add, each holding a role you define. A personal account has no members.</dd>

        <dt>Roles</dt>
        <dd>
          Named sets of grants — who may change a load balancer, who may only read, who may rotate
          keys. New accounts start with a sensible set that you can edit or replace.
        </dd>

        <dt>Projects</dt>
        <dd>
          As many as you need. A personal account holds five; a team account has no limit.
        </dd>

        <!--
          THE CREDITS ROW IS BEHIND THE SWITCH, not deleted. A deployment that charges has to
          explain the balance on the screen where somebody acquires one; a deployment that charges
          nobody must not describe a limit it does not apply, and "a team account works while its
          balance is above zero" is exactly the sentence that would send a customer looking for a
          Settings panel with nothing in it.
        -->
        @if (monetized()) {
          <dt>Credits</dt>
          <dd>
            A team account works while its balance is above zero. You add credit here, and you can
            add more at any time from Settings.
          </dd>
        }
      </dl>

      <!--
        Buying IS creating (design C1): there is no unfunded team account to make first and pay for
        later, so this button is the only way one comes into existence. Where nothing is charged
        that sentence loses its first half and the button says the half that is left.

        No cart icon off the monetized path, and not merely because the word changed: a trolley is
        a promise that the next screen asks for money, and the next screen asks for a name.
      -->
      <button nz-button nzType="primary" nzSize="large" (click)="buy()">
        @if (monetized()) {
          <span nz-icon nzType="shopping-cart"></span>
          Buy now
        } @else {
          Create
        }
      </button>

      <!--
        A note box, not a hint line. This answers the question somebody actually hesitates on before
        paying — "does buying disturb what I already have?" — and a grey line under a button is where
        reassurance goes to be skimmed past.

        The second sentence is deliberately in the FUTURE tense. Moving projects between accounts is
        not built. Writing it as though it were would be worse here than anywhere else on the site or
        in the console, because this is the one screen where somebody spends money on the strength of
        what it says: they would buy a team account expecting to move their personal projects into
        it, and find they cannot. So it says what is true — that nothing is permanently tied — and
        does not claim the mechanism exists yet.
      -->
      <app-info-box
        key="personal-is-safe"
        heading="Your personal account is not affected"
        [text]="personalIsSafe()"
      />
    </div>
  `,
  styles: [
    `
      .up {
        max-width: 44rem;
      }
      /* The note under the button needs room above it. The box's own margin is a BOTTOM one, set
       * for the boxes higher up the page that precede their content; this one FOLLOWS the control
       * it qualifies, so without a top margin it sits against the button and reads as part of it.
       *
       * It selects app-info-box and not nz-alert. It said nz-alert until these boxes became
       * collapsible, at which point the selector matched nothing and the button and the box closed
       * up against each other — a rule that silently stops applying is the failure mode of styling
       * by element name, and the reason this one now names the component. */
      .up > app-info-box:last-of-type {
        margin-block-start: 1.25rem;
      }
      /* Names the component, not the element it used to render. app-info-box sets its own
         display; what is left here is the gap this screen wants between its boxes. */
      app-info-box {
        margin-block-end: 1.5rem;
      }
      .lead {
        font-size: 1.05rem;
        opacity: 0.85;
      }
      dl {
        display: grid;
        grid-template-columns: 8rem 1fr;
        gap: 0.5rem 1.5rem;
        margin-block: 1.5rem 2rem;
      }
      dt {
        font-weight: 500;
        color: rgb(255 255 255 / 85%);
      }
      dd {
        margin: 0;
        opacity: 0.75;
      }
    `,
  ],
})
export class UpgradeComponent {
  private readonly modal = inject(NzModalService);
  private readonly router = inject(Router);
  private readonly session = inject(SessionService);

  /**
   * Whether this deployment charges for anything, from /me.
   *
   * Everything about money on this screen hangs off it — the Credits row, the word on the button,
   * the trolley, the first clause of the note below it. Not deleted, because the credit machinery
   * underneath is intact and this screen is what a deployment that turns the switch on needs to
   * have kept.
   *
   * It defaults to FALSE while /me is still loading, which is the safe direction: a screen that
   * flickers "Buy now" on a service that charges nobody is worse than one that says Create and
   * gains a price a moment later, because only the first one is a claim about money.
   */
  protected readonly monetized = computed(() => this.session.me()?.monetized ?? false);

  /** The note's body. A field rather than markup so the sentences stay one paragraph. */
  protected readonly personalIsSafe = computed(
    () =>
      (this.monetized()
        ? 'Buying does not change your personal account, move anything out of it, or use it up. '
        : 'Adding one does not change your personal account, move anything out of it, or use it up. ') +
      'Projects are not permanently tied to the account they were made in either — moving them ' +
      'between accounts is not built yet, and when it is, nothing you create now will be stranded.',
  );

  /**
   * The screen somebody asked for and did not get, when a guard sent them here. Bound from the
   * query string by the router's component-input binding; absent when they came by the
   * navigation or by the switcher, which is the ordinary way in.
   */
  readonly from = input<string | undefined>();

  protected readonly NOT_IN_PERSONAL_ACCOUNT = NOT_IN_PERSONAL_ACCOUNT;

  protected buy(): void {
    openCheckout(this.modal, { mode: 'buy' }, this.monetized()).afterClose.subscribe((created?: Account) => {
      // The service selects a freshly bought account for its owner, so the honest next screen is
      // the account itself rather than this one with a message on it.
      if (created) void this.router.navigateByUrl('/projects');
    });
  }
}
