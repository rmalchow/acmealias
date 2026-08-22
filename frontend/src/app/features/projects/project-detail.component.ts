import { Component, OnChanges, computed, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalService } from 'ng-zorro-antd/modal';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { ApiService, apiErrorMessage } from '../../core/api.service';
import { SessionService } from '../../core/session.service';
import { PageHeaderComponent } from '../../shared/page-header.component';
import { confirmAction } from '../../shared/confirm';
import { openNameSlugModal } from '../../shared/name-slug.modal';
import { ProjectInventoryPanelComponent } from '../reporting/project-inventory-panel.component';
import { ProjectUsagePanelComponent } from '../reporting/project-usage-panel.component';
import { CAN_WRITE, LoadBalancer, Project, may } from '../models';

/** 19.1 — a project's load balancers; create and delete offered only to `ADMIN`. */
@Component({
  selector: 'app-project-detail',
  imports: [
    RouterLink,
    PageHeaderComponent,
    ProjectInventoryPanelComponent,
    ProjectUsagePanelComponent,
    NzButtonModule,
    NzIconModule,
    NzTableModule,
    NzToolTipModule,
  ],
  styles: [
    `
      /* Icons only, with the label in the tooltip and the aria-label. */
      .row-actions {
        white-space: nowrap;
      }
      .ac-panel {
        margin-top: 2rem;
      }
    `,
  ],
  template: `
    <app-page-header
      [crumbs]="[{ label: 'Projects', link: '/projects' }, { label: project()?.slug ?? '…' }]"
      [title]="project()?.name ?? 'Project'"
      [subtitle]="subtitle()"
    />

    <div class="ac-filters">
      @if (canWrite()) {
        <button class="ac-spacer" nz-button nzType="primary" (click)="createLoadBalancer()">
          <span nz-icon nzType="plus"></span>
          New load balancer
        </button>
      }
    </div>

    @if (error(); as msg) {
      <p class="ac-error">{{ msg }}</p>
    }

    <nz-table
      class="ac-table"
      nzSize="small"
      nzBordered
      [nzData]="loadBalancers()"
      [nzShowPagination]="false"
      nzNoResult="No load balancers yet."
    >
      <thead>
        <tr>
          <th nzWidth="14rem">Slug</th>
          <th>Name</th>
          <th nzWidth="7rem">Nodes</th>
          <th nzWidth="7rem">Domains</th>
          <th nzWidth="5rem"></th>
        </tr>
      </thead>
      <tbody>
        @for (lb of loadBalancers(); track lb.id) {
          <tr>
            <td><a [routerLink]="['/loadbalancers', lb.id]">{{ lb.slug }}</a></td>
            <td>{{ lb.name }}</td>
            <td>{{ lb.nodes.length }}</td>
            <td>{{ lb.domains?.length ?? 0 }}</td>
            <td class="row-actions">
              @if (canWrite()) {
                <button
                  nz-button
                  nzType="text"
                  nzDanger
                  nzSize="small"
                  nz-tooltip
                  nzTooltipTitle="Delete load balancer"
                  aria-label="Delete load balancer"
                  (click)="deleteLoadBalancer(lb)"
                >
                  <span nz-icon nzType="delete"></span>
                </button>
              }
            </td>
          </tr>
        }
      </tbody>
    </nz-table>

    <!-- Reporting, per project. There is no per-domain view and none is planned: the
         never-queried signal is drilled into from the load balancer's interactions list. -->
    <section class="ac-panel">
      <app-project-usage-panel [projectId]="id()" />
    </section>
    <section class="ac-panel">
      <app-project-inventory-panel [projectId]="id()" />
    </section>
  `,
})
export class ProjectDetailComponent implements OnChanges {
  private readonly api = inject(ApiService);
  private readonly modal = inject(NzModalService);
  readonly session = inject(SessionService);

  /** Bound from the `:id` route param (`withComponentInputBinding`). */
  readonly id = input.required<string>();

  readonly project = signal<Project | null>(null);

  /**
   * Whether load balancers may be added to THIS project, as the service reported it with the
   * row — not "does this person hold a role that writes something somewhere", which is the
   * question the permission model exists to stop asking.
   */
  readonly canWrite = computed(() => may(this.project(), CAN_WRITE));
  readonly loadBalancers = signal<LoadBalancer[]>([]);
  readonly error = signal<string | null>(null);

  readonly subtitle = () => {
    const p = this.project();
    if (!p) return null;
    const n = this.loadBalancers().length;
    return `${p.slug} · ${n === 1 ? '1 load balancer' : `${n} load balancers`}`;
  };

  ngOnChanges(): void {
    this.load();
  }

  private load(): void {
    const id = this.id();
    // By id, not by searching the list: the list pages, so "find it in /projects" would only
    // ever find projects on the first page.
    this.api.get<Project>(`/projects/${id}`).subscribe({
      next: (p) => this.project.set(p),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
    this.api.get<LoadBalancer[]>(`/projects/${id}/loadbalancers`).subscribe({
      next: (ps) => this.loadBalancers.set(ps),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }

  createLoadBalancer(): void {
    openNameSlugModal<LoadBalancer>(this.modal, 'New load balancer', {
      path: `/projects/${this.id()}/loadbalancers`,
      slugHint: 'Lower case, digits and hyphens — e.g. prod, edge-fra.',
    }).afterClose.subscribe((created?: LoadBalancer) => {
      if (created) this.load();
    });
  }

  async deleteLoadBalancer(lb: LoadBalancer): Promise<void> {
    const ok = await confirmAction(this.modal, {
      title: `Delete load balancer “${lb.slug}”?`,
      message: 'This cannot be undone. A load balancer that still owns domains or keys cannot be deleted.',
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.api.delete(`/loadbalancers/${lb.id}`).subscribe({
      next: () => this.load(),
      error: (err) => this.error.set(apiErrorMessage(err)),
    });
  }
}
