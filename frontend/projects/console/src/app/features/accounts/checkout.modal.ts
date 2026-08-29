import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { Account, DerivedSlug, MARKETING_SITE, SessionService, apiErrorMessage } from '@acme/shared';
import { AccountsService } from './accounts.service';

/**
 * Which of the two checkouts this is.
 *
 * `buy` creates a team account and funds it in one act; `credit` adds to one that exists. They
 * are one dialog because they are one flow with one step's difference — the same amounts, the
 * same refusals, the same balance afterwards — and because the second is what the first becomes
 * on the day the account lapses.
 */
export interface CheckoutData {
  mode: 'buy' | 'credit';
  /** The account being credited. Absent in `buy`, where it does not exist yet. */
  account?: Account;
}

/**
 * Checkout: choose an amount, pay, come back with the balance (task 6.8).
 *
 * The POST happens HERE rather than in the screen behind it, which is the pattern every dialog
 * in this console follows for one reason: the failures that actually happen — a slug already
 * taken, a deployment with account creation switched off, an account somebody else has since
 * transferred away — have to land next to the fields, with what was typed still in place.
 *
 * There is no "no payment will be taken" banner and that is deliberate (task 5.5). What makes
 * this safe is that there is no processor behind it, not a notice that would have to be deleted
 * the day there is one — and a screen carrying such a notice is not the screen that ships.
 *
 * The amounts are fetched rather than written here, from the same list the service validates
 * against (design C8), so this cannot offer a button the API refuses.
 */
@Component({
  selector: 'app-checkout-modal',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, NzAlertModule, NzButtonModule, NzFormModule, NzInputModule, NzRadioModule],
  template: `
    <!--
      ABOVE the form, not beside the button.

      The credit is real — it is written to the account and spent like any other — and no money
      moves. Somebody has to know that BEFORE they fill in a slug and a name and pick an amount,
      not after: a person who works through a form and is then told it was not a purchase has been
      allowed to misunderstand for the length of the task, which is worse than not being told.

      It links out rather than explaining here. A modal is not where to write three paragraphs
      about the state of a business, and the page it points at can say more and change without a
      release.
    -->
    <!--
      THE WARNING EXISTS ONLY WHERE THERE IS SOMETHING TO WARN ABOUT. It said "payment is not live"
      beside a form that asks for money; a deployment that asks for no money has no such form, and a
      notice that nothing is being charged, on a screen that charges nothing and mentions no price,
      is a sentence that raises the question it answers.
    -->
    @if (monetized()) {
      <nz-alert class="not-live" nzType="warning" nzMessage="Payment is not live" [nzDescription]="notLive" />
      <ng-template #notLive>
        Nothing is charged and no card is asked for. The credit is real — it lands in the account and
        is spent like any other — but no money moves.
        <a [href]="about" target="_blank" rel="noopener">
          There are other ways to support this</a
        >.
      </ng-template>
    }

    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      @if (data.mode === 'buy') {
        <!--
          Name first and the slug derived from it, the same way New project does it. Somebody
          buying a team account knows what the team is called; the URL-safe spelling of it is a
          consequence, not a second decision, and asking for the slug first opened the dialog on
          the one field they had not thought about.

          DerivedSlug and not NameSlugModalComponent itself, though the two fields are now
          identical: this dialog also carries the not-live alert and an amount, and it POSTs to
          /accounts with that amount, which the shared modal has no concept of. Sharing the pair
          rather than the component keeps the rule in one place without pretending the two dialogs
          are one.
        -->
        <nz-form-item>
          <nz-form-label nzRequired>Name</nz-form-label>
          <nz-form-control>
            <input
              nz-input
              name="name"
              [ngModel]="pair.name"
              (ngModelChange)="pair.setName($event)"
              required
              autocomplete="off"
            />
          </nz-form-control>
        </nz-form-item>

        <nz-form-item>
          <nz-form-label nzRequired>Slug</nz-form-label>
          <nz-form-control nzExtra="Lower case, digits and hyphens. Appears in URLs and CLI arguments.">
            <input
              nz-input
              name="slug"
              [ngModel]="pair.slug"
              (ngModelChange)="pair.setSlug($event)"
              required
              pattern="[a-z0-9-]+"
              autocomplete="off"
            />
          </nz-form-control>
        </nz-form-item>
      } @else {
        <p>
          {{ data.account?.name }} has {{ data.account?.credits }} {{ currency() }} in it. Adding
          credit takes effect immediately — there is no new sign-in and no wait.
        </p>
      }

      @if (monetized()) {
      <nz-form-item>
        <nz-form-label nzRequired>Amount</nz-form-label>
        <nz-form-control>
          <nz-radio-group [(ngModel)]="amount" name="amount">
            @for (a of amounts(); track a) {
              <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
         nz-radio-button renders its own input inside this label and owns the association; the
         rule cannot see through the directive. -->
              <label nz-radio-button [nzValue]="a">{{ a }} {{ currency() }}</label>
            }
          </nz-radio-group>
        </nz-form-control>
      </nz-form-item>
      }

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <!-- Submit lives in the form so Enter works; the modal's own footer is switched off. -->
      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button nz-button nzType="primary" type="submit" [nzLoading]="saving()" [disabled]="!ready()">
          {{ okLabel() }}
        </button>
      </div>
    </form>
  `,
  styles: [
    `
      /* Above the form and clear of it. The modal's own padding puts the form hard against the
         alert otherwise, and a warning touching the field it warns about reads as that field's
         validation error. */
      .not-live {
        display: block;
        margin-block-end: 1.25rem;
      }

      .actions {
        display: flex;
        justify-content: flex-end;
        gap: 0.5rem;
        margin-top: 1.5rem;
      }
    `,
  ],
})
export class CheckoutModalComponent implements OnInit {
  /** The site explains what "not live" means; a modal is not the place for three paragraphs. */
  protected readonly about = MARKETING_SITE + '/about';

  private readonly accounts = inject(AccountsService);
  private readonly session = inject(SessionService);
  readonly ref = inject(NzModalRef<CheckoutModalComponent, Account | undefined>);
  readonly data = inject<CheckoutData>(NZ_MODAL_DATA);

  protected readonly amounts = signal<number[]>([]);
  protected readonly currency = signal('EUR');
  protected readonly saving = signal(false);
  protected readonly error = signal<string | null>(null);

  protected readonly pair = new DerivedSlug();
  protected amount: number | null = null;

  async ngOnInit(): Promise<void> {
    try {
      const offer = await this.accounts.offer();
      this.amounts.set(offer.amounts);
      this.currency.set(offer.currency);
      // Preselected rather than left blank: every amount is a valid answer and the smallest is
      // the one a person choosing nothing in particular means.
      this.amount = offer.amounts[0] ?? null;
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    }
  }

  /**
   * Whether this deployment charges for anything, from /me.
   *
   * The dialog keeps BOTH shapes rather than losing the paid one: the credit machinery underneath
   * is intact, and this is the screen a deployment that turns the switch on gets back.
   */
  protected readonly monetized = computed(() => this.session.me()?.monetized ?? false);

  /**
   * What the primary button says.
   *
   * `Create` and not `Buy now` where nothing is charged, and the distinction is not decoration: a
   * button that says Buy is a promise about what happens next, and on this deployment what happens
   * next is that an account exists.
   */
  protected readonly okLabel = computed(() => {
    if (this.data.mode === 'credit') return 'Add credit';
    return this.monetized() ? 'Buy now' : 'Create';
  });

  protected ready(): boolean {
    // The amount is only a requirement where it is a question. Unmonetized, the service ignores
    // whatever is sent (createAccount zeroes it), so demanding one here would be the console
    // enforcing a rule the service has stopped having.
    if (this.monetized() && this.amount === null) return false;
    return this.data.mode === 'credit' || this.pair.complete;
  }

  protected async save(): Promise<void> {
    if (!this.ready() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      const account =
        this.data.mode === 'buy'
          ? await this.accounts.buy(this.pair.slug, this.pair.name, this.amount as number)
          : await this.accounts.checkout(this.data.account!.id, this.amount as number);
      // The account comes back through afterClose so the caller can show the new balance without
      // reloading the page — the balance is the one thing the person was here for.
      this.ref.close(account);
    } catch (err) {
      this.saving.set(false);
      this.error.set(apiErrorMessage(err));
    }
  }
}

/**
 * Opens checkout. `afterClose` emits the funded account, or undefined if it was cancelled.
 *
 * `monetized` is a PARAMETER and not read from the session here, which looks like duplication of
 * what the component already computes and is not. A modal's title is set when the modal is created,
 * from outside any component, so this function runs in no injection context and cannot reach
 * SessionService at all — and the caller that opens a `buy` dialog is the upgrade screen, which
 * has the answer in hand because its own button is labelled from it. Defaulted to false so the two
 * `credit` callers, whose title does not depend on it, say nothing about money they do not mean.
 */
export function openCheckout(
  modal: NzModalService,
  data: CheckoutData,
  monetized = false,
): NzModalRef<CheckoutModalComponent, Account | undefined> {
  return modal.create<CheckoutModalComponent, CheckoutData>({
    // "Buy a team account" is a claim about what pressing the button does. Where nothing is
    // charged it does not buy anything, and the heading of the dialog is the first place somebody
    // reads what they are about to do.
    nzTitle: data.mode !== 'buy' ? 'Add credit' : monetized ? 'Buy a team account' : 'Create a team account',
    nzContent: CheckoutModalComponent,
    nzData: data,
    nzFooter: null,
    nzMaskClosable: false,
    nzWidth: 480,
  }) as NzModalRef<CheckoutModalComponent, Account | undefined>;
}
