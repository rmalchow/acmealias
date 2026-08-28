import { Component, inject, input, OnChanges, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzTableModule, NzTableQueryParams } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { apiErrorMessage, ApiService, HelpHintComponent, Interaction } from '@acme/shared';
/**
 * Recent interactions: every challenge this load balancer published, who published it and from where, and
 * whether the CA ever came to look.
 *
 * This is the panel that answers the question the rest of the console cannot. A renewal that
 * never happened leaves no trace in the live challenge view — nothing is live — and only a thin
 * one in the audit trail, because nobody changed any inventory. It shows up here as a
 * publication with a query count of zero, which almost always means the customer's
 * `_acme-challenge` CNAME is missing or points somewhere else. Hence the "never queried" filter:
 * it turns "renewals are failing somewhere" into a list of domains to go and fix.
 */
@Component({
  selector: 'app-interactions-panel',
  imports: [
    DatePipe,
    FormsModule,
    HelpHintComponent,
    NzButtonModule,
    NzIconModule,
    NzInputModule,
    NzSwitchModule,
    NzTableModule,
    NzTagModule,
    NzToolTipModule,
  ],
  template: `
    <div class="panel-head">
      <h2>
        Recent interactions
        <app-help-hint
          heading="Recent interactions"
          [paragraphs]="[
            'One row per publication: an ACME client asked us to answer a challenge, and this is what became of it. It is the first place to look when a certificate did not renew, because it says whether your client ever reached us at all.',
            'Which key published, and from which address, are on the row — so a refusal caused by a key’s IP restriction is visible as a refusal rather than as silence.',
            'The outcome is worked out when the row is read, not stored, so it is right for a publication still in flight: pending is published and not yet asked for, live is being served now, validated means the certificate authority queried it, and unqueried means nobody ever did — which usually means the CNAME is missing or the CA never got as far as asking.',
            'The spelling column says how the client addressed us: our own alias name, your _acme-challenge name followed through the CNAME, or the acme-dns protocol, which names the alias by its bare label. acme-dns has no withdrawal operation, so those publications end by expiring rather than by being cleaned up.'
          ]"
        />
      </h2>
      <button nz-button nzSize="small" (click)="load()">
        <span nz-icon nzType="reload"></span>
        Refresh
      </button>
    </div>

    <div class="ac-filters">
      <nz-input-group class="ac-grow" [nzPrefix]="searchIcon" [nzSuffix]="query ? clearIcon : undefined">
        <input
          nz-input
          name="q"
          [(ngModel)]="query"
          (ngModelChange)="onQueryChange()"
          placeholder="Filter by domain, alias, key or source address"
          autocomplete="off"
        />
      </nz-input-group>
      <ng-template #searchIcon><span nz-icon nzType="search"></span></ng-template>
      <ng-template #clearIcon>
        <button type="button" class="clear" aria-label="Clear filter" (click)="clearQuery()">
          <span nz-icon nzType="close-circle"></span>
        </button>
      </ng-template>

      <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
         nz-switch renders its own control inside this label and owns the association; the
         rule cannot see through the directive. -->
      <label class="only-unqueried">
        <nz-switch [(ngModel)]="onlyUnqueried" name="unqueried" (ngModelChange)="reload()" nzSize="small" />
        Never queried only
      </label>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="interactions()"
      [nzLoading]="loading()"
      [nzFrontPagination]="false"
      [nzTotal]="total()"
      [nzPageIndex]="pageIndex()"
      [nzPageSize]="pageSize()"
      [nzPageSizeOptions]="[25, 50, 100]"
      [nzShowSizeChanger]="true"
      [nzShowTotal]="rangeTemplate"
      [nzNoResult]="onlyUnqueried ? noUnqueried : nothingYet"
      (nzQueryParams)="onQueryParams($event)"
    >
      <thead>
        <tr>
          <!-- Twelfths, like the tables above this panel. Domains was the only column with no
               width at all, so it absorbed whatever the others left — which put a variable-width
               column in the middle of a table whose other five are fixed. It is now three
               twelfths, one less than the four it was taking, and the twelfth goes to From: an
               IPv6 address is 39 characters at its longest and was wrapping in 10rem. -->
          <th nzWidth="16.6667%">When</th>
          <th nzWidth="25%">Domains</th>
          <th nzWidth="16.6667%">From</th>
          <th nzWidth="16.6667%">Key</th>
          <th nzWidth="8.3333%">Queried</th>
          <th nzWidth="16.6667%">Outcome</th>
        </tr>
      </thead>
      <tbody>
        @for (i of interactions(); track i.id) {
          <tr>
            <td class="ac-nowrap">
              {{ i.publishedAt | date: 'medium' }}
              <div class="ac-hint">{{ lifetime(i) }}</div>
            </td>
            <td>
              @for (d of i.domainNames; track d) {
                <div>{{ d }}</div>
              } @empty {
                <span class="ac-hint">no domains recorded</span>
              }
              <div class="ac-hint ac-mono">{{ i.aliasLabel }}</div>
            </td>
            <td class="ac-mono">
              {{ i.clientIp ?? '—' }}
              <div class="ac-hint" nz-tooltip [nzTooltipTitle]="formHint(i)">{{ i.fqdnForm }}</div>
            </td>
            <td class="ac-mono">{{ i.keyId ?? '—' }}</td>
            <td>
              @if (i.queryCount) {
                {{ i.queryCount }}×
                <div class="ac-hint">first {{ i.firstQueriedAt | date: 'shortTime' }}</div>
              } @else {
                <span class="ac-hint">never</span>
              }
            </td>
            <td>
              <nz-tag [nzColor]="outcomeColour(i.outcome)" nz-tooltip [nzTooltipTitle]="outcomeHint(i.outcome)">
                {{ i.outcome }}
              </nz-tag>
            </td>
          </tr>
        }
      </tbody>
    </nz-table>

    <ng-template #rangeTemplate let-range="range" let-total>
      {{ range[0] }}–{{ range[1] }} of {{ total }}
    </ng-template>
    <ng-template #nothingYet>
      This load balancer has not published a challenge yet — nothing has tried to renew a certificate.
    </ng-template>
    <ng-template #noUnqueried>
      Every publication was queried. Nothing here points at a missing CNAME.
    </ng-template>
  `,
  styles: [
    `
      .panel-head {
        /* THE HEADING LEFT, THE CONTROLS RIGHT — and margin-left:auto on whatever follows the
           heading rather than justify-content:space-between, because a panel with TWO buttons
           has three children and space-between would spread all three, parking one button in the
           middle of the row. The auto margin takes up the slack once, so every control after the
           heading stays a group at the right edge.

           Why it matters at all: with gap alone the buttons sat against the end of the heading,
           so their position moved with the length of a word — "Domains" and "Recent interactions"
           put their buttons in different places on the same page, and a reader looking for the
           button on the next panel down had to find it again. The right edge does not move. */
        display: flex;
        align-items: center;
        gap: 0.75rem;
        /* A few pixels more, so the heading row reads as a label for the table below rather
           than as part of its header row. */
        margin-bottom: 0.75rem;
      }
      /* Everything after the heading goes right, as a group: on the Domains panel that is BOTH
         buttons — Recheck and Add domain — with their gap preserved between them. Targeting the
         second child rather than the buttons by name means a panel that gains a third control
         needs no new rule. */
      .panel-head > :nth-child(2) {
        margin-inline-start: auto;
      }
      h2 {
        margin: 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      .only-unqueried {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        font-size: 0.875rem;
        white-space: nowrap;
      }
      .clear {
        border: 0;
        padding: 0;
        background: none;
        color: inherit;
        cursor: pointer;
        opacity: 0.45;
      }
      .clear:hover,
      .clear:focus-visible {
        opacity: 0.85;
      }
    `,
  ],
})
export class InteractionsPanelComponent implements OnChanges {
  private readonly api = inject(ApiService);

  readonly projectId = input.required<string>();

  readonly interactions = signal<Interaction[]>([]);
  readonly total = signal(0);
  readonly pageIndex = signal(1);
  readonly pageSize = signal(25);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);

  query = '';
  onlyUnqueried = false;
  private debounce?: ReturnType<typeof setTimeout>;

  ngOnChanges(): void {
    this.load();
  }

  onQueryChange(): void {
    clearTimeout(this.debounce);
    this.debounce = setTimeout(() => this.reload(), 250);
  }

  clearQuery(): void {
    this.query = '';
    this.reload();
  }

  reload(): void {
    this.pageIndex.set(1);
    this.load();
  }

  onQueryParams(params: NzTableQueryParams): void {
    if (params.pageIndex === this.pageIndex() && params.pageSize === this.pageSize()) return;
    this.pageIndex.set(params.pageIndex);
    this.pageSize.set(params.pageSize);
    this.load();
  }

  /** How long the value stayed published — the shape of a healthy validation is seconds. */
  lifetime(i: Interaction): string {
    if (!i.withdrawnAt) return 'still published';
    const ms = new Date(i.withdrawnAt).getTime() - new Date(i.publishedAt).getTime();
    if (ms < 1000) return 'withdrawn immediately';
    const s = Math.round(ms / 1000);
    return s < 90 ? `withdrawn after ${s}s` : `withdrawn after ${Math.round(s / 60)}m`;
  }

  formHint(i: Interaction): string {
    switch (i.fqdnForm) {
      case 'alias':
        return 'The client followed the customer CNAME and sent our alias name.';
      case 'acme-dns':
        // Worth spelling out, because the row will end differently from the others and somebody
        // reading the column should know why before they go looking for a bug.
        return 'The client spoke the acme-dns protocol, which names the alias by its label. It has no withdrawal operation, so this value expires on its own rather than being cleaned up.';
      default:
        return 'The client sent the customer’s own _acme-challenge name and left the aliasing to us.';
    }
  }

  outcomeColour(outcome: Interaction['outcome']): string {
    switch (outcome) {
      case 'validated':
        return 'green';
      case 'unqueried':
        return 'red';
      case 'live':
        return 'blue';
      default:
        return 'gold';
    }
  }

  outcomeHint(outcome: Interaction['outcome']): string {
    switch (outcome) {
      case 'validated':
        return 'Published, queried by the CA, then withdrawn — a normal validation. An acme-dns client withdraws nothing, so for those it means the value expired after being queried, which is the same good ending.';
      case 'unqueried':
        return 'Published and withdrawn without ever being queried. The customer’s _acme-challenge CNAME is the first thing to check.';
      case 'live':
        return 'Still published and already queried — a validation in progress.';
      default:
        return 'Still published, nothing has queried it yet.';
    }
  }

  load(): void {
    this.error.set(null);
    this.loading.set(true);
    const params: Record<string, string | number> = {
      limit: this.pageSize(),
      offset: (this.pageIndex() - 1) * this.pageSize(),
    };
    if (this.query.trim()) params['q'] = this.query.trim();
    if (this.onlyUnqueried) params['unqueried'] = 'true';
    this.api.list<Interaction>(`/projects/${this.projectId()}/interactions`, params).subscribe({
      next: (page) => {
        this.interactions.set(page.items);
        this.total.set(page.total);
        this.loading.set(false);
      },
      error: (err) => {
        this.loading.set(false);
        this.error.set(apiErrorMessage(err));
      },
    });
  }
}
