import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiKeyCreated, ApiService, LBNode, apiErrorMessage } from '@acme/shared';
export interface ApiKeyCreateData {
  loadBalancerId: string;
  /** The load balancer's nodes, offered as one-click source-address restrictions. */
  nodes: LBNode[];
}

/**
 * "New API key", as a modal.
 *
 * The source-address restrictions are the reason this is a form and not two fields: a key that
 * may only be used from the load balancer's own addresses is the difference between a leaked secret
 * being an incident and being a nuisance, and nobody types a /32 correctly from memory. The
 * load balancer's node addresses are therefore offered as chips.
 */
@Component({
  selector: 'app-apikey-create-modal',
  imports: [FormsModule, NzButtonModule, NzFormModule, NzIconModule, NzInputModule, NzSwitchModule, NzTagModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      <nz-form-item>
        <nz-form-label>Note</nz-form-label>
        <nz-form-control nzExtra="What this key is for — shown in the key list and the audit trail.">
          <input nz-input name="note" [(ngModel)]="note" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label>Expires in</nz-form-label>
        <nz-form-control nzExtra="A Go duration such as 720h. Leave blank for a key that never expires.">
          <input nz-input name="expiresIn" [(ngModel)]="expiresIn" placeholder="720h" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label>Source-address restrictions (CIDR)</nz-form-label>
        <nz-form-control nzExtra="No restriction means the key works from anywhere.">
          @if (nodeAddresses().length) {
            <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
         nz-switch renders its own control inside this label and owns the association; the
         rule cannot see through the directive. -->
            <label class="pin">
              <nz-switch
                nzSize="small"
                name="pinToNodes"
                [(ngModel)]="pinToNodes"
                (ngModelChange)="onPinChange($event)"
              />
              Restrict to this load balancer's node addresses
            </label>
            <p class="ac-hint pin-hint">
              {{ nodeAddresses().join(', ') }} — a leaked secret is then useless from anywhere else.
            </p>
          } @else {
            <p class="ac-hint">This load balancer has no node addresses yet, so there is nothing to pin the key to.</p>
          }

          @if (networks().length) {
            <div class="chips">
              @for (n of networks(); track n; let i = $index) {
                <nz-tag nzColor="blue" [nzMode]="'closeable'" (nzOnClose)="removeNetwork(i)">{{ n }}</nz-tag>
              }
            </div>
          }

          <div class="cidr-row">
            <input
              nz-input
              name="cidr"
              [(ngModel)]="cidrInput"
              placeholder="203.0.113.0/24"
              autocomplete="off"
              (keydown.enter)="$event.preventDefault(); addNetworkFromInput()"
            />
            <button nz-button type="button" (click)="addNetworkFromInput()">Add</button>
          </div>
        </nz-form-control>
      </nz-form-item>

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button nz-button nzType="primary" type="submit" [nzLoading]="saving()">Create key</button>
      </div>
    </form>
  `,
  styles: [
    `
      .pin {
        display: flex;
        align-items: center;
        gap: 0.5rem;
      }
      .pin-hint {
        margin: 0.25rem 0 0.5rem;
      }
      .chips {
        display: flex;
        flex-wrap: wrap;
        gap: 0.25rem;
        margin-bottom: 0.5rem;
      }
      .add {
        cursor: pointer;
      }
      .cidr-row {
        display: flex;
        gap: 0.5rem;
      }
      .actions {
        display: flex;
        justify-content: flex-end;
        gap: 0.5rem;
        margin-top: 1.5rem;
      }
    `,
  ],
})
export class ApiKeyCreateModalComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<ApiKeyCreateModalComponent, ApiKeyCreated | undefined>);
  readonly data = inject<ApiKeyCreateData>(NZ_MODAL_DATA);

  readonly saving = signal(false);
  readonly error = signal<string | null>(null);
  readonly networks = signal<string[]>([]);

  note = '';
  expiresIn = '';
  cidrInput = '';
  /** The one-switch version of the common case: pin the key to the load balancer's own nodes. */
  pinToNodes = false;

  readonly nodeAddresses = computed(() => {
    const addrs: string[] = [];
    for (const n of this.data.nodes) {
      if (n.ipv4) addrs.push(`${n.ipv4}/32`);
      if (n.ipv6) addrs.push(`${n.ipv6}/128`);
    }
    return addrs;
  });

  /**
   * Adds or removes the node addresses as a block.
   *
   * Removing only what the switch added — a CIDR typed by hand that happens to equal a node
   * address stays, because the operator put it there for their own reason and a toggle should not
   * quietly discard it. In practice that is the same list either way; the difference only shows
   * when someone has been editing by hand, which is exactly when surprises are unwelcome.
   */
  onPinChange(on: boolean): void {
    if (on) {
      for (const addr of this.nodeAddresses()) this.addNetwork(addr);
      return;
    }
    const node = new Set(this.nodeAddresses());
    this.networks.update((ns) => ns.filter((n) => !node.has(n)));
  }

  addNetwork(cidr: string): void {
    if (!this.networks().includes(cidr)) this.networks.update((ns) => [...ns, cidr]);
  }

  addNetworkFromInput(): void {
    const v = this.cidrInput.trim();
    if (!v) return;
    this.addNetwork(v);
    this.cidrInput = '';
  }

  removeNetwork(i: number): void {
    this.networks.update((ns) => ns.filter((_, idx) => idx !== i));
  }

  save(): void {
    if (this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    this.api
      .post<ApiKeyCreated>(`/loadbalancers/${this.data.loadBalancerId}/keys`, {
        note: this.note,
        expiresIn: this.expiresIn,
        networks: this.networks(),
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

export function openApiKeyCreateModal(modal: NzModalService, data: ApiKeyCreateData) {
  return modal.create<ApiKeyCreateModalComponent, ApiKeyCreateData>({
    nzTitle: 'New API key',
    nzContent: ApiKeyCreateModalComponent,
    nzData: data,
    nzFooter: null,
    nzWidth: 520,
  }) as NzModalRef<ApiKeyCreateModalComponent, ApiKeyCreated | undefined>;
}
