import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { Account, accountIcon, accountLabel, apiErrorMessage } from '@acme/shared';
import { AccountsService } from './accounts.service';
import { InvitationsService } from '../invitations/invitations.service';
import { InvitationOffer } from '../invitations/invitations.models';

/**
 * The picker: which account do you want to work in (design P19, C1).
 *
 * This is the front door now. Sign-in no longer selects an account, so every session arrives
 * here once — the least-travelled path in the console became the most-travelled one — and it is
 * NEVER EMPTY: a user has a personal account because they are a user, materialized at their
 * first sign-in. "No account selected" therefore means "you have not yet said which" and never
 * "you have nothing", which is the distinction P19 refused to blur when it declined to answer
 * that state with an empty result.
 *
 * The personal account is shown as a distinct entry rather than as one named account among the
 * team ones (C1): it is a different object, not a small team.
 *
 * A suspended account is listed and marked rather than omitted (task 6.1b). Its owner is the
 * only person who can restore it and this is how they reach it; a member who picks one is
 * refused by the service, with a sentence about the account rather than about their permissions
 * (task 6.7).
 */
@Component({
  selector: 'app-select-account',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    RouterLink,
    NzAlertModule,
    NzButtonModule,
    NzCardModule,
    NzIconModule,
    NzInputModule,
    NzTagModule,
  ],
  template: `
    <div class="ac-select">
      <h1>Choose an account</h1>

      <!--
        Invitations, above the accounts, because somebody who has one is here BECAUSE of it.
        The link in the mail is the path we advertise and it is the path that fails: the message
        lands in spam, a corporate gateway rewrites the URL and breaks it, somebody reads it on a
        phone and signs in on a laptop. Without this, arriving with no team account means an empty
        product and an instruction to go and find an email (design V11).

        It grants nothing the link grants. Both require being signed in as the invited address, and
        being signed in as it is stronger evidence than holding a message sent to it.
      -->
      @for (i of invitations.mine(); track i.accountId) {
        <nz-alert
          class="invite"
          nzType="info"
          [nzMessage]="'You have been invited to join ' + i.account"
          [nzDescription]="inviteBody"
        >
        </nz-alert>
        <ng-template #inviteBody>
          <p>Invited as {{ i.email }}. It lapses on {{ expiry(i) }}.</p>
          <div class="invite-actions">
            <button nz-button nzType="primary" nzSize="small" [disabled]="busy()" (click)="join(i)">
              Join {{ i.account }}
            </button>
            <button nz-button nzSize="small" [disabled]="busy()" (click)="refuse(i)">Decline</button>
          </div>
        </ng-template>
      }

      @if (accounts.accounts().length > 0) {
        <p class="ac-lead">Everything in this console belongs to one account. Pick the one you want to work in.</p>

        <!-- A filter rather than a scroll, once there are enough of them to scroll (C12). -->
        @if (accounts.accounts().length > 5) {
          <input
            nz-input
            class="filter"
            name="q"
            [ngModel]="query()"
            (ngModelChange)="query.set($event)"
            placeholder="Filter accounts"
            autocomplete="off"
          />
        }

        <div class="ac-list">
          @for (a of shown(); track a.id) {
            <nz-card [nzHoverable]="true" (click)="choose(a)">
              <!--
                The icon and the word come from accountLabel/accountIcon in shared, the same pair
                the switcher renders. Both controls are on screen together once an account is
                selected, so a rule kept in two places is a rule that will be edited in one.
              -->
              <h3><span nz-icon [nzType]="icon(a)" class="kind"></span>{{ label(a) }}</h3>
              @if (a.kind === 'personal') {
                <!--
                  No 'Personal' tag. The heading already says the word, and a badge repeating the
                  line above it is a label for a distinction the card is not making — every other
                  card here is a team account and none of them says so.
                -->
                <p class="ac-hint">Yours alone, free, five projects.</p>
              } @else {
                <code>{{ a.slug }}</code>
                @if (a.suspended) {
                  <nz-tag nzColor="gold">Suspended</nz-tag>
                  <p class="ac-hint">
                    {{ a.owner ? 'Add credit to start it again.' : 'Its owner can restore it by adding credit.' }}
                  </p>
                } @else {
                  <p class="ac-hint">{{ a.credits }} EUR</p>
                }
              }
            </nz-card>
          } @empty {
            <p class="ac-hint">No account matches “{{ query() }}”.</p>
          }
        </div>
      } @else {
        <!--
          This should not happen: a personal account is materialized at first sign-in, so every
          signed-in caller belongs to at least one. It is rendered honestly rather than papered
          over with an invitation to create something — the account the user is missing is one
          they cannot ask for.
        -->
        <nz-alert
          nzType="error"
          nzMessage="No account was found for you"
          nzDescription="Your personal account should already exist. Sign out and in again; if it is still missing, this is a fault to report rather than something to work around."
        />
      }

      <div class="add">
        <button nz-button routerLink="/upgrade">
          <span nz-icon nzType="plus"></span>
          Add a team account
        </button>
        <p class="ac-hint">Colleagues, roles, and no project limit.</p>
      </div>

      @if (error(); as e) {
        <nz-alert nzType="error" [nzMessage]="e" />
      }
    </div>
  `,
  styles: [
    `
      .ac-select {
        max-width: 44rem;
        margin: 3rem auto;
      }
      .ac-lead {
        opacity: 0.75;
      }
      .filter {
        margin-block-end: 0.75rem;
      }
      .ac-list {
        display: grid;
        gap: 0.75rem;
        grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
        margin-block-end: 2rem;
      }
      .ac-list h3 {
        margin: 0 0 0.25rem;
        display: flex;
        align-items: center;
      }
      /* Beside the word rather than in a reserved column: the two kinds get different glyphs, not
         one kind an indent and the other a gap. */
      .ac-list h3 .kind {
        margin-inline-end: 0.45rem;
        opacity: 0.75;
      }
      .add {
        margin-block-start: 2rem;
      }
      .invite {
        margin-block-end: 1rem;
      }
      .invite-actions {
        margin-block-start: 0.5rem;
        display: flex;
        gap: 0.5rem;
      }
    `,
  ],
})
export class SelectAccountComponent implements OnInit {
  protected readonly accounts = inject(AccountsService);
  protected readonly invitations = inject(InvitationsService);
  private readonly router = inject(Router);

  protected readonly query = signal('');
  protected readonly error = signal<string | null>(null);
  protected readonly busy = signal(false);

  ngOnInit(): void {
    // Unlike the account list — loaded before the first paint by app.config's initializer — this
    // is fetched on arrival, because it is the one thing on this screen that changes without the
    // person doing anything: somebody else issues it.
    void this.invitations.refreshMine();
  }

  protected expiry(i: InvitationOffer): string {
    return new Date(i.expiresAt).toLocaleDateString();
  }

  /**
   * Accepting from here rather than from the link.
   *
   * There is no token, and none is needed: the invitation is bound to the ADDRESS, and this
   * session is authenticated as it. The token only ever addressed the row.
   */
  protected async join(i: InvitationOffer): Promise<void> {
    this.error.set(null);
    this.busy.set(true);
    try {
      if (!i.id) return;
      const joined = await this.invitations.acceptById(i.id);
      // The service has already made the joined account this session's selection; `select`
      // re-posts it, points the API at it and reloads both the session and the account list, so
      // the screen after this one belongs to the account just joined.
      await this.accounts.select(joined.accountId);
      await this.invitations.refreshMine();
      await this.router.navigateByUrl('/projects');
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.busy.set(false);
    }
  }

  protected async refuse(i: InvitationOffer): Promise<void> {
    this.error.set(null);
    this.busy.set(true);
    try {
      if (!i.id) return;
      await this.invitations.declineById(i.id);
      await this.invitations.refreshMine();
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    } finally {
      this.busy.set(false);
    }
  }

  /** What each account is called and shown as, from the one rule both this and the switcher use. */
  protected readonly label = accountLabel;
  protected readonly icon = accountIcon;

  /** Personal first: it is the one account everybody has, and the one a new user means. */
  protected readonly shown = computed(() => {
    const q = this.query().trim().toLowerCase();
    const rows = this.accounts.accounts().filter(
      (a) =>
        !q ||
        // The DISPLAYED word is matched too, not only the stored one. The personal account is shown
        // as "Personal" and its stored name is derived from an address nobody would think to type,
        // so without this the one account everybody has is the one the filter cannot find.
        accountLabel(a).toLowerCase().includes(q) ||
        a.name.toLowerCase().includes(q) ||
        a.slug.toLowerCase().includes(q),
    );
    return [...rows].sort((a, b) => Number(b.kind === 'personal') - Number(a.kind === 'personal'));
  });

  // The list is loaded before the first render by app.config's initializer, so there is no
  // ngOnInit here: this screen and the shell's navigation read the same signal, and a second
  // fetch on arrival would only be a second chance for them to disagree.

  protected async choose(account: Account): Promise<void> {
    this.error.set(null);
    try {
      await this.accounts.select(account.id);
      // A suspended account has exactly one screen (C5), and its owner is the only person the
      // service let through to it.
      await this.router.navigateByUrl(account.suspended ? '/suspended' : '/projects');
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    }
  }
}
