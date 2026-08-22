import { Component, inject, input, OnChanges, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { NzTableModule } from 'ng-zorro-antd/table';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { InventoryHistoryResponse } from './reporting.models';

/**
 * How big this project is, now and over the last ninety days.
 *
 * Current counts are read live; the history comes from the daily snapshot, which is the only
 * stored aggregate in this capability. It exists because inventory rows carry no deletion
 * record: nobody can say in April how many aliases a project had in March unless it was
 * written down in March.
 *
 * Days the snapshot did not run are simply absent — never filled forward, never shown as zero.
 * An invented reading for an unmeasured day is worse than a gap.
 */
@Component({
  selector: 'app-project-inventory-panel',
  imports: [DatePipe, NzTableModule],
  template: `
    <div class="panel-head">
      <h2>Inventory size</h2>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <!-- A separate block rather than an @else if: Angular allows the "as" alias only on a
         primary @if. -->
    @if (!error() && history(); as h) {
      <dl class="current">
        <div>
          <dt>Aliases</dt>
          <dd>{{ h.current.aliasCount }}</dd>
        </div>
        <div>
          <dt>Domains</dt>
          <dd>{{ h.current.domainCount }}</dd>
        </div>
        <div>
          <dt>Load balancers</dt>
          <dd>{{ h.current.loadBalancerCount }}</dd>
        </div>
      </dl>
      <p class="ac-hint">Current counts, read live.</p>

      @if (!h.recorded) {
        <p class="no-data">
          No daily snapshot has been recorded for this project yet.
          <span class="ac-hint">
            The snapshot runs hourly and records the day’s counts; the first entry appears within
            the hour. Days before it cannot be reconstructed, so they are unknown rather than zero.
          </span>
        </p>
      } @else {
        <nz-table
          class="ac-table"
          nzSize="small"
          nzBordered
          [nzData]="h.days"
          [nzShowPagination]="false"
          nzNoResult="No snapshot was recorded in this period."
        >
          <thead>
            <tr>
              <th nzWidth="10rem">Day</th>
              <th nzWidth="7rem">Aliases</th>
              <th nzWidth="7rem">Domains</th>
              <th>Load balancers</th>
            </tr>
          </thead>
          <tbody>
            @for (d of h.days; track d.day) {
              <tr>
                <td class="ac-nowrap">{{ d.day | date: 'mediumDate' }}</td>
                <td>{{ d.aliasCount }}</td>
                <td>{{ d.domainCount }}</td>
                <td>{{ d.loadBalancerCount }}</td>
              </tr>
            }
          </tbody>
        </nz-table>
        <p class="ac-hint">
          Recorded once a day. A missing day was not measured — it is not a day on which the
          project was empty.
        </p>
      }
    }
  `,
  styles: [
    `
      .panel-head h2 {
        margin: 0 0 0.5rem;
        font-size: 1.125rem;
        font-weight: 500;
      }
      .current {
        display: flex;
        gap: 2.5rem;
        margin: 0;
      }
      .current dt {
        font-size: 0.8125rem;
        opacity: 0.65;
      }
      .current dd {
        margin: 0;
        font-size: 1.5rem;
        line-height: 1.2;
      }
      .no-data {
        margin: 1rem 0;
      }
      .no-data .ac-hint {
        display: block;
        max-width: 48rem;
      }
    `,
  ],
})
export class ProjectInventoryPanelComponent implements OnChanges {
  private readonly api = inject(ApiService);

  readonly projectId = input.required<string>();

  readonly history = signal<InventoryHistoryResponse | null>(null);
  readonly error = signal<string | null>(null);

  ngOnChanges(): void {
    this.error.set(null);
    const to = new Date();
    const from = new Date(to.getTime() - 89 * 86_400_000);
    this.api
      .get<InventoryHistoryResponse>(`/projects/${this.projectId()}/inventory-history`, {
        from: isoDate(from),
        to: isoDate(to),
      })
      .subscribe({
        next: (h) => this.history.set(h),
        error: (err) => this.error.set(apiErrorMessage(err)),
      });
  }
}

/** Local calendar date — `toISOString()` would shift the day for anyone east of Greenwich. */
function isoDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
