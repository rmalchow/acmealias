import { ChangeDetectionStrategy, Component, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { Account, apiErrorMessage } from '@acme/shared';
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
  imports: [FormsModule, NzButtonModule, NzFormModule, NzInputModule, NzRadioModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      @if (data.mode === 'buy') {
        <nz-form-item>
          <nz-form-label nzRequired>Slug</nz-form-label>
          <nz-form-control nzExtra="Lower case, digits and hyphens. Appears in URLs and CLI arguments.">
            <input nz-input name="slug" [(ngModel)]="slug" required pattern="[a-z0-9-]+" autocomplete="off" />
          </nz-form-control>
        </nz-form-item>

        <nz-form-item>
          <nz-form-label nzRequired>Name</nz-form-label>
          <nz-form-control>
            <input nz-input name="name" [(ngModel)]="name" required autocomplete="off" />
          </nz-form-control>
        </nz-form-item>
      } @else {
        <p>
          {{ data.account?.name }} has {{ data.account?.credits }} {{ currency() }} in it. Adding
          credit takes effect immediately — there is no new sign-in and no wait.
        </p>
      }

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

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <!-- Submit lives in the form so Enter works; the modal's own footer is switched off. -->
      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button nz-button nzType="primary" type="submit" [nzLoading]="saving()" [disabled]="!ready()">
          {{ data.mode === 'buy' ? 'Buy now' : 'Add credit' }}
        </button>
      </div>
    </form>
  `,
  styles: [
    `
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
  private readonly accounts = inject(AccountsService);
  readonly ref = inject(NzModalRef<CheckoutModalComponent, Account | undefined>);
  readonly data = inject<CheckoutData>(NZ_MODAL_DATA);

  protected readonly amounts = signal<number[]>([]);
  protected readonly currency = signal('EUR');
  protected readonly saving = signal(false);
  protected readonly error = signal<string | null>(null);

  protected slug = '';
  protected name = '';
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

  protected ready(): boolean {
    if (this.amount === null) return false;
    return this.data.mode === 'credit' || (!!this.slug && !!this.name);
  }

  protected async save(): Promise<void> {
    if (!this.ready() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    try {
      const account =
        this.data.mode === 'buy'
          ? await this.accounts.buy(this.slug, this.name, this.amount as number)
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

/** Opens checkout. `afterClose` emits the funded account, or undefined if it was cancelled. */
export function openCheckout(modal: NzModalService, data: CheckoutData): NzModalRef<CheckoutModalComponent, Account | undefined> {
  return modal.create<CheckoutModalComponent, CheckoutData>({
    nzTitle: data.mode === 'buy' ? 'Buy a team account' : 'Add credit',
    nzContent: CheckoutModalComponent,
    nzData: data,
    nzFooter: null,
    nzMaskClosable: false,
    nzWidth: 480,
  }) as NzModalRef<CheckoutModalComponent, Account | undefined>;
}
