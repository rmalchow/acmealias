import { Component, computed, inject, input, OnChanges, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiService, CAAStatus, CAN_WRITE, CertificateState, DelegationStatus, Domain, LBNode, PageHeaderComponent, Project, RecordCheck, SessionService, VerificationResult, apiErrorMessage, confirmAction, may , NOT_PERMITTED} from '@acme/shared';
import { openDomainEditor } from '../domains/domain-editor.component';
import { ApiKeysPanelComponent } from '../apikeys/apikeys-panel.component';
import { ProjectUsagePanelComponent } from '../reporting/project-usage-panel.component';
import { InteractionsPanelComponent } from './interactions-panel.component';
import { openNodeCreateModal } from './node-create.modal';
import { openNodeEditModal } from './node-edit.modal';
import { openDelegationHelpModal } from './delegation-help.modal';
import { openCaaHelpModal } from './caa-help.modal';
import { openCertificateModal } from './certificate.modal';
import { openDomainSettings } from '../domains/domain-settings.modal';
import { StatusChip, activationChip, caaChip, certificateChip, delegationChip, domainCheckFor, dotTitle } from './domain-status';

/**
 * A project: its nodes, its domain table, the API keys panel, and the reporting the project level
 * always had — usage over time.
 *
 * THE DNS PLAN PANEL IS GONE (design G2), and with it the record table, the zone-snippet
 * download, `Copy records` and `Run verification`. It described the whole project as a record set
 * and then joined back onto the domain rows by NAME, which is the indirection that made a
 * per-domain answer hard to give in the first place. Everything it carried is on the domain row
 * now: the CNAME target and the expected CAA entries come with the domain read, and the dots say
 * what is true of each. The two endpoints behind it no longer exist, so nothing here may call
 * them.
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
            <!-- TWELFTHS, and the same twelfths the Domains table below uses, so the two tables
                 share a rhythm instead of each finding its own: 2 / 3 / 6 / 1 here against
                 2 / 3 / 2 / 2 / 2 / 1 there, which puts a column boundary at the same place on
                 both at 2, 5 and 11. Percentages rather than rem because the tables are the full
                 width of the page and rem stops tracking it the moment the window changes. -->
            <th nzWidth="16.6667%">Label</th>
            <th nzWidth="25%">IPv4</th>
            <th nzWidth="50%">IPv6</th>
            <th nzWidth="8.3333%"></th>
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
        <!--
          RECHECK IS ONE BUTTON BECAUSE IT WAS ALWAYS ONE ACTION. It used to sit on every row,
          which read as "check this domain" — and it never was: the control posts
          /projects/:id/verification, which looks up every domain in the project and records what
          it saw for all of them. A per-row button for a whole-project action is a promise the
          code does not keep, and it also put a spinner on one row while the others changed
          underneath it.

          Outline rather than primary: the primary action on this panel is adding a domain. This
          one is a refresh, and two filled buttons side by side make neither of them the answer to
          "what do I do here".
        -->
        <button
          nz-button
          nzSize="small"
          [nzLoading]="verifying()"
          [disabled]="!project()?.domains?.length"
          nz-tooltip
          nzTooltipTitle="Look up every domain's delegation and CAA now, and record what we see"
          (click)="checkNow()"
        >
          <span nz-icon nzType="reload"></span>
          Recheck
        </button>
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
            <th nzWidth="16.6667%">Name</th>
            <th nzWidth="25%">Challenge name</th>
            <th nzWidth="16.6667%">Alias</th>
            <!-- Two twelfths now the cell holds one badge. It was 16rem when it also carried a
                 timestamp and a button. -->
            <th nzWidth="16.6667%">Challenges</th>
            <!-- Three dots and their type names, not three labelled chips: the column was 18rem
                 of mostly whitespace, and the width no longer depends on what the verdicts say. -->
            <th nzWidth="16.6667%">Validation</th>
            <th nzWidth="8.3333%"></th>
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
                <!--
                  THE BADGE ALONE. This cell also carried a "Last looked / Last seen" timestamp
                  and a Check now button, and both moved rather than went: the button is the
                  Recheck control above, because it always acted on every domain rather than on
                  this row, and the timestamp is in the badge's tooltip, where it answers the
                  question it always answered — how long has this been true — without spending a
                  line of every row on a date most rows never need.
                -->
                @if (activation(d); as act) {
                  <nz-tag [nzColor]="act.colour" nz-tooltip [nzTooltipTitle]="act.tooltip + lookedAt(d)">{{ act.label }}</nz-tag>
                }
              </td>
              <!-- A dot per record type this domain HAS: a filled circle, the type beside it, the
                   verdict in the tooltip, and a click that opens the dialog for that type alone
                   (G3). The label is the record TYPE and never the verdict — a label reading
                   "configured incorrectly" makes a row's width depend on how broken it is, and a
                   table whose columns move as state changes is a table nobody can scan.

                   A type this domain does not have gets NO dot rather than a grey one: CAA with
                   no issuer configured, TLS with the switch off. A grey dot would claim we meant
                   to look. And every dot is clickable, green included — a control that appears
                   only when something is wrong teaches people to read its presence as an alarm. -->
              <td>
                @if (checks(d); as ch) {
                  <div class="dots">
                    <button type="button" class="dot-btn" nz-tooltip [nzTooltipTitle]="title(ch.delegation)" (click)="explainDelegation(d)">
                      <span class="dot" [class]="'dot-' + ch.delegation.colour"></span>
                      CNAME
                    </button>
                    @if (ch.caa; as caa) {
                      <button type="button" class="dot-btn" nz-tooltip [nzTooltipTitle]="title(caa)" (click)="explainCaa(d)">
                        <span class="dot" [class]="'dot-' + caa.colour"></span>
                        CAA
                      </button>
                    }
                    @if (ch.tls; as tls) {
                      <button type="button" class="dot-btn" nz-tooltip [nzTooltipTitle]="title(tls)" (click)="explainCertificate(d)">
                        <span class="dot" [class]="'dot-' + tls.colour"></span>
                        TLS
                      </button>
                    }
                  </div>
                }
              </td>
              <td class="row-actions">
                <!--
                  NO acme-dns CREDENTIALS ACTION HERE. It used to sit at the head of this cell and
                  mint a key bound to the domain, which put "create a credential" in the middle of
                  a table people read to see whether their DNS is right. Minting belongs where the
                  other keys are minted; a domain row is for looking at a domain.
                -->
                <button
                  nz-button
                  nzType="text"
                  nzSize="small"
                  [disabled]="!canWrite()"
                  nz-tooltip
                  [nzTooltipTitle]="canWrite() ? 'Observation settings' : NOT_PERMITTED"
                  aria-label="Observation settings"
                  (click)="domainSettings(d)"
                >
                  <span nz-icon nzType="safety-certificate"></span>
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

      <!-- A failure somebody PRESSED for. It used to sit under the plan panel, which is where the
           button was; the button is on the row now, and the banner follows it rather than the
           panel it outlived. A check nobody asked for still gets no banner (design D1) — it
           degrades the dots it concerns and never the page. -->
      @if (verifyError(); as msg) {
        <nz-alert nzType="error" [nzMessage]="msg" nzShowIcon />
      }
    </section>

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
        gap: 0.5rem;
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
        margin: 0 0.5rem 0 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      /* A dot per record type, on one line (design G3). Each is a real <button>, so the keyboard
       * and a screen reader reach it the same way the mouse does — a clickable <span> is a
       * control only for people using a pointer. */
      .dots {
        display: flex;
        align-items: center;
        gap: 0.75rem;
      }
      .dot-btn {
        display: inline-flex;
        align-items: center;
        gap: 0.375rem;
        padding: 0;
        border: 0;
        background: none;
        font: inherit;
        font-size: 0.8125rem;
        color: var(--ac-muted);
        cursor: pointer;
      }
      .dot-btn:hover,
      .dot-btn:focus-visible {
        color: inherit;
      }
      .dot {
        width: 0.625rem;
        height: 0.625rem;
        border-radius: 50%;
        display: inline-block;
      }
      /* The three colours and grey, and grey is "we did not look" rather than "this is fine".
       * Falling back to literals because a deployment that has not defined these tokens would
       * otherwise render four invisible dots, which is worse than four wrong ones. */
      .dot-green {
        background: var(--ac-ok, #52c41a);
      }
      .dot-orange {
        background: var(--ac-warn, #fa8c16);
      }
      .dot-red {
        background: var(--ac-error, #f5222d);
      }
      .dot-default {
        background: var(--ac-line, #d9d9d9);
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
  private readonly session = inject(SessionService);

  readonly id = input.required<string>();

  readonly project = signal<Project | null>(null);
  /**
   * What was observed on the wire, per domain, keyed by domain id.
   *
   * One request per domain WITH THE SWITCH ON and none for the rest, which is what keeps this
   * from being a request per row: observation is opt-in, and a project where nobody has turned it
   * on makes no extra call at all. A domain still loading is simply absent from the map, and the
   * dot reads "not observed yet" until it arrives — the same thing it says for a switch turned on
   * a minute ago, and the same instruction, which is to wait.
   */
  readonly certificates = signal<Record<string, CertificateState>>({});
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

  protected readonly NOT_PERMITTED = NOT_PERMITTED;

  /** Whether this session may change this project's inventory — nodes and domains. */
  readonly canWrite = computed(() => may(this.project(), CAN_WRITE));

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
    this.certificates.set({});
    this.load();
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
      next: (p) => {
        this.project.set(p);
        this.loadCertificates(p);
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  /**
   * What each observed domain's dot is coloured by.
   *
   * A FAILURE HERE IS SILENT. The state read is one request per observed domain and it colours a
   * dot; a banner for it would put the whole screen into an error state because one dot could not
   * be filled in, and the dot already has an honest answer for "we do not know", which is grey.
   * The screen is reused across ids, so a late answer is dropped rather than painted onto another
   * project's rows.
   */
  private loadCertificates(p: Project): void {
    const asked = this.id();
    for (const d of p.domains ?? []) {
      if (!d.observeTls) continue;
      this.api.get<CertificateState>(`/domains/${d.id}/certificate`).subscribe({
        next: (s) => {
          if (this.id() !== asked) return;
          this.certificates.update((m) => ({ ...m, [d.id]: s }));
        },
        error: () => undefined,
      });
    }
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
    // whole fix: an answer that arrives for the project somebody has just left would otherwise
    // colour the dots of the one they are looking at now.
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
   * When we last looked, as a line appended to the activation chip's tooltip.
   *
   * It used to be a paragraph under the chip on every row. That spent a line of vertical space in
   * every row on a date that matters for the few rows where something is wrong, and it made the
   * row height depend on whether a domain had ever been checked — so a table of ten domains had
   * two different row heights for no reason a reader could name.
   *
   * The wording is unchanged and so is the reasoning behind it: "since when", never "how many
   * failures", because the question anybody actually asks is how long this has been true and the
   * timestamp answers it exactly (design A1). No deadline is stated, because there is none —
   * nothing is ever switched off for this (A3).
   */
  lookedAt(d: Domain): string {
    if (d.lastSuccessAt && this.stoppedResolving(d)) {
      return `\nLast seen ${new Date(d.lastSuccessAt).toLocaleString()}`;
    }
    if (d.lastCheckedAt) {
      return `\nLast looked ${new Date(d.lastCheckedAt).toLocaleString()}`;
    }
    return '\nNot looked at yet';
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
   * A dot per record type this domain HAS (design G3).
   *
   * Null is "no dot at all", never a grey one. CAA appears only where an issuer is configured for
   * the deployment — the domain says so by carrying `expectedCaa`, so this is answerable without
   * a check having run, and with no issuer there is nothing to authorise and so nothing about CAA
   * that is true or false here. TLS appears only where somebody turned the switch on: we have not
   * connected to anything, and a grey dot would claim we meant to.
   */
  checks(d: Domain): { delegation: StatusChip; caa: StatusChip | null; tls: StatusChip | null } {
    const a = this.answerFor(d);
    return {
      // Whether this domain has an alias at all is known from the row itself, not from the
      // check: without one there is no target, nothing is ever asked, and the dot would sit at
      // "not checked" forever unless it says why.
      delegation: delegationChip(a.delegation.status, !!d.aliasLabel),
      caa: d.expectedCaa?.length ? caaChip(a.caa?.status ?? (this.checkFailed() ? 'undetermined' : 'not-checked')) : null,
      // The band is the SERVICE's (G9). A domain whose state has not arrived is `unobserved`,
      // which is the same grey and the same instruction as one whose first pass has not run.
      tls: d.observeTls ? certificateChip(this.certificate(d)?.band ?? 'unobserved') : null,
    };
  }

  /** A dot's tooltip: the verdict, then the sentence behind it. The dot itself carries no word. */
  title(chip: StatusChip): string {
    return dotTitle(chip);
  }

  /** What was observed for this domain, or null while the read is in flight or has failed. */
  certificate(d: Domain): CertificateState | null {
    return this.certificates()[d.id] ?? null;
  }

  /**
   * This row's answer for both DNS record types, or the state we are in when none arrived.
   *
   * Three things are folded together here on purpose:
   *
   *   - A request that FAILED is "could not check" — we looked and could not. One still in
   *     flight, or a deployment without a resolver, is "not checked": we have not looked, and
   *     that must never read as a fault in the customer's setup (design D4).
   *   - A failed re-check degrades an answer we already have rather than leaving it standing.
   *     The previous answer is kept only for its SHAPE — which record types this domain has —
   *     because the alternative is the CAA dot vanishing from a row that said "blocks us" a
   *     second ago. Its statuses are not kept: an answer we could not refresh is not an answer.
   *   - THE TARGET IS NOT IN HERE ANY MORE. It used to be dug out of the project's DNS plan by
   *     matching the challenge name, so a dialog opened before the plan arrived could not say
   *     what to publish. It is a field on the domain now (task 11.2), which the dialogs read
   *     directly — which record a domain needs is knowable without asking DNS anything, and this
   *     is what keeps it that way.
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
    return {
      delegation: { name: d.challengeName, status: failed ? 'undetermined' : 'not-checked' },
      caa: null,
    };
  }

  /**
   * The three dialogs behind the three dots (design G7).
   *
   * One per record type, because a single dialog covering all of them has to lead with a section
   * the reader did not click on. Each carries THIS domain's own values: the general instructions
   * are in `docs/`, and what is missing at the moment somebody is confused is their own value,
   * which a documentation page cannot know.
   */
  explainDelegation(d: Domain): void {
    openDelegationHelpModal(this.modal, { domain: d, delegation: this.answerFor(d).delegation });
  }

  explainCaa(d: Domain): void {
    openCaaHelpModal(this.modal, { domain: d, caa: this.answerFor(d).caa });
  }

  explainCertificate(d: Domain): void {
    openCertificateModal(this.modal, {
      domain: d,
      // Nothing observed yet is an empty list rather than an absence the dialog has to test for,
      // and the server name is the one we would ask for — the test hostname on a wildcard.
      state: this.certificate(d) ?? {
        serverName: d.tlsTestHost || d.name,
        band: 'unobserved',
        addresses: [],
      },
    });
  }

  editNode(n: LBNode): void {
    openNodeEditModal(this.modal, n).afterClose.subscribe((updated?: LBNode) => {
      if (!updated) return;
      // The addresses are what an observation connects to, so the states this reloads are the
      // ones that just became about somewhere else.
      this.load();
    });
  }

  createNode(): void {
    openNodeCreateModal(this.modal, this.id()).afterClose.subscribe((created?: LBNode) => {
      if (!created) return;
      this.load();
    });
  }

  addDomain(): void {
    openDomainEditor(this.modal, this.project()!.id).afterClose.subscribe((created?: boolean) => {
      if (!created) return;
      this.load();
      // The answers are per domain now, so a set of domains that changed leaves them describing
      // a table that no longer exists — and the new row is the one somebody is waiting on.
      this.verify();
    });
  }

  async deleteNode(n: LBNode): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Delete node “${n.label}”?`,
      message: 'Challenges stop being answered from it, and nothing is observed on it any more.',
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/nodes/${n.id}`).subscribe({
      next: () => this.load(),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }


  /**
   * The observation switch, opened from the row rather than only from "Add domain" — a switch
   * that could only be set at creation would be a switch nobody could turn on for a domain they
   * already have (design G-corrected 9.1). Reloads on save so the dot for this domain reflects
   * the new setting immediately rather than after the next poll.
   */
  domainSettings(d: Domain): void {
    openDomainSettings(this.modal, d).afterClose.subscribe((updated?: Domain) => {
      if (!updated) return;
      this.load();
    });
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
        this.verify();
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }
}
