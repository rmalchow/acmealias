import { inject, Injectable, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiService } from '@acme/shared';
import { Invitation, InvitationOffer } from './invitations.models';

/**
 * The two halves of the invitation flow, which use two different prefixes — and that split is the
 * whole reason this service exists rather than the calls being written where they are used.
 *
 * ISSUING, REVOKING AND RE-SENDING are things an account does, so they go through the
 * account-scoped prefix and the service checks that the caller is in the account's unrestricted
 * role.
 *
 * RESOLVING, ANSWERING AND LISTING YOUR OWN are things a person does BEFORE they belong to
 * anything. They go through `root*`, because a session with no account selected has no scoped
 * prefix to use — `ApiService.base` throws for exactly that case — and because requiring a
 * selected account to accept an invitation would be a closed loop for the person the invitation
 * exists to let in.
 *
 * THE TOKEN IS ALWAYS SENT IN A BODY, never in a path. A token in a URL lands in the browser's
 * history and in the Referer header of every request the page afterwards makes; this one is enough
 * to see which account somebody was invited to. That is also why resolving is a POST despite
 * reading nothing.
 */
@Injectable({ providedIn: 'root' })
export class InvitationsService {
  private readonly api = inject(ApiService);

  /**
   * The invitations issued to the signed-in person's own address, across every account.
   *
   * A signal rather than a call, because two screens show it — the account picker and the accept
   * page — and a person who has just accepted one must not still see it on the other.
   */
  readonly mine = signal<InvitationOffer[]>([]);

  // --- the account's half ---------------------------------------------------

  outstanding(): Promise<Invitation[]> {
    return firstValueFrom(this.api.get<Invitation[]>('/invitations'));
  }

  invite(email: string): Promise<Invitation> {
    return firstValueFrom(this.api.post<Invitation>('/invitations', { email }));
  }

  revoke(id: string): Promise<void> {
    return firstValueFrom(this.api.delete<void>(`/invitations/${id}`));
  }

  resend(id: string): Promise<Invitation> {
    return firstValueFrom(this.api.post<Invitation>(`/invitations/${id}/resend`));
  }

  // --- the invited person's half --------------------------------------------

  /** Reads the offer a token addresses. Consumes nothing, so it is safe to call twice. */
  resolve(token: string): Promise<InvitationOffer> {
    return firstValueFrom(this.api.rootPost<InvitationOffer>('/invitations/resolve', { token }));
  }

  accept(token: string): Promise<InvitationOffer> {
    return firstValueFrom(this.api.rootPost<InvitationOffer>('/invitations/accept', { token }));
  }

  decline(token: string): Promise<void> {
    return firstValueFrom(this.api.rootPost<void>('/invitations/decline', { token }));
  }

  /**
   * Answering an invitation off your own list, with no token.
   *
   * The link in the mail is the path we advertise and it is the path that fails — spam folders,
   * URL-rewriting gateways, reading on a phone and signing in on a laptop. This is the other way
   * to the same row, and it grants nothing the link grants: the invitation is bound to the
   * ADDRESS, both paths require being signed in as it, and being signed in as it is stronger
   * evidence than holding a message sent to it.
   */
  acceptById(id: string): Promise<InvitationOffer> {
    return firstValueFrom(this.api.rootPost<InvitationOffer>('/invitations/accept', { id }));
  }

  declineById(id: string): Promise<void> {
    return firstValueFrom(this.api.rootPost<void>('/invitations/decline', { id }));
  }

  /**
   * Refreshes `mine`.
   *
   * A failure is swallowed on purpose. This runs on the account picker, which is the front door,
   * and an invitation listing that could not load must not stop somebody reaching the accounts
   * they already have — they would then be locked out of the product by a feature that exists to
   * let people in.
   */
  async refreshMine(): Promise<void> {
    try {
      this.mine.set(await firstValueFrom(this.api.rootGet<InvitationOffer[]>('/invitations')));
    } catch {
      this.mine.set([]);
    }
  }
}
