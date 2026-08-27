import { Routes } from '@angular/router';

/**
 * The staff console has one screen, and it says so.
 *
 * There is no `accountGuard` here, and that absence is the whole difference
 * between the two applications: a member of staff does not act *inside* an
 * account, they act *on* accounts. When `staff-plane` adds screens, what gates
 * them is a staff permission, not an account selection.
 */
export const routes: Routes = [
  {
    path: '',
    loadComponent: () => import('./overview.component').then((m) => m.OverviewComponent),
  },
  { path: '**', redirectTo: '' },
];
