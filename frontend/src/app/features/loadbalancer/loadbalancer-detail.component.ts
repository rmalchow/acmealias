import { Component, computed, inject, input, OnChanges, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzMessageService } from 'ng-zorro-antd/message';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { SessionService } from '../../core/session.service';
import { PageHeaderComponent } from '../../shared/page-header.component';
import { confirmAction } from '../../shared/confirm';
import { CheckResult, Domain, DNSPlan, LBNode, LoadBalancer, Project, VerificationResult } from '../models';
import { openDomainEditor } from '../domains/domain-editor.component';
import { ApiKeysPanelComponent } from '../apikeys/apikeys-panel.component';
import { InteractionsPanelComponent } from './interactions-panel.component';
import { openNodeCreateModal } from './node-create.modal';
import { openNodeEditModal } from './node-edit.modal';
import { domainStatus as computeDomainStatus } from './domain-status';

/**
 * 19.2 — a load balancer: its nodes, its domain table, the DNS plan panel (records, live
 * verification, copy and zone-snippet download) and the API keys panel.
 */
@Component({
  selector: 'app-loadbalancer-detail',
  imports: [
    PageHeaderComponent,
    ApiKeysPanelComponent,
    InteractionsPanelComponent,
    NzToolTipModule,
    NzAlertModule,
    NzButtonModule,
    NzIconModule,
    NzTableModule,
    NzTagModule,
  ],
  template: `
    <app-page-header
      [crumbs]="[
        { label: 'Projects', link: '/projects' },
        { label: projectSlug() ?? 'Project', link: loadBalancer() ? ['/projects', loadBalancer()!.projectId] : null },
        { label: loadBalancer()?.slug ?? '…' },
      ]"
      [title]="loadBalancer()?.name ?? 'Load balancer'"
      [subtitle]="subtitle()"
    />

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <section>
      <div class="panel-head">
        <h2>Nodes</h2>
        @if (canWrite()) {
          <button nz-button nzType="primary" nzSize="small" (click)="createNode()">
            <span nz-icon nzType="plus"></span>
            Add node
          </button>
        }
      </div>

      <nz-table
        class="ac-table"
        nzSize="small"
        nzBordered
        [nzData]="loadBalancer()?.nodes ?? []"
        [nzShowPagination]="false"
        nzNoResult="No nodes yet."
      >
        <thead>
          <tr>
            <th nzWidth="12rem">Label</th>
            <th>IPv4</th>
            <th>IPv6</th>
            <th nzWidth="6rem"></th>
          </tr>
        </thead>
        <tbody>
          @for (n of loadBalancer()?.nodes ?? []; track n.id) {
            <tr>
              <td>{{ n.label }}</td>
              <td class="ac-mono">{{ n.ipv4 || '—' }}</td>
              <td class="ac-mono">{{ n.ipv6 || '—' }}</td>
              <td class="row-actions">
                @if (canWrite()) {
                  <button
                    nz-button
                    nzType="text"
                    nzSize="small"
                    nz-tooltip
                    nzTooltipTitle="Edit addresses"
                    aria-label="Edit addresses"
                    (click)="editNode(n)"
                  >
                    <span nz-icon nzType="edit"></span>
                  </button>
                  <button
                    nz-button
                    nzType="text"
                    nzDanger
                    nzSize="small"
                    nz-tooltip
                    nzTooltipTitle="Delete node"
                    aria-label="Delete node"
                    (click)="deleteNode(n)"
                  >
                    <span nz-icon nzType="delete"></span>
                  </button>
                }
              </td>
            </tr>
          }
        </tbody>
      </nz-table>
    </section>

    <section>
      <div class="panel-head">
        <h2>Domains</h2>
        @if (canWrite() && loadBalancer()) {
          <button nz-button nzType="primary" nzSize="small" (click)="addDomain()">
            <span nz-icon nzType="plus"></span>
            Add domain
          </button>
        }
      </div>

      <nz-table
        class="ac-table"
        nzSize="small"
        nzBordered
        [nzData]="loadBalancer()?.domains ?? []"
        [nzShowPagination]="false"
        nzNoResult="No domains yet."
      >
        <thead>
          <tr>
            <th>Name</th>
            <th>Challenge name</th>
            <th nzWidth="12rem">Alias</th>
            <th nzWidth="18rem">Validation</th>
            <th nzWidth="5rem"></th>
          </tr>
        </thead>
        <tbody>
          @for (d of loadBalancer()?.domains ?? []; track d.id) {
            <tr>
              <td>{{ d.name }}</td>
              <td class="ac-mono">{{ d.challengeName }}</td>
              <td>{{ d.aliasLabel }}</td>
              <td>
                @switch (domainStatus(d)) {
                  @case ('ok') {
                    <nz-tag nzColor="green">verified</nz-tag>
                  }
                  @case ('problem') {
                    <nz-tag nzColor="red">not verified</nz-tag>
                    <div class="ac-hint">see the DNS plan below</div>
                  }
                  @default {
                    <nz-tag nzColor="gold">never validated</nz-tag>
                    <div class="ac-hint">run verification below to see what is missing</div>
                  }
                }
              </td>
              <td class="row-actions">
                @if (canWrite()) {
                  <button
                    nz-button
                    nzType="text"
                    nzDanger
                    nzSize="small"
                    nz-tooltip
                    nzTooltipTitle="Delete domain"
                    aria-label="Delete domain"
                    (click)="deleteDomain(d)"
                  >
                    <span nz-icon nzType="delete"></span>
                  </button>
                }
              </td>
            </tr>
          }
        </tbody>
      </nz-table>
    </section>

    @if (plan(); as pl) {
      <section>
        <div class="panel-head">
          <h2>DNS plan</h2>
          <button nz-button nzSize="small" (click)="copyPlan(pl)">
            <span nz-icon nzType="copy"></span>
            Copy records
          </button>
          <a
            nz-button
            nzSize="small"
            [href]="zoneHref()"
            target="_blank"
            rel="noopener"
          >
            <span nz-icon nzType="download"></span>
            Zone snippet
          </a>
          <button nz-button nzType="primary" nzSize="small" [nzLoading]="verifying()" (click)="verify()">
            Run verification
          </button>
        </div>

        <nz-table
          class="ac-table"
          nzSize="small"
          nzBordered
          [nzData]="pl.records"
          [nzShowPagination]="false"
        >
          <thead>
            <tr>
              <th>Name</th>
              <th nzWidth="6rem">Type</th>
              <th nzWidth="5rem">TTL</th>
              <th>Value</th>
              <th nzWidth="10rem">Status</th>
              <th>Found</th>
            </tr>
          </thead>
          <tbody>
            @for (r of pl.records; track r.name + r.type + r.value) {
              <tr>
                <td>{{ r.name }}</td>
                <td>{{ r.type }}</td>
                <td>{{ r.ttl }}</td>
                <td class="ac-mono">{{ r.value }}</td>
                <td>
                  @if (statusFor(r.name, r.type); as s) {
                    <nz-tag [nzColor]="statusColour(s.status)">{{ s.status }}</nz-tag>
                  } @else {
                    <span class="ac-hint">not checked yet</span>
                  }
                </td>
                <td class="ac-mono">{{ statusFor(r.name, r.type)?.found || '—' }}</td>
              </tr>
            }
          </tbody>
        </nz-table>

        <p class="ac-hint">
          A CAA row of “not-published” is informational, not an error: an absent CAA record is the
          default state of the internet and blocks nothing. A CAA record naming a different issuer
          is the one CAA failure that matters.
        </p>

        @if (verifyError(); as msg) {
          <nz-alert nzType="error" [nzMessage]="msg" nzShowIcon />
        }
      </section>
    }

    @if (loadBalancer(); as p) {
      <section>
        <app-apikeys-panel [loadBalancerId]="p.id" [nodes]="p.nodes" />
      </section>

      <section>
        <app-interactions-panel [loadBalancerId]="p.id" />
      </section>
    }
  `,
  styles: [
    `
      section {
        margin-bottom: 2rem;
      }
      .panel-head {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin-bottom: 0.5rem;
      }
      h2 {
        margin: 0 0.5rem 0 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      /* Row actions are icons only — the label is in the tooltip and the aria-label, so a table
       * of twenty rows is not twenty repetitions of the word "Delete". */
      .row-actions {
        white-space: nowrap;
      }
    `,
  ],
})
export class LoadBalancerDetailComponent implements OnChanges {
  private readonly api = inject(ApiService);
  private readonly modal = inject(NzModalService);
  private readonly message = inject(NzMessageService);
  private readonly session = inject(SessionService);

  readonly id = input.required<string>();

  /**
   * The zone snippet is fetched by the browser rather than by HttpClient — it is a download, not
   * a request the app renders — so it needs the full path, account and all. Building it here
   * keeps the one place that knows the URL shape next to the one that knows the account.
   */
  protected zoneHref(): string {
    return `/api/v1/accounts/${this.api.currentAccount()}/loadbalancers/${this.id()}/plan/zone`;
  }

  readonly loadBalancer = signal<LoadBalancer | null>(null);
  /** Only for the breadcrumb — the load balancer carries a projectId, not the slug a human reads. */
  readonly projectSlug = signal<string | null>(null);
  readonly plan = signal<DNSPlan | null>(null);
  readonly verification = signal<VerificationResult | null>(null);
  readonly verifying = signal(false);
  readonly verifyError = signal<string | null>(null);
  readonly error = signal<string | null>(null);

  readonly canWrite = computed(() => this.session.hasAnyRole(['ADMIN', 'CONFIG_ADMIN']));

  readonly subtitle = () => {
    const p = this.loadBalancer();
    if (!p) return null;
    const nodes = p.nodes.length;
    const domains = p.domains?.length ?? 0;
    return `${p.slug} · ${nodes} ${nodes === 1 ? 'node' : 'nodes'} · ${domains} ${domains === 1 ? 'domain' : 'domains'}`;
  };

  ngOnChanges(): void {
    this.load();
    this.loadPlan();
  }

  load(): void {
    this.api.get<LoadBalancer>(`/loadbalancers/${this.id()}`).subscribe({
      next: (p) => {
        this.loadBalancer.set(p);
        this.api.get<Project>(`/projects/${p.projectId}`).subscribe({
          next: (proj) => this.projectSlug.set(proj.slug),
          // A missing project name costs a breadcrumb label, not the screen: leave it generic.
          error: () => this.projectSlug.set(null),
        });
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  private loadPlan(): void {
    this.api.get<DNSPlan>(`/loadbalancers/${this.id()}/plan`).subscribe({
      next: (p) => this.plan.set(p),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  verify(): void {
    this.verifying.set(true);
    this.verifyError.set(null);
    this.api.get<VerificationResult>(`/loadbalancers/${this.id()}/verification`).subscribe({
      next: (v) => {
        this.verifying.set(false);
        this.verification.set(v);
      },
      error: (err) => {
        this.verifying.set(false);
        this.verifyError.set(apiErrorMessage(err));
      },
    });
  }

  statusFor(name: string, type: string): CheckResult | undefined {
    return this.verification()?.results.find((r) => r.name === name && r.type === type);
  }

  /** The five states the verifier can report, coloured by what they mean for the operator. */
  statusColour(status: string): string {
    switch (status) {
      case 'correct':
        return 'green';
      case 'missing':
      case 'incorrect':
        return 'red';
      case 'not-published':
        return 'blue';
      default:
        return 'default';
    }
  }

  domainStatus(d: Domain): 'ok' | 'problem' | 'never' {
    return computeDomainStatus(d, this.verification()?.results ?? null);
  }

  copyPlan(p: DNSPlan): void {
    const text = p.records.map((r) => `${r.name}\t${r.ttl}\tIN\t${r.type}\t${r.value}`).join('\n');
    navigator.clipboard?.writeText(text).then(
      () => this.message.success('DNS records copied'),
      () => {
        // Clipboard access can be denied by the browser; the zone-snippet download is the fallback.
        this.message.error('The browser refused clipboard access — use the zone snippet instead.');
      },
    );
  }

  editNode(n: LBNode): void {
    openNodeEditModal(this.modal, n).afterClose.subscribe((updated?: LBNode) => {
      if (!updated) return;
      this.load();
      // The plan is derived from these addresses, so it is stale the moment they change.
      this.loadPlan();
    });
  }

  createNode(): void {
    openNodeCreateModal(this.modal, this.id()).afterClose.subscribe((created?: LBNode) => {
      if (!created) return;
      this.load();
      this.loadPlan();
    });
  }

  addDomain(): void {
    openDomainEditor(this.modal, this.loadBalancer()!.id).afterClose.subscribe((created?: boolean) => {
      if (!created) return;
      this.load();
      this.loadPlan();
    });
  }

  async deleteNode(n: LBNode): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Delete node “${n.label}”?`,
      message: 'Its addresses disappear from the DNS plan, so the records you publish have to change too.',
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/nodes/${n.id}`).subscribe({
      next: () => {
        this.load();
        this.loadPlan();
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  async deleteDomain(d: Domain): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Delete domain “${d.name}”?`,
      message: 'Renewals for it stop being answerable as soon as this is done.',
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/domains/${d.id}`).subscribe({
      next: () => {
        this.load();
        this.loadPlan();
      },
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }
}
