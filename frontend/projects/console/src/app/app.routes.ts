import { Routes } from '@angular/router';
import { MessageComponent, accountGuard } from '@acme/shared';
/**
 * The shell's routes.
 *
 * There is no `sessions` and no `settings` screen. Both endpoints still exist and are still
 * role-gated server-side (`GET /sessions` and `DELETE /sessions/...` need ADMIN, `GET /config`
 * needs a signed-in user) — dropping the screens removed navigation, not authority. They were
 * cut because neither is part of an operator's work: revoking a session is a support action
 * taken through the CLI on the rare day it is needed, and the effective configuration is
 * something you read from the service's own logs or `/config` while diagnosing, not something
 * you page through in a browser.
 *
 * `roleGuard` in core/ is therefore currently unused by any route. It is kept, with its tests:
 * the role model has not changed, and the next role-gated screen needs exactly it.
 */
export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'projects' },
  {
    // The shell a session with no account selected sees (design P19). It is the
    // only screen reachable from that state, so it is deliberately outside the
    // account guard that sends people here.
    path: 'accounts',
    loadComponent: () =>
      import('./features/accounts/select-account.component').then((m) => m.SelectAccountComponent),
  },
  {
    path: 'projects',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/projects/project-list.component').then((m) => m.ProjectListComponent),
  },
  {
    path: 'projects/:id',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/projects/project-detail.component').then((m) => m.ProjectDetailComponent),
  },
  {
    path: 'loadbalancers/:id',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/loadbalancer/loadbalancer-detail.component').then((m) => m.LoadBalancerDetailComponent),
  },
  {
    path: 'challenges',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/challenges/challenges.component').then((m) => m.ChallengesComponent),
  },
  {
    path: 'audit',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/audit/audit.component').then((m) => m.AuditComponent),
  },
  {
    // Roles and membership: who may do what inside this account.
    path: 'roles',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/roles/roles.component').then((m) => m.RolesComponent),
  },
  {
    path: 'forbidden',
    component: MessageComponent,
    data: {
      heading: 'Not available with your roles',
      detail:
        'Your account does not hold a role that covers this page. Ask an administrator about your group membership.',
    },
  },
  { path: '**', component: MessageComponent },
];
