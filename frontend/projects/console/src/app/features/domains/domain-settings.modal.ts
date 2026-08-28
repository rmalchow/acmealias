import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import {
  ApiService,
  Domain,
  apiErrorMessage,
  coveredByWildcard,
  domainNameProblem,
} from '@acme/shared';

export interface DomainSettingsData {
  domain: Domain;
}

/**
 * The observation switch, in its own dialog because `domain-editor.component.ts` is create-only
 * (design G-corrected 9.1) and a switch that can only be set at creation is a switch nobody can
 * turn on for a domain they already have — which, five minutes after "Add domain" closes, is
 * every domain.
 *
 * Template-driven with a `clientProblem()` getter, matching the create dialog: there is no
 * reactive form anywhere in this feature, and introducing one for two fields is a second idiom
 * for nothing.
 */
@Component({
  selector: 'app-domain-settings-modal',
  imports: [FormsModule, NzAlertModule, NzButtonModule, NzFormModule, NzInputModule, NzSwitchModule],
  template: `
    <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="submit()">
      <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
           nz-switch renders its own control inside this label and owns the association. -->
      <label class="switch-row">
        <nz-switch name="observeTls" [(ngModel)]="observeTls" />
        Observe this domain's certificate
      </label>
      <p class="ac-hint">
        Free, on every plan. When it is on, we complete a TLS handshake against this project's
        node addresses every few hours, the same way a browser visiting the domain would — nothing
        else about the domain changes, and nothing connects to your machines until this is on.
      </p>

      @if (isWildcard() && observeTls) {
        <nz-form-item>
          <nz-form-label nzRequired>Hostname to test</nz-form-label>
          <nz-form-control
            [nzValidateStatus]="clientProblem() ? 'error' : 'success'"
            [nzErrorTip]="clientProblem() ?? ''"
            nzExtra="A wildcard has no single certificate name of its own — this is the exact
              hostname we ask for at the handshake, so it must be one you have actually deployed."
          >
            <input
              nz-input
              [(ngModel)]="tlsTestHost"
              name="tlsTestHost"
              [placeholder]="'a.' + stripStar(data.domain.name)"
              autocomplete="off"
            />
          </nz-form-control>
        </nz-form-item>
      }

      @if (serverError(); as msg) {
        <nz-alert nzType="error" [nzMessage]="msg" nzShowIcon />
      }

      <div class="actions">
        <button nz-button type="button" (click)="ref.close()">Cancel</button>
        <button
          nz-button
          nzType="primary"
          type="submit"
          [nzLoading]="saving()"
          [disabled]="!!clientProblem()"
        >
          Save
        </button>
      </div>
    </form>
  `,
  styles: [
    `
      .switch-row {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        font-weight: 500;
      }
      .ac-hint {
        margin: 0.35rem 0 1rem;
        font-size: 0.8125rem;
        color: rgb(255 255 255 / 45%);
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
export class DomainSettingsModalComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<DomainSettingsModalComponent, Domain | undefined>);
  readonly data = inject<DomainSettingsData>(NZ_MODAL_DATA);

  readonly saving = signal(false);
  readonly serverError = signal<string | null>(null);

  /** Seeded from what is actually stored — never a guess (G8, task 2.4). */
  observeTls = this.data.domain.observeTls;
  tlsTestHost = this.data.domain.tlsTestHost ?? '';

  readonly isWildcard = computed(() => this.data.domain.name.startsWith('*.'));

  stripStar(name: string): string {
    return name.startsWith('*.') ? name.slice(2) : name;
  }

  /**
   * Only asks anything of the hostname field on a wildcard with the switch on — a plain domain
   * is observed as itself, and the switch off has nothing left to validate (9.3: the field is
   * kept, not read, once the switch is off).
   *
   * This is a courtesy check only. `coveredByWildcard` mirrors `dnsname.Covers` (2.3), but the
   * service re-runs the same rule and its refusal — not a second client-side matcher — is what a
   * customer sees if the two ever disagree.
   */
  clientProblem(): string | null {
    if (!this.isWildcard() || !this.observeTls) return null;
    const host = this.tlsTestHost.trim();
    if (!host) return 'A wildcard needs a hostname to test — there is no certificate name of its own.';
    const syntax = domainNameProblem(host);
    if (syntax) return syntax;
    if (!coveredByWildcard(this.data.domain.name, host)) {
      return `${host} is not covered by ${this.data.domain.name} — it must sit exactly one label under it.`;
    }
    return null;
  }

  submit(): void {
    if (this.saving() || this.clientProblem()) return;
    this.saving.set(true);
    this.serverError.set(null);
    // Turning the switch off leaves the stored hostname alone (A/2.5, task 9.3): omitting the
    // field is how the API spells "do not touch it", so the field is sent only while it is the
    // thing in force.
    const body: { observeTls: boolean; tlsTestHost?: string } = { observeTls: this.observeTls };
    if (this.isWildcard() && this.observeTls) body.tlsTestHost = this.tlsTestHost.trim();
    this.api.patch<Domain>(`/domains/${this.data.domain.id}`, body).subscribe({
      next: (updated) => {
        this.saving.set(false);
        this.ref.close(updated);
      },
      error: (err) => {
        this.saving.set(false);
        this.serverError.set(apiErrorMessage(err));
      },
    });
  }
}

/** Opens it; `afterClose` emits the updated domain, or undefined if nothing was saved. */
export function openDomainSettings(modal: NzModalService, domain: Domain) {
  return modal.create<DomainSettingsModalComponent, DomainSettingsData>({
    nzTitle: `Observation settings for ${domain.name}`,
    nzContent: DomainSettingsModalComponent,
    nzData: { domain },
    nzFooter: null,
    nzWidth: 480,
  }) as NzModalRef<DomainSettingsModalComponent, Domain | undefined>;
}
