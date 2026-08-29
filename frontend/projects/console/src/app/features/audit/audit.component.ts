import { Component, inject, OnInit, signal } from '@angular/core';
import { DatePipe, JsonPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCollapseModule } from 'ng-zorro-antd/collapse';
import { NzDatePickerModule } from 'ng-zorro-antd/date-picker';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule, NzTableQueryParams } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { ApiService, AuditEntry, PageHeaderComponent, apiErrorMessage } from '@acme/shared';
/** 19.6 — the audit trail: a date range, the field filters, and a page at a time. */
@Component({
  selector: 'app-audit',
  imports: [
    FormsModule,
    DatePipe,
    JsonPipe,
    PageHeaderComponent,
    NzButtonModule,
    NzCollapseModule,
    NzDatePickerModule,
    NzIconModule,
    NzInputModule,
    NzSelectModule,
    NzTableModule,
    NzTagModule,
  ],
  template: `
    <app-page-header [crumbs]="[{ label: 'Audit log' }]" title="Audit log" [subtitle]="subtitle()" />

    <div class="ac-filters">
      <nz-range-picker
        name="range"
        [(ngModel)]="range"
        (ngModelChange)="reload()"
        [nzAllowClear]="true"
        [nzPlaceHolder]="['From', 'To']"
      />

      <input nz-input class="short" name="actor" [(ngModel)]="actor" placeholder="Actor" autocomplete="off" />
      <input
        nz-input
        class="short"
        name="action"
        [(ngModel)]="action"
        placeholder="Action, e.g. project.create"
        autocomplete="off"
      />
      <nz-select
        class="short"
        name="objectType"
        [(ngModel)]="objectType"
        (ngModelChange)="reload()"
        [nzAllowClear]="true"
        nzPlaceHolder="Object type"
      >
        @for (t of objectTypes; track t) {
          <nz-option [nzValue]="t" [nzLabel]="t" />
        }
      </nz-select>
      <nz-select
        class="short"
        name="actionClass"
        [(ngModel)]="actionClass"
        (ngModelChange)="reload()"
        [nzAllowClear]="true"
        nzPlaceHolder="Action class"
      >
        <nz-option nzValue="config" nzLabel="config — the setup" />
        <nz-option nzValue="credential" nzLabel="credential — keys and sessions" />
      </nz-select>
      <input
        nz-input
        class="tiny"
        name="projectId"
        [(ngModel)]="projectId"
        placeholder="Project ID"
        inputmode="numeric"
        autocomplete="off"
      />

      <button nz-button nzType="primary" (click)="reload()">
        <span nz-icon nzType="filter"></span>
        Apply
      </button>
      <button nz-button nzType="text" (click)="clear()">Clear</button>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="entries()"
      [nzLoading]="loading()"
      [nzFrontPagination]="false"
      [nzTotal]="total()"
      [nzPageIndex]="pageIndex()"
      [nzPageSize]="pageSize()"
      [nzPageSizeOptions]="[25, 50, 100, 250]"
      [nzShowSizeChanger]="true"
      [nzShowTotal]="rangeTemplate"
      nzNoResult="No audit entry matches these filters."
      (nzQueryParams)="onQueryParams($event)"
    >
      <thead>
        <tr>
          <th nzWidth="14rem">When</th>
          <th nzWidth="16rem">Actor</th>
          <th nzWidth="8rem">Action</th>
          <th>Object</th>
          <th nzWidth="7rem">Project</th>
          <th nzWidth="22rem">Detail</th>
        </tr>
      </thead>
      <tbody>
        @for (e of entries(); track e.id) {
          <tr>
            <td class="ac-nowrap">{{ e.occurredAt | date: 'medium' }}</td>
            <td>
              {{ e.actorName || e.actorSubject || 'unknown' }}
              <div class="ac-hint">{{ e.actorRoles.join(', ') || e.actorSource }}</div>
            </td>
            <td><nz-tag>{{ e.action }}</nz-tag></td>
            <td>{{ e.objectType }} {{ e.objectLabel }}</td>
            <td>{{ e.projectId ?? '—' }}</td>
            <td>
              <nz-collapse nzGhost nzAccordion>
                <nz-collapse-panel nzHeader="before / after">
                  <pre class="ac-mono">{{ e.before | json }}</pre>
                  <pre class="ac-mono">{{ e.after | json }}</pre>
                </nz-collapse-panel>
              </nz-collapse>
            </td>
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
      .short {
        width: 12rem;
      }
      .tiny {
        width: 7rem;
      }
      pre {
        margin: 0.25rem 0;
        white-space: pre-wrap;
        max-height: 14rem;
        overflow: auto;
      }
    `,
  ],
})
export class AuditComponent implements OnInit {
  private readonly api = inject(ApiService);

  readonly entries = signal<AuditEntry[]>([]);
  readonly total = signal(0);
  readonly pageIndex = signal(1);
  readonly pageSize = signal(50);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);

  /** `[from, to]`, as the range picker hands it over; empty when cleared. */
  range: Date[] = [];
  actor = '';
  action = '';
  projectId = '';
  /** Both are chosen from a fixed list rather than typed: the API rejects an unknown
      action class with a 400, and a typo'd object type returns an empty page that reads
      as "nothing happened" — the one answer an audit screen must never give by accident. */
  objectType: string | null = null;
  actionClass: string | null = null;

  /** authz.Object* in backend/internal/authz/vocab.go — the values stored in object_type. */
  readonly objectTypes = ['project', 'load_balancer', 'lb_node', 'domain', 'alias', 'api_key', 'session'];

  readonly subtitle = () => {
    const n = this.total();
    const rows = n === 1 ? '1 entry' : `${n.toLocaleString()} entries`;
    return this.filtered() ? `${rows} matching the current filters` : `${rows} recorded`;
  };

  ngOnInit(): void {
    this.load();
  }

  /** Any filter change starts from page one: page 4 of the old result set means nothing in the new one. */
  reload(): void {
    this.pageIndex.set(1);
    this.load();
  }

  clear(): void {
    this.range = [];
    this.actor = this.action = this.projectId = '';
    this.objectType = this.actionClass = null;
    this.reload();
  }

  onQueryParams(params: NzTableQueryParams): void {
    if (params.pageIndex === this.pageIndex() && params.pageSize === this.pageSize()) return;
    this.pageIndex.set(params.pageIndex);
    this.pageSize.set(params.pageSize);
    this.load();
  }

  private filtered(): boolean {
    return (
      this.range.length === 2 ||
      !!(this.actor || this.action || this.projectId || this.objectType || this.actionClass)
    );
  }

  private load(): void {
    this.error.set(null);
    this.loading.set(true);
    const params: Record<string, string | number> = {
      limit: this.pageSize(),
      offset: (this.pageIndex() - 1) * this.pageSize(),
    };
    // Dates go over as plain YYYY-MM-DD. The service reads a bare date at the START of that day
    // for `from` and at the END of it for `to`, which is what an operator naming a day means —
    // and it keeps the browser's timezone out of a filter expressed in local days.
    if (this.range.length === 2) {
      params['from'] = isoDate(this.range[0]);
      params['to'] = isoDate(this.range[1]);
    }
    if (this.actor.trim()) params['actor'] = this.actor.trim();
    if (this.action.trim()) params['action'] = this.action.trim();
    if (this.projectId.trim()) params['projectId'] = this.projectId.trim();
    if (this.objectType) params['objectType'] = this.objectType;
    if (this.actionClass) params['actionClass'] = this.actionClass;

    this.api.list<AuditEntry>('/audit', params).subscribe({
      next: (page) => {
        this.entries.set(page.items);
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

/** Local calendar date, not `toISOString()` — that shifts to UTC and can name the wrong day. */
export function isoDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
