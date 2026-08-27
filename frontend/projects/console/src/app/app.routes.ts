import { Routes } from '@angular/router';
import { MessageComponent, accountGuard } from '@acme/shared';
import { teamAccountGuard, usableAccountGuard } from './features/accounts/account-kind.guard';

/**
 * The shell's routes.
 *
 * There is no `sessions` and no effective-configuration screen. Both endpoints still exist and
 * are still checked server-side — dropping the screens removed navigation, not authority. They
 * were cut because neither is part of an operator's work: revoking a session is a support action
 * taken through the CLI on the rare day it is needed, and the effective configuration is
 * something you read from the service's own logs or `/config` while diagnosing, not something
 * you page through in a browser.
 *
 * Three guards, and they refuse three different things — which is the point, because a single
 * "may I be here" guard would have to answer them with one sentence:
 *
 *   accountGuard       no account is selected           → the picker (P19)
 *   usableAccountGuard the account is suspended         → the balance and checkout (C5)
 *   teamAccountGuard   a personal account has no such screen → the way to get one (C9, C11)
 *
 * `roleGuard` in core/ is used by no route, and is kept with its tests: what it checks — a named
 * role in the selected account — is still a thing, it is just not how any of these screens are
 * decided.
 */
export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'projects' },
  {
    // The shell a session with no account selected sees (design P19). Every session starts
    // there now, so this is the front door rather than an edge case, and it is deliberately
    // outside the account guard that sends people here.
    path: 'accounts',
    loadComponent: () =>
      import('./features/accounts/select-account.component').then((m) => m.SelectAccountComponent),
  },
  {
    // Buying a team account. Outside the account guard as well: it needs no account selected,
    // and it is one of the two doors into the same room (design C12) — the other is `Upgrade` in
    // a personal account's navigation, which points here.
    path: 'upgrade',
    loadComponent: () => import('./features/accounts/upgrade.component').then((m) => m.UpgradeComponent),
  },
  {
    // The only screen a suspended account has. Guarded on selection but NOT on usability, for
    // the obvious reason: it is the screen the unusable account is sent to.
    path: 'suspended',
    canActivate: [accountGuard],
    loadComponent: () => import('./features/accounts/suspended.component').then((m) => m.SuspendedComponent),
  },
  {
    path: 'projects',
    canActivate: [accountGuard, usableAccountGuard],
    loadComponent: () => import('./features/projects/project-list.component').then((m) => m.ProjectListComponent),
  },
  {
    path: 'projects/:id',
    canActivate: [accountGuard, usableAccountGuard],
    loadComponent: () => import('./features/projects/project-detail.component').then((m) => m.ProjectDetailComponent),
  },
  {
    path: 'loadbalancers/:id',
    canActivate: [accountGuard, usableAccountGuard],
    loadComponent: () => import('./features/loadbalancer/loadbalancer-detail.component').then((m) => m.LoadBalancerDetailComponent),
  },
  {
    // Deferred from the navigation, not deleted (task 6.2d, design C12): it is the only screen
    // that shows what the service is doing right now, which is worth having when something is
    // wrong and worth nothing the rest of the time. The route stays so the people who have
    // bookmarked it keep it, and so that putting it back is one line.
    path: 'challenges',
    canActivate: [accountGuard, usableAccountGuard],
    loadComponent: () => import('./features/challenges/challenges.component').then((m) => m.ChallengesComponent),
  },
  {
    path: 'audit',
    canActivate: [accountGuard, usableAccountGuard],
    loadComponent: () => import('./features/audit/audit.component').then((m) => m.AuditComponent),
  },
  {
    // What administers the account: its members, their roles, and its credits (task 6.5).
    path: 'settings',
    canActivate: [accountGuard, usableAccountGuard, teamAccountGuard],
    loadComponent: () => import('./features/settings/settings.component').then((m) => m.SettingsComponent),
  },
  {
    // Roles stopped being a top-level screen (task 6.5a). The redirect is for the links and
    // bookmarks that already exist; the screen itself moved, it did not go.
    path: 'roles',
    pathMatch: 'full',
    redirectTo: 'settings',
  },
  {
    path: 'forbidden',
    component: MessageComponent,
    data: {
      // Forbidden means a grant would lift it, and this wording has to keep saying so: the other
      // refusal — a screen this KIND of account does not have — is answered by the upgrade route
      // above, never here (design C11). It no longer mentions group membership, which granted
      // nothing after the identity-provider mapping was removed (C10).
      heading: 'Not available with your permissions',
      detail:
        'You are not granted this in the account you are working in. An unrestricted member of it can give you the grant.',
    },
  },
  { path: '**', component: MessageComponent },
];
