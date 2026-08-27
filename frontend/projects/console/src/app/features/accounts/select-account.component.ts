import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { Account, apiErrorMessage } from '@acme/shared';
import { AccountsService } from './accounts.service';

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
              <h3>{{ a.name }}</h3>
              @if (a.kind === 'personal') {
                <nz-tag>Personal</nz-tag>
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
      }
      .add {
        margin-block-start: 2rem;
      }
    `,
  ],
})
export class SelectAccountComponent {
  protected readonly accounts = inject(AccountsService);
  private readonly router = inject(Router);

  protected readonly query = signal('');
  protected readonly error = signal<string | null>(null);

  /** Personal first: it is the one account everybody has, and the one a new user means. */
  protected readonly shown = computed(() => {
    const q = this.query().trim().toLowerCase();
    const rows = this.accounts
      .accounts()
      .filter((a) => !q || a.name.toLowerCase().includes(q) || a.slug.toLowerCase().includes(q));
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
