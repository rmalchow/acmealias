import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiService, DomainCreateRequest, DomainCreated, WILDCARD_NOTES, apiErrorMessage, counterpartDomain, domainNameProblem, normalizeDomain } from '@acme/shared';
export interface DomainCreateData {
  projectId: string;
}

/**
 * 19.3 — the domain editor, as a modal.
 *
 * Client-side validation (`domainNameProblem`) mirrors the server's syntax rules and gives
 * as-you-type feedback; the server remains the authority (spec "Server remains the authority")
 * for the one rule the client cannot check — a wildcard sitting at a public suffix such as
 * `*.co.uk` — and for the "already covered by a wildcard" warning. Both are surfaced verbatim.
 *
 * The warnings are why this modal does not close on success: a domain can be created AND come
 * back with something the operator needs to read. Closing on 201 would throw that away, so the
 * dialog switches to a result state and the operator dismisses it themselves.
 */
@Component({
  selector: 'app-domain-editor',
  imports: [
    FormsModule,
    NzAlertModule,
    NzButtonModule,
    NzCheckboxModule,
    NzFormModule,
    NzInputModule,
  ],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="submit()">
      <nz-form-item>
        <nz-form-label nzRequired>Domain name</nz-form-label>
        <nz-form-control
          [nzValidateStatus]="clientProblem() ? 'error' : 'success'"
          [nzErrorTip]="clientProblem() ?? ''"
        >
          <input
            nz-input
            name="name"
            [(ngModel)]="name"
            placeholder="shop.example.com or *.example.com"
            autocomplete="off"
          />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-control>
          <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
               nz-checkbox renders its own <input> inside this label and owns the association; the
               rule cannot see through the directive. -->
          <label nz-checkbox name="includeWildcard" [(ngModel)]="includeWildcard">
            {{ counterpartLabel() }}
          </label>
          <p class="hint">
            A wildcard covers every host exactly one label under the domain, and needs its own
            address records in the DNS plan the customer publishes.
          </p>
        </nz-form-control>
      </nz-form-item>

      <ul class="notes">
        @for (note of wildcardNotes; track note) {
          <li>{{ note }}</li>
        }
      </ul>

      @if (serverError(); as msg) {
        <nz-alert nzType="error" [nzMessage]="msg" nzShowIcon />
      }

      @if (warnings().length) {
        <nz-alert nzType="warning" nzShowIcon [nzMessage]="warningList" />
        <ng-template #warningList>
          <ul class="warnings">
            @for (w of warnings(); track w) {
              <li>{{ w }}</li>
            }
          </ul>
        </ng-template>
      }

      <div class="actions">
        @if (done()) {
          <button nz-button nzType="primary" type="button" (click)="ref.close(true)">Close</button>
        } @else {
          <button nz-button type="button" (click)="ref.close(false)">Cancel</button>
          <button
            nz-button
            nzType="primary"
            type="submit"
            [nzLoading]="submitting()"
            [disabled]="!name.trim() || !!clientProblem()"
          >
            Add domain
          </button>
        }
      </div>
    </form>
  `,
  styles: [
    `
      .hint {
        margin: 0.25rem 0 0;
        font-size: 0.8125rem;
        color: rgb(255 255 255 / 45%);
      }
      .notes {
        margin: 0 0 1rem;
        padding-left: 1.1rem;
        font-size: 0.8125rem;
        color: rgb(255 255 255 / 45%);
      }
      .warnings {
        margin: 0;
        padding-left: 1.1rem;
      }
      .actions {
        display: flex;
        justify-content: flex-end;
        gap: 0.5rem;
        margin-top: 1.5rem;
      }
      nz-alert {
        display: block;
        margin-bottom: 0.75rem;
      }
    `,
  ],
})
export class DomainEditorComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<DomainEditorComponent, boolean>);
  readonly data = inject<DomainCreateData>(NZ_MODAL_DATA);

  readonly wildcardNotes = WILDCARD_NOTES;
  readonly submitting = signal(false);
  readonly serverError = signal<string | null>(null);
  readonly warnings = signal<string[]>([]);
  /** True once the domain exists — the form is then a result to read, not a form to fill. */
  readonly done = signal(false);

  name = '';
  /**
   * Off every time the dialog opens, and never remembered between openings (design B4): the
   * counterpart carries a wildcard address record over the customer's whole zone and a
   * certificate covering every host beneath the domain, and a pre-ticked box is not evidence
   * that anyone decided that.
   */
  includeWildcard = false;

  /** Recomputed on every render off the live value — cheap, and never stale. */
  clientProblem(): string | null {
    return this.name.trim() ? domainNameProblem(this.name) : null;
  }

  /**
   * The label names the exact name the box would add, so the operator never has to work the
   * option's effect out. Before anything is typed it states the rule instead of naming `*.`.
   */
  counterpartLabel(): string {
    const typed = normalizeDomain(this.name);
    if (!typed) return 'Also add the counterpart of this name';
    return typed.startsWith('*.')
      ? `Also add the apex ${counterpartDomain(typed)}`
      : `Also add the wildcard ${counterpartDomain(typed)}`;
  }

  submit(): void {
    if (this.done() || this.submitting()) return;
    const problem = this.clientProblem();
    if (problem || !this.name.trim()) return;
    this.submitting.set(true);
    this.serverError.set(null);
    this.warnings.set([]);
    // Only the name and, when it is asked for, the flag: the service assigns the alias label,
    // and a request carrying anything else is refused as an unknown field.
    const body: DomainCreateRequest = { name: this.name.trim() };
    if (this.includeWildcard) body.includeWildcard = true;
    this.api
      .post<DomainCreated>(`/projects/${this.data.projectId}/domains`, body)
      .subscribe({
        next: (d) => {
          this.submitting.set(false);
          this.warnings.set(d.warnings ?? []);
          this.done.set(true);
          // Nothing to read? Then there is nothing to keep the dialog open for.
          if (!this.warnings().length) this.ref.close(true);
        },
        error: (err) => {
          this.submitting.set(false);
          this.serverError.set(apiErrorMessage(err));
        },
      });
  }
}

/** Opens the editor; `afterClose` emits true when a domain was created. */
export function openDomainEditor(modal: NzModalService, projectId: string) {
  return modal.create<DomainEditorComponent, DomainCreateData>({
    nzTitle: 'Add domain',
    nzContent: DomainEditorComponent,
    nzData: { projectId },
    nzFooter: null,
    nzWidth: 520,
  }) as NzModalRef<DomainEditorComponent, boolean | undefined>;
}
