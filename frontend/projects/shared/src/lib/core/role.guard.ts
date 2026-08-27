import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { Role, SessionService } from './session.service';

/**
 * Keeps a user out of a route their roles do not cover — a nicety, not a control. The server
 * checks the same permission on every request the route would make (D11).
 */
export function roleGuard(...allowed: Role[]): CanActivateFn {
  return () => {
    const session = inject(SessionService);
    const router = inject(Router);
    return session.hasAnyRole(allowed) || router.parseUrl('/forbidden');
  };
}
