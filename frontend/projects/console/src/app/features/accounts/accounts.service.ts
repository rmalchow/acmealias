import { computed, inject, Injectable, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { Account, ApiService, SessionService } from '@acme/shared';

/** What a checkout may ask for, as `GET /api/v1/checkout/amounts` offers it. */
export interface CheckoutOffer {
  amounts: number[];
  currency: string;
}

/**
 * The accounts a session can act in, which one it is acting in, and what that one is.
 *
 * A session is in one of exactly two states (design P19): no account selected, from which only
 * the profile, this list, buying an account and selecting one are reachable; or one selected,
 * from which everything that account's grants permit is. The first state is where every session
 * now begins, and it is never empty — a user has a personal account because they are a user
 * (C1).
 *
 * This is also where the console learns what KIND of account it is in, which the navigation is
 * computed from (C11) and which nothing else can answer: `/api/v1/me` carries the selection but
 * not the account behind it. That is why the list is loaded before the first render (see the
 * initializer in app.config.ts) rather than from a screen's ngOnInit — navigation that has to
 * wait for a second request renders in the personal shape and then changes under the pointer.
 */
@Injectable({ providedIn: 'root' })
export class AccountsService {
  private readonly api = inject(ApiService);
  private readonly session = inject(SessionService);

  readonly accounts = signal<Account[]>([]);
  readonly loading = signal(false);

  /**
   * The account this session is acting in, or null.
   *
   * A signal rather than a method, because the shell's navigation and the account-scoped screens
   * are recomputed from it the moment a switch lands — which is what replaces the page reload
   * the switcher used to do (task 6.2c).
   */
  readonly current = computed<Account | null>(() => this.accounts().find((a) => a.selected) ?? null);

  /**
   * Whether there is anything to switch TO. Not whether to render the switcher: it is rendered
   * either way (task 6.1a, design C12), showing the one account and offering no choice, so that
   * the navbar does not change shape the day a second account is bought.
   */
  readonly hasChoice = computed(() => this.accounts().length > 1);

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.accounts.set(await firstValueFrom(this.api.rootGet<Account[]>('/accounts')));
    } finally {
      this.loading.set(false);
    }
  }

  /**
   * Switches the session into one account.
   *
   * Membership is re-evaluated server-side at the switch rather than trusted from whenever the
   * session was established, so a membership revoked mid-session takes effect here — and so does
   * a balance that has run out: the service refuses to select a suspended account for anyone but
   * its owner, with a reason that says "suspended" rather than "forbidden" (design C4, task
   * 6.7). That refusal arrives here as a rejected promise and is shown verbatim.
   */
  async select(id: string): Promise<void> {
    await firstValueFrom(this.api.rootPost<void>(`/accounts/${id}/select`));
    this.api.setAccount(id);
    await this.session.load();
    await this.load();
  }

  /**
   * Leaves the account this session is acting in, without signing out.
   *
   * Selection is nullable for exactly this reason (design P19): "no account selected" is a state
   * the service understands and answers distinctly, not an error to be avoided. A person in
   * several accounts who has finished with one should not have to sign out to get back to the
   * picker.
   */
  async leave(): Promise<void> {
    await firstValueFrom(this.api.rootPost<void>('/accounts/none/select'));
    this.api.setAccount(null);
    await this.session.load();
    await this.load();
  }

  /** The amounts a checkout may ask for, served from the same list the service validates against. */
  offer(): Promise<CheckoutOffer> {
    return firstValueFrom(this.api.rootGet<CheckoutOffer>('/checkout/amounts'));
  }

  /**
   * Creates a team account by paying for it (task 5.4, design C1, C8).
   *
   * Creating and buying are one act, so there is no create() without an amount: an account that
   * exists and was never funded would put its owner on the pay screen of something they had just
   * made. The service selects it for them, which is why this reloads the session too.
   */
  async buy(slug: string, name: string, amount: number): Promise<Account> {
    const created = await firstValueFrom(this.api.rootPost<Account>('/accounts', { slug, name, amount }));
    this.api.setAccount(created.id);
    await this.session.load();
    await this.load();
    return created;
  }

  /**
   * Adds credit to an account that already exists.
   *
   * Deliberately NOT an account-scoped call: the person this exists for owns an account that is
   * suspended, and every scoped route refuses one of those. Ownership is what gates it
   * server-side (design C5).
   */
  async checkout(id: string, amount: number): Promise<Account> {
    const after = await firstValueFrom(this.api.rootPost<Account>(`/accounts/${id}/checkout`, { amount }));
    await this.load();
    return after;
  }
}
