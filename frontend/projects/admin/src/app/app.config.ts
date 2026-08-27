import {
  ApplicationConfig,
  importProvidersFrom,
  inject,
  provideAppInitializer,
  provideZoneChangeDetection,
} from '@angular/core';
import { provideHttpClient, withInterceptors, withXsrfConfiguration } from '@angular/common/http';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { registerLocaleData } from '@angular/common';
import en from '@angular/common/locales/en';
import { en_US, provideNzI18n } from 'ng-zorro-antd/i18n';
import { provideNzIcons } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzMessageModule } from 'ng-zorro-antd/message';
import { LogoutOutline, ReloadOutline, UserOutline } from '@ant-design/icons-angular/icons';
import { routes } from './app.routes';
import { SessionService, authInterceptor } from '@acme/shared';

registerLocaleData(en);

/**
 * The staff application's providers.
 *
 * Deliberately a copy of the console's rather than a shared factory. The two
 * applications are separate so that their dependencies can diverge — a staff-only
 * screen pulling in a charting library should not enlarge what a customer
 * downloads — and a shared `appConfig` would quietly re-couple exactly that. What
 * IS shared is the part that must not diverge: the interceptor, the session
 * service and the XSRF convention, all from `@acme/shared`.
 *
 * The icon list is short for the same reason the console's is long: only what this
 * application's own templates draw. An unregistered `nz-icon` throws rather than
 * rendering a gap, so this list grows with the templates.
 */
export const appConfig: ApplicationConfig = {
  providers: [
    provideZoneChangeDetection({ eventCoalescing: true }),
    provideAnimationsAsync(),
    provideNzI18n(en_US),
    provideNzIcons([LogoutOutline, ReloadOutline, UserOutline]),
    importProvidersFrom(NzModalModule, NzMessageModule),
    provideRouter(routes, withComponentInputBinding()),
    provideHttpClient(
      withXsrfConfiguration({ cookieName: 'XSRF-TOKEN', headerName: 'X-XSRF-TOKEN' }),
      withInterceptors([authInterceptor]),
    ),
    provideAppInitializer(() => inject(SessionService).load()),
  ],
};
