import { inject, Injectable, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiService, SessionService } from '@acme/shared';
/** One account this person belongs to, as `GET /api/v1/accounts` reports it. */
export interface Account {
  id: string;
  slug: string;
  name: string;
  createdAt: string;
  selected: boolean;
}

/**
 * The accounts a session can act in, and which one it is acting in.
 *
 * A session is in one of exactly two states (design P19): no account selected, from which only
 * the profile, this list, creating an account and accepting an invitation are reachable; or one
 * selected, from which everything that account's grants permit is.
 *
 * A single-account deployment selects at sign-in and never enters the first state, which is why
 * the shell below renders neither a picker nor a switcher there — and why an operator of the
 * internal edition sees no change.
 */
@Injectable({ providedIn: 'root' })
export class AccountsService {
  private readonly api = inject(ApiService);
  private readonly session = inject(SessionService);

  readonly accounts = signal<Account[]>([]);
  readonly loading = signal(false);

  /** Whether to render any account UI at all: more than one to choose between. */
  hasChoice(): boolean {
    return this.accounts().length > 1;
  }

  current(): Account | null {
    return this.accounts().find((a) => a.selected) ?? null;
  }

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
   * session was established, so a membership revoked mid-session takes effect here. The whole
   * app reloads its data afterwards, because everything below is scoped to the account that just
   * changed.
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
   * picker — and a person in one is unlikely to want this, which is why it lives in the switcher
   * rather than beside every screen.
   */
  async leave(): Promise<void> {
    await firstValueFrom(this.api.rootPost<void>('/accounts/none/select'));
    this.api.setAccount(null);
    await this.session.load();
    await this.load();
  }

  async create(slug: string, name: string): Promise<Account> {
    const created = await firstValueFrom(this.api.rootPost<Account>('/accounts', { slug, name }));
    this.api.setAccount(created.id);
    await this.session.load();
    await this.load();
    return created;
  }
}
