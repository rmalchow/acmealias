import { ChangeDetectionStrategy, Component, inject, OnInit, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { ApiKey, ApiKeyCreated, ApiService, apiErrorMessage, confirmAction } from '@acme/shared';
import { Role } from '../roles/roles.models';
import { openApiKeyRevealModal } from './apikey-reveal.modal';

/**
 * System keys: the members of this account that are not people.
 *
 * WHY THIS IS IN SETTINGS AND NOT ON A PROJECT (design K8). A system key outlives every project
 * in the account; a project key dies with its project. Putting both on one screen would make that
 * difference invisible, and somebody would discover it by having their automation revoked when a
 * project was deleted. So the two kinds sit where the thing they belong to sits — and this one
 * belongs beside the roster, because a system key is a member that happens not to be a person.
 *
 * IT HOLDS A ROLE, NOT GRANTS (design K6). That is what buys the whole role machinery: the subset
 * rule, the role editor, the audit vocabulary, and the property somebody actually wants when they
 * realise the automation has too much authority — edit the role, and every key holding it changes
 * at once. A fleet of keys carrying their own grants can only be audited one key at a time and
 * adjusted one key at a time, which is what this replaces.
 *
 * TEAM ACCOUNTS ONLY, and the route guard on Settings is what makes that true rather than a
 * condition here — exactly as it is for roles and members. A personal account has no roles, so
 * there is nothing for such a key to hold; the service refuses with "there cannot be one here"
 * rather than a permission error, because in an account of one there is nobody to ask.
 */
@Component({
  selector: 'app-system-keys-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    DatePipe,
    FormsModule,
    NzButtonModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzSelectModule,
    NzTableModule,
    NzTagModule,
  ],
  template: `
    <h2>System keys</h2>

    <p class="ac-hint">
      A credential for something that is not a person — a pipeline, a controller, a script. It
      holds a role, so editing that role changes what every key holding it may do, at once. It
      belongs to the account and is not deleted when a project is; a key for one project's
      challenge answering is made on that project's page instead.
    </p>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <form nz-form nzLayout="inline" class="new" (ngSubmit)="create()">
      <nz-form-item>
        <nz-form-control>
          <input nz-input name="note" [(ngModel)]="note" placeholder="What this key is for" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>
      <nz-form-item>
        <nz-form-control>
          <nz-select name="roleId" [(ngModel)]="roleId" nzPlaceHolder="Role" class="role">
            @for (r of roles(); track r.id) {
              <nz-option [nzValue]="r.id" [nzLabel]="r.name" />
            }
          </nz-select>
        </nz-form-control>
      </nz-form-item>
      <nz-form-item>
        <nz-form-control>
          <button nz-button nzType="primary" type="submit" [disabled]="!roleId" [nzLoading]="saving()">
            <span nz-icon nzType="plus"></span>
            New system key
          </button>
        </nz-form-control>
      </nz-form-item>
    </form>

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="keys()"
      [nzShowPagination]="false"
      nzNoResult="No system keys."
    >
      <thead>
        <tr>
          <th>Identifier</th>
          <th>Note</th>
          <th>Authority</th>
          <th>Restrictions</th>
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
              @if (k.roleName) {
                <nz-tag>{{ k.roleName }}</nz-tag>
              } @else if (k.grants.length) {
                <!--
                  A key made before there were two kinds, holding its own grants. It still works
                  and is shown as it is: hiding it would be the worst possible way to describe an
                  active credential, and converting it into a role is a decision nobody asked for.
                -->
                @for (g of k.grants; track $index) {
                  <div class="ac-mono ac-grant">{{ g.summary }}</div>
                }
                <p class="ac-hint">Its own grants, from before keys held roles.</p>
              } @else {
                <span class="ac-hint">nothing — this key can do nothing</span>
              }
            </td>
            <td>
              @if (k.networks.length) {
                <span class="ac-mono">{{ k.networks.join(', ') }}</span>
              } @else {
                <span class="ac-hint">unrestricted</span>
              }
              @if (k.lastRefusedIp) {
                <p class="refused">
                  Last refused from <span class="ac-mono">{{ k.lastRefusedIp }}</span>
                  @if (k.lastRefusedAt) {
                    on {{ k.lastRefusedAt | date: 'medium' }}
                  }
                </p>
              }
            </td>
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
              <button nz-button nzType="link" nzSize="small" (click)="rotate(k)">Rotate</button>
              @if (!k.disabledAt) {
                <button nz-button nzType="link" nzDanger nzSize="small" (click)="revoke(k)">Revoke</button>
              }
            </td>
          </tr>
        }
      </tbody>
    </nz-table>
  `,
  styles: [
    `
      h2 {
        margin: 0 0 0.25rem;
        font-size: 1.125rem;
        font-weight: 500;
      }
      .new {
        margin-block: 0.75rem;
      }
      .role {
        min-width: 12rem;
      }
      tr.inactive td {
        opacity: 0.55;
      }
      .refused {
        margin: 0.25rem 0 0;
        font-size: 0.8125rem;
        color: var(--ac-warn);
        max-width: 24rem;
      }
    `,
  ],
})
export class SystemKeysPanelComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly modal = inject(NzModalService);

  readonly keys = signal<ApiKey[]>([]);
  readonly roles = signal<Role[]>([]);
  readonly error = signal<string | null>(null);
  readonly saving = signal(false);

  note = '';
  roleId: string | null = null;

  ngOnInit(): void {
    this.load();
    this.api.get<Role[]>('/roles').subscribe({
      next: (rs) => this.roles.set(rs),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  private load(): void {
    this.api.get<ApiKey[]>('/keys').subscribe({
      next: (ks) => this.keys.set(ks),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  create(): void {
    if (this.saving() || !this.roleId) return;
    this.saving.set(true);
    this.error.set(null);
    this.api
      .post<ApiKeyCreated>('/keys', { note: this.note, roleId: this.roleId })
      .subscribe({
        next: (created) => {
          this.saving.set(false);
          this.note = '';
          this.roleId = null;
          openApiKeyRevealModal(this.modal, created);
          this.load();
        },
        error: (err) => {
          this.saving.set(false);
          this.error.set(apiErrorMessage(err));
        },
      });
  }

  rotate(k: ApiKey): void {
    this.api.post<ApiKeyCreated>(`/keys/${k.id}/rotate`).subscribe({
      next: (created) => {
        openApiKeyRevealModal(this.modal, created);
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
}
