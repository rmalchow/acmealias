import { Component, inject, signal } from '@angular/core';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { DelegationStatus, Domain, RecordCheck } from '@acme/shared';

/** What the CNAME dot describes, handed to the modal so it needs no fetch of its own. */
export interface DelegationHelpData {
  domain: Domain;
  delegation: RecordCheck<DelegationStatus>;
}

/**
 * The prescription behind the CNAME dot (design D5, G7).
 *
 * One of three dialogs where there was one covering every record type. A single dialog has to
 * lead with a section the reader did not click on: somebody who clicks the CAA dot because CAA is
 * the thing that is red should not have to scroll past a delegation that is fine.
 *
 * THE RECORD IS PRINTED IN EVERY STATE, green included, where it names the record in effect
 * (G7). It comes from the DOMAIN's own `cnameTarget` and not from a check, which is what lets
 * this dialog answer "what do I publish" on a deployment that never configured a verification
 * resolver — which record a domain needs is knowable without asking DNS anything.
 *
 * Read-only and dismiss-only: every fix it describes happens in somebody else's DNS.
 *
 * One thing it deliberately refuses: it never shows what a FAILED lookup found. `found` on
 * `undetermined` is our resolver's error text, addressed to an operator, and pasting it at a
 * customer reads as a verdict on their zone when it is a verdict on our lookup (design D4).
 */
@Component({
  selector: 'app-delegation-help-modal',
  imports: [NzButtonModule, NzIconModule],
  template: `
    <p>{{ meaning() }}</p>

    @if (data.delegation.status === 'incorrect') {
      <p class="label">What is published now</p>
      <code class="ac-mono block found">{{ data.delegation.found || 'something other than the record below' }}</code>
    }

    @if (record(); as r) {
      @if (data.delegation.status === 'correct') {
        <p class="label">The record</p>
      } @else {
        <p class="label">What it should be</p>
      }
      <div class="copyable">
        <code class="ac-mono block">{{ r }}</code>
        <button nz-button nzType="text" (click)="copy(r)" aria-label="Copy record">
          <span nz-icon [nzType]="copied() ? 'check' : 'copy'"></span>
          {{ copied() ? 'Copied' : 'Copy' }}
        </button>
      </div>
      @if (copyProblem(); as msg) {
        <p class="ac-hint">{{ msg }}</p>
      }
    } @else {
      <p class="ac-hint">
        This domain has no alias yet, so there is no target to publish. That is ours to fix, not
        yours — the alias is issued on our side.
      </p>
    }

    @if (data.delegation.status === 'incorrect') {
      <!-- The cause a customer will not guess, and the only one this product creates on its own:
           an alias is never re-labelled, but deleting a domain and adding it again issues a NEW
           one (design D2). Kept verbatim from the dialog this one was split out of. -->
      <p>
        The commonest cause is a domain that was deleted and added again. That issues a new alias,
        and a delegation left pointing at the old one is published, well-formed, and will never
        validate. Compare the two values above: if the target you published is an alias we no
        longer know, this is what happened, and repointing the CNAME at the value above fixes it.
      </p>
    }

    <div class="actions">
      <button nz-button nzType="primary" (click)="ref.close()">Close</button>
    </div>
  `,
  styles: [
    `
      p {
        margin: 0 0 0.75rem;
      }
      .label {
        margin-bottom: 0.25rem;
        color: var(--ac-muted);
        font-size: 0.8125rem;
      }
      /* The record is the point of the dialog: a block of its own to select and paste. */
      .block {
        display: block;
        margin-bottom: 0.75rem;
        padding: 0.75rem;
        background: var(--ac-ground);
        border: 1px solid var(--ac-line);
        border-radius: 4px;
      }
      .copyable {
        display: flex;
        align-items: flex-start;
        gap: 0.5rem;
      }
      .copyable .block {
        flex: 1;
      }
      .found {
        color: var(--ac-error);
      }
      .actions {
        display: flex;
        justify-content: flex-end;
      }
    `,
  ],
})
export class DelegationHelpModalComponent {
  readonly ref = inject(NzModalRef<DelegationHelpModalComponent, void>);
  readonly data = inject<DelegationHelpData>(NZ_MODAL_DATA);

  readonly copied = signal(false);
  readonly copyProblem = signal<string | null>(null);

  /**
   * The whole record, ready to paste, or null when this domain has no alias to point at.
   *
   * The target is the domain's own field rather than the check's `expected`, so this is the same
   * sentence whether or not a lookup ever ran (design G2, task 11.2).
   */
  record(): string | null {
    const t = this.data.domain.cnameTarget;
    return t ? `${this.data.domain.challengeName}. IN CNAME ${t}.` : null;
  }

  /**
   * The clipboard, and what to do when the browser refuses it.
   *
   * The fallback used to be "use the zone snippet instead" — a download that went with the DNS
   * plan panel. There is nothing left to fall back TO but the text on screen, so that is what the
   * message says. It is written here rather than shared with the two other dialogs that have
   * their own copy of it: three inline copies of four lines have never cost anybody anything.
   */
  copy(text: string): void {
    navigator.clipboard?.writeText(text).then(
      () => {
        this.copied.set(true);
        this.copyProblem.set(null);
      },
      () => this.copyProblem.set('The browser refused clipboard access — select the record above and copy it by hand.'),
    );
  }

  meaning(): string {
    switch (this.data.delegation.status) {
      case 'correct':
        return 'The CNAME is published and points at this domain’s alias. Renewals for this domain can be answered.';
      case 'missing':
        return 'Nothing resolves at this domain’s challenge name. The record below has not been published yet, and until it is, no certificate can be issued for this domain.';
      case 'incorrect':
        return 'A record is published at this domain’s challenge name, but it points somewhere else. It looks done and it will never validate.';
      case 'undetermined':
        return 'The lookup failed — on our side or on the resolver’s. This is not a finding about your DNS, and there is nothing for you to change because of it.';
      default:
        return 'We have not checked this domain. Either this deployment has no verification resolver or the answer has not arrived yet; either way we are not claiming anything about your DNS.';
    }
  }
}

/** Opens the explanation. It returns nothing: every fix it describes happens elsewhere. */
export function openDelegationHelpModal(modal: NzModalService, data: DelegationHelpData) {
  return modal.create<DelegationHelpModalComponent, DelegationHelpData>({
    nzTitle: `The delegation for “${data.domain.name}”`,
    nzContent: DelegationHelpModalComponent,
    nzData: data,
    nzFooter: null,
    // Wide enough that a challenge name and an alias target sit on one line: a record split
    // across two lines is a record somebody mis-copies.
    nzWidth: 640,
  });
}
