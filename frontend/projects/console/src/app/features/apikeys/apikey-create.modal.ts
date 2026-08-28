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
  projectId: string;
  /**
   * The project's nodes — and this modal now uses them only to SAY what the switch below would
   * permit. It does not copy them anywhere.
   */
  nodes: LBNode[];
}

/**
 * "New project key", as a modal.
 *
 * The key it creates is bound to the project and dies with it, and its authority is that
 * project's challenge answering. There is nothing to compose: the grant grammar is still there
 * for the expert case, and it is not what almost every key is for.
 *
 * THE ADDRESS LIMIT IS A SWITCH, AND THAT IS THE CHANGE. It used to copy the node addresses into
 * the CIDR list, which is the obvious implementation and reintroduces the exact problem the
 * feature exists to remove: the copy is right until somebody adds a node, and then the new node
 * publishes, is refused for its address, and the symptom is a renewal that silently does not
 * happen — surfacing weeks later as an expired certificate nobody connects to the node added
 * last month. On means "only from this project's nodes", resolved when a request arrives.
 *
 * The hand-typed CIDR list stays, for the cases the switch cannot express: a NAT gateway, an
 * egress range, a bastion. It is a different thing from the switch and is not mixed with it.
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
        <nz-form-label>Restrict to this project's nodes</nz-form-label>
        <nz-form-control>
          @if (nodeAddresses().length) {
            <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
         nz-switch renders its own control inside this label and owns the association; the
         rule cannot see through the directive. -->
            <label class="pin">
              <nz-switch nzSize="small" name="restrictToNodes" [(ngModel)]="restrictToNodes" />
              Only accept requests from this project's node addresses
            </label>
            <p class="ac-hint pin-hint">
              Currently {{ nodeAddresses().join(', ') }} — and it follows the project: add a node
              and the new node works, with no change here. A leaked secret is useless from anywhere
              else.
            </p>
            <p class="ac-hint pin-hint">
              The address compared is the one the request ARRIVES FROM. Behind NAT, a CDN or an
              egress gateway that is not the address configured on the node, and this would refuse
              everything while looking correct — the key's row shows the address actually seen when
              that happens.
            </p>
          } @else {
            <!--
              Design K4: the switch is not offered as an inert control here. "Only from my nodes"
              with no nodes means "from nowhere", which is the correct reading and a trap if it is
              only correct — somebody's first act on a new project may well be to make a key before
              adding a node, and they would meet a refusal that reads as a broken key.
            -->
            <p class="ac-hint">
              This project has no node addresses yet, so there is nothing to permit. Add a node and
              this becomes available.
            </p>
          }
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label>Source-address restrictions (CIDR)</nz-form-label>
        <nz-form-control nzExtra="For anything the switch above cannot express — a NAT gateway, an egress range. No restriction means the key works from anywhere.">
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
  /**
   * The common case, as one boolean sent to the service. Nothing is copied out of `nodes` — that
   * list is read only to say what the switch would permit today.
   */
  restrictToNodes = false;

  /**
   * What the switch permits AT THIS MOMENT, for the sentence under it. Bare addresses rather than
   * /32 and /128, because the switch compares addresses and not prefixes — writing a mask here
   * would suggest a list that could be edited, which is the thing this replaced.
   */
  readonly nodeAddresses = computed(() => {
    const addrs: string[] = [];
    for (const n of this.data.nodes) {
      if (n.ipv4) addrs.push(n.ipv4);
      if (n.ipv6) addrs.push(n.ipv6);
    }
    return addrs;
  });

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
      .post<ApiKeyCreated>(`/projects/${this.data.projectId}/keys`, {
        note: this.note,
        expiresIn: this.expiresIn,
        restrictToNodes: this.restrictToNodes,
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
    nzTitle: 'New project key',
    nzContent: ApiKeyCreateModalComponent,
    nzData: data,
    nzFooter: null,
    nzWidth: 520,
  }) as NzModalRef<ApiKeyCreateModalComponent, ApiKeyCreated | undefined>;
}
