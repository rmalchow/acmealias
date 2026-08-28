import { inject } from '@angular/core';
import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { catchError, throwError } from 'rxjs';
import { SessionService } from './session.service';

/**
 * Every `401` from the management API means there is no session.
 *
 * - Once we HAD one (it expired or was revoked mid-use): do NOT navigate away under the user's
 *   hands. Raise `expired`, which the shell turns into a re-authentication prompt, so the
 *   failure is surfaced rather than silent.
 * - Before we ever had one: do nothing here. `SessionService.signedOut` becomes true when `/me`
 *   answers, and the shell renders a sign-in screen on it.
 *
 * That second case used to call `signIn()`, which handed the browser straight to the identity
 * provider. It was removed deliberately — see `SignedOutComponent`. Doing it from an INTERCEPTOR
 * made it worse than the same redirect done deliberately: any request that happened to 401 could
 * navigate the window away, so the trigger was whichever call lost the race rather than anything
 * the person did.
 */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const session = inject(SessionService);
  return next(req).pipe(
    catchError((err: unknown) => {
      if (err instanceof HttpErrorResponse && err.status === 401 && session.me()) {
        session.expired.set(true);
      }
      return throwError(() => err);
    }),
  );
};
