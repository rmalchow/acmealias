import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiService, apiErrorMessage } from '../core/api.service';

export interface NameSlugData {
  /** Where to POST `{slug, name}`. */
  path: string;
  slugHint?: string;
}

/**
 * "New project", "New load balancer" — anything created from a slug and a display name.
 *
 * One modal rather than one per object type: the two fields, the slug pattern and the
 * error-stays-on-the-form behaviour are identical, and a second copy would only be a second
 * place for the pattern to drift.
 *
 * The POST happens HERE, not in the list behind it. A slug collision is the failure that
 * actually happens, and it has to land next to the field the operator must change, with
 * everything they typed still in place. Closing first and reporting the error on the page behind
 * would throw the form away at the exact moment it is needed.
 */
@Component({
  selector: 'app-name-slug-modal',
  imports: [FormsModule, NzButtonModule, NzFormModule, NzInputModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      <nz-form-item>
        <nz-form-label nzRequired>Slug</nz-form-label>
        <nz-form-control [nzExtra]="data.slugHint ?? 'Lower case, digits and hyphens.'">
          <input nz-input name="slug" [(ngModel)]="slug" required pattern="[a-z0-9-]+" autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label nzRequired>Name</nz-form-label>
        <nz-form-control>
          <input nz-input name="name" [(ngModel)]="name" required autocomplete="off" />
        </nz-form-control>
      </nz-form-item>

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <!-- Submit lives in the form so Enter works; the modal's own footer is switched off. -->
      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button nz-button nzType="primary" type="submit" [nzLoading]="saving()" [disabled]="!slug || !name">
          Create
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
export class NameSlugModalComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<NameSlugModalComponent, unknown>);
  readonly data = inject<NameSlugData>(NZ_MODAL_DATA);

  readonly saving = signal(false);
  readonly error = signal<string | null>(null);
  slug = '';
  name = '';

  save(): void {
    if (!this.slug || !this.name || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    this.api.post<unknown>(this.data.path, { slug: this.slug, name: this.name }).subscribe({
      next: (created) => this.ref.close(created),
      error: (err) => {
        this.saving.set(false);
        this.error.set(apiErrorMessage(err));
      },
    });
  }
}

/**
 * Opens the modal. `afterClose` emits the created object, or undefined if the operator cancelled.
 *
 * The generic is on the RETURN, not on `create`: ng-zorro's second type parameter is the type of
 * `nzData`, and the created object only ever comes back through `afterClose`.
 */
export function openNameSlugModal<T>(
  modal: NzModalService,
  title: string,
  data: NameSlugData,
): NzModalRef<NameSlugModalComponent, T | undefined> {
  return modal.create<NameSlugModalComponent, NameSlugData>({
    nzTitle: title,
    nzContent: NameSlugModalComponent,
    nzData: data,
    nzFooter: null,
    nzWidth: 420,
  }) as NzModalRef<NameSlugModalComponent, T | undefined>;
}
