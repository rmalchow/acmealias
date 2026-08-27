import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { SessionService } from './session.service';

/**
 * Sends a session with no account selected to the picker (design P19).
 *
 * The service refuses account-scoped requests from that state with a distinct answer rather than
 * an empty result, so without this guard every screen would render its own version of the same
 * error. One redirect, to the one screen that can resolve it.
 *
 * This is the most travelled path in the console, not the least. Sign-in used to select the one
 * account there was, so the state was unreachable; nothing selects an account now (design C10),
 * so every session arrives here once and the picker it leads to always has at least the user's
 * personal account in it (C1).
 */
export const accountGuard: CanActivateFn = () => {
  const session = inject(SessionService);
  const router = inject(Router);
  if (session.me()?.selectedAccountId) return true;
  return router.parseUrl('/accounts');
};
