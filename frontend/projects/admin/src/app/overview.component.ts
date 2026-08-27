import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { SessionService } from '@acme/shared';

/**
 * What the staff console shows until `staff-plane` gives it something to do.
 *
 * It is not empty on purpose: it reports who the identity provider says you are
 * and which groups it sent. That makes this screen the fastest way to answer the
 * question the whole two-application arrangement turns on — did I arrive through
 * the STAFF application, with the claims a staff session should carry — without
 * reading a token by hand.
 */
@Component({
  selector: 'app-overview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NzAlertModule],
  template: `
    <h1>Staff console</h1>
    <p class="lead">
      This surface exists before the tools that will live on it. Suspension, blocklists and
      support access arrive with <code>staff-plane</code>; until then it is here so the hostname,
      the identity-provider application and the address restriction in front of it are real and
      exercised rather than designed.
    </p>

    <nz-alert
      nzType="info"
      nzShowIcon
      nzMessage="No staff endpoints yet"
      nzDescription="/api/admin/** answers 404 with the path you asked for, so a caller learns the surface exists and their route does not."
    />

    <section class="who">
      <h2>This session</h2>
      <dl>
        <dt>Subject</dt>
        <dd>{{ subject() ?? '—' }}</dd>
        <dt>Display name</dt>
        <dd>{{ name() ?? '—' }}</dd>
        <dt>Groups from the identity provider</dt>
        <dd>
          @if (groups().length) {
            {{ groups().join(', ') }}
          } @else {
            <em>none — this session would be refused by the service</em>
          }
        </dd>
      </dl>
    </section>
  `,
  styles: [
    `
      h1 {
        margin-block-end: 0.25rem;
      }
      .lead {
        opacity: 0.75;
        max-inline-size: 60ch;
        margin-block-end: 1.5rem;
      }
      .who {
        margin-block-start: 2rem;
      }
      dl {
        display: grid;
        grid-template-columns: minmax(0, 18rem) minmax(0, 1fr);
        gap: 0.5rem 1.5rem;
        margin: 0;
      }
      dt {
        opacity: 0.6;
      }
      dd {
        margin: 0;
      }
      em {
        opacity: 0.6;
      }
    `,
  ],
})
export class OverviewComponent {
  private readonly session = inject(SessionService);

  protected readonly subject = computed(() => this.session.me()?.subject ?? null);
  protected readonly name = computed(() => this.session.me()?.displayName ?? null);
  protected readonly groups = computed<readonly string[]>(() => this.session.me()?.groups ?? []);
}
