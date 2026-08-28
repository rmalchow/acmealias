import { Component, computed, inject, input, OnChanges, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiKey, ApiKeyCreated, ApiService, CAN_WRITE_KEYS, HelpHintComponent, LBNode, NOT_PERMITTED, Project, SessionService, apiErrorMessage, confirmAction, may } from '@acme/shared';
import { openApiKeyCreateModal } from './apikey-create.modal';
import { openApiKeyRevealModal } from './apikey-reveal.modal';

/**
 * This project's keys.
 *
 * PROJECT keys only, and that is design K8 rather than a filter that happens to be convenient. A
 * project key is bound to this project and is deleted with it; a system key belongs to the account
 * and outlives every project in it. Listing both here would leave somebody asking, later and
 * angrily, why deleting a project revoked their automation — and the answer would be a rule they
 * had no way to see. System keys are in the account's settings, beside roles and members.
 *
 * Creation and the once-only reveal are both modals; the reveal's rule (acknowledge before
 * dismissal) lives in ApiKeyRevealModalComponent, which is also what `rotate` opens — a rotation mints a
 * secret exactly like a creation does, and it must not be dismissible either.
 */
@Component({
  selector: 'app-apikeys-panel',
  imports: [DatePipe, HelpHintComponent, NzAlertModule, NzButtonModule, NzIconModule, NzTableModule, NzTagModule, NzToolTipModule],
  template: `
    <div class="panel-head">
      <h2>
        Project keys
        <app-help-hint
          heading="Project keys"
          [paragraphs]="[
            'The credentials your ACME clients authenticate with. A key goes into the DNS-01 configuration of your own client — Traefik, cert-manager, acme.sh — and is what lets that client hand us a challenge value for one of this project’s domains.',
            'A key reaches this project and nothing else, and it is deleted with the project. It cannot read your account, cannot touch another project, and cannot publish for a name that is not in the Domains table above.',
            'Restrictions narrow it further. A key can be confined to CIDR ranges you name, and to the addresses of this project’s own nodes — in which case a node added later works with no change here. A key with neither restriction works from anywhere, which is convenient and is worth a moment’s thought.',
            'The secret is shown once, at creation, and is never recoverable afterwards. Rotating mints a successor without revoking the current key: deploy the new secret, confirm it works, then revoke the old one. Both are valid side by side on purpose, so there is no gap.'
          ]"
        />
      </h2>
      <button
        nz-button
        nzType="primary"
        nzSize="small"
        [disabled]="!canWrite()"
        nz-tooltip
        [nzTooltipTitle]="canWrite() ? null : NOT_PERMITTED"
        (click)="create()"
      >
        <span nz-icon nzType="plus"></span>
        New project key
      </button>
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="keys()"
      [nzShowPagination]="false"
      nzNoResult="No API keys yet."
    >
      <thead>
        <tr>
          <!-- TWELFTHS, like the tables above this panel: 2 / 2 / 2 / 2 / 2 / 1 / 1. Percentages
               rather than rem, because this table is as wide as the page and rem stops tracking
               that the moment somebody resizes the window. The two date columns are two twelfths
               each because that is what a formatted date needs; Status holds one short tag and the
               actions are two icons. -->
          <th nzWidth="16.6667%">Identifier</th>
          <th nzWidth="16.6667%">Note</th>
          <th nzWidth="16.6667%">Restrictions</th>
          <th nzWidth="16.6667%">Expires</th>
          <th nzWidth="16.6667%">Last used</th>
          <th nzWidth="8.3333%">Status</th>
          <th nzWidth="8.3333%"></th>
        </tr>
      </thead>
      <tbody>
        @for (k of keys(); track k.id) {
          <tr [class.inactive]="!k.active">
            <!--
              THE IDENTIFIER, WHICH IS WHAT THIS COLUMN HAS ALWAYS BEEN CALLED. It used to render
              displayPrefix — the first six characters of the SECRET — so the one PUBLIC value a
              client needs appeared nowhere in the console: not here, and not in the reveal dialog
              either. It could only be got out of the API or the CLI. Anything authenticating with
              HTTP Basic needs it as the username: lego's httpreq, acme-dns, curl.

              The secret fragment is gone rather than demoted. It identified a key by six
              characters of the one value that must not be on a screen at all, and the note in the
              next column is what a person actually recognises a key by.
            -->
            <td class="ac-mono">{{ k.identifier }}</td>
            <td>{{ k.note || '—' }}</td>
            <!--
              There is no Authority column, and its absence is the point: every key here reaches
              exactly this project's challenges. A column repeating one sentence on every row is a
              column that stops being read.
            -->
            <td>
              @if (k.restrictToNodes) {
                <div>this project&rsquo;s node addresses</div>
                <p class="ac-hint">
                  Follows the project — a node added later works with no change here.
                </p>
              }
              @if (k.networks.length) {
                <div class="ac-mono">{{ k.networks.join(', ') }}</div>
              }
              @if (!k.restrictToNodes && !k.networks.length) {
                <span class="ac-hint">unrestricted</span>
              }
              <!--
                The address we actually SAW. Behind NAT or an egress gateway it is not the address
                configured on the node, and without this the operator has a key that looks
                correctly configured and refuses everything (design K5). Shown only when there is
                one, because a key that has never been refused should say nothing.
              -->
              @if (k.lastRefusedIp) {
                <p class="refused">
                  Last refused from <span class="ac-mono">{{ k.lastRefusedIp }}</span>
                  @if (k.lastRefusedAt) {
                    on {{ k.lastRefusedAt | date: 'medium' }}
                  }
                  — that is the address the request arrived from, which is not always the one on
                  the node.
                </p>
              }
            </td>
            <td class="ac-nowrap">{{ k.expiresAt ? (k.expiresAt | date: 'medium') : 'never' }}</td>
            <td class="ac-nowrap">{{ k.lastUsedAt ? (k.lastUsedAt | date: 'medium') : 'never used' }}</td>
            <td>
              @if (k.disabledAt) {
                <nz-tag nzColor="red">revoked</nz-tag>
              } @else if (k.active) {
                <nz-tag nzColor="green">active</nz-tag>
              } @else {
                <nz-tag>expired</nz-tag>
              }
            </td>
            <!-- ICONS, with the words in the tooltip and the aria-label — the same treatment the
                 domain row's actions already use, so a table of twenty keys is not twenty
                 repetitions of "Rotate" and "Revoke". The sync glyph for rotation, because antd publishes no
                 recycle icon and sync is the circular one; the delete glyph for revocation, which is
                 the same glyph every other destructive row action in this console uses. The words
                 survive where they carry weight: the confirmation dialog still says Revoke. -->
            <td class="row-actions">
              <button
                nz-button
                nzType="text"
                nzSize="small"
                [disabled]="!canWrite()"
                nz-tooltip
                [nzTooltipTitle]="canWrite() ? 'Rotate' : NOT_PERMITTED"
                aria-label="Rotate key"
                (click)="rotate(k)"
              >
                <span nz-icon nzType="sync"></span>
              </button>
              @if (!k.disabledAt) {
                <!-- Still conditional, and deliberately: a revoked key has nothing to revoke.
                     That is a fact about the key, not about the caller. -->
                <button
                  nz-button
                  nzType="text"
                  nzDanger
                  nzSize="small"
                  [disabled]="!canWrite()"
                  nz-tooltip
                  [nzTooltipTitle]="canWrite() ? 'Revoke' : NOT_PERMITTED"
                  aria-label="Revoke key"
                  (click)="revoke(k)"
                >
                  <span nz-icon nzType="delete"></span>
                </button>
              }
            </td>
          </tr>
        }
      </tbody>
    </nz-table>

    <!--
      UNDER THE TABLE, AND IN A BOX, and it says WHOSE configuration the key goes into. It used
      to open with "a project key answers this project's ACME challenges", which describes what
      the key is FOR from our side and leaves the reader to work out what to do with it. What they
      do with it is paste it into their own cert resolver — traefik, cert-manager, acme.sh — and
      the sentence now says so first.

      It was also a plain paragraph above the table, which put four lines of explanation between
      the heading and the thing being explained — so the first time
      you open the screen you read the theory, and every time after that you scroll past it to
      reach the rows you came for.
      Below and boxed, it is available to the person who needs it and out of the way of the person
      who does not. The rotation sentence is the load-bearing one: rotating does NOT revoke, and
      somebody who assumes otherwise leaves a live key they think is dead.
    -->
    <nz-alert
      class="panel-note"
      nzType="info"
      nzShowIcon
      nzMessage="About project keys"
      nzDescription="A project key goes in YOUR ACME client — the credentials of its DNS-01 resolver. It is what lets your client hand us the challenge value for a name in this project, so we can serve it. It reaches nothing else, and it is deleted with the project. Rotating mints a successor but does not revoke the current key: deploy the new secret, confirm it works, then revoke the old one. The two are valid side by side on purpose, so there is no gap."
    />
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
        gap: 0.5rem;
        /* A few pixels, so the heading row reads as a label for the table rather than as
           part of its first row. It was 0.25rem, which on a bordered table put the
           buttons all but touching the header cells. */
        margin-bottom: 0.75rem;
      }
      .panel-head > :nth-child(2) {
        margin-inline-start: auto;
      }
      h2 {
        margin: 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      tr.inactive td {
        opacity: 0.55;
      }
      .refused {
        margin: 0.25rem 0 0;
        font-size: 0.8125rem;
        color: var(--ac-warn);
        max-width: 26rem;
      }
    
      /* Icons only, so the words do not repeat down the table — the label lives in the tooltip
         and the aria-label. Nowrap keeps the pair on one line in a narrow column. */
      .row-actions {
        white-space: nowrap;
      }
      .panel-note {
        margin-block-start: 1rem;
      }
    `,
  ],
})
export class ApiKeysPanelComponent implements OnChanges {
  private readonly api = inject(ApiService);
  private readonly modal = inject(NzModalService);
  private readonly session = inject(SessionService);

  readonly projectId = input.required<string>();
  /** The row this panel belongs to, for the per-object permission answer it carries. */
  readonly project = input<Project | null>(null);
  readonly nodes = input<LBNode[]>([]);

  readonly keys = signal<ApiKey[]>([]);
  readonly error = signal<string | null>(null);

  /**
   * Whether keys may be minted here, answered for THIS load balancer rather than for the
   * caller's role. The panel is handed the row it belongs to, so the answer travels with it.
   */
  readonly canWrite = computed(() => may(this.project(), CAN_WRITE_KEYS));
  protected readonly NOT_PERMITTED = NOT_PERMITTED;

  ngOnChanges(): void {
    this.load();
  }

  private load(): void {
    this.api.get<ApiKey[]>(`/projects/${this.projectId()}/keys`).subscribe({
      next: (ks) => this.keys.set(ks),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  create(): void {
    openApiKeyCreateModal(this.modal, { projectId: this.projectId(), nodes: this.nodes() })
      .afterClose.subscribe((created?: ApiKeyCreated) => {
        if (!created) return;
        this.reveal(created);
        this.load();
      });
  }

  rotate(k: ApiKey): void {
    this.api.post<ApiKeyCreated>(`/keys/${k.id}/rotate`).subscribe({
      next: (created) => {
        this.reveal(created);
        this.load();
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  async revoke(k: ApiKey): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Revoke key ${k.identifier}?`,
      message: 'Anything still using it will start failing immediately. This cannot be undone.',
      okText: 'Revoke',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/keys/${k.id}`).subscribe({
      next: () => this.load(),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  private reveal(created: ApiKeyCreated): void {
    openApiKeyRevealModal(this.modal, created);
  }
}
