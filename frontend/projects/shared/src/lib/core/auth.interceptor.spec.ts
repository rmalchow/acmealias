import { TestBed } from '@angular/core/testing';
import { ApiService } from './api.service';
import { provideRouter } from '@angular/router';
import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { authInterceptor } from './auth.interceptor';
import { SessionService } from './session.service';

describe('authInterceptor', () => {
  let http: HttpClient;
  let backend: HttpTestingController;
  let session: SessionService;
  let signIn: jasmine.Spy;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting(),
      ],
    });
    http = TestBed.inject(HttpClient);
    backend = TestBed.inject(HttpTestingController);
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');
    session = TestBed.inject(SessionService);
    // signIn hands the browser to the backend's login route; karma must not actually navigate.
    signIn = spyOn(session, 'signIn');
  });

  afterEach(() => backend.verify());

  function fail401(url = '/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/projects') {
    const failed = jasmine.createSpy('error');
    http.get(url).subscribe({ error: failed });
    backend.expectOne(url).flush({ message: 'no' }, { status: 401, statusText: 'Unauthorized' });
    return failed;
  }

  it('goes to sign-in when there is no session yet, remembering the URL', () => {
    const failed = fail401();
    expect(signIn).toHaveBeenCalled();
    expect(session.expired()).toBeFalse();
    expect(failed).toHaveBeenCalled();
  });

  it('prompts instead of navigating when a session expires mid-use', () => {
    session.me.set({ displayName: 'Test', groups: [], roles: ['READ_ONLY'] , selectedAccountId: 'acct-1', permissions: [] });
    fail401();
    expect(session.expired()).toBeTrue();
    expect(signIn).not.toHaveBeenCalled();
  });

  it('leaves other failures alone', () => {
    const failed = jasmine.createSpy('error');
    http.get('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/projects').subscribe({ error: failed });
    backend.expectOne('/api/v1/accounts/00000000-0000-7000-8000-0000000000a1/projects').flush({ message: 'nope' }, { status: 403, statusText: 'Forbidden' });
    expect(signIn).not.toHaveBeenCalled();
    expect(session.expired()).toBeFalse();
    expect(failed).toHaveBeenCalled();
  });
});
