import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiService, LBNode, apiErrorMessage } from '@acme/shared';
/**
 * Editing a node's addresses.
 *
 * Only the addresses: the label is identity — it names the node in the DNS plan an operator has
 * already handed to a customer, and renaming it would silently invalidate that. Changing an
 * address is the opposite: it is the routine act of replacing a machine, and the plan is
 * *supposed* to change with it.
 *
 * Clearing both is refused. A node with no address contributes nothing to the plan, so the A and
 * AAAA records for every domain on the load balancer would quietly lose a target while the node still
 * appears to exist.
 */
@Component({
  selector: 'app-node-edit-modal',
  imports: [FormsModule, NzButtonModule, NzFormModule, NzInputModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      <nz-form-item>
        <nz-form-label>IPv4</nz-form-label>
        <nz-form-control>
          <input nz-input name="ipv4" [(ngModel)]="ipv4" placeholder="203.0.113.10" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label>IPv6</nz-form-label>
        <nz-form-control nzExtra="At least one address is required. Clearing both is refused.">
          <input nz-input name="ipv6" [(ngModel)]="ipv6" placeholder="2001:db8::10" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <p class="ac-hint">
        The DNS plan changes with this. Records already published by the customer keep pointing at
        the old address until they republish.
      </p>

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button nz-button nzType="primary" type="submit" [nzLoading]="saving()" [disabled]="!valid()">
          Save addresses
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
export class NodeEditModalComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<NodeEditModalComponent, LBNode | undefined>);
  readonly data = inject<LBNode>(NZ_MODAL_DATA);

  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  ipv4 = this.data.ipv4 ?? '';
  ipv6 = this.data.ipv6 ?? '';

  valid(): boolean {
    return !!(this.ipv4.trim() || this.ipv6.trim());
  }

  save(): void {
    if (!this.valid() || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    this.api
      .patch<LBNode>(`/nodes/${this.data.id}`, { ipv4: this.ipv4.trim(), ipv6: this.ipv6.trim() })
      .subscribe({
        next: (updated) => this.ref.close(updated),
        error: (err) => {
          this.saving.set(false);
          this.error.set(apiErrorMessage(err));
        },
      });
  }
}

export function openNodeEditModal(modal: NzModalService, node: LBNode) {
  return modal.create<NodeEditModalComponent, LBNode>({
    nzTitle: `Addresses of node “${node.label}”`,
    nzContent: NodeEditModalComponent,
    nzData: node,
    nzFooter: null,
    nzWidth: 420,
  }) as NzModalRef<NodeEditModalComponent, LBNode | undefined>;
}
