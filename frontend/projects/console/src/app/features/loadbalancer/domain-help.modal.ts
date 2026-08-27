import { Component, inject } from '@angular/core';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { CAAStatus, DelegationStatus, Domain, RecordCheck } from '@acme/shared';

/** What the row's chips describe, handed to the modal so it needs no fetch of its own. */
export interface DomainHelpData {
  domain: Domain;
  delegation: RecordCheck<DelegationStatus>;
  caa: RecordCheck<CAAStatus> | null;
}

/**
 * The prescription behind the diagnosis (design D5).
 *
 * A chip says what is true; this says what to do about it, and it does so with THIS domain's
 * challenge name and THIS domain's alias rather than a placeholder. The general instructions
 * already exist in `docs/`; what is missing at the moment somebody is confused is their own
 * value, and a documentation page cannot know it.
 *
 * Read-only and dismiss-only: there is nothing here to submit, because every fix happens in
 * somebody else's DNS.
 *
 * Two things it deliberately refuses:
 *
 *   - It never shows what was found for a lookup that failed. `found` on `undetermined` is our
 *     resolver's error text, addressed to an operator; pasting it at a customer would read as a
 *     verdict on their zone when it is a verdict on our lookup (design D4).
 *   - The fix it offers for a blocking CAA is a CAA record, never a delegation one. The dialog
 *     explains the whole domain, so the delegation section is present and still true — but the
 *     CAA section's prescription stays inside the CAA record set, because a delegation record
 *     offered as the cure for `blocking` sends somebody to the wrong file with the wrong value
 *     while issuance goes on being refused (design D5).
 */
@Component({
  selector: 'app-domain-help-modal',
  imports: [NzButtonModule],
  template: `
    <section>
      <h3>Delegation</h3>
      <p>{{ delegationMeaning() }}</p>

      @if (data.delegation.status === 'incorrect') {
        <p class="label">What is published now</p>
        <code class="ac-mono block found">{{ data.delegation.found || 'something other than the target below' }}</code>
      }

      @if (target(); as t) {
        @if (data.delegation.status !== 'correct') {
          <p class="label">What it should be</p>
        } @else {
          <p class="label">The record</p>
        }
        <code class="ac-mono block">{{ data.domain.challengeName }}. IN CNAME {{ t }}.</code>
      } @else if (data.domain.aliasLabel) {
        <!-- Absent and unknown are different answers here too, which is the whole point of this
             change: an alias this domain HAS but whose plan has not loaded yet is not an alias it
             lacks, and telling somebody we owe them a fix we do not owe is the same false
             confidence in the other direction (design D2). -->
        <p class="ac-hint">
          The record for this domain has not loaded yet. Close this and open it again in a moment.
        </p>
      } @else {
        <p class="ac-hint">
          This domain has no alias yet, so there is no target to publish. That is ours to fix, not
          yours — the alias is issued on our side.
        </p>
      }

      @if (data.delegation.status === 'incorrect') {
        <!-- The cause a customer will not guess, and the only one this product creates on its
             own: an alias is never re-labelled, but deleting a domain and adding it again issues
             a NEW one (design D2). -->
        <p>
          The commonest cause is a domain that was deleted and added again. That issues a new
          alias, and a delegation left pointing at the old one is published, well-formed, and will
          never validate. Compare the two values above: if the target you published is an alias we
          no longer know, this is what happened, and repointing the CNAME at the value above fixes
          it.
        </p>
      }
    </section>

    @if (data.caa; as caa) {
      <section>
        <h3>CAA</h3>
        <p>{{ caaMeaning() }}</p>

        @if (caa.status === 'blocking') {
          @if (caa.found) {
            <!-- Which tag is the offender is not something we can work out for somebody — a set
                 can authorise us for plain issuance and not for wildcards — so show what is
                 published and let them read it against the lines below. This is their own zone's
                 content, not a resolver error, so it is safe to print — unlike what a lookup
                 that failed "found", which is our own error text. -->
            <p class="label">What the CAA set names now</p>
            <code class="ac-mono block found">{{ caa.found }}</code>
          }
          <p class="label">Add these to the CAA record set at {{ caa.name }}</p>
          @for (line of caaLines(caa.name, caa.expected ?? []); track line) {
            <code class="ac-mono block">{{ line }}</code>
          } @empty {
            <p class="ac-hint">No issuer is configured on this deployment, so we cannot name one here.</p>
          }
          <p>
            Leave the entries already there alone: CAA is a list, and adding an issuer authorises
            one more without withdrawing the rest.
          </p>
        }
      </section>
    }

    <div class="actions">
      <button nz-button nzType="primary" (click)="ref.close()">Close</button>
    </div>
  `,
  styles: [
    `
      section {
        margin-bottom: 1.5rem;
      }
      h3 {
        margin: 0 0 0.5rem;
        font-size: 1rem;
        font-weight: 500;
      }
      p {
        margin: 0 0 0.75rem;
      }
      .label {
        margin-bottom: 0.25rem;
        color: var(--ac-muted);
        font-size: 0.8125rem;
      }
      /* The record is the point of the dialog: give it a block of its own to select and paste. */
      .block {
        display: block;
        margin-bottom: 0.75rem;
        padding: 0.75rem;
        background: #141414;
        border: 1px solid #303030;
        border-radius: 2px;
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
export class DomainHelpModalComponent {
  readonly ref = inject(NzModalRef<DomainHelpModalComponent, void>);
  readonly data = inject<DomainHelpData>(NZ_MODAL_DATA);

  /**
   * The customer's own target, as the server carried it with the answer whatever the status —
   * the plan knows it even when the check never ran, so "what to publish" is answerable on a
   * deployment with no resolver at all.
   */
  target(): string | null {
    return this.data.delegation.expected?.[0] ?? null;
  }

  delegationMeaning(): string {
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

  /**
   * Both tags, for every configured issuer — the same set the DNS plan asks for.
   *
   * `issue` alone is not the fix. A present `issuewild` governs WILDCARD issuance instead of
   * `issue`, so a set reading `0 issue "letsencrypt.org"` plus `0 issuewild "someone.else"`
   * blocks us for exactly the case this service exists for, and a customer who added only the
   * `issue` line would have added a line that was already there and stayed blocked with nothing
   * further to try.
   */
  caaLines(name: string, issuers: string[]): string[] {
    return issuers.flatMap((issuer) => [
      `${name}. IN CAA 0 issue "${issuer}"`,
      `${name}. IN CAA 0 issuewild "${issuer}"`,
    ]);
  }

  caaMeaning(): string {
    switch (this.data.caa?.status) {
      case 'correct':
        return 'A CAA record set exists at this name and names our issuer. Nothing to do.';
      case 'not-published':
        return 'No CAA record is published, which is the default state of the internet: any authority may issue for this name. Nothing to do — this is not something you need to add.';
      case 'blocking':
        return 'A CAA record set exists at this name and does not name our issuer, so issuance is refused however the delegation is set up. This is a CAA change, not a delegation one.';
      case 'undetermined':
        return 'The CAA lookup failed — on our side or on the resolver’s. This is not a finding about your DNS.';
      default:
        return 'We have not checked CAA for this name.';
    }
  }
}

/** Opens the explanation. It returns nothing: every fix it describes happens elsewhere. */
export function openDomainHelpModal(modal: NzModalService, data: DomainHelpData) {
  return modal.create<DomainHelpModalComponent, DomainHelpData>({
    nzTitle: `What is published for “${data.domain.name}”`,
    nzContent: DomainHelpModalComponent,
    nzData: data,
    nzFooter: null,
    // Wide enough that a challenge name and an alias target sit on one line each: a record split
    // across two lines is a record somebody mis-copies.
    nzWidth: 640,
  });
}
