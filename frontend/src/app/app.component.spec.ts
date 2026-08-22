import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { AppComponent } from './app.component';
import { SessionService } from './core/session.service';

describe('AppComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AppComponent],
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        // The expiry prompt is an nz-modal, and ng-zorro's overlays carry animation metadata:
        // without an animations provider Angular refuses to render them (NG05105).
        provideNoopAnimations(),
      ],
    }).compileComponents();
  });

  it('offers every screen to a read-only user', () => {
    // Nothing in the navigation is role-gated any more: the two screens that were (sessions and
    // effective configuration) are gone from the console entirely. If a role-gated screen comes
    // back, this is the test that has to change with it.
    TestBed.inject(SessionService).me.set({ displayName: 'Reader', groups: [], roles: ['READ_ONLY'] });
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    const labels = fixture.componentInstance.nav().map((i) => i.label);
    expect(labels).toEqual(['Projects', 'Live challenges', 'Audit log']);
  });

  it('prompts when the session has expired', () => {
    const session = TestBed.inject(SessionService);
    session.me.set({ displayName: 'Reader', groups: [], roles: ['READ_ONLY'] });
    session.expired.set(true);
    const fixture = TestBed.createComponent(AppComponent);
    fixture.detectChanges();
    // The prompt is an nz-modal, and ng-zorro renders overlays into the CDK overlay container on
    // document.body — not inside the component's own element, which is where this test used to
    // look.
    expect(document.body.textContent).toContain('session has expired');
  });
});
