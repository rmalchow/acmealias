import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiKeyCreated } from '../models';

/**
 * The once-only reveal of a freshly minted secret.
 *
 * The one hard rule of this screen (spec "Secret is acknowledged before dismissal"): the secret
 * is shown exactly once and the dialog cannot be dismissed until the operator explicitly says
 * they have stored it. There is no recovery path — the service keeps only a hash — so that
 * checkbox is the only thing standing between "shown" and "lost". Hence no close button in the
 * corner, no dismissal on Esc, and no dismissal by clicking the mask: all three are switched off
 * where this modal is opened.
 */
@Component({
  selector: 'app-apikey-reveal-modal',
  imports: [FormsModule, NzAlertModule, NzButtonModule, NzCheckboxModule, NzIconModule],
  template: `
    <nz-alert nzType="warning" [nzMessage]="data.notice" nzShowIcon />

    <div class="secret">
      <code class="ac-mono">{{ data.secret }}</code>
      <button nz-button nzType="text" (click)="copy()" [attr.aria-label]="'Copy secret'">
        <span nz-icon [nzType]="copied() ? 'check' : 'copy'"></span>
        {{ copied() ? 'Copied' : 'Copy' }}
      </button>
    </div>

    <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
         nz-checkbox renders its own <input> inside this label and owns the association; the rule
         cannot see through the directive. -->
    <label nz-checkbox [(ngModel)]="acknowledged" name="ack" class="ack">
      I have stored this secret somewhere safe.
    </label>

    <div class="actions">
      <button nz-button nzType="primary" [disabled]="!acknowledged" (click)="ref.close()">Close</button>
    </div>
  `,
  styles: [
    `
      .secret {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin: 1rem 0;
        padding: 0.75rem;
        background: #141414;
        border: 1px solid #303030;
        border-radius: 2px;
      }
      .secret code {
        flex: 1;
      }
      .ack {
        display: block;
        margin-bottom: 1rem;
      }
      .actions {
        display: flex;
        justify-content: flex-end;
      }
    `,
  ],
})
export class ApiKeyRevealModalComponent {
  readonly ref = inject(NzModalRef<ApiKeyRevealModalComponent, void>);
  readonly data = inject<ApiKeyCreated>(NZ_MODAL_DATA);

  readonly copied = signal(false);
  acknowledged = false;

  copy(): void {
    navigator.clipboard?.writeText(this.data.secret).then(
      () => this.copied.set(true),
      () => {
        /* Clipboard access can be denied; the secret is on screen to select by hand. */
      },
    );
  }
}

/** Opens the reveal. Every escape route is closed but the acknowledged Close button. */
export function openApiKeyRevealModal(modal: NzModalService, created: ApiKeyCreated) {
  return modal.create<ApiKeyRevealModalComponent, ApiKeyCreated>({
    nzTitle: 'Store this secret now',
    nzContent: ApiKeyRevealModalComponent,
    nzData: created,
    nzFooter: null,
    nzClosable: false,
    nzMaskClosable: false,
    nzKeyboard: false,
    nzWidth: 520,
  });
}
