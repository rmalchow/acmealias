import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
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
import { ApiService, SessionService, apiErrorMessage } from '@acme/shared';
import { NzModalService } from 'ng-zorro-antd/modal';
import { openRoleEditor } from './role-editor.modal';
import { DatePipe } from '@angular/common';
import { Invitation } from '../invitations/invitations.models';
import { InvitationsService } from '../invitations/invitations.service';
import { Member, Role } from './roles.models';

/**
 * Roles and membership: who may do what, in this account (tasks 9.2 and 9.4).
 *
 * A SECTION OF SETTINGS, not a screen of its own (task 6.5a) — hence no page header and no h1.
 * It renders only inside a team account: the routes below refuse in a personal one, where there
 * is nobody to give a role to (design C9), and the route guard on Settings is what keeps it out
 * of there rather than a condition in here.
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
    DatePipe,
    FormsModule,
    NzAlertModule,
    NzButtonModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzSelectModule,
    NzTableModule,
    NzTagModule,
  ],
  template: `
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

      <div class="ac-actions">
        <button nz-button nzType="primary" (click)="newRole()">
          <span nz-icon nzType="plus"></span>
          New role
        </button>
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
              <td>
                @if (m.roleId) {
                  {{ m.roleName }}
                } @else {
                  <!--
                    Somebody who accepted an invitation and is waiting. They are IN the account and
                    can do nothing in it, which is a state to act on rather than a blank cell — and
                    it is the only place in the console that says the second half of joining has
                    not happened yet.
                  -->
                  <span class="assign">
                    <nz-tag nzColor="blue">no role yet</nz-tag>
                    <nz-select
                      [ngModel]="pending()[m.id] ?? ''"
                      (ngModelChange)="setPending(m.id, $event)"
                      [name]="'assign-' + m.id"
                      nzPlaceHolder="Assign a role"
                      nzSize="small"
                      style="min-width: 12rem"
                    >
                      @for (r of roles(); track r.id) {
                        <nz-option [nzValue]="r.id" [nzLabel]="r.name" />
                      }
                    </nz-select>
                    <button nz-button nzSize="small" [disabled]="!pending()[m.id]" (click)="assign(m)">
                      Assign
                    </button>
                  </span>
                }
              </td>
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
                <!--
                  Membership is rows we wrote, and only that (design P18): the branch that derived
                  it from identity-provider groups is gone, so an empty roster is now genuinely an
                  account with one person in it rather than a deployment answering elsewhere.
                -->
                Nobody has been added yet. Add a member below.
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

    <section>
      <h2>Invitations</h2>
      <!--
        The form asks for an ADDRESS AND NOTHING ELSE. There is no role picker here on purpose:
        an invitation offers membership, and what the new person may do is decided once they are
        in the account and somebody has them in front of them — rather than a fortnight early,
        against a list of roles that may have changed by then.
      -->
      <p class="ac-muted lead">
        Invite somebody by email address. They join with no role, and you assign one above once
        they have accepted.
      </p>

      <nz-table #it [nzData]="invitations()" nzSize="small" [nzShowPagination]="false">
        <thead>
          <tr>
            <th>Address</th>
            <th>Invited by</th>
            <th>Last sent</th>
            <th>Expires</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          @for (i of it.data; track i.id) {
            <tr>
              <td>{{ i.email }}</td>
              <td>{{ i.invitedBy }}</td>
              <td>{{ i.lastSentAt | date: 'mediumDate' }}</td>
              <td>{{ i.expiresAt | date: 'mediumDate' }}</td>
              <td class="row-actions">
                @if (unrestricted()) {
                  <!--
                    Re-send is what somebody clicks when told "I never got it". It kills the
                    previous link, which matters: the message that was never got is exactly the one
                    that might have gone somewhere unintended.
                  -->
                  <button nz-button nzSize="small" (click)="resend(i)">Re-send</button>
                  <button nz-button nzSize="small" nzDanger (click)="revoke(i)">Revoke</button>
                }
              </td>
            </tr>
          } @empty {
            <tr>
              <td colspan="5" class="ac-muted">
                <!--
                  An invitation that has been accepted, declined or revoked is not here — it has
                  been ANSWERED, and the member list above is where an acceptance shows up. What
                  this list means is "waiting on somebody".
                -->
                Nobody has been invited and not yet answered.
              </td>
            </tr>
          }
        </tbody>
      </nz-table>

      @if (unrestricted()) {
        <form nz-form nzLayout="inline" class="ac-add" (ngSubmit)="invite()">
          <nz-form-item>
            <nz-form-control>
              <input
                nz-input
                type="email"
                name="inviteEmail"
                [(ngModel)]="inviteEmail"
                placeholder="name@company.example"
                style="min-width: 18rem"
              />
            </nz-form-control>
          </nz-form-item>
          <button nz-button nzType="primary" [disabled]="!inviteEmail() || inviting()">Send invitation</button>
        </form>
      }
    </section>
  `,
  styles: [
    `
      section {
        margin-block-end: 2.5rem;
      }
      section:last-of-type {
        margin-block-end: 0;
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
      /* Two buttons touching read as one control, and one of them deletes. */
      .row-actions {
        white-space: nowrap;
        display: flex;
        gap: 0.5rem;
        justify-content: flex-end;
      }
      .ac-add {
        margin-block-start: 0.75rem;
      }
      .lead {
        margin-block-end: 0.75rem;
      }
      /* The tag, the picker and the button read as one control for one decision. */
      .assign {
        display: inline-flex;
        align-items: center;
        gap: 0.5rem;
      }
    `,
  ],
})
export class RolesComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly message = inject(NzMessageService);
  private readonly session = inject(SessionService);
  private readonly modal = inject(NzModalService);

  protected readonly roles = signal<Role[]>([]);
  protected readonly members = signal<Member[]>([]);
  protected readonly error = signal<string | null>(null);


  protected readonly memberSubject = signal('');
  protected readonly memberRole = signal('');

  private readonly invites = inject(InvitationsService);
  protected readonly invitations = signal<Invitation[]>([]);
  protected readonly inviteEmail = signal('');
  protected readonly inviting = signal(false);

  /**
   * Whether the person looking at this may issue, revoke and re-send — which is membership of the
   * account's unrestricted role, the owner and the administrators.
   *
   * Hiding the controls is a courtesy and NOT the enforcement. The service refuses the same
   * requests from the same people whatever this returns; what this prevents is offering somebody
   * a button that will answer 403.
   */
  protected readonly unrestricted = computed(() => {
    const held = this.session.me()?.roles ?? [];
    return this.roles().some((r) => r.unrestricted && held.includes(r.name));
  });

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
    // A membership holding no role can always go. It cannot be the last member of the unrestricted
    // role, since it is a member of no role at all, and somebody invited by mistake has to be
    // removable before anybody gets round to giving them one.
    if (!m.roleId) return true;
    const role = this.roles().find((r) => r.id === m.roleId);
    if (!role?.unrestricted) return true;
    return this.members().filter((other) => other.roleId === role.id).length > 1;
  }

  /** The role picked, but not yet submitted, for each member who holds none. */
  protected readonly pending = signal<Record<string, string | undefined>>({});

  protected setPending(memberId: string, roleId: string): void {
    this.pending.update((p) => ({ ...p, [memberId]: roleId }));
  }

  /**
   * Gives a role to somebody who holds none — the second half of accepting an invitation.
   *
   * A PUT on the membership rather than a POST to /members, because they joined already: adding
   * them again would claim they joined at this moment, which the roster and the audit trail both
   * contradict.
   */
  protected async assign(m: Member): Promise<void> {
    const roleId = this.pending()[m.id];
    if (!roleId) return;
    try {
      await firstValueFrom(this.api.put<void>(`/members/${m.id}/role`, { roleId }));
      this.pending.update((p) => {
        const next = { ...p };
        delete next[m.id];
        return next;
      });
      await this.load();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    }
  }

  private async load(): Promise<void> {
    this.error.set(null);
    try {
      const [roles, members, invitations] = await Promise.all([
        firstValueFrom(this.api.get<Role[]>('/roles')),
        firstValueFrom(this.api.get<Member[]>('/members')),
        this.invites.outstanding(),
      ]);
      this.roles.set(roles);
      this.members.set(members);
      this.invitations.set(invitations);
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    }
  }


  /** Opens the editor for a new role. */
  protected newRole(): void {
    this.open(null);
  }

  /** Opens the editor for an existing one. */
  protected edit(r: Role): void {
    this.open(r);
  }

  private open(role: Role | null): void {
    openRoleEditor(this.modal, { role, held: this.heldActions() })
      .afterClose.subscribe((written?: boolean) => {
        if (written) void this.load();
      });
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

  protected async invite(): Promise<void> {
    this.inviting.set(true);
    try {
      await this.invites.invite(this.inviteEmail());
      this.inviteEmail.set('');
      this.message.success('Invitation sent.');
      await this.load();
    } catch (err) {
      // Shown verbatim, including the rate-limit refusal, which names the limit and when to retry.
      // "You are doing that too much" without a time is an instruction to retry in a loop.
      this.message.error(apiErrorMessage(err));
    } finally {
      this.inviting.set(false);
    }
  }

  protected async resend(i: Invitation): Promise<void> {
    try {
      await this.invites.resend(i.id);
      this.message.success(`Sent again to ${i.email}.`);
      await this.load();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    }
  }

  protected async revoke(i: Invitation): Promise<void> {
    try {
      await this.invites.revoke(i.id);
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
