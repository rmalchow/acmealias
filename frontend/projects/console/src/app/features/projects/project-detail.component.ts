import { DatePipe } from '@angular/common';
import { Component, computed, inject, input, OnChanges, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiService, CAAStatus, CAN_WRITE, CAN_WRITE_KEYS, CheckResult, DNSPlan, DelegationStatus, Domain, LBNode, PageHeaderComponent, Project, RecordCheck, SessionService, VerificationResult, apiErrorMessage, confirmAction, may , NOT_PERMITTED} from '@acme/shared';
import { openDomainEditor } from '../domains/domain-editor.component';
import { ApiKeysPanelComponent } from '../apikeys/apikeys-panel.component';
import { ProjectUsagePanelComponent } from '../reporting/project-usage-panel.component';
import { InteractionsPanelComponent } from './interactions-panel.component';
import { openNodeCreateModal } from './node-create.modal';
import { openNodeEditModal } from './node-edit.modal';
import { openDomainHelpModal } from './domain-help.modal';
import { openAcmeDnsCredentialsModal } from '../domains/acmedns-credentials.modal';
import { StatusChip, activationChip, caaChip, delegationChip, domainCheckFor } from './domain-status';

/**
 * A project: its nodes, its domain table, the DNS plan panel (records, live verification, copy
 * and zone-snippet download), the API keys panel, and the reporting the project level always
 * had — usage over time.
 *
 * This is the merge of two screens (change collapse-project-and-loadbalancer, design L6), and
 * the asymmetry is worth naming because it decided the shape: the load balancer screen was the
 * REAL one — nodes, domains, the plan, the keys — and the project screen was a list of links to
 * it plus two reporting panels. So this keeps the former whole and folds the latter's two
 * panels in at the bottom. What disappeared is the table of load balancers, which had exactly
 * one row in every deployment anybody ran.
 */
@Component({
  selector: 'app-project-detail',
  imports: [
    DatePipe,
    PageHeaderComponent,
    ApiKeysPanelComponent,
    InteractionsPanelComponent,
    ProjectUsagePanelComponent,
    NzToolTipModule,
    NzAlertModule,
    NzButtonModule,
    NzIconModule,
    NzTableModule,
    NzTagModule,
  ],
  template: `
    <!-- Two crumbs where there were three. "Projects › my-project › prod" is now
         "Projects › prod", which is the visible form of the whole change (design L6). -->
    <app-page-header
      [crumbs]="[{ label: 'Projects', link: '/projects' }, { label: project()?.slug ?? '…' }]"
      [title]="project()?.name ?? 'Project'"
      [subtitle]="subtitle()"
    />

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <section>
      <div class="panel-head">
        <h2>Nodes</h2>
        <button
          nz-button
          nzType="primary"
          nzSize="small"
          [disabled]="!canWrite()"
          nz-tooltip
          [nzTooltipTitle]="canWrite() ? null : NOT_PERMITTED"
          (click)="createNode()"
        >
          <span nz-icon nzType="plus"></span>
          Add node
        </button>
      </div>

      <nz-table
        class="ac-table"
        nzSize="small"
        nzBordered
        [nzData]="project()?.nodes ?? []"
        [nzShowPagination]="false"
        nzNoResult="No nodes yet."
      >
        <thead>
          <tr>
            <th nzWidth="12rem">Label</th>
            <th>IPv4</th>
            <th>IPv6</th>
            <th nzWidth="6rem"></th>
          </tr>
        </thead>
        <tbody>
          @for (n of project()?.nodes ?? []; track n.id) {
            <tr>
              <td>{{ n.label }}</td>
              <td class="ac-mono">{{ n.ipv4 || '—' }}</td>
              <td class="ac-mono">{{ n.ipv6 || '—' }}</td>
              <td class="row-actions">
                <button
                  nz-button
                  nzType="text"
                  nzSize="small"
                  [disabled]="!canWrite()"
                  nz-tooltip
                  [nzTooltipTitle]="canWrite() ? 'Edit addresses' : NOT_PERMITTED"
                  aria-label="Edit addresses"
                  (click)="editNode(n)"
                >
                  <span nz-icon nzType="edit"></span>
                </button>
                <button
                  nz-button
                  nzType="text"
                  nzDanger
                  nzSize="small"
                  [disabled]="!canWrite()"
                  nz-tooltip
                  [nzTooltipTitle]="canWrite() ? 'Delete node' : NOT_PERMITTED"
                  aria-label="Delete node"
                  (click)="deleteNode(n)"
                >
                  <span nz-icon nzType="delete"></span>
                </button>
              </td>
            </tr>
          }
        </tbody>
      </nz-table>
    </section>

    <section>
      <div class="panel-head">
        <h2>Domains</h2>
        <button
          nz-button
          nzType="primary"
          nzSize="small"
          [disabled]="!canWrite() || !project()"
          nz-tooltip
          [nzTooltipTitle]="canWrite() ? null : NOT_PERMITTED"
          (click)="addDomain()"
        >
          <span nz-icon nzType="plus"></span>
          Add domain
        </button>
      </div>

      <nz-table
        class="ac-table"
        nzSize="small"
        nzBordered
        [nzData]="project()?.domains ?? []"
        [nzShowPagination]="false"
        nzNoResult="No domains yet."
      >
        <thead>
          <tr>
            <th>Name</th>
            <th>Challenge name</th>
            <th nzWidth="12rem">Alias</th>
            <th nzWidth="16rem">Challenges</th>
            <th nzWidth="18rem">Validation</th>
            <th nzWidth="5rem"></th>
          </tr>
        </thead>
        <tbody>
          @for (d of project()?.domains ?? []; track d.id) {
            <tr>
              <td>{{ d.name }}</td>
              <td class="ac-mono">{{ d.challengeName }}</td>
              <td>{{ d.aliasLabel }}</td>
              <!-- What the SERVICE will do for this domain, from what it has stored — as opposed
                   to the live lookup in the next cell, which says what DNS says this second. The
                   two disagree for a few minutes after somebody publishes the record, and that
                   gap is exactly what the check-now button below is for (task 4.3). -->
              <td>
                @if (activation(d); as act) {
                  <nz-tag [nzColor]="act.colour" nz-tooltip [nzTooltipTitle]="act.tooltip">{{ act.label }}</nz-tag>
                }
                <p class="ac-hint stamp">
                  @if (d.lastSuccessAt && stoppedResolving(d)) {
                    <!-- "Since when", never "how many failures": the question anybody actually
                         asks is how long this has been true, and the timestamp answers it exactly
                         (design A1). No deadline is stated because there is none — nothing is
                         ever switched off for this (A3). -->
                    Last seen {{ d.lastSuccessAt | date: 'medium' }}
                  } @else if (d.lastCheckedAt) {
                    Last looked {{ d.lastCheckedAt | date: 'medium' }}
                  } @else {
                    Not looked at yet
                  }
                </p>
                <!-- Offered in BOTH states a check can change: a domain that has never been seen,
                     and one whose delegation has stopped resolving. The second is the state
                     somebody has just fixed — they republished the CNAME and want the row to say
                     so — and gating on "never seen" alone left exactly that person with no
                     way to ask, waiting out the settled cadence for a chip to go green. Nothing
                     is at risk in pressing it: a check that fails records only that we looked
                     (A3). -->
                @if (canCheckNow(d)) {
                  <button nz-button nzType="link" nzSize="small" class="explain" [nzLoading]="verifying()" (click)="checkNow()">
                    Check now
                  </button>
                }
              </td>
              <td>
                @if (checks(d); as ch) {
                  <div class="check">
                    <span class="check-label">Delegation</span>
                    <nz-tag [nzColor]="ch.delegation.colour" nz-tooltip [nzTooltipTitle]="ch.delegation.tooltip">
                      {{ ch.delegation.label }}
                    </nz-tag>
                  </div>
                  @if (ch.caa; as caa) {
                    <div class="check">
                      <span class="check-label">CAA</span>
                      <nz-tag [nzColor]="caa.colour" nz-tooltip [nzTooltipTitle]="caa.tooltip">{{ caa.label }}</nz-tag>
                    </div>
                  }
                  <button nz-button nzType="link" nzSize="small" class="explain" (click)="explain(d)">
                    What does this mean?
                  </button>
                }
              </td>
              <td class="row-actions">
                <!--
                  What acme-dns's unauthenticated POST /register does elsewhere, done here by
                  somebody who is already authorized. It mints a key, so it is gated on the same
                  permission the key panel is, not on the one that reads this table.
                -->
                <button
                  nz-button
                  nzType="text"
                  nzSize="small"
                  [disabled]="!canWriteKeys()"
                  nz-tooltip
                  [nzTooltipTitle]="canWriteKeys() ? 'acme-dns credentials for this domain' : NOT_PERMITTED"
                  aria-label="acme-dns credentials"
                  (click)="acmeDnsCredentials(d)"
                >
                  <span nz-icon nzType="key"></span>
                </button>
                <button
                  nz-button
                  nzType="text"
                  nzDanger
                  nzSize="small"
                  [disabled]="!canWrite()"
                  nz-tooltip
                  [nzTooltipTitle]="canWrite() ? 'Delete domain' : NOT_PERMITTED"
                  aria-label="Delete domain"
                  (click)="deleteDomain(d)"
                >
                  <span nz-icon nzType="delete"></span>
                </button>
              </td>
            </tr>
          }
        </tbody>
      </nz-table>
    </section>

    @if (plan(); as pl) {
      <section>
        <div class="panel-head">
          <h2>DNS plan</h2>
          <button nz-button nzSize="small" (click)="copyPlan(pl)">
            <span nz-icon nzType="copy"></span>
            Copy records
          </button>
          <a
            nz-button
            nzSize="small"
            [href]="zoneHref()"
            target="_blank"
            rel="noopener"
          >
            <span nz-icon nzType="download"></span>
            Zone snippet
          </a>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="verifying()" (click)="checkNow()">
            Run verification
          </button>
        </div>

        <nz-table
          class="ac-table"
          nzSize="small"
          nzBordered
          [nzData]="pl.records"
          [nzShowPagination]="false"
        >
          <thead>
            <tr>
              <th>Name</th>
              <th nzWidth="6rem">Type</th>
              <th nzWidth="5rem">TTL</th>
              <th>Value</th>
              <th nzWidth="10rem">Status</th>
              <th>Found</th>
            </tr>
          </thead>
          <tbody>
            @for (r of pl.records; track r.name + r.type + r.value) {
              <tr>
                <td>{{ r.name }}</td>
                <td>{{ r.type }}</td>
                <td>{{ r.ttl }}</td>
                <td class="ac-mono">{{ r.value }}</td>
                <td>
                  @if (statusFor(r.name, r.type); as s) {
                    <nz-tag [nzColor]="statusColour(s.status)">{{ s.status }}</nz-tag>
                  } @else {
                    <span class="ac-hint">not checked yet</span>
                  }
                </td>
                <td class="ac-mono">{{ foundFor(r.name, r.type) }}</td>
              </tr>
            }
          </tbody>
        </nz-table>

        <p class="ac-hint">
          A CAA row of “not-published” is informational, not an error: an absent CAA record is the
          default state of the internet and blocks nothing. A CAA record naming a different issuer
          is the one CAA failure that matters.
        </p>

        @if (verifyError(); as msg) {
          <nz-alert nzType="error" [nzMessage]="msg" nzShowIcon />
        }
      </section>
    }

    @if (project(); as p) {
      <section>
        <app-apikeys-panel [projectId]="p.id" [project]="p" [nodes]="p.nodes ?? []" />
      </section>

      <section>
        <app-interactions-panel [projectId]="p.id" />
      </section>
    }

    <!-- Reporting, folded in from the screen this one absorbed. Usage over time is
         project-level and has no per-domain view; the never-queried signal is drilled
         into from the interactions list above.

         The inventory-size panel is GONE. It plotted how many nodes, domains and
         aliases the project held over time — a chart of numbers the page already
         states exactly, a few centimetres above, for the only moment that matters:
         now. Growth in a count nobody sets a target for answers no question, and it
         was the third chart on a screen whose job is the configuration. -->
    <section>
      <app-project-usage-panel [projectId]="id()" />
    </section>
  `,
  styles: [
    `
      section {
        margin-bottom: 2rem;
      }
      .panel-head {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin-bottom: 0.5rem;
      }
      h2 {
        margin: 0 0.5rem 0 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      /* A chip per record type, each labelled, because the two answer different questions on
       * different axes: whether the delegation is published, and what CAA permits (design D3). */
      .check {
        display: flex;
        align-items: baseline;
        gap: 0.5rem;
        margin-bottom: 0.25rem;
      }
      .check-label {
        width: 5.5rem;
        color: var(--ac-muted);
        font-size: 0.8125rem;
      }
      .explain {
        padding: 0;
        height: auto;
      }
      /* When we last looked, under the chip that says what we found. Small and quiet: it is
       * context for the chip, not a second verdict. */
      .stamp {
        margin: 0.25rem 0 0;
      }
      /* Row actions are icons only — the label is in the tooltip and the aria-label, so a table
       * of twenty rows is not twenty repetitions of the word "Delete". */
      .row-actions {
        white-space: nowrap;
      }
    `,
  ],
})
export class ProjectDetailComponent implements OnChanges {
  private readonly api = inject(ApiService);
  private readonly modal = inject(NzModalService);
  private readonly message = inject(NzMessageService);
  private readonly session = inject(SessionService);

  readonly id = input.required<string>();

  /**
   * The zone snippet is fetched by the browser rather than by HttpClient — it is a download, not
   * a request the app renders — so it needs the full path, account and all. Building it here
   * keeps the one place that knows the URL shape next to the one that knows the account.
   */
  protected zoneHref(): string {
    return `/api/v1/accounts/${this.api.currentAccount()}/projects/${this.id()}/plan/zone`;
  }

  readonly project = signal<Project | null>(null);
  readonly plan = signal<DNSPlan | null>(null);
  readonly verification = signal<VerificationResult | null>(null);
  readonly verifying = signal(false);
  readonly verifyError = signal<string | null>(null);
  /**
   * The automatic check's request itself failed — the network, not the resolver.
   *
   * It is kept apart from `verifyError` on purpose: `verifyError` is a banner, and a banner is
   * the right answer to a button somebody pressed and the wrong one to a check that runs whenever
   * this screen opens. A lookup that fails degrades the rows it concerns and never the page
   * (design D1), so this only turns the chips into "could not check".
   */
  readonly checkFailed = signal(false);
  readonly error = signal<string | null>(null);

  /**
   * What this caller may do to THIS project, as the service reported it with the row.
   *
   * It used to be a role-name test, which could only ever answer "may this person write
   * anything, anywhere" — the question the permission model exists to stop asking. A caller
   * granted one project and not another now sees the right controls on each.
   *
   * Until the row arrives the answer is no, which renders a read-only screen for an instant
   * rather than a set of buttons that turn out to be refused.
   */
  readonly canWrite = computed(() => may(this.project(), CAN_WRITE));
  /**
   * Minting an acme-dns credentials file mints a KEY, so it is gated on the key permission and
   * not on the one that edits the inventory. They differ: KEY_ROTATION rotates secrets and may
   * not add a domain, and the whole point of that role is that the two are separable.
   */
  readonly canWriteKeys = computed(() => may(this.project(), CAN_WRITE_KEYS));
  protected readonly NOT_PERMITTED = NOT_PERMITTED;

  readonly subtitle = () => {
    const p = this.project();
    if (!p) return null;
    const nodes = p.nodes?.length ?? 0;
    const domains = p.domains?.length ?? 0;
    return `${p.slug} · ${nodes} ${nodes === 1 ? 'node' : 'nodes'} · ${domains} ${domains === 1 ? 'domain' : 'domains'}`;
  };

  ngOnChanges(): void {
    // The id is a routed input and this component is reused across ids, so the previous
    // project's answers have to go before the new one's arrive — a stale chip is a claim about
    // somebody else's DNS.
    this.verification.set(null);
    this.checkFailed.set(false);
    this.load();
    this.loadPlan();
    // Opening the screen asks the question (design D1). It was a button below the table whose
    // rows it explained, so a customer who mistyped a CNAME read "never validated" — which
    // sounds like "nothing has happened yet" and means "this will never work". The check stores
    // nothing, so running it on load changes when it happens, not what it does.
    this.verify();
  }

  load(): void {
    // One request where there were two. The project read carries its own domains now, so the
    // second call that fetched the parent's name for a breadcrumb has nothing left to fetch.
    this.api.get<Project>(`/projects/${this.id()}`).subscribe({
      next: (p) => this.project.set(p),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  private loadPlan(): void {
    this.api.get<DNSPlan>(`/projects/${this.id()}/plan`).subscribe({
      next: (p) => this.plan.set(p),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  /**
   * The check that runs when the screen opens: an observation, and nothing else (design
   * domain-setup-feedback D1). It stores nothing, which is what makes it safe to run on every
   * open and what keeps it a GET.
   */
  verify(): void {
    this.runCheck(false);
  }

  /**
   * The check somebody asked for — which is also what brings a domain online (design
   * domain-activation task 4.3).
   *
   * Same lookup, against a POST that records what it saw. Pressing it is the difference between
   * a customer who has just published their CNAME waiting out the poller's interval and being
   * answerable now. There is no way to hurt yourself with it: a check that fails to see a
   * delegation records only that we looked, and never takes an active domain offline (A3).
   */
  checkNow(): void {
    this.runCheck(true);
  }

  /**
   * One route, two readers: the per-domain chips and the whole-plan table below.
   *
   * `record` is whether somebody pressed a button, and it decides two things. It picks the POST,
   * which stores the delegation answer, over the GET, which does not — a page must never activate
   * domains on a customer's behalf as a side effect of being opened. And it decides whether a
   * failure is worth a banner: an answer that was asked for and did not come deserves one, an
   * answer nobody asked for does not (design D1).
   *
   * A deployment with no resolver is not an error either way — the route answers "not checked"
   * for every row rather than refusing (design D4) — and, since nothing was asked, records
   * nothing.
   */
  private runCheck(record: boolean): void {
    // The screen is reused across ids and this request now takes as long as DNS does, so an
    // answer can arrive after somebody has navigated to another project. Dropping it is the
    // whole fix: the plan table joins its rows by NAME, so a late answer for the previous
    // project would annotate this one's rows wherever the two share a name.
    const asked = this.id();
    // Only a press spins the button. The automatic pass used to set it too, so every screen open
    // showed a spinner on a control nobody had touched — which reads as if something were
    // running on the customer's behalf, and is exactly the manual control task 2.9 keeps honest.
    if (record) this.verifying.set(true);
    this.verifyError.set(null);
    const path = `/projects/${asked}/verification`;
    const answer = record
      ? this.api.post<VerificationResult>(path)
      : this.api.get<VerificationResult>(path);
    answer.subscribe({
      next: (v) => {
        if (record) this.verifying.set(false);
        if (this.id() !== asked) return;
        this.checkFailed.set(false);
        this.verification.set(v);
        // The recorded check has just changed the stored activation stamps, and the rows on
        // screen still carry the old ones. Reloading is what turns "waiting for delegation" into
        // "active" in front of the person who published the record ten seconds ago.
        if (record) this.load();
      },
      error: (err) => {
        if (record) this.verifying.set(false);
        if (this.id() !== asked) return;
        this.checkFailed.set(true);
        if (record) this.verifyError.set(apiErrorMessage(err));
      },
    });
  }

  statusFor(name: string, type: string): CheckResult | undefined {
    return this.verification()?.results.find((r) => r.name === name && r.type === type);
  }

  /** The states the verifier can report, coloured by what they mean for the operator. */
  statusColour(status: string): string {
    switch (status) {
      case 'correct':
        return 'green';
      case 'missing':
      case 'incorrect':
      // A CAA set that names somebody else stops issuance however perfect the delegation is, so
      // it is red here as well as in the chips. It used to fall through to grey, which put the
      // one blocking state in the same colour as "not checked" (design D3).
      case 'blocking':
        return 'red';
      case 'not-published':
        return 'blue';
      default:
        return 'default';
    }
  }

  /**
   * What was found for a plan row, and never why a lookup failed.
   *
   * `found` on `undetermined` is our resolver's error text — an address and a transport failure,
   * written for an operator. Rendering it verbatim reads as a verdict on the customer's zone when
   * it is a verdict on our lookup, and this table is now painted on every screen open rather than
   * only when somebody pressed the button (design D4).
   */
  foundFor(name: string, type: string): string {
    const s = this.statusFor(name, type);
    if (!s || s.status === 'undetermined') return '—';
    return s.found || '—';
  }

  /**
   * What the service will actually do for this domain, from the state it has stored (design
   * domain-activation A1). Deliberately not derived from the live check beside it: `/present`
   * consults the stored stamps and nothing else, so this is the only honest answer to "will a
   * renewal work right now".
   */
  activation(d: Domain): StatusChip {
    return activationChip(d);
  }

  /**
   * The last look did not find the delegation — we checked after we last saw it.
   *
   * This is what makes "since when" sayable from two timestamps and no counter: `lastSuccessAt`
   * is the moment it was last there, and its distance from now is the answer to the only question
   * anybody asks (design A1). It is never a countdown; nothing is switched off for it (A3).
   */
  stoppedResolving(d: Domain): boolean {
    if (!d.lastSuccessAt || !d.lastCheckedAt) return false;
    return Date.parse(d.lastCheckedAt) > Date.parse(d.lastSuccessAt);
  }

  /**
   * Whether this row offers **Check now** — true in both states a check can change.
   *
   * "Waiting" is the obvious one: the customer has just published the record and does not want to
   * wait out the poller. "Not resolving" is the one that was missing, and it is the same person a
   * day later: they broke the delegation, saw the orange chip, fixed it, and had no way to say
   * so. Without this they wait out the settled cadence — a day — for a chip to go green.
   *
   * An "active" row offers nothing, because there is nothing a check could tell it that it does
   * not already say. Pressing it anywhere is safe regardless: a check that fails to see a
   * delegation records only that we looked, and never takes a domain offline (design A3).
   */
  canCheckNow(d: Domain): boolean {
    return !d.lastSuccessAt || this.stoppedResolving(d);
  }

  /**
   * A chip per record type for one row. CAA is rendered only where the server sent one: with no
   * issuer configured there is no CAA row in the plan, and so nothing about CAA is true or false
   * for this domain — saying "not checked" there would claim we meant to look.
   */
  checks(d: Domain): { delegation: StatusChip; caa: StatusChip | null } {
    const a = this.answerFor(d);
    return {
      // Whether this domain has an alias at all is known from the row itself, not from the
      // check: without one there is no target, nothing is ever asked, and the chip would sit at
      // "not checked" forever unless it says why.
      delegation: delegationChip(a.delegation.status, !!d.aliasLabel),
      caa: a.caa ? caaChip(a.caa.status) : null,
    };
  }

  /**
   * This row's answer for both record types, or the state we are in when none arrived.
   *
   * Three things are folded together here on purpose:
   *
   *   - The expected target comes from the plan when no answer has arrived, which is what lets
   *     the help modal print the record to publish on a deployment that never configured a
   *     resolver: which record a domain needs is known without asking DNS anything.
   *   - A request that FAILED is "could not check" — we looked and could not. One still in
   *     flight, or a deployment without a resolver, is "not checked": we have not looked, and
   *     that must never read as a fault in the customer's setup (design D4).
   *   - A failed re-check degrades an answer we already have rather than leaving it standing.
   *     The previous answer is kept only for its SHAPE — which record types this domain has, and
   *     what its target is — because the alternative is the CAA chip vanishing from a row that
   *     said "blocks us" a second ago. Its statuses are not kept: an answer we could not refresh
   *     is not an answer, and chips that go on asserting one while the page says the check failed
   *     are two claims about the same moment.
   */
  private answerFor(d: Domain): { delegation: RecordCheck<DelegationStatus>; caa: RecordCheck<CAAStatus> | null } {
    const answered = domainCheckFor(d.id, this.verification());
    const failed = this.checkFailed();
    if (answered) {
      return {
        delegation: failed ? { ...answered.delegation, status: 'undetermined' } : answered.delegation,
        caa: answered.caa ? (failed ? { ...answered.caa, status: 'undetermined' } : answered.caa) : null,
      };
    }
    const planned = this.plan()?.records.find((r) => r.type === 'CNAME' && r.name === d.challengeName);
    return {
      delegation: {
        name: d.challengeName,
        status: failed ? 'undetermined' : 'not-checked',
        expected: planned ? [planned.value] : undefined,
      },
      caa: null,
    };
  }

  /** The prescription behind the chips, carrying this domain's own values (design D5). */
  explain(d: Domain): void {
    const a = this.answerFor(d);
    openDomainHelpModal(this.modal, { domain: d, delegation: a.delegation, caa: a.caa });
  }

  copyPlan(p: DNSPlan): void {
    const text = p.records.map((r) => `${r.name}\t${r.ttl}\tIN\t${r.type}\t${r.value}`).join('\n');
    navigator.clipboard?.writeText(text).then(
      () => this.message.success('DNS records copied'),
      () => {
        // Clipboard access can be denied by the browser; the zone-snippet download is the fallback.
        this.message.error('The browser refused clipboard access — use the zone snippet instead.');
      },
    );
  }

  editNode(n: LBNode): void {
    openNodeEditModal(this.modal, n).afterClose.subscribe((updated?: LBNode) => {
      if (!updated) return;
      this.load();
      // The plan is derived from these addresses, so it is stale the moment they change.
      this.loadPlan();
    });
  }

  createNode(): void {
    openNodeCreateModal(this.modal, this.id()).afterClose.subscribe((created?: LBNode) => {
      if (!created) return;
      this.load();
      this.loadPlan();
    });
  }

  addDomain(): void {
    openDomainEditor(this.modal, this.project()!.id).afterClose.subscribe((created?: boolean) => {
      if (!created) return;
      this.load();
      this.loadPlan();
      // The answers are per domain now, so a set of domains that changed leaves them describing
      // a table that no longer exists — and the new row is the one somebody is waiting on.
      this.verify();
    });
  }

  async deleteNode(n: LBNode): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Delete node “${n.label}”?`,
      message: 'Its addresses disappear from the DNS plan, so the records you publish have to change too.',
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/nodes/${n.id}`).subscribe({
      next: () => {
        this.load();
        this.loadPlan();
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  /**
   * The credentials an acme-dns client reads, for one domain.
   *
   * It reloads afterwards because the key it created shows up in this project's key panel — the
   * file is not the only record of it, and somebody who loses the file revokes the key rather
   * than being stuck with a credential they cannot see.
   */
  acmeDnsCredentials(d: Domain): void {
    openAcmeDnsCredentialsModal(this.modal, { domain: d, nodes: this.project()?.nodes ?? [] })
      .afterClose.subscribe(() => this.load());
  }

  async deleteDomain(d: Domain): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Delete domain “${d.name}”?`,
      message: 'Renewals for it stop being answerable as soon as this is done.',
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/domains/${d.id}`).subscribe({
      next: () => {
        this.load();
        this.loadPlan();
        this.verify();
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }
}
