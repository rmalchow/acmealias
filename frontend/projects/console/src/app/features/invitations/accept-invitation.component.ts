import {
  ChangeDetectionStrategy,
  Component,
  inject,
  OnInit,
  signal,
} from "@angular/core";
import { ActivatedRoute, Router } from "@angular/router";
import { NzAlertModule } from "ng-zorro-antd/alert";
import { NzButtonModule } from "ng-zorro-antd/button";
import { NzCardModule } from "ng-zorro-antd/card";
import { NzIconModule } from "ng-zorro-antd/icon";
import { apiErrorMessage } from "@acme/shared";
import { AccountsService } from "../accounts/accounts.service";
import { InvitationsService } from "./invitations.service";
import { InvitationOffer } from "./invitations.models";

/**
 * The screen an invitation link lands on.
 *
 * It renders for a session that belongs to NOTHING, which is the state the person arriving here is
 * usually in: they signed up because they were invited, and at that instant they are a member of
 * no team account. So this route sits outside the account guard, and every call it makes goes to
 * the unscoped prefix.
 *
 * THE QUESTION IS "JOIN <ACCOUNT>?" AND NOT "JOIN <ACCOUNT> AS <ROLE>?". An invitation names no
 * role: joining and being given one are two steps, and the second happens after somebody with the
 * account in front of them decides. Saying so here would be inventing an answer the offer does not
 * contain.
 *
 * TWO ANSWERS, NOT ONE AND A WAY OUT OF THE PAGE. Declining is offered as plainly as accepting,
 * because to the account those two outcomes and silence are three different things that a closed
 * tab makes into one: fourteen days of nothing tells an administrator neither that the message was
 * never seen nor that the person said no, and the two call for opposite responses.
 *
 * THE TOKEN IS TAKEN OUT OF THE ADDRESS BAR IMMEDIATELY. It arrives as a query parameter because a
 * link in an email has nowhere else to put it, and it stays in the browser's history and in the
 * Referer header of every subsequent request for as long as it is there. `replaceState` is what
 * makes that window one paint long rather than the life of the tab.
 */
@Component({
  selector: "app-accept-invitation",
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NzAlertModule, NzButtonModule, NzCardModule, NzIconModule],
  template: `
    <div class="ac-invite">
      @if (loading()) {
        <p class="ac-hint">Looking up this invitation…</p>
      } @else {
        @if (offer(); as o) {
          <nz-card>
            <h1>Join {{ o.account }}?</h1>
            <p class="ac-lead">
              You have been invited to join <strong>{{ o.account }}</strong> as
              <strong>{{ o.email }}</strong
              >.
            </p>
            <!--
            Said before they click, not after they are confused. Joining and being given a role are
            two steps, and somebody who accepts and then finds an account they cannot do anything
            in has met a bug as far as they are concerned.
          -->
            <p class="ac-hint">
              Accepting puts you in the account. An administrator there decides
              what you can do and assigns you a role — until they do, you will
              see the account but nothing inside it.
            </p>
            <div class="ac-actions">
              <button
                nz-button
                nzType="primary"
                [nzLoading]="busy()"
                (click)="accept()"
              >
                <span nz-icon nzType="check"></span>
                Join {{ o.account }}
              </button>
              <button nz-button [disabled]="busy()" (click)="decline()">
                Decline
              </button>
            </div>
            <p class="ac-hint expiry">
              This invitation lapses on {{ expiry(o) }} if you do nothing.
            </p>
          </nz-card>
        } @else {
          @if (error(); as e) {
            <!--
          One message for every reason a token does not work — unknown, expired, revoked, already
          answered. The service answers them all alike on purpose, so this screen cannot
          distinguish them and must not pretend to.
        -->
            <nz-alert
              nzType="warning"
              nzMessage="This invitation cannot be used"
              [nzDescription]="e"
            />
            <div class="ac-actions">
              <button nz-button (click)="goToAccounts()">
                Go to your accounts
              </button>
            </div>
          }
        }
      }
    </div>
  `,
  styles: [
    `
      .ac-invite {
        max-width: 34rem;
        margin: 4rem auto;
      }
      h1 {
        margin-block-end: 0.5rem;
      }
      .ac-lead {
        margin-block-end: 0.75rem;
      }
      .ac-hint {
        opacity: 0.7;
      }
      .ac-actions {
        margin-block-start: 1.25rem;
        display: flex;
        gap: 0.5rem;
      }
      .expiry {
        margin-block-start: 1rem;
        font-size: 0.9em;
      }
    `,
  ],
})
export class AcceptInvitationComponent implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly invitations = inject(InvitationsService);
  private readonly accounts = inject(AccountsService);

  protected readonly offer = signal<InvitationOffer | null>(null);
  protected readonly error = signal<string | null>(null);
  protected readonly loading = signal(true);
  protected readonly busy = signal(false);

  private token = "";

  ngOnInit(): void {
    this.token = this.route.snapshot.queryParamMap.get("token") ?? "";
    // Out of the address bar before anything else happens. History and Referer are the two places
    // a URL leaks to that nobody thinks about, and this URL carries a credential.
    if (this.token) {
      history.replaceState(null, "", "/invitations/accept");
    }
    void this.load();
  }

  private async load(): Promise<void> {
    if (!this.token) {
      this.loading.set(false);
      this.error.set(
        "This link carries no invitation. Open the link from the message you were sent, or sign in " +
          "and look for the invitation on your accounts page.",
      );
      return;
    }
    try {
      this.offer.set(await this.invitations.resolve(this.token));
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  protected expiry(o: InvitationOffer): string {
    return new Date(o.expiresAt).toLocaleDateString();
  }

  protected async accept(): Promise<void> {
    this.busy.set(true);
    try {
      const joined = await this.invitations.accept(this.token);
      // The service has already made this the session's selection. The console has to catch up
      // rather than choose — `select` re-posts the selection, points the API at the account and
      // reloads both the session and the account list, which is what makes the next screen render
      // the account just joined rather than the one the session was in a moment ago.
      await this.accounts.select(joined.accountId);
      await this.invitations.refreshMine();
      await this.router.navigateByUrl("/projects");
    } catch (err) {
      this.error.set(apiErrorMessage(err));
      this.offer.set(null);
    } finally {
      this.busy.set(false);
    }
  }

  protected async decline(): Promise<void> {
    this.busy.set(true);
    try {
      await this.invitations.decline(this.token);
      await this.invitations.refreshMine();
      await this.router.navigateByUrl("/accounts");
    } catch (err) {
      this.error.set(apiErrorMessage(err));
      this.offer.set(null);
    } finally {
      this.busy.set(false);
    }
  }

  protected goToAccounts(): void {
    void this.router.navigateByUrl("/accounts");
  }
}
