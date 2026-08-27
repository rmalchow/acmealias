import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { firstValueFrom } from 'rxjs';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { GrantComposerComponent } from './grant-composer.component';
import { Grant, Role } from './roles.models';

interface RoleEditorData {
  /** The role being edited, or null to create one. */
  role: Role | null;
  /** The actions the acting user holds, so the composer offers no more than that. */
  held: string[];
}

/**
 * Creating and editing a role, in a dialog with room to work in.
 *
 * It is a modal rather than a form under the table because composing grants is not filling in two
 * fields — it is deciding what a set of people will be able to do, one row at a time, against a
 * list of what you hold yourself. That deserves the width and the undivided attention, and it keeps
 * the table above readable while you do it.
 *
 * Grants are replaced wholesale on save, which is the contract the screen shows: "these are the
 * grants", not "these are some more grants". A merge would need a stable identity per grant and an
 * intent for the rows the caller did not mention, and neither is obvious enough to guess.
 */
@Component({
  selector: 'app-role-editor',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, NzAlertModule, NzButtonModule, NzFormModule, NzInputModule, GrantComposerComponent],
  template: `
    <form nz-form nzLayout="vertical">
      <nz-form-item>
        <nz-form-label nzRequired>Name</nz-form-label>
        <nz-form-control nzExtra="Unique within this account. It appears in the audit trail as written.">
          <input nz-input name="name" [(ngModel)]="name" placeholder="operators" />
        </nz-form-control>
      </nz-form-item>
      <nz-form-item>
        <nz-form-label>Description</nz-form-label>
        <nz-form-control nzExtra="What this role is for, in the words the person granting it would use.">
          <input nz-input name="description" [(ngModel)]="description" placeholder="Manage inventory in the staging project" />
        </nz-form-control>
      </nz-form-item>
    </form>

    <h4>Grants</h4>
    <p class="ac-lead">
      Each row is one permission: what it applies to, which object, and what may be done. A grant that
      reaches contained objects covers everything beneath the one it names — a project grant reaching
      that project's load balancers, nodes and domains.
    </p>
    <p class="ac-lead">
      You can only grant what you hold yourself, and only from a single grant of your own: holding two
      load balancers separately does not let you grant the project containing them, because tomorrow
      that project may contain a third.
    </p>

    <app-grant-composer [(grants)]="grants" [held]="data.held" />

    @if (error(); as e) {
      <nz-alert nzType="error" [nzMessage]="e" class="ac-error" />
    }

    <div *nzModalFooter>
      <button nz-button (click)="close()">Cancel</button>
      <button nz-button nzType="primary" [nzLoading]="saving()" [disabled]="!name()" (click)="save()">
        {{ data.role ? 'Save role' : 'Create role' }}
      </button>
    </div>
  `,
  styles: [
    `
      .ac-lead {
        opacity: 0.75;
        margin-block-end: 0.5rem;
      }
      .ac-error {
        margin-block-start: 1rem;
      }
      h4 {
        margin-block-start: 1.5rem;
      }
    `,
  ],
})
export class RoleEditorModal {
  protected readonly data = inject<RoleEditorData>(NZ_MODAL_DATA);
  private readonly ref = inject(NzModalRef<RoleEditorModal, boolean>);
  private readonly api = inject(ApiService);

  protected readonly name = signal(this.data.role?.name ?? '');
  protected readonly description = signal(this.data.role?.description ?? '');
  protected readonly grants = signal<Grant[]>([...(this.data.role?.grants ?? [])]);
  protected readonly saving = signal(false);
  protected readonly error = signal<string | null>(null);

  protected readonly editing = computed(() => this.data.role !== null);

  protected close(): void {
    this.ref.close(false);
  }

  protected async save(): Promise<void> {
    this.saving.set(true);
    this.error.set(null);
    const body = { name: this.name(), description: this.description(), grants: this.grants() };
    try {
      if (this.data.role) {
        await firstValueFrom(this.api.put<Role>(`/roles/${this.data.role.id}`, body));
      } else {
        await firstValueFrom(this.api.post<Role>('/roles', body));
      }
      this.ref.close(true);
    } catch (err) {
      // The dialog stays open on a refusal: the message names the grant that was
      // not covered, and closing would take the composed grants away with it.
      this.error.set(apiErrorMessage(err));
    } finally {
      this.saving.set(false);
    }
  }
}

/** Opens the editor. Resolves true when a role was written. */
export function openRoleEditor(
  modal: NzModalService,
  data: RoleEditorData,
): NzModalRef<RoleEditorModal, boolean | undefined> {
  return modal.create<RoleEditorModal, RoleEditorData>({
    nzTitle: data.role ? `Edit ${data.role.name}` : 'New role',
    nzContent: RoleEditorModal,
    nzData: data,
    // Wide, because a grant is five fields across and reading a list of them
    // wrapped over two lines each is how a permission gets misread.
    nzWidth: '56rem',
    nzMaskClosable: false,
  }) as NzModalRef<RoleEditorModal, boolean | undefined>;
}
