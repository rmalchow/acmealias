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
 * A single-account deployment selects at sign-in and never reaches here, which is what keeps the
 * internal edition's behaviour unchanged.
 */
export const accountGuard: CanActivateFn = () => {
  const session = inject(SessionService);
  const router = inject(Router);
  if (session.me()?.selectedAccountId) return true;
  return router.parseUrl('/accounts');
};
