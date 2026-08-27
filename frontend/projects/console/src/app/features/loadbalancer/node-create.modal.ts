import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiService, LBNode, apiErrorMessage } from '@acme/shared';
export interface NodeCreateData {
  loadBalancerId: string;
}

/**
 * "Add node", as a modal.
 *
 * Either address may be blank but not both — a node with no address publishes nothing, and the
 * DNS plan it feeds would silently lose a record. The server enforces it too; checking here means
 * the operator finds out before the round trip, with the fields still in front of them.
 */
@Component({
  selector: 'app-node-create-modal',
  imports: [FormsModule, NzButtonModule, NzFormModule, NzInputModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      <nz-form-item>
        <nz-form-label nzRequired>Label</nz-form-label>
        <nz-form-control nzExtra="How this node is known — a, b, fra-1.">
          <input nz-input name="label" [(ngModel)]="label" required autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label>IPv4</nz-form-label>
        <nz-form-control>
          <input nz-input name="ipv4" [(ngModel)]="ipv4" placeholder="203.0.113.10" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label>IPv6</nz-form-label>
        <nz-form-control nzExtra="At least one address is required.">
          <input nz-input name="ipv6" [(ngModel)]="ipv6" placeholder="2001:db8::10" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button nz-button nzType="primary" type="submit" [nzLoading]="saving()" [disabled]="!valid()">
          Add node
        </button>
      </div>
    </form>
  `,
  styles: [
    `
      .actions {
        display: flex;
        justify-content: flex-end;
        gap: 0.5rem;
        margin-top: 1.5rem;
      }
    `,
  ],
})
export class NodeCreateModalComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<NodeCreateModalComponent, LBNode | undefined>);
  readonly data = inject<NodeCreateData>(NZ_MODAL_DATA);

  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  label = '';
  ipv4 = '';
  ipv6 = '';

  valid(): boolean {
    return !!this.label.trim() && !!(this.ipv4.trim() || this.ipv6.trim());
  }

  save(): void {
    if (!this.valid() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    this.api
      .post<LBNode>(`/loadbalancers/${this.data.loadBalancerId}/nodes`, {
        label: this.label.trim(),
        ipv4: this.ipv4.trim(),
        ipv6: this.ipv6.trim(),
      })
      .subscribe({
        next: (created) => this.ref.close(created),
        error: (err) => {
          this.saving.set(false);
          this.error.set(apiErrorMessage(err));
        },
      });
  }
}

export function openNodeCreateModal(modal: NzModalService, loadBalancerId: string) {
  return modal.create<NodeCreateModalComponent, NodeCreateData>({
    nzTitle: 'Add node',
    nzContent: NodeCreateModalComponent,
    nzData: { loadBalancerId },
    nzFooter: null,
    nzWidth: 420,
  }) as NzModalRef<NodeCreateModalComponent, LBNode | undefined>;
}
