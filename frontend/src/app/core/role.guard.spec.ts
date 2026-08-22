import { TestBed } from '@angular/core/testing';
import { provideRouter, Router, RouterStateSnapshot, ActivatedRouteSnapshot } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { roleGuard } from './role.guard';
import { Role, SessionService } from './session.service';

function run(guard: ReturnType<typeof roleGuard>) {
  return TestBed.runInInjectionContext(() =>
    guard({} as ActivatedRouteSnapshot, {} as RouterStateSnapshot),
  );
}

describe('roleGuard', () => {
  let session: SessionService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting()],
    });
    session = TestBed.inject(SessionService);
  });

  function signedInWith(...roles: Role[]) {
    session.me.set({ displayName: 'Test', groups: [], roles, selectedAccountId: 'acct-1' });
  }

  it('lets a holder of one of the allowed roles through', () => {
    signedInWith('CONFIG_ADMIN');
    expect(run(roleGuard('ADMIN', 'CONFIG_ADMIN'))).toBeTrue();
  });

  it('sends everyone else to /forbidden', () => {
    signedInWith('READ_ONLY');
    const result = run(roleGuard('ADMIN'));
    expect(String(result)).toBe(String(TestBed.inject(Router).parseUrl('/forbidden')));
  });

  it('lets any signed-in user through a route that names no role', () => {
    signedInWith('READ_ONLY');
    expect(run(roleGuard())).toBeTrue();
  });
});
