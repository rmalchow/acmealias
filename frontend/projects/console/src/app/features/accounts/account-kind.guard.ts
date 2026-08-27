import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { AccountsService } from './accounts.service';

/**
 * Keeps a screen a TEAM account has out of a personal one (design C9, C11).
 *
 * This is not a permission check and must not be read as one. The service refuses the roster
 * routes in a personal account with a 409 that says the account cannot have members — not a 403
 * — precisely so a client can tell "this does not exist here" from "you may not". The guard is
 * what stops a deep link from rendering that refusal as if it were a wall the user could get
 * past by asking someone.
 *
 * It sends the user to `/upgrade` rather than to `/forbidden` for the same reason: the remedy is
 * a different kind of account, and there is nobody to ask for a grant in an account of one.
 *
 * Client-side and advisory, like every guard here. The enforcement is in the service, where a
 * request that arrives anyway is refused (design D11).
 */
export const teamAccountGuard: CanActivateFn = (_route, state) => {
  const accounts = inject(AccountsService);
  const router = inject(Router);
  const account = accounts.current();
  if (!account) return router.parseUrl('/accounts');
  if (account.kind === 'team') return true;
  // The screen that was asked for travels with the redirect, so the page it lands on can say
  // which capability is missing instead of leaving somebody to work out why they moved. A
  // redirect that arrives silently reads as a bug in the console.
  return router.createUrlTree(['/upgrade'], { queryParams: { from: state.url } });
};

/**
 * Sends the member of a suspended account to the one screen a suspended account has (design C5).
 *
 * A suspended team account refuses every scoped route with 402, so without this every screen
 * would render its own version of the same payment refusal. One redirect, to the one screen that
 * can resolve it — the same shape as the account guard, for the same reason.
 *
 * Only the owner can reach a suspended account at all: selection is refused for anybody else
 * (C4). The guard does not check ownership, because what it is deciding is which screen to show,
 * and the answer is the same one either way — it says different things to the two of them.
 */
export const usableAccountGuard: CanActivateFn = () => {
  const accounts = inject(AccountsService);
  const router = inject(Router);
  const account = accounts.current();
  if (!account) return router.parseUrl('/accounts');
  return !account.suspended || router.parseUrl('/suspended');
};
