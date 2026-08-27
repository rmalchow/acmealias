import { ChangeDetectionStrategy, Component, computed, input, model } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSelectModule } from 'ng-zorro-antd/select';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { ACTIONS, ANY, Grant, OBJECT_TYPES } from './roles.models';

/**
 * Composes the grants a role or a key holds (task 9.3).
 *
 * It offers only what the acting user holds. That is a convenience, not a control: the service
 * refuses a grant the writer does not hold whatever this renders, one grant at a time against a
 * single held grant. Offering less than the service would accept is safe; offering more would
 * produce a form that submits and is refused, which teaches people to distrust the form.
 *
 * The coverage test the service applies is deliberately an under-approximation — it will not
 * assemble two held grants to cover one proposed grant — so this mirrors that by comparing
 * against each held grant separately rather than against their union.
 */
@Component({
  selector: 'app-grant-composer',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
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
    <nz-table #t [nzData]="grants()" nzSize="small" [nzShowPagination]="false">
      <thead>
        <tr>
          <th>Applies to</th>
          <th>Object</th>
          <th>Action</th>
          <th>Reaches contained objects</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        @for (g of t.data; track $index) {
          <tr>
            <td>{{ label(g.type) }}</td>
            <td class="ac-mono">{{ g.objectId || 'anything of that type' }}</td>
            <td>{{ actionLabel(g.action) }}</td>
            <td>{{ g.inheritable ? 'yes' : 'no' }}</td>
            <td>
              <button nz-button nzSize="small" nzDanger (click)="remove($index)">Remove</button>
            </td>
          </tr>
        } @empty {
          <tr>
            <td colspan="5" class="ac-empty">
              No grants. A role with none confers nothing, which is a valid thing to save — it is how a
              role is parked without deleting it.
            </td>
          </tr>
        }
      </tbody>
    </nz-table>

    <form nz-form nzLayout="inline" class="ac-add" (ngSubmit)="add()">
      <nz-form-item>
        <nz-form-control>
          <nz-select [(ngModel)]="draftType" name="type" nzPlaceHolder="Applies to" style="min-width: 14rem">
            @for (t of types; track t.value) {
              <nz-option [nzValue]="t.value" [nzLabel]="t.label" />
            }
          </nz-select>
        </nz-form-control>
      </nz-form-item>
      <nz-form-item>
        <nz-form-control>
          <input nz-input name="objectId" [(ngModel)]="draftObjectId" placeholder="object id (blank = any)" />
        </nz-form-control>
      </nz-form-item>
      <nz-form-item>
        <nz-form-control>
          <nz-select [(ngModel)]="draftAction" name="action" nzPlaceHolder="Action" style="min-width: 18rem">
            @for (a of offeredActions(); track a.value) {
              <nz-option [nzValue]="a.value" [nzLabel]="a.label" />
            }
          </nz-select>
        </nz-form-control>
      </nz-form-item>
      <nz-form-item>
        <nz-form-control>
          <label>
            <input type="checkbox" name="inheritable" [(ngModel)]="draftInheritable" />
            reaches contained objects
          </label>
        </nz-form-control>
      </nz-form-item>
      <button nz-button nzType="dashed" [disabled]="!draftType() || !draftAction()">
        <span nz-icon nzType="plus"></span>
        Add grant
      </button>
    </form>
  `,
  styles: [
    `
      .ac-add {
        margin-block-start: 0.75rem;
      }
      .ac-empty {
        opacity: 0.7;
      }
      .ac-mono {
        font-family: ui-monospace, monospace;
        font-size: 0.85em;
      }
    `,
  ],
})
export class GrantComposerComponent {
  /** The grants being composed. Two-way, so the parent saves what is on screen. */
  readonly grants = model.required<Grant[]>();

  /**
   * What the acting user holds, so the composer offers no more than that. Empty means "offer
   * everything" — which is right for an unrestricted writer, who holds everything in the account.
   */
  readonly held = input<string[]>([]);

  protected readonly types = OBJECT_TYPES;

  protected readonly draftType = model<string>(ANY);
  protected readonly draftObjectId = model<string>('');
  protected readonly draftAction = model<string>('');
  protected readonly draftInheritable = model<boolean>(true);

  /**
   * The actions on offer: those the writer holds, or all of them for a writer who holds
   * everything. A writer holding a wildcard action is offered the wildcard.
   */
  protected readonly offeredActions = computed(() => {
    const held = this.held();
    if (held.length === 0 || held.includes(ANY)) return ACTIONS;
    return ACTIONS.filter((a) => a.value === ANY || held.includes(a.value));
  });

  protected label(type: string): string {
    return OBJECT_TYPES.find((t) => t.value === type)?.label ?? type;
  }

  protected actionLabel(action: string): string {
    return ACTIONS.find((a) => a.value === action)?.label ?? action;
  }

  protected add(): void {
    const id = this.draftObjectId().trim();
    this.grants.update((gs) => [
      ...gs,
      {
        type: this.draftType(),
        objectId: id === '' ? null : id,
        action: this.draftAction(),
        inheritable: this.draftInheritable(),
      },
    ]);
    this.draftObjectId.set('');
    this.draftAction.set('');
  }

  protected remove(i: number): void {
    this.grants.update((gs) => gs.filter((_, n) => n !== i));
  }
}
