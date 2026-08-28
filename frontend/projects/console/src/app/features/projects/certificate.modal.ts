import { DatePipe } from '@angular/common';
import { Component, OnInit, inject, signal } from '@angular/core';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { AddressObservation, ApiService, CertificateState, Domain, apiErrorMessage } from '@acme/shared';
import { certificateChip } from './domain-status';

/** What the TLS dot describes: the domain, and the state that coloured the dot. */
export interface CertificateModalData {
  domain: Domain;
  state: CertificateState;
}

/**
 * What we saw when we connected (design G7, G10).
 *
 * IT PRINTS NO RECORD, because there is nothing in DNS to fix. Everything else in this feature
 * ends in a line somebody publishes; this one ends in a certificate somebody deploys, and telling
 * them to add a record would be an instruction to the wrong system.
 *
 * THE DOT IS THE WORST ADDRESS; THIS IS EVERY ADDRESS (G10). They are listed in node order with
 * the families together within each node, so "renewed on IPv4 and not on IPv6" is one glance
 * rather than a comparison the reader has to construct — which is the failure that is invisible
 * to a dual-stack visitor, including the customer checking in their own browser.
 *
 * THE HOSTNAME IS NAMED IN EVERY STATE (task 8.7). A wildcard is observed under a test hostname
 * somebody chose, and a hostname that has gone stale shows red for a name nobody deployed. Naming
 * it at the top is what makes that dot explain itself the moment it is opened.
 */
@Component({
  selector: 'app-certificate-modal',
  imports: [DatePipe, NzButtonModule],
  template: `
    <p>{{ verdict().tooltip }}</p>

    <p class="ac-hint">
      Observed as <code class="ac-mono">{{ data.state.serverName }}</code> on port 443, against the
      addresses of this project’s nodes. We hold no certificate and no key for this domain — this
      is a handshake and nothing more.
    </p>

    <h3>Addresses</h3>
    @for (o of addresses(); track o.nodeId + o.family) {
      <div class="address">
        <p class="head">
          <span class="dot" [class]="'dot-' + band(o).colour"></span>
          <strong>{{ o.nodeLabel || 'Unknown node' }}</strong>
          <span class="ac-mono">{{ o.address }}</span>
          <span class="ac-hint">{{ o.family }}</span>
        </p>
        @if (o.outcome === 'observed') {
          <dl>
            <dt>Expires</dt>
            <dd>{{ o.notAfter | date: 'medium' }} · {{ remaining(o) }}</dd>
            <dt>Issuer</dt>
            <dd>{{ o.issuer || 'not stated' }}</dd>
            <dt>Covers {{ data.state.serverName }}</dt>
            <dd>{{ o.coversServerName ? 'yes' : 'no — every client refuses it for this name' }}</dd>
            <dt>Chain</dt>
            <!-- Recorded and shown, and it colours nothing (G12, task 12.5). We complete the
                 handshake without verifying, so that an expired or untrusted chain is a finding
                 rather than the same "could not look" a firewall drop produces — and an untrusted
                 chain is routine in a test deployment whose root nobody installed. -->
            <dd>{{ o.chainTrusted ? 'trusted by this machine' : 'not trusted by this machine' }}</dd>
          </dl>
        } @else {
          <p class="ac-hint">
            {{ failure(o) }} This says nothing about whether your node is working — we cannot tell
            that apart from our own transit, a firewall dropping us, or a listener that is not on
            443.
          </p>
        }
        <p class="ac-hint stamp">Looked at {{ o.observedAt | date: 'medium' }}</p>
      </div>
    } @empty {
      <p class="ac-hint">
        Nothing has been observed yet. The first pass runs within the observation interval — there
        is nothing to do and nothing has gone wrong.
      </p>
    }

    <h3>History</h3>
    @if (historyProblem(); as msg) {
      <!-- The service's own sentence, not a second copy of it kept here (task 8.8). A refusal
           this console restated would be the copy that goes stale the day the wording changes. -->
      <p class="ac-hint">{{ msg }}</p>
    } @else if (history() === null) {
      <p class="ac-hint">Loading…</p>
    } @else {
      @if (history()!.length) {
        <table class="history">
          <thead>
            <tr>
              <th>When</th>
              <th>Node</th>
              <th>Address</th>
              <th>Outcome</th>
              <th>Expires</th>
            </tr>
          </thead>
          <tbody>
            @for (o of history()!; track o.observedAt + o.nodeId + o.family) {
              <tr>
                <td>{{ o.observedAt | date: 'short' }}</td>
                <td>{{ o.nodeLabel || '—' }}</td>
                <td class="ac-mono">{{ o.address }}</td>
                <td>{{ o.outcome }}</td>
                <td>{{ o.notAfter ? (o.notAfter | date: 'shortDate') : '—' }}</td>
              </tr>
            }
          </tbody>
        </table>
      } @else {
        <p class="ac-hint">Nothing recorded yet.</p>
      }
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
      h3 {
        margin: 1.5rem 0 0.5rem;
        font-size: 1rem;
        font-weight: 500;
      }
      .address {
        margin-bottom: 1rem;
        padding-bottom: 0.75rem;
        border-bottom: 1px solid var(--ac-line);
      }
      .head {
        display: flex;
        align-items: baseline;
        gap: 0.5rem;
        margin-bottom: 0.25rem;
      }
      .dot {
        width: 0.625rem;
        height: 0.625rem;
        border-radius: 50%;
        display: inline-block;
      }
      .dot-green {
        background: var(--ac-ok, #52c41a);
      }
      .dot-orange {
        background: var(--ac-warn, #fa8c16);
      }
      .dot-red {
        background: var(--ac-error, #f5222d);
      }
      .dot-default {
        background: var(--ac-line, #d9d9d9);
      }
      dl {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 0.125rem 0.75rem;
        margin: 0;
      }
      dt {
        color: var(--ac-muted);
        font-size: 0.8125rem;
      }
      dd {
        margin: 0;
      }
      .stamp {
        margin: 0.25rem 0 0;
      }
      .history {
        width: 100%;
        border-collapse: collapse;
        font-size: 0.8125rem;
      }
      .history th,
      .history td {
        text-align: left;
        padding: 0.25rem 0.5rem 0.25rem 0;
        border-bottom: 1px solid var(--ac-line);
      }
      .actions {
        display: flex;
        justify-content: flex-end;
        margin-top: 1rem;
      }
    `,
  ],
})
export class CertificateModalComponent implements OnInit {
  readonly ref = inject(NzModalRef<CertificateModalComponent, void>);
  readonly data = inject<CertificateModalData>(NZ_MODAL_DATA);
  private readonly api = inject(ApiService);

  readonly history = signal<AddressObservation[] | null>(null);
  readonly historyProblem = signal<string | null>(null);

  /**
   * The history, and the two ways it can fail to arrive.
   *
   * A PERSONAL ACCOUNT IS REFUSED BY THE SERVICE, not by this dialog hiding a section it received
   * (G15) — and the refusal's own message is what is rendered, so there is no second copy of the
   * sentence here to go stale.
   *
   * IT KEYS ON THE MESSAGE AND NEVER ON THE STATUS. The status is 418, chosen because nothing
   * else in anybody's client library means anything by it — but for that same reason some proxies
   * and libraries treat it as unknown, strip the reason phrase, or collapse it into a generic
   * error. A console branching on `status === 418` is a console that shows the wrong thing the
   * first time this runs behind something that rewrites it. Anything we do not recognise is
   * reported as a load that failed, which is what it is.
   */
  ngOnInit(): void {
    this.api.get<AddressObservation[]>(`/domains/${this.data.domain.id}/certificate/history`).subscribe({
      next: (rows) => this.history.set(rows ?? []),
      error: (err) => {
        const msg = apiErrorMessage(err);
        this.historyProblem.set(
          msg.includes('part of a team account')
            ? msg
            : 'The history could not be loaded. The current state above is unaffected.',
        );
      },
    });
  }

  verdict() {
    return certificateChip(this.data.state.band);
  }

  band(o: AddressObservation) {
    return certificateChip(o.band);
  }

  /**
   * Every address, in node order with the families together within each node (G10).
   *
   * Sorted by the LABEL rather than by the identifier the service happens to order by, because
   * the reader is comparing the machines they named, and a v7 uuid's ordering is creation time.
   */
  addresses(): AddressObservation[] {
    return [...this.data.state.addresses].sort(
      (a, b) => a.nodeLabel.localeCompare(b.nodeLabel) || a.family.localeCompare(b.family),
    );
  }

  /** How long is left, in the words somebody would use. The BAND is the service's; this is prose. */
  remaining(o: AddressObservation): string {
    if (!o.notAfter) return 'unknown';
    const days = Math.floor((Date.parse(o.notAfter) - Date.now()) / 86_400_000);
    if (days < 0) return `expired ${-days} ${-days === 1 ? 'day' : 'days'} ago`;
    if (days === 0) return 'expires today';
    return `${days} ${days === 1 ? 'day' : 'days'} left`;
  }

  /**
   * Our classification of a handshake that did not happen, in our words.
   *
   * Never the transport's error text (G11): it names our source address and our resolver, is
   * written for an operator, and reads as a verdict on the customer's machine when it is a
   * verdict on our attempt.
   */
  failure(o: AddressObservation): string {
    switch (o.outcome) {
      case 'refused':
        return 'Nothing accepted a connection on port 443 at this address.';
      case 'timed-out':
        return 'The connection was not answered before our timeout.';
      case 'rejected':
        return 'The connection was accepted and the TLS handshake was rejected.';
      case 'unreadable':
        return 'A certificate was served and we could not read it.';
      default:
        return 'We could not complete a handshake with this address.';
    }
  }
}

/** Opens the observation. It returns nothing: the fix, where there is one, is a deployment. */
export function openCertificateModal(modal: NzModalService, data: CertificateModalData) {
  return modal.create<CertificateModalComponent, CertificateModalData>({
    nzTitle: `The certificate on “${data.domain.name}”`,
    nzContent: CertificateModalComponent,
    nzData: data,
    nzFooter: null,
    nzWidth: 720,
  });
}
