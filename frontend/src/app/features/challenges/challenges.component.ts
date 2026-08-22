import { Component, inject, OnInit, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzTableModule, NzTableQueryParams } from 'ng-zorro-antd/table';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { PageHeaderComponent } from '../../shared/page-header.component';
import { ChallengeEntry } from '../models';

/**
 * 19.5 — what is answerable right now. The screen that answers "is a renewal happening at this
 * moment", which is otherwise invisible (D11): alias, age and the key that published it.
 */
@Component({
  selector: 'app-challenges',
  imports: [
    DatePipe,
    FormsModule,
    PageHeaderComponent,
    NzButtonModule,
    NzIconModule,
    NzInputModule,
    NzTableModule,
  ],
  template: `
    <app-page-header
      [crumbs]="[{ label: 'Live challenges' }]"
      title="Live challenges"
      [subtitle]="subtitle()"
    />

    <div class="ac-filters">
      <nz-input-group class="ac-grow" [nzPrefix]="searchIcon" [nzSuffix]="query ? clearIcon : undefined">
        <input
          nz-input
          name="q"
          [(ngModel)]="query"
          (ngModelChange)="onQueryChange()"
          placeholder="Filter by alias, challenge name or key"
          autocomplete="off"
        />
      </nz-input-group>
      <ng-template #searchIcon><span nz-icon nzType="search"></span></ng-template>
      <ng-template #clearIcon>
        <button type="button" class="clear" aria-label="Clear filter" (click)="clearQuery()">
          <span nz-icon nzType="close-circle"></span>
        </button>
      </ng-template>

      <button class="ac-spacer" nz-button (click)="load()">
        <span nz-icon nzType="reload"></span>
        Refresh
      </button>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="challenges()"
      [nzLoading]="loading()"
      [nzFrontPagination]="false"
      [nzTotal]="total()"
      [nzPageIndex]="pageIndex()"
      [nzPageSize]="pageSize()"
      [nzPageSizeOptions]="[25, 50, 100]"
      [nzShowSizeChanger]="true"
      [nzShowTotal]="rangeTemplate"
      [nzNoResult]="query ? filteredEmpty : quiet"
      (nzQueryParams)="onQueryParams($event)"
    >
      <thead>
        <tr>
          <th>Alias</th>
          <th>Challenge name</th>
          <th>Value</th>
          <th nzWidth="12rem">Published by</th>
          <th nzWidth="6rem">Age</th>
          <th nzWidth="14rem">Expires</th>
        </tr>
      </thead>
      <tbody>
        @for (c of challenges(); track c.id) {
          <tr>
            <td>{{ c.aliasLabel }}</td>
            <td class="ac-mono">{{ c.challengeName }}</td>
            <td class="ac-mono">{{ c.value }}</td>
            <td>{{ c.publishedBy || 'unknown' }}</td>
            <td>{{ c.ageSeconds }}s</td>
            <td class="ac-nowrap">{{ c.expiresAt | date: 'medium' }}</td>
          </tr>
        }
      </tbody>
    </nz-table>

    <ng-template #rangeTemplate let-range="range" let-total>
      {{ range[0] }}–{{ range[1] }} of {{ total }}
    </ng-template>
    <ng-template #quiet>No challenge is currently answerable — no renewal is in progress.</ng-template>
    <ng-template #filteredEmpty>No live challenge matches this filter.</ng-template>
  `,
  styles: [
    `
      .clear {
        /* A real button, so it is tabbable and announced — it just has to look like the icon it
         * replaced rather than like a button. */
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
export class ChallengesComponent implements OnInit {
  private readonly api = inject(ApiService);

  readonly challenges = signal<ChallengeEntry[]>([]);
  readonly total = signal(0);
  readonly pageIndex = signal(1);
  readonly pageSize = signal(25);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);

  query = '';
  private debounce?: ReturnType<typeof setTimeout>;

  readonly subtitle = () => {
    const n = this.total();
    return n === 1 ? '1 challenge answerable right now' : `${n} challenges answerable right now`;
  };

  ngOnInit(): void {
    this.load();
  }

  onQueryChange(): void {
    clearTimeout(this.debounce);
    this.debounce = setTimeout(() => {
      this.pageIndex.set(1);
      this.load();
    }, 250);
  }

  clearQuery(): void {
    this.query = '';
    this.pageIndex.set(1);
    this.load();
  }

  onQueryParams(params: NzTableQueryParams): void {
    if (params.pageIndex === this.pageIndex() && params.pageSize === this.pageSize()) return;
    this.pageIndex.set(params.pageIndex);
    this.pageSize.set(params.pageSize);
    this.load();
  }

  load(): void {
    this.error.set(null);
    this.loading.set(true);
    const params: Record<string, string | number> = {
      limit: this.pageSize(),
      offset: (this.pageIndex() - 1) * this.pageSize(),
    };
    if (this.query.trim()) params['q'] = this.query.trim();
    this.api.list<ChallengeEntry>('/challenges', params).subscribe({
      next: (page) => {
        this.challenges.set(page.items);
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
