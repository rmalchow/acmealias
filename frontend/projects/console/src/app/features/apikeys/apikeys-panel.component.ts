import { Component, computed, inject, input, OnChanges, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiKey, ApiKeyCreated, ApiService, CAN_WRITE_KEYS, LBNode, NOT_PERMITTED, Project, SessionService, apiErrorMessage, confirmAction, may } from '@acme/shared';
import { openApiKeyCreateModal } from './apikey-create.modal';
import { openApiKeyRevealModal } from './apikey-reveal.modal';

/**
 * 19.4 — API keys for one load balancer.
 *
 * Creation and the once-only reveal are both modals; the reveal's rule (acknowledge before
 * dismissal) lives in ApiKeyRevealModalComponent, which is also what `rotate` opens — a rotation mints a
 * secret exactly like a creation does, and it must not be dismissible either.
 */
@Component({
  selector: 'app-apikeys-panel',
  imports: [DatePipe, NzButtonModule, NzIconModule, NzTableModule, NzTagModule, NzToolTipModule],
  template: `
    <div class="panel-head">
      <h2>API keys</h2>
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
        New API key
      </button>
    </div>

    <p class="ac-hint">
      Rotating mints a successor but does not revoke the current key — deploy the new secret,
      confirm it works, then revoke the old one. The two are valid side by side on purpose, so
      there is no gap.
    </p>

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
          <th>Identifier</th>
          <th>Note</th>
          <th>Authority</th>
          <th>Restrictions</th>
          <th>Expires</th>
          <th>Last used</th>
          <th nzWidth="7rem">Status</th>
          <th nzWidth="11rem"></th>
        </tr>
      </thead>
      <tbody>
        @for (k of keys(); track k.id) {
          <tr [class.inactive]="!k.active">
            <td class="ac-mono">{{ k.displayPrefix }}&hellip;</td>
            <td>{{ k.note || '—' }}</td>
            <!--
              What this key can actually do. It used to be implied by which load balancer the key
              belonged to; a key is a subject holding grants now, so showing the grants is the only
              honest answer — and a key that reaches a whole project looks different here from one
              that reaches a single load balancer, which is the point of the change.
            -->
            <td>
              @for (g of k.grants; track $index) {
                <div class="ac-mono ac-grant">{{ g.summary }}</div>
              } @empty {
                <span class="ac-hint">no grants — this key can do nothing</span>
              }
            </td>
            <td>
              @if (k.networks.length) {
                {{ k.networks.join(', ') }}
              } @else {
                <span class="ac-hint">unrestricted</span>
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
            <td>
              <button
                nz-button
                nzType="link"
                nzSize="small"
                [disabled]="!canWrite()"
                nz-tooltip
                [nzTooltipTitle]="canWrite() ? null : NOT_PERMITTED"
                (click)="rotate(k)"
              >
                Rotate
              </button>
              @if (!k.disabledAt) {
                <!-- Still conditional, and deliberately: a revoked key has nothing to revoke.
                     That is a fact about the key, not about the caller. -->
                <button
                  nz-button
                  nzType="link"
                  nzDanger
                  nzSize="small"
                  [disabled]="!canWrite()"
                  nz-tooltip
                  [nzTooltipTitle]="canWrite() ? null : NOT_PERMITTED"
                  (click)="revoke(k)"
                >
                  Revoke
                </button>
              }
            </td>
          </tr>
        }
      </tbody>
    </nz-table>
  `,
  styles: [
    `
      .panel-head {
        display: flex;
        align-items: center;
        gap: 1rem;
        margin-bottom: 0.25rem;
      }
      h2 {
        margin: 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      tr.inactive td {
        opacity: 0.55;
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
      title: `Revoke key ${k.displayPrefix}…?`,
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
