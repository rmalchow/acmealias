import { inject } from '@angular/core';
import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { catchError, throwError } from 'rxjs';
import { SessionService } from './session.service';

/**
 * Every `401` from the management API means the session is gone.
 *
 * - Before we ever had one (first load, or a deep link opened cold): go straight to sign-in,
 *   remembering the requested URL so the user lands back on it afterwards.
 * - Once we had one (it expired or was revoked mid-use): do NOT navigate away under the user's
 *   hands. Raise `expired`, which the shell turns into a re-authentication prompt, so the
 *   failure is surfaced rather than silent.
 */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const session = inject(SessionService);
  return next(req).pipe(
    catchError((err: unknown) => {
      if (err instanceof HttpErrorResponse && err.status === 401) {
        if (session.me()) session.expired.set(true);
        else session.signIn();
      }
      return throwError(() => err);
    }),
  );
};
