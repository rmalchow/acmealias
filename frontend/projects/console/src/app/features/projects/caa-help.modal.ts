import { Component, inject, signal } from '@angular/core';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { CAAStatus, Domain, RecordCheck } from '@acme/shared';

/** What the CAA dot describes. `caa` is null until a check has answered for this domain. */
export interface CaaHelpData {
  domain: Domain;
  caa: RecordCheck<CAAStatus> | null;
}

/**
 * The prescription behind the CAA dot (design D5, G7).
 *
 * Two rules this dialog exists to keep, both carried over from the single dialog it was split out
 * of:
 *
 *   - THE FIX OFFERED FOR A BLOCKING CAA IS A CAA RECORD, NEVER A DELEGATION ONE (task 8.5). A
 *     delegation record offered as the cure sends somebody to the wrong file with the wrong value
 *     while issuance goes on being refused — and this dialog cannot make that mistake by
 *     accident, because it is the CAA dialog and has no delegation section to reach for.
 *   - It never prints what a failed lookup found. `found` on `undetermined` is our resolver's
 *     error text, addressed to an operator (design D4). What a BLOCKING set names is the
 *     customer's own zone content and is safe to print, which is why that one case does.
 */
@Component({
  selector: 'app-caa-help-modal',
  imports: [NzButtonModule, NzIconModule],
  template: `
    <p>{{ meaning() }}</p>

    @if (data.caa?.status === 'blocking' && data.caa?.found) {
      <!-- Which tag is the offender is not something we can work out for somebody — a set can
           authorise us for plain issuance and not for wildcards — so show what is published and
           let them read it against the lines below. -->
      <p class="label">What the CAA set names now</p>
      <code class="ac-mono block found">{{ data.caa?.found }}</code>
    }

    @if (lines(); as ls) {
      @if (data.caa?.status === 'blocking') {
        <p class="label">Add these to the CAA record set{{ at() }}</p>
      } @else {
        <p class="label">The entries we look for{{ at() }}</p>
      }
      @for (line of ls; track line) {
        <div class="copyable">
          <code class="ac-mono block">{{ line }}</code>
          <button nz-button nzType="text" (click)="copy(line)" aria-label="Copy record">
            <span nz-icon [nzType]="copied() === line ? 'check' : 'copy'"></span>
            {{ copied() === line ? 'Copied' : 'Copy' }}
          </button>
        </div>
      }
      @if (copyProblem(); as msg) {
        <p class="ac-hint">{{ msg }}</p>
      }
      @if (!name()) {
        <p class="ac-hint">
          They belong at this domain’s registrable name — <code class="ac-mono">example.com</code>
          for <code class="ac-mono">shop.example.com</code> — which is where CAA is read from for
          every host beneath it.
        </p>
      }
      <p>
        Leave the entries already there alone: CAA is a list, and adding an issuer authorises one
        more without withdrawing the rest.
      </p>
      <p class="ac-hint">
        <!-- issue alone is not the fix. A present issuewild governs WILDCARD issuance instead of
             issue, so a set naming us for issue and somebody else for issuewild blocks us for
             exactly the case this service exists for — and somebody who added only the issue line
             would have added a line that was already there and stayed blocked (G7). -->
        Both tags are needed. A published <code class="ac-mono">issuewild</code> governs wildcard
        issuance <em>instead of</em> <code class="ac-mono">issue</code>, so authorising us for one
        and not the other leaves half of it refused.
      </p>
    } @else {
      <p class="ac-hint">No issuer is configured on this deployment, so we cannot name one here.</p>
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
export class CaaHelpModalComponent {
  readonly ref = inject(NzModalRef<CaaHelpModalComponent, void>);
  readonly data = inject<CaaHelpData>(NZ_MODAL_DATA);

  readonly copied = signal<string | null>(null);
  readonly copyProblem = signal<string | null>(null);

  /**
   * The registrable name these entries belong at, when we have been told it.
   *
   * It arrives with a CHECK and not with the domain, so a dialog opened before the verification
   * has answered knows the entries but not the owner name. That is worth saying rather than
   * guessing at: deriving a registrable name from a hostname needs the public suffix list, which
   * the console does not carry and must not approximate.
   */
  name(): string | null {
    return this.data.caa?.name ?? null;
  }

  at(): string {
    const n = this.name();
    return n ? ` at ${n}` : '';
  }

  /**
   * Both tags, for every configured issuer — from the DOMAIN, which carries them whether or not a
   * lookup ever ran (task 10.2).
   *
   * The tags are no longer built here. `expectedCaa` is rendered rdata — `0 issue "x"` — because
   * the service is the one that knows which issuers are configured and how a CAA entry is spelled,
   * and one renderer means the dialog and the check cannot disagree about what they are looking
   * for. All that is left to do is put the owner name in front of it.
   */
  lines(): string[] | null {
    const rdata = this.data.domain.expectedCaa;
    if (!rdata || rdata.length === 0) return null;
    const n = this.name();
    return rdata.map((r) => (n ? `${n}. IN CAA ${r}` : `IN CAA ${r}`));
  }

  /** The clipboard, with the only fallback there is left: the text is on screen to select. */
  copy(text: string): void {
    navigator.clipboard?.writeText(text).then(
      () => {
        this.copied.set(text);
        this.copyProblem.set(null);
      },
      () => this.copyProblem.set('The browser refused clipboard access — select the entry above and copy it by hand.'),
    );
  }

  meaning(): string {
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
export function openCaaHelpModal(modal: NzModalService, data: CaaHelpData) {
  return modal.create<CaaHelpModalComponent, CaaHelpData>({
    nzTitle: `CAA for “${data.domain.name}”`,
    nzContent: CaaHelpModalComponent,
    nzData: data,
    nzFooter: null,
    nzWidth: 640,
  });
}
