import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule, NzTableQueryParams } from 'ng-zorro-antd/table';
import { ApiService, CAN_ADMIN_PROJECT, PageHeaderComponent, Project, SessionService, apiErrorMessage, may, openNameSlugModal } from '@acme/shared';
/** 19.1 — the project list: filter, page, and create through a modal (creation is ADMIN-only, D10). */
@Component({
  selector: 'app-project-list',
  imports: [
    RouterLink,
    FormsModule,
    DatePipe,
    PageHeaderComponent,
    NzButtonModule,
    NzIconModule,
    NzInputModule,
    NzTableModule,
  ],
  template: `
    <app-page-header [crumbs]="[{ label: 'Projects' }]" title="Projects" [subtitle]="subtitle()" />

    <div class="ac-filters">
      <nz-input-group class="ac-grow" [nzPrefix]="searchIcon" [nzSuffix]="query ? clearIcon : undefined">
        <input
          nz-input
          name="q"
          [(ngModel)]="query"
          (ngModelChange)="onQueryChange()"
          placeholder="Filter by slug or name"
          autocomplete="off"
        />
      </nz-input-group>
      <ng-template #searchIcon><span nz-icon nzType="search"></span></ng-template>
      <ng-template #clearIcon>
        <button type="button" class="clear" aria-label="Clear filter" (click)="clearQuery()">
          <span nz-icon nzType="close-circle"></span>
        </button>
      </ng-template>

      @if (canCreateProjects()) {
        <button class="ac-spacer" nz-button nzType="primary" (click)="create()">
          <span nz-icon nzType="plus"></span>
          New project
        </button>
      }
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="projects()"
      [nzLoading]="loading()"
      [nzFrontPagination]="false"
      [nzTotal]="total()"
      [nzPageIndex]="pageIndex()"
      [nzPageSize]="pageSize()"
      [nzPageSizeOptions]="[25, 50, 100]"
      [nzShowSizeChanger]="true"
      [nzShowTotal]="rangeTemplate"
      (nzQueryParams)="onQueryParams($event)"
    >
      <thead>
        <tr>
          <th nzWidth="14rem">Slug</th>
          <th>Name</th>
          <th nzWidth="16rem">Created</th>
        </tr>
      </thead>
      <tbody>
        @for (p of projects(); track p.id) {
          <tr>
            <td><a [routerLink]="['/projects', p.id]">{{ p.slug }}</a></td>
            <td>{{ p.name }}</td>
            <td class="ac-nowrap">{{ p.createdAt | date: 'medium' }}</td>
          </tr>
        }
      </tbody>
    </nz-table>

    <ng-template #rangeTemplate let-range="range" let-total>
      {{ range[0] }}–{{ range[1] }} of {{ total }}
    </ng-template>
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
export class ProjectListComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly modal = inject(NzModalService);
  readonly session = inject(SessionService);

  readonly projects = signal<Project[]>([]);

  /**
   * Creating a project is governed by MEMBERSHIP of the account's unrestricted role rather than
   * by a grant (design P14) — an account is not an object a grant can name. There is no row to
   * ask about before the first project exists, so this reads the answer the service reports for
   * an existing one, and falls back to the profile's permissions for an empty account.
   */
  readonly canCreateProjects = computed(() => {
    const rows = this.projects();
    if (rows.length > 0) return may(rows[0], CAN_ADMIN_PROJECT);
    return this.session.me()?.permissions?.includes(CAN_ADMIN_PROJECT) ?? false;
  });
  readonly total = signal(0);
  readonly pageIndex = signal(1);
  readonly pageSize = signal(25);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);

  query = '';
  private debounce?: ReturnType<typeof setTimeout>;

  readonly subtitle = () => {
    const n = this.total();
    const of = n === 1 ? '1 project' : `${n} projects`;
    return this.query ? `${of} matching “${this.query}”` : of;
  };

  ngOnInit(): void {
    this.load();
  }

  /**
   * Typing filters, but not on every keystroke: the filter runs server-side (it must — the rows
   * on screen are one page of many), so an unthrottled input would fire a request per character
   * and race its own answers.
   */
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

  /** The table's own pager and page-size changer both arrive here. */
  onQueryParams(params: NzTableQueryParams): void {
    if (params.pageIndex === this.pageIndex() && params.pageSize === this.pageSize()) return;
    this.pageIndex.set(params.pageIndex);
    this.pageSize.set(params.pageSize);
    this.load();
  }

  create(): void {
    openNameSlugModal<Project>(this.modal, 'New project', {
      path: '/projects',
      slugHint: 'Lower case, digits and hyphens. Appears in URLs and CLI arguments.',
    }).afterClose.subscribe((created?: Project) => {
      if (!created) return;
      // Back to page one: projects are ordered by slug, the new one can land anywhere, and
      // leaving the operator on a page that does not contain what they just made is worse than
      // losing their place.
      this.pageIndex.set(1);
      this.load();
    });
  }

  private load(): void {
    this.error.set(null);
    this.loading.set(true);
    const params: Record<string, string | number> = {
      limit: this.pageSize(),
      offset: (this.pageIndex() - 1) * this.pageSize(),
    };
    if (this.query.trim()) params['q'] = this.query.trim();
    this.api.list<Project>('/projects', params).subscribe({
      next: (page) => {
        this.projects.set(page.items);
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
