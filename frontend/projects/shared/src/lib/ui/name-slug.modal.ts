import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { NzButtonModule } from "ng-zorro-antd/button";
import { NzFormModule } from "ng-zorro-antd/form";
import { NzInputModule } from "ng-zorro-antd/input";
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from "ng-zorro-antd/modal";
import { ApiService, apiErrorMessage } from "../core/api.service";

export interface NameSlugData {
  /** Where to POST `{slug, name}`. */
  path: string;
  slugHint?: string;
}

/**
 * The name an operator typed, as a slug: lower case, digits and hyphens, no leading or trailing
 * hyphen. Anything else — spaces, punctuation, accented letters, emoji — becomes a hyphen and runs
 * of them collapse, so "ACME Shop (EU)" gives "acme-shop-eu".
 *
 * Deliberately not transliterating: "münchen" gives "m-nchen", which is visibly wrong and invites
 * the operator to fix the field, where a silent "munchen" would look right enough to ship.
 */
export function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/**
 * "New project", "New load balancer" — anything created from a slug and a display name.
 *
 * One modal rather than one per object type: the two fields, the slug pattern and the
 * error-stays-on-the-form behaviour are identical, and a second copy would only be a second
 * place for the pattern to drift.
 *
 * Name comes first and the slug follows it as you type, because that is the order the operator
 * thinks in: they know what the thing is called, not what it should be called in a URL. The slug
 * stays an ordinary editable field — deriving it is a default, not a constraint — and it stops
 * following the name the moment anybody touches it.
 *
 * The POST happens HERE, not in the list behind it. A slug collision is the failure that
 * actually happens, and it has to land next to the field the operator must change, with
 * everything they typed still in place. Closing first and reporting the error on the page behind
 * would throw the form away at the exact moment it is needed.
 */
@Component({
  selector: "app-name-slug-modal",
  imports: [FormsModule, NzButtonModule, NzFormModule, NzInputModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="save()">
      <nz-form-item>
        <nz-form-label nzRequired>Name</nz-form-label>
        <nz-form-control>
          <input
            nz-input
            name="name"
            [(ngModel)]="name"
            (ngModelChange)="onNameChange($event)"
            required
            autocomplete="off"
          />
        </nz-form-control>
      </nz-form-item>

      <nz-form-item>
        <nz-form-label nzRequired>Slug</nz-form-label>
        <nz-form-control
          [nzExtra]="data.slugHint ?? 'Lower case, digits and hyphens.'"
        >
          <input
            nz-input
            name="slug"
            [(ngModel)]="slug"
            (ngModelChange)="onSlugChange($event)"
            required
            pattern="[a-z0-9-]+"
            autocomplete="off"
          />
        </nz-form-control>
      </nz-form-item>

      @if (error(); as msg) {
        <p class="ac-error">{{ msg }}</p>
      }

      <!-- Submit lives in the form so Enter works; the modal's own footer is switched off. -->
      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button
          nz-button
          nzType="primary"
          type="submit"
          [nzLoading]="saving()"
          [disabled]="!slug || !name"
        >
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
  name = "";
  slug = "";

  /**
   * True once the operator has typed in the slug field themselves. From then on the name stops
   * driving the slug: the one thing worse than not deriving a slug is overwriting the one somebody
   * deliberately chose. Emptying the field hands control back — the next keystroke in the name
   * starts deriving again — so a slug typed by mistake does not need the dialog reopened.
   */
  private slugEdited = false;

  onNameChange(value: string): void {
    if (!this.slugEdited) this.slug = slugify(value);
  }

  /**
   * Note what this does NOT do on an empty value: refill the field from the name. Deleting the
   * last character would then put the whole slug straight back, and the field could never be
   * cleared. It stays empty until the name is typed in again.
   */
  onSlugChange(value: string): void {
    this.slugEdited = value !== "";
  }

  save(): void {
    if (!this.slug || !this.name || this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    this.api
      .post<unknown>(this.data.path, { slug: this.slug, name: this.name })
      .subscribe({
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
