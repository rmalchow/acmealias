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
import { apiErrorMessage, ApiService, confirmAction, SessionService } from '@acme/shared';
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
              <!--
                Name in bold, address in brackets. The identity-provider SUBJECT used to be here
                and is gone: it is an opaque string nobody recognises, it was the widest thing in
                the row, and the value an administrator actually matches a person against is the
                address they sent the invitation to. The subject is still on the wire for anything
                that needs to identify a row precisely.

                The brackets are conditional. A member added by subject from the command line has
                no address until they first sign in, and "Kim ()" reads as data that failed to
                load.
              -->
              <td>
                <strong>{{ m.displayName }}</strong>
                @if (m.email) {
                  <span class="ac-muted">({{ m.email }})</span>
                }
              </td>
              <td>
                <!--
                  ONE CONTROL FOR BOTH STATES. It used to be a label for a member who held a role
                  and a select for one who did not, because assigning was the last step of an
                  invitation and nothing else — so moving somebody from read-only to operator meant
                  removing them from the account and adding them back, which loses the membership
                  and is impossible outright for the last administrator.

                  The "no role yet" tag stays beside it, because that state is worth naming: such a
                  member is IN the account and authorised for nothing in it, which is the only place
                  the console says the second half of joining has not happened.
                -->
                <span class="assign">
                  @if (!m.roleId) {
                    <nz-tag nzColor="blue">no role yet</nz-tag>
                  }
                  <nz-select
                    [ngModel]="pending()[m.id] ?? m.roleId ?? ''"
                    (ngModelChange)="setPending(m.id, $event)"
                    [name]="'assign-' + m.id"
                    [nzDisabled]="isSelf(m)"
                    nzPlaceHolder="Assign a role"
                    nzSize="small"
                    style="min-width: 12rem"
                  >
                    @for (r of roles(); track r.id) {
                      <nz-option [nzValue]="r.id" [nzLabel]="r.name" />
                    }
                  </nz-select>
                  @if (isSelf(m)) {
                    <!--
                      Disabled with a reason rather than hidden. The authority to change roles is
                      the unrestricted role itself, so an administrator editing their own
                      membership is using a power to alter the source of that power — and the
                      mundane version, picking the wrong row in a list of colleagues and locking
                      yourself out, is the one that actually happens. The service refuses it too.
                    -->
                    <span class="ac-muted" title="Another administrator of this account can change it.">
                      your own role
                    </span>
                  } @else {
                    <button
                      nz-button
                      nzSize="small"
                      [disabled]="!changed(m)"
                      (click)="assign(m)"
                    >
                      {{ m.roleId ? 'Change' : 'Assign' }}
                    </button>
                  }
                </span>
              </td>
              <td class="row-actions">
                @if (removable(m)) {
                  <button nz-button nzSize="small" nzDanger (click)="removeMember(m)">Remove</button>
                } @else {
                  <!--
                    Two different refusals, said differently on purpose: they send somebody to two
                    different actions. One wants a second administrator appointed; the other wants
                    the account transferred, and appointing administrators will never help with it.
                  -->
                  <span class="ac-muted" [title]="whyNotRemovable(m).why">
                    {{ whyNotRemovable(m).label }}
                  </span>
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

      <!--
        NO TABLE WHEN THERE IS NOTHING IN IT. An empty table is five column headings and a row of
        apology — furniture explaining its own emptiness — and this list is empty in most accounts
        most of the time, because an invitation lives here only while somebody has not answered it.
        One line says the same thing and reads as a state rather than as a failed load.

        An invitation that has been accepted, declined or revoked is not here: it has been ANSWERED,
        and an acceptance shows up in the member list above.
      -->
      @if (invitations().length) {
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
                  <!--
                    ANY ADMINISTRATOR, not only whoever sent it. An invitation is the account's
                    business rather than the inviter's — the person who needs it cancelled is
                    usually whoever noticed the address was wrong — and the service agrees:
                    revokeInvitation authorises on the unrestricted role and never consults
                    invited_by.

                    Re-send is what somebody clicks when told "I never got it". It kills the
                    previous link, which matters: the message that was never got is exactly the one
                    that might have gone somewhere unintended.
                  -->
                  @if (unrestricted()) {
                    <button nz-button nzSize="small" (click)="resend(i)">Re-send</button>
                    <button nz-button nzSize="small" nzDanger (click)="revoke(i)">Cancel</button>
                  }
                </td>
              </tr>
            }
          </tbody>
        </nz-table>
      } @else {
        <p class="ac-muted">No pending invitations.</p>
      }

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
      /* A <td>, kept a table cell.
       *
       * This was 'display: flex', which takes the element OUT of table layout: a cell that is not
       * display:table-cell stops participating in its row's column grid, so the buttons drifted
       * outside the table's right border and sat at the top of rows whose other cells were tall.
       * The markup was never wrong — four headers, four cells — which is why it looked like a
       * malformed row.
       *
       * The contents are laid out instead of the cell: text-align for the alignment, an adjacent
       * sibling margin for the gap, and vertical-align because the grants column runs to seven
       * lines and baseline-aligned buttons against it read as belonging to the row above. */
      .row-actions {
        white-space: nowrap;
        text-align: right;
        vertical-align: middle;
      }
      .row-actions button + button {
        margin-inline-start: 0.5rem;
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
    // The owner never leaves the unrestricted role: they are billed for the account and would
    // otherwise be paying for something they could no longer administer, invite into, or read the
    // roster of. The way out is a transfer, not a removal.
    if (m.owner) return false;
    return this.members().filter((other) => other.roleId === role.id).length > 1;
  }

  /**
   * Why a membership cannot be removed, in the caller's terms.
   *
   * The two reasons are kept apart because their remedies are unrelated: one is fixed by appointing
   * a second administrator, the other only by transferring the account. Collapsing them into "this
   * member cannot be removed" would send half the readers to do something that cannot work.
   *
   * The owner comes first because when the owner is also the only administrator both hold, and
   * appointing a second one still would not release the removal.
   */
  protected whyNotRemovable(m: Member): { label: string; why: string } {
    if (m.owner) {
      return {
        label: 'account owner',
        why:
          'This account is billed to them, so they always administer it. ' +
          'Transfer the account to somebody else first.',
      };
    }
    return {
      label: 'last administrator',
      why: 'An account must keep at least one administrator. Appoint another one first.',
    };
  }

  /** The role picked, but not yet submitted, for each member who holds none. */
  protected readonly pending = signal<Record<string, string | undefined>>({});

  protected setPending(memberId: string, roleId: string): void {
    this.pending.update((p) => ({ ...p, [memberId]: roleId }));
  }

  /**
   * Whether this row is the person reading it.
   *
   * Compared on SUBJECT, which is the only identifier /me and a member row share — /me answers who
   * you are to the identity provider, and the roster carries the same string per member. Comparing
   * membership ids would not work at all here, and comparing user ids would need a second read.
   *
   * Memberships are per-role, so one person can hold several rows in one account; every one of
   * them is theirs, and each is disabled.
   */
  protected isSelf(m: Member): boolean {
    const me = this.session.me()?.subject;
    return !!me && m.subject === me;
  }

  /**
   * Whether the select has moved off what the member actually holds.
   *
   * This is what keeps the button from offering to make a change that is not one. The select is
   * seeded from `m.roleId`, so with no pending edit the two are equal and the button is disabled;
   * a member with no role has nothing to be equal to, so any choice enables it.
   */
  protected changed(m: Member): boolean {
    const chosen = this.pending()[m.id];
    if (!chosen) return false;
    return chosen !== m.roleId;
  }

  /**
   * Points a member at a role — the second half of accepting an invitation, and every change
   * after it.
   *
   * A PUT on the membership rather than a POST to /members, because they joined already: adding
   * them again would claim they joined at this moment, which the roster and the audit trail both
   * contradict. The service refuses a change to one's own membership, and the button for that row
   * is disabled rather than absent, so this method is not the only thing standing between an
   * administrator and their own authority.
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

  /**
   * Cancels a pending invitation. Any administrator may, not only whoever sent it.
   *
   * Confirmed, unlike Re-send beside it, because it is the destructive one: the link in the
   * message stops working, so a person who is midway through accepting is stopped without being
   * told, and getting them back in means inviting them again. Named `revoke` after the API route
   * and labelled Cancel in the table, which is the word for what it does to something still
   * waiting for an answer.
   */
  protected async revoke(i: Invitation): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Cancel the invitation to ${i.email}?`,
      message: 'The link already sent stops working. Inviting them again sends a new one.',
      okText: 'Cancel invitation',
      danger: true,
    });
    if (!ok) return;
    try {
      await this.invites.revoke(i.id);
      this.message.success(`Invitation to ${i.email} cancelled.`);
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
