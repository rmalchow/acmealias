import { Component, inject, input, OnChanges, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { NzDatePickerModule } from 'ng-zorro-antd/date-picker';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiService, HelpHintComponent, apiErrorMessage } from '@acme/shared';
import { Outcome, UsageBucket, UsageResponse } from './reporting.models';

/**
 * Attempts over time for one project, by outcome.
 *
 * The chart is stacked bars drawn in CSS. No charting library was added: the project has none,
 * nothing in this build is fetched from a CDN, and a stacked bar over a few dozen buckets is a
 * flex row with percentage heights. Each bar carries an aria-label naming its period and its
 * counts, so the series is readable without being seen.
 *
 * Two measures, one chart, never mixed — see `measureNote`. Switching the selector redraws the
 * same buckets with the other number; it does not refetch, because the response carries both.
 */
@Component({
  selector: 'app-project-usage-panel',
  imports: [DatePipe, FormsModule, HelpHintComponent, NzDatePickerModule, NzToolTipModule],
  template: `
    <div class="panel-head">
      <h2>
        Attempts over time
        <app-help-hint
          heading="Attempts over time"
          [paragraphs]="[
            'Every challenge this project asked us to answer, counted over time. A healthy project is quiet and periodic — a small cluster of attempts per certificate, roughly every sixty days, one per node that renews independently.',
            'The shape is what to read rather than the numbers. A flat line where you expected a renewal means your client never asked us; a sudden dense run means something is retrying, which is worth catching before it meets a rate limit at the certificate authority.',
            'Attempts are also what a team account is billed on, so this is the same count the bill is drawn from.'
          ]"
        />
      </h2>
    </div>

    <p class="ac-hint measure-note">{{ measureNote() }}</p>

    <div class="ac-filters">
      <nz-range-picker name="range" [(ngModel)]="range" (ngModelChange)="load()" [nzAllowClear]="false" />

      <label class="pick">
        Count
        <select name="measure" [(ngModel)]="measure">
          <option value="issuance">Issuance attempts</option>
          <option value="challenge">Challenge attempts</option>
        </select>
      </label>

      <label class="pick">
        Bucket
        <select name="bucket" [(ngModel)]="bucket" (ngModelChange)="load()">
          <option value="day">Daily</option>
          <option value="month">Monthly</option>
        </select>
      </label>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <!-- A separate block rather than an @else if: Angular allows the "as" alias only on a
         primary @if. -->
    @if (!error() && usage(); as u) {
      @if (!u.recorded) {
        <!-- Not a zero, and deliberately not drawn as one: nothing has been recorded for this
             project, and history cannot be backfilled. -->
        <p class="no-data">
          No attempts have been recorded for this project yet.
          <span class="ac-hint">
            Recording starts when a load balancer first publishes a challenge here. Nothing before
            that can be reconstructed, so this is not the same as “zero renewals”.
          </span>
        </p>
      } @else {
        <div class="chart" role="group" [attr.aria-label]="'Attempts per ' + bucket + ', by outcome'">
          @for (b of u.buckets; track b.start) {
            <div class="col">
              <div
                class="bar"
                role="img"
                [attr.aria-label]="barLabel(b)"
                nz-tooltip
                [nzTooltipTitle]="barLabel(b)"
                [style.height.%]="heightPercent(b)"
              >
                @for (o of outcomes; track o) {
                  @if (count(b, o); as n) {
                    <span class="seg" [class]="'o-' + o" [style.flex]="n"></span>
                  }
                }
              </div>
              <div class="tick">{{ b.start | date: tickFormat() }}</div>
            </div>
          }
        </div>

        <ul class="legend">
          @for (o of outcomes; track o) {
            <li><span class="swatch" [class]="'o-' + o"></span>{{ o }} — {{ totalFor(o) }}</li>
          }
        </ul>

        <p class="ac-hint">
          {{ total() }} {{ measure === 'issuance' ? 'issuance' : 'challenge' }} attempts in this
          period. Neither figure is a count of certificates: the certificate is finalized between
          the load balancer’s ACME client and the CA, which this service never sees.
        </p>
      }
    }
  `,
  styles: [
    `
      .panel-head h2 {
        margin: 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      .measure-note {
        margin: 0.25rem 0 0.75rem;
        max-width: 60rem;
      }
      .pick {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        font-size: 0.8125rem;
        white-space: nowrap;
      }
      .pick select {
        padding: 0.25rem 0.5rem;
        border: 1px solid rgba(0, 0, 0, 0.2);
        border-radius: 2px;
        background: inherit;
        color: inherit;
        font: inherit;
      }
      .chart {
        display: flex;
        align-items: flex-end;
        gap: 2px;
        height: 10rem;
        padding-top: 0.5rem;
        overflow-x: auto;
      }
      .col {
        display: flex;
        flex: 1 0 1.25rem;
        flex-direction: column;
        justify-content: flex-end;
        height: 100%;
        min-width: 1.25rem;
      }
      .bar {
        display: flex;
        flex-direction: column-reverse;
        min-height: 2px;
        background: rgba(0, 0, 0, 0.06);
      }
      .seg {
        display: block;
        min-height: 2px;
      }
      .tick {
        overflow: hidden;
        font-size: 0.6875rem;
        line-height: 1.4;
        text-align: center;
        white-space: nowrap;
        opacity: 0.6;
      }
      .legend {
        display: flex;
        flex-wrap: wrap;
        gap: 0.25rem 1rem;
        margin: 0.75rem 0 0.25rem;
        padding: 0;
        font-size: 0.8125rem;
        list-style: none;
      }
      .legend li {
        display: flex;
        align-items: center;
        gap: 0.375rem;
      }
      .swatch {
        display: inline-block;
        width: 0.75rem;
        height: 0.75rem;
        border-radius: 2px;
      }
      .no-data {
        margin: 1rem 0;
      }
      .no-data .ac-hint {
        display: block;
        max-width: 48rem;
      }
      /* The same colours the interactions panel gives these outcomes, so one vocabulary reads
         the same on both screens — and now by token rather than by hand, which is what makes
         that claim true. It was not: the live swatch was written here as #1890ff, Ant's LIGHT-theme
         blue, while the tag it claimed to match rendered in the dark theme's. Both are now the
         same hue at the same step of the same compiled palette, rather than two of them. */
      .o-validated {
        background: var(--ac-status-green);
      }
      .o-live {
        background: var(--ac-status-blue);
      }
      .o-pending {
        background: var(--ac-status-gold);
      }
      .o-unqueried {
        background: var(--ac-status-red);
      }
      .o-abandoned {
        background: var(--ac-status-grey);
      }
    `,
  ],
})
export class ProjectUsagePanelComponent implements OnChanges {
  private readonly api = inject(ApiService);

  readonly projectId = input.required<string>();

  readonly outcomes: Outcome[] = ['validated', 'live', 'pending', 'unqueried', 'abandoned'];

  readonly usage = signal<UsageResponse | null>(null);
  readonly error = signal<string | null>(null);

  measure: 'issuance' | 'challenge' = 'issuance';
  bucket: 'day' | 'month' = 'day';
  /** Last 30 days: long enough to show a renewal pattern, short enough to read. */
  range: [Date, Date] = [new Date(Date.now() - 29 * 86_400_000), new Date()];

  ngOnChanges(): void {
    this.load();
  }

  /** 8.6 — the difference between the two measures, in one line, beside the figure. */
  measureNote(): string {
    return this.measure === 'issuance'
      ? 'Issuance attempts: publications at one challenge name whose lifetimes overlap, counted once. ' +
          'A certificate covering example.com and *.example.com publishes two values for one request, and this counts it once.'
      : 'Challenge attempts: every published TXT value. The same wildcard certificate counts twice here — ' +
          'raw publishing activity, not the number of certificates requested.';
  }

  count(b: UsageBucket, o: Outcome): number {
    const counts = this.measure === 'issuance' ? b.issuanceAttempts : b.challengeAttempts;
    return counts[o] ?? 0;
  }

  bucketTotal(b: UsageBucket): number {
    return this.measure === 'issuance' ? b.issuanceAttemptsTotal : b.challengeAttemptsTotal;
  }

  /**
   * A method rather than a `computed`: the scale depends on `measure`, which is a
   * template-driven field and not a signal, so a cached value would keep the previous
   * measure's scale after the selector changes.
   */
  private max(): number {
    return Math.max(1, ...(this.usage()?.buckets ?? []).map((b) => this.bucketTotal(b)));
  }

  heightPercent(b: UsageBucket): number {
    return (this.bucketTotal(b) / this.max()) * 100;
  }

  tickFormat(): string {
    return this.bucket === 'month' ? 'MMM yyyy' : 'd MMM';
  }

  barLabel(b: UsageBucket): string {
    const parts = this.outcomes.filter((o) => this.count(b, o) > 0).map((o) => `${this.count(b, o)} ${o}`);
    const noun = this.measure === 'issuance' ? 'issuance attempts' : 'challenge attempts';
    const when = new Date(b.start).toDateString();
    return parts.length ? `${when}: ${this.bucketTotal(b)} ${noun} — ${parts.join(', ')}` : `${when}: none`;
  }

  totalFor(o: Outcome): number {
    return (this.usage()?.buckets ?? []).reduce((sum, b) => sum + this.count(b, o), 0);
  }

  total(): number {
    return (this.usage()?.buckets ?? []).reduce((sum, b) => sum + this.bucketTotal(b), 0);
  }

  load(): void {
    this.error.set(null);
    this.api
      .get<UsageResponse>(`/projects/${this.projectId()}/usage`, {
        from: isoDate(this.range[0]),
        to: isoDate(this.range[1]),
        bucket: this.bucket,
      })
      .subscribe({
        next: (u) => this.usage.set(u),
        error: (err) => this.error.set(apiErrorMessage(err)),
      });
  }
}

/**
 * The local calendar date, not `toISOString()` — which converts to UTC and would ask for the
 * previous day for anyone east of Greenwich after their local midnight.
 */
function isoDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
