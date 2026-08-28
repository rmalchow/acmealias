import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzRadioModule } from 'ng-zorro-antd/radio';
import { NzSwitchModule } from 'ng-zorro-antd/switch';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiKey, ApiService, Domain, LBNode, apiErrorMessage } from '@acme/shared';

/** The four fields every acme-dns client reads, in the names they read them by. */
export interface AcmeDnsCredentials {
  username: string;
  password: string;
  fulldomain: string;
  subdomain: string;
}

export interface AcmeDnsIssued {
  credentials: AcmeDnsCredentials;
  domain: string;
  key: ApiKey;
  notice: string;
  cname: string;
}

/**
 * THERE IS NO SINGLE ACME-DNS CREDENTIALS FILE, and getting this wrong is the failure the whole
 * screen exists to avoid: a near-miss is discovered weeks later, at renewal, inside somebody's
 * cluster. The four FIELDS are the same everywhere; the wrapper around them is not.
 *
 *   keyed      a JSON object keyed by domain, `{"shop.example.com": {...}}` — lego, Traefik,
 *              cert-manager's accountSecretRef, and certbot's certbot-dns-acmedns plugin
 *   flat       the four fields on their own — win-acme (in a file named for the domain), Certify
 *              The Web's "credentials JSON" box, and Caddy's single-account form
 *   posh       a positional FOUR-ELEMENT ARRAY keyed by the challenge name, and the ORDER is
 *              load-bearing: subdomain, username, password, fulldomain
 *
 * Offering one and calling it "the credentials file" would be right for one client and silently
 * wrong for the others, which is worse than offering three.
 */
export type CredentialFormat = 'keyed' | 'flat' | 'posh';

export interface AcmeDnsModalData {
  domain: Domain;
  /** The project's nodes, read only to say what the address switch would permit today. */
  nodes: LBNode[];
}

/**
 * "acme-dns credentials", as a modal — and it is what stands in for acme-dns's `POST /register`.
 *
 * In acme-dns, a client bootstraps by calling an UNAUTHENTICATED endpoint that creates an account
 * and a subdomain and hands back these four fields. That is right for a daemon somebody runs for
 * themselves and exactly wrong here: this service has accounts that are bought, quotas that bound
 * them, domains added deliberately and an activation gate before any of it answers. So the
 * registration is this screen — the same four fields, produced by somebody who is already
 * authorized — and the server's `/register` refuses and says so.
 *
 * TWO STATES IN ONE COMPONENT, and deliberately not two modals. Issuing mints a secret, and the
 * one hard rule of a secret in this product is that it is shown exactly once and cannot be
 * dismissed until the operator says they have stored it. Closing a form modal and opening a reveal
 * modal would put a moment between the two where a stray Esc loses the credential; here the form
 * is replaced in place by the reveal, and every escape route is shut when the modal is opened.
 */
@Component({
  selector: 'app-acmedns-credentials-modal',
  imports: [
    FormsModule,
    NzAlertModule,
    NzButtonModule,
    NzCheckboxModule,
    NzFormModule,
    NzIconModule,
    NzInputModule,
    NzRadioModule,
    NzSwitchModule,
  ],
  template: `
    @if (issued(); as got) {
      <nz-alert nzType="warning" [nzMessage]="got.notice" nzShowIcon />

      <p class="ac-hint intro">
        Give this file to the client BEFORE its first run. It is what acme-dns's registration would
        have returned, and no <code>/register</code> call is needed — but most clients try to
        register when they find no credentials, and this service refuses that. Place the file first
        and the client never asks.
      </p>

      <nz-radio-group [(ngModel)]="format" name="format" nzSize="small" class="formats">
        <label nz-radio-button nzValue="keyed">cert-manager, lego, Traefik, certbot</label>
        <label nz-radio-button nzValue="flat">win-acme, Certify, Caddy</label>
        <label nz-radio-button nzValue="posh">Posh-ACME</label>
      </nz-radio-group>
      <p class="ac-hint">{{ formatHint() }}</p>

      <div class="file">
        <pre class="ac-mono">{{ fileText(got) }}</pre>
        <button nz-button nzType="text" (click)="copy(fileText(got))" [attr.aria-label]="'Copy credentials'">
          <span nz-icon [nzType]="copied() === 'file' ? 'check' : 'copy'"></span>
          {{ copied() === 'file' ? 'Copied' : 'Copy' }}
        </button>
      </div>

      <p class="ac-hint">
        And the record the domain's owner publishes, once, if they have not already:
      </p>
      <div class="file">
        <pre class="ac-mono">{{ got.cname }}</pre>
        <button nz-button nzType="text" (click)="copy(got.cname)" [attr.aria-label]="'Copy CNAME'">
          <span nz-icon [nzType]="copied() === 'cname' ? 'check' : 'copy'"></span>
          {{ copied() === 'cname' ? 'Copied' : 'Copy' }}
        </button>
      </div>

      <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
           nz-checkbox renders its own input inside this label and owns the association; the rule
           cannot see through the directive. -->
      <label nz-checkbox [(ngModel)]="acknowledged" name="ack" class="ack">
        I have stored these credentials somewhere safe.
      </label>

      <div class="actions">
        <button nz-button nzType="primary" [disabled]="!acknowledged" (click)="ref.close()">Close</button>
      </div>
    } @else {
      <form nz-form nzLayout="vertical" class="ac-modal-form" (ngSubmit)="issue()">
        <p class="ac-hint intro">
          Creates a key for this project and hands back the <code>username</code>,
          <code>password</code>, <code>fulldomain</code> and <code>subdomain</code> that cert-manager,
          acme.sh, win-acme, Posh-ACME, lego and Traefik read. The key answers this project's
          challenges and nothing else, and it is deleted with the project.
        </p>

        <nz-form-item>
          <nz-form-label>Note</nz-form-label>
          <nz-form-control nzExtra="Shown in the project's key list and the audit trail.">
            <input nz-input name="note" [(ngModel)]="note" [placeholder]="defaultNote()" autocomplete="off" />
          </nz-form-control>
        </nz-form-item>

        <nz-form-item>
          <nz-form-label>Restrict to this project's nodes</nz-form-label>
          <nz-form-control>
            @if (nodeAddresses().length) {
              <!-- eslint-disable-next-line @angular-eslint/template/label-has-associated-control --
                   nz-switch renders its own control inside this label. -->
              <label class="pin">
                <nz-switch nzSize="small" name="restrictToNodes" [(ngModel)]="restrictToNodes" />
                Only accept requests from this project's node addresses
              </label>
              <p class="ac-hint">
                Currently {{ nodeAddresses().join(', ') }}, and it follows the project. Leave it off
                if the client runs somewhere else — a CI runner, a Kubernetes cluster with its own
                egress.
              </p>
            } @else {
              <p class="ac-hint">This project has no node addresses yet, so there is nothing to permit.</p>
            }
          </nz-form-control>
        </nz-form-item>

        @if (error(); as msg) {
          <p class="ac-error">{{ msg }}</p>
        }

        <div class="actions">
          <button nz-button type="button" (click)="ref.close()">Cancel</button>
          <button nz-button nzType="primary" type="submit" [nzLoading]="saving()">Create credentials</button>
        </div>
      </form>
    }
  `,
  styles: [
    `
      .intro {
        margin-block-end: 1rem;
      }
      .formats {
        display: block;
        margin-block-end: 0.35rem;
      }
      .file {
        display: flex;
        align-items: flex-start;
        gap: 0.5rem;
        margin: 0.5rem 0 1rem;
        padding: 0.75rem;
        background: var(--ac-ground);
        border: 1px solid var(--ac-line);
        border-radius: 4px;
      }
      .file pre {
        flex: 1;
        margin: 0;
        white-space: pre-wrap;
        word-break: break-all;
      }
      .pin {
        display: flex;
        align-items: center;
        gap: 0.5rem;
      }
      .ack {
        display: block;
        margin-block-end: 1rem;
      }
      .actions {
        display: flex;
        justify-content: flex-end;
        gap: 0.5rem;
        margin-top: 1.5rem;
      }
    `,
  ],
})
export class AcmeDnsCredentialsModalComponent {
  private readonly api = inject(ApiService);
  readonly ref = inject(NzModalRef<AcmeDnsCredentialsModalComponent, void>);
  readonly data = inject<AcmeDnsModalData>(NZ_MODAL_DATA);

  readonly saving = signal(false);
  readonly error = signal<string | null>(null);
  readonly issued = signal<AcmeDnsIssued | null>(null);
  readonly copied = signal<'file' | 'cname' | null>(null);

  note = '';
  restrictToNodes = false;
  acknowledged = false;
  format: CredentialFormat = 'keyed';

  readonly defaultNote = computed(() => `acme-dns for ${this.data.domain.name}`);

  readonly nodeAddresses = computed(() => {
    const addrs: string[] = [];
    for (const n of this.data.nodes) {
      if (n.ipv4) addrs.push(n.ipv4);
      if (n.ipv6) addrs.push(n.ipv6);
    }
    return addrs;
  });

  /**
   * The file as the selected client reads it.
   *
   * The field ORDER inside each object is acme-dns's own rather than alphabetical — somebody
   * comparing this against acme-dns's documentation should find the same four lines in the same
   * places. The Posh-ACME array's order is not a preference at all: that plugin indexes it.
   */
  fileText(got: AcmeDnsIssued): string {
    const c = got.credentials;
    const fields = {
      username: c.username,
      password: c.password,
      fulldomain: c.fulldomain,
      subdomain: c.subdomain,
    };
    switch (this.format) {
      case 'flat':
        return JSON.stringify(fields, null, 2);
      case 'posh':
        return JSON.stringify(
          { [`_acme-challenge.${got.domain}`]: [c.subdomain, c.username, c.password, c.fulldomain] },
          null,
          2,
        );
      default:
        return JSON.stringify({ [got.domain]: fields }, null, 2);
    }
  }

  formatHint(): string {
    switch (this.format) {
      case 'flat':
        return 'The four fields on their own. win-acme wants this in a file named for the domain; Certify The Web takes it in its credentials box; Caddy reads it as a single account.';
      case 'posh':
        return 'A positional array — subdomain, username, password, fulldomain — keyed by the challenge name. Pass it as -ACMERegistration, and set -DnsAlias to the fulldomain.';
      default:
        return 'A JSON object keyed by the domain. This is cert-manager’s accountSecretRef value, lego and Traefik’s ACME_DNS_STORAGE_PATH file, and certbot-dns-acmedns’s registration file.';
    }
  }

  copy(text: string): void {
    const what = text.trim().startsWith('{') ? 'file' : 'cname';
    navigator.clipboard?.writeText(text).then(
      () => this.copied.set(what),
      () => {
        /* Clipboard access can be denied; the text is on screen to select by hand. */
      },
    );
  }

  issue(): void {
    if (this.saving()) return;
    this.saving.set(true);
    this.error.set(null);
    this.api
      .post<AcmeDnsIssued>(`/domains/${this.data.domain.id}/acme-dns`, {
        note: this.note,
        restrictToNodes: this.restrictToNodes,
      })
      .subscribe({
        next: (got) => this.issued.set(got),
        error: (err) => {
          this.saving.set(false);
          this.error.set(apiErrorMessage(err));
        },
      });
  }
}

/**
 * Opens it. Every escape route is closed from the start, not from the moment the secret arrives:
 * the modal is never dismissible by Esc or by the mask, because by the time it would matter the
 * secret is already on screen and there is no second chance to show it.
 */
export function openAcmeDnsCredentialsModal(modal: NzModalService, data: AcmeDnsModalData) {
  return modal.create<AcmeDnsCredentialsModalComponent, AcmeDnsModalData>({
    nzTitle: `acme-dns credentials for ${data.domain.name}`,
    nzContent: AcmeDnsCredentialsModalComponent,
    nzData: data,
    nzFooter: null,
    nzClosable: false,
    nzMaskClosable: false,
    nzKeyboard: false,
    nzWidth: 560,
  });
}
