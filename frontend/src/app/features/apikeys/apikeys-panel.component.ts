import { Component, computed, inject, input, OnChanges, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { SessionService } from '../../core/session.service';
import { confirmAction } from '../../shared/confirm';
import { ApiKey, ApiKeyCreated, LBNode } from '../models';
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
      @if (canWrite()) {
        <button nz-button nzType="primary" nzSize="small" (click)="create()">
          <span nz-icon nzType="plus"></span>
          New API key
        </button>
      }
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
              @if (canWrite()) {
                <button nz-button nzType="link" nzSize="small" (click)="rotate(k)">Rotate</button>
                @if (!k.disabledAt) {
                  <button nz-button nzType="link" nzDanger nzSize="small" (click)="revoke(k)">Revoke</button>
                }
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

  readonly loadBalancerId = input.required<number>();
  readonly nodes = input<LBNode[]>([]);

  readonly keys = signal<ApiKey[]>([]);
  readonly error = signal<string | null>(null);

  readonly canWrite = computed(() => this.session.hasAnyRole(['ADMIN', 'CONFIG_ADMIN', 'KEY_ROTATION']));

  ngOnChanges(): void {
    this.load();
  }

  private load(): void {
    this.api.get<ApiKey[]>(`/loadbalancers/${this.loadBalancerId()}/keys`).subscribe({
      next: (ks) => this.keys.set(ks),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  create(): void {
    openApiKeyCreateModal(this.modal, { loadBalancerId: this.loadBalancerId(), nodes: this.nodes() })
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
