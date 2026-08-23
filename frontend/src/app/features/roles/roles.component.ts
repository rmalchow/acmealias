import { ChangeDetectionStrategy, Component, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { firstValueFrom } from 'rxjs';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { SessionService } from '../../core/session.service';
import { GrantComposerComponent } from './grant-composer.component';
import { Grant, Member, Role } from './roles.models';

/**
 * Roles and membership: who may do what, in this account (tasks 9.2 and 9.4).
 *
 * Two rules the screen has to make visible rather than merely obey.
 *
 * The unrestricted role is rendered as uneditable, and its last member's removal is not offered.
 * It confers everything in the account by definition, so editing it would mean editing what
 * "everything" means; and an account with no unrestricted member is one nobody can administer,
 * which no screen should be able to produce by accident. The service refuses both regardless —
 * this is about not offering an action that will be refused.
 *
 * The roster is readable by any member without a grant. Knowing who else is in the room is not a
 * privilege, and a roster nobody can see is how an account ends up with a member nobody remembers
 * adding.
 */
@Component({
  selector: 'app-roles',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    NzAlertModule,
    NzButtonModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzSelectModule,
    NzTableModule,
    NzTagModule,
    GrantComposerComponent,
  ],
  template: `
    <h1>Roles and members</h1>

    @if (error(); as e) {
      <nz-alert nzType="error" [nzMessage]="e" />
    }

    <section>
      <h2>Roles</h2>
      <nz-table #rt [nzData]="roles()" nzSize="small" [nzShowPagination]="false">
        <thead>
          <tr>
            <th>Name</th>
            <th>Description</th>
            <th>Grants</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          @for (r of rt.data; track r.id) {
            <tr>
              <td>
                {{ r.name }}
                @if (r.unrestricted) {
                  <nz-tag nzColor="gold">unrestricted</nz-tag>
                }
              </td>
              <td>{{ r.description }}</td>
              <td>
                @if (r.unrestricted) {
                  <span class="ac-muted">every action on every object in this account</span>
                } @else {
                  @for (g of r.grants; track $index) {
                    <div class="ac-mono">{{ g.summary }}</div>
                  } @empty {
                    <span class="ac-muted">none</span>
                  }
                }
              </td>
              <td class="row-actions">
                @if (r.unrestricted) {
                  <!--
                    Not merely disabled: there is nothing to edit. The role's authority is a
                    property of the account, not a list of rows someone could shorten.
                  -->
                  <span class="ac-muted">fixed</span>
                } @else {
                  <button nz-button nzSize="small" (click)="edit(r)">Edit</button>
                  <button nz-button nzSize="small" nzDanger (click)="remove(r)">Delete</button>
                }
              </td>
            </tr>
          }
        </tbody>
      </nz-table>

      <h3>{{ editing() ? 'Edit role' : 'New role' }}</h3>
      <form nz-form nzLayout="inline" class="ac-role-form">
        <nz-form-item>
          <nz-form-control>
            <input nz-input name="name" [(ngModel)]="draftName" placeholder="Name" />
          </nz-form-control>
        </nz-form-item>
        <nz-form-item>
          <nz-form-control>
            <input nz-input name="description" [(ngModel)]="draftDescription" placeholder="Description" />
          </nz-form-control>
        </nz-form-item>
      </form>
      <app-grant-composer [(grants)]="draftGrants" [held]="heldActions()" />
      <div class="ac-actions">
        <button nz-button nzType="primary" [disabled]="!draftName()" (click)="save()">
          {{ editing() ? 'Save role' : 'Create role' }}
        </button>
        @if (editing()) {
          <button nz-button (click)="cancel()">Cancel</button>
        }
      </div>
    </section>

    <section>
      <h2>Members</h2>
      <nz-table #mt [nzData]="members()" nzSize="small" [nzShowPagination]="false">
        <thead>
          <tr>
            <th>Person</th>
            <th>Role</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          @for (m of mt.data; track m.id) {
            <tr>
              <td>{{ m.displayName }} <code>{{ m.subject }}</code></td>
              <td>{{ m.roleName }}</td>
              <td class="row-actions">
                @if (removable(m)) {
                  <button nz-button nzSize="small" nzDanger (click)="removeMember(m)">Remove</button>
                } @else {
                  <!--
                    The last unrestricted member. Removing them would leave an account nobody can
                    administer, and no screen should be able to produce that by accident.
                  -->
                  <span class="ac-muted">last administrator</span>
                }
              </td>
            </tr>
          } @empty {
            <tr>
              <td colspan="3" class="ac-muted">
                No stored memberships. This deployment derives them from identity-provider groups.
              </td>
            </tr>
          }
        </tbody>
      </nz-table>

      <form nz-form nzLayout="inline" class="ac-add" (ngSubmit)="addMember()">
        <nz-form-item>
          <nz-form-control>
            <input nz-input name="subject" [(ngModel)]="memberSubject" placeholder="identity-provider subject" />
          </nz-form-control>
        </nz-form-item>
        <nz-form-item>
          <nz-form-control>
            <nz-select [(ngModel)]="memberRole" name="role" nzPlaceHolder="Role" style="min-width: 14rem">
              @for (r of roles(); track r.id) {
                <nz-option [nzValue]="r.id" [nzLabel]="r.name" />
              }
            </nz-select>
          </nz-form-control>
        </nz-form-item>
        <button nz-button nzType="dashed" [disabled]="!memberSubject() || !memberRole()">Add member</button>
      </form>
    </section>
  `,
  styles: [
    `
      section {
        margin-block-end: 2.5rem;
      }
      .ac-muted {
        opacity: 0.65;
      }
      .ac-mono {
        font-family: ui-monospace, monospace;
        font-size: 0.85em;
      }
      .ac-actions {
        margin-block-start: 0.75rem;
        display: flex;
        gap: 0.5rem;
      }
      .ac-add {
        margin-block-start: 0.75rem;
      }
    `,
  ],
})
export class RolesComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly message = inject(NzMessageService);
  private readonly session = inject(SessionService);

  protected readonly roles = signal<Role[]>([]);
  protected readonly members = signal<Member[]>([]);
  protected readonly error = signal<string | null>(null);

  protected readonly editing = signal<Role | null>(null);
  protected readonly draftName = signal('');
  protected readonly draftDescription = signal('');
  protected readonly draftGrants = signal<Grant[]>([]);

  protected readonly memberSubject = signal('');
  protected readonly memberRole = signal('');

  ngOnInit(): void {
    void this.load();
  }

  /**
   * What the acting user holds, which is what the composer may offer. Empty means everything:
   * a member of the unrestricted role holds every action in the account.
   */
  protected heldActions(): string[] {
    return this.session.me()?.permissions ?? [];
  }

  /**
   * Whether a membership may be removed. The last member of the unrestricted role may not: the
   * service refuses it under a row lock, and offering the button would be offering a refusal.
   */
  protected removable(m: Member): boolean {
    const role = this.roles().find((r) => r.id === m.roleId);
    if (!role?.unrestricted) return true;
    return this.members().filter((other) => other.roleId === role.id).length > 1;
  }

  private async load(): Promise<void> {
    this.error.set(null);
    try {
      const [roles, members] = await Promise.all([
        firstValueFrom(this.api.get<Role[]>('/roles')),
        firstValueFrom(this.api.get<Member[]>('/members')),
      ]);
      this.roles.set(roles);
      this.members.set(members);
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    }
  }

  protected edit(r: Role): void {
    this.editing.set(r);
    this.draftName.set(r.name);
    this.draftDescription.set(r.description);
    this.draftGrants.set([...r.grants]);
  }

  protected cancel(): void {
    this.editing.set(null);
    this.draftName.set('');
    this.draftDescription.set('');
    this.draftGrants.set([]);
  }

  protected async save(): Promise<void> {
    const body = {
      name: this.draftName(),
      description: this.draftDescription(),
      grants: this.draftGrants(),
    };
    try {
      const current = this.editing();
      if (current) {
        await firstValueFrom(this.api.put<Role>(`/roles/${current.id}`, body));
      } else {
        await firstValueFrom(this.api.post<Role>('/roles', body));
      }
      this.cancel();
      await this.load();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    }
  }

  protected async remove(r: Role): Promise<void> {
    try {
      await firstValueFrom(this.api.delete<void>(`/roles/${r.id}`));
      await this.load();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    }
  }

  protected async addMember(): Promise<void> {
    try {
      await firstValueFrom(
        this.api.post<void>('/members', { subject: this.memberSubject(), roleId: this.memberRole() }),
      );
      this.memberSubject.set('');
      await this.load();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    }
  }

  protected async removeMember(m: Member): Promise<void> {
    try {
      await firstValueFrom(this.api.delete<void>(`/members/${m.id}`));
      await this.load();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    }
  }
}
