import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiKeyCreated } from '@acme/shared';

/**
 * The once-only reveal of a freshly minted key: its IDENTIFIER and its SECRET.
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

    <!--
      TWO VALUES, NOT ONE, and the identifier is here because a client needs both AT THE SAME
      MOMENT. Everything that authenticates against this service does it with HTTP Basic — lego's
      httpreq, acme-dns, curl — where the identifier is the username and the secret is the
      password. This dialog used to show the password alone, and the key list beside it showed six
      characters of that same secret under a column headed "Identifier", so the username appeared
      nowhere in the console at all.

      The identifier is NOT secret and stays visible on the key's row afterwards. It is repeated
      here anyway: somebody who has just minted a key is about to paste both into a configuration
      file, and sending them to another screen for half of it is how the wrong half gets pasted.
    -->
    <dl class="pair">
      <dt>Identifier <span class="ac-hint">— the username</span></dt>
      <dd>
        <code class="ac-mono">{{ data.key.identifier }}</code>
        <button nz-button nzType="text" (click)="copyIdentifier()" [attr.aria-label]="'Copy identifier'">
          <span nz-icon [nzType]="copiedIdentifier() ? 'check' : 'copy'"></span>
          {{ copiedIdentifier() ? 'Copied' : 'Copy' }}
        </button>
      </dd>

      <dt>Secret <span class="ac-hint">— the password, shown once</span></dt>
      <dd>
        <code class="ac-mono">{{ data.secret }}</code>
        <button nz-button nzType="text" (click)="copy()" [attr.aria-label]="'Copy secret'">
          <span nz-icon [nzType]="copied() ? 'check' : 'copy'"></span>
          {{ copied() ? 'Copied' : 'Copy' }}
        </button>
      </dd>
    </dl>

    <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
         nz-checkbox renders its own <input> inside this label and owns the association; the rule
         cannot see through the directive. -->
    <label nz-checkbox [(ngModel)]="acknowledged" name="ack" class="ack">
      I have stored these credentials somewhere safe.
    </label>

    <div class="actions">
      <button nz-button nzType="primary" [disabled]="!acknowledged" (click)="ref.close()">Close</button>
    </div>
  `,
  styles: [
    `
      .pair {
        margin: 1rem 0;
      }
      .pair dt {
        margin-bottom: 0.25rem;
        font-weight: 600;
      }
      .pair dt:not(:first-of-type) {
        margin-top: 1rem;
      }
      .pair dd {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin: 0;
        padding: 0.75rem;
        background: var(--ac-ground);
        border: 1px solid var(--ac-line);
        border-radius: 4px;
      }
      .pair dd code {
        flex: 1;
        /* A 43-character secret in a 520px dialog: wrap it rather than clip it,
           because a value somebody must copy by hand when the clipboard is
           denied has to be entirely on screen. */
        word-break: break-all;
      }
      /* FLEX, NOT BLOCK. As a block the label is full width and the checkbox's
         own inline-block wrapper takes the whole line, so the sentence dropped
         onto a second line beneath an apparently orphaned box. This is the one
         control standing between "shown once" and "lost", and a checkbox that
         looks detached from its sentence is one somebody ticks without reading
         it. */
      .ack {
        display: flex;
        align-items: center;
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
  readonly copiedIdentifier = signal(false);
  acknowledged = false;

  copy(): void {
    navigator.clipboard?.writeText(this.data.secret).then(
      () => this.copied.set(true),
      () => {
        /* Clipboard access can be denied; the secret is on screen to select by hand. */
      },
    );
  }

  copyIdentifier(): void {
    navigator.clipboard?.writeText(this.data.key.identifier).then(
      () => this.copiedIdentifier.set(true),
      () => {
        /* As above: it is on screen. */
      },
    );
  }
}

/** Opens the reveal. Every escape route is closed but the acknowledged Close button. */
export function openApiKeyRevealModal(modal: NzModalService, created: ApiKeyCreated) {
  return modal.create<ApiKeyRevealModalComponent, ApiKeyCreated>({
    nzTitle: 'Store these credentials now',
    nzContent: ApiKeyRevealModalComponent,
    nzData: created,
    nzFooter: null,
    nzClosable: false,
    nzMaskClosable: false,
    nzKeyboard: false,
    nzWidth: 520,
  });
}
