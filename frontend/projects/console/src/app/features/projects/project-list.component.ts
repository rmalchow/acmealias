import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule, NzTableQueryParams } from 'ng-zorro-antd/table';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { NzAlertModule } from 'ng-zorro-antd/alert';
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
    NzToolTipModule,
    NzAlertModule,
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

      <button
        class="ac-spacer"
        nz-button
        nzType="primary"
        [disabled]="!canCreateProjects()"
        nz-tooltip
        [nzTooltipTitle]="canCreateProjects() ? null : NOT_PERMITTED_PROJECT"
        (click)="create()"
      >
        <span nz-icon nzType="plus"></span>
        New project
      </button>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <!--
      THE EMPTY ACCOUNT IS THE ONE SCREEN WITH NOTHING TO READ, and it is the first screen
      somebody sees. The table's "No projects" is a true statement that answers no question: the
      person looking at it does not yet know what a project is, so telling them there are none of
      them is telling them nothing.

      It shows only when the account really is empty — not when a filter matched nothing, where
      the reader has already seen the list and needs their filter back rather than an
      explanation. Hence showIntro() rather than a bare length check.
    -->
    @if (showIntro()) {
      <nz-alert
        class="intro"
        nzType="info"
        nzShowIcon
        nzMessage="What is a project?"
        [nzDescription]="intro"
      />
      <ng-template #intro>
        <p>
          A project is one deployment held together: the load balancer nodes that terminate TLS,
          the domains they answer for, and the API keys their ACME clients authenticate with. Most
          accounts have one per environment — production, staging — rather than one per domain.
        </p>
        <p>
          The grouping is what enforces the boundary. A key belonging to a project can publish a
          challenge only for domains in that same project, and we serve only what such a key
          published. A key from staging asking us to answer for a production domain is refused —
          not filtered out of a view, refused at the door — so a compromised staging deployment
          cannot obtain a certificate for a name it was never meant to have.
        </p>
        <p>
          Keys can be narrowed further. A key may be restricted to the CIDR ranges you name, and to
          the addresses of this project's own nodes, so a secret that leaks out of your network
          cannot be used from outside it.
        </p>
        <p>
          Nothing about your DNS is stored here beyond one label per domain. You publish a single
          CNAME per domain, once; after that every renewal happens without you.
        </p>
      </ng-template>
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
          <th nzWidth="18rem">Contents</th>
          <th nzWidth="16rem">Created</th>
        </tr>
      </thead>
      <tbody>
        @for (p of projects(); track p.id) {
          <tr>
            <td><a [routerLink]="['/projects', p.id]">{{ p.slug }}</a></td>
            <td>{{ p.name }}</td>
            <td class="ac-nowrap" [class.ac-hint]="isEmpty(p)">{{ contents(p) }}</td>
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
      .intro {
        margin-bottom: 1rem;
      }
      .intro p {
        margin: 0 0 0.5rem;
      }
      .intro p:last-child {
        margin-bottom: 0;
      }
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

  /**
   * The explanation appears for an account with no projects at all, and NOT for a filter that
   * matched nothing: `total()` is the unfiltered count only while `query` is empty, and someone
   * who has filtered their way to zero rows has already seen what a project list looks like.
   *
   * `loading()` is in the condition so the box does not flash on first paint, when the count is
   * still 0 because nothing has come back yet.
   */
  readonly showIntro = computed(
    () => !this.loading() && this.total() === 0 && this.query.trim() === '',
  );

  /**
   * A different sentence from the generic one, because this refusal has a different cause and a
   * different remedy: creating a project needs MEMBERSHIP of the account's unrestricted role
   * (design P14), which no grant can confer. Telling someone their permissions are short would
   * send them asking for a grant that cannot exist.
   */
  protected readonly NOT_PERMITTED_PROJECT =
    'Only an unrestricted member of this account can create projects.';
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
   * What is IN a project, which is what stops this list from being the least useful one in the
   * product (design L5).
   *
   * Before the collapse a row could at least say "3 load balancers"; a project is a concrete
   * thing now, and a list of concrete things has to say how big each one is. The two numbers are
   * the two questions somebody actually has: nodes is "how much infrastructure is this", domains
   * is "how much of my certificate estate depends on it" — the one that decides how carefully to
   * touch it. They come with the listing rather than a request per row.
   */
  contents(p: Project): string {
    if (this.isEmpty(p)) return 'No nodes, no domains';
    const nodes = p.nodeCount ?? 0;
    const domains = p.domainCount ?? 0;
    return `${nodes} ${nodes === 1 ? 'node' : 'nodes'} · ${domains} ${domains === 1 ? 'domain' : 'domains'}`;
  }

  /**
   * Empty says so in words rather than as "0 · 0".
   *
   * A freshly created project is the commonest empty state in the product and it is the one with
   * an obvious next action, so the row is where to say it — two zeroes read as a measurement
   * where the sentence reads as an instruction.
   *
   * Undefined counts are NOT empty. The single-project read omits them because it carries the
   * nodes and domains themselves, and a row rendering that as "no nodes, no domains" would be
   * describing the response rather than the project.
   */
  isEmpty(p: Project): boolean {
    return p.nodeCount === 0 && p.domainCount === 0;
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
