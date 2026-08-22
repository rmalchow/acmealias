import {
  ApplicationConfig,
  importProvidersFrom,
  inject,
  provideAppInitializer,
  provideZoneChangeDetection,
} from '@angular/core';
import { provideHttpClient, withInterceptors, withXsrfConfiguration } from '@angular/common/http';
import { provideRouter, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { registerLocaleData } from '@angular/common';
import en from '@angular/common/locales/en';
import { en_US, provideNzI18n } from 'ng-zorro-antd/i18n';
import { provideNzIcons } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzMessageModule } from 'ng-zorro-antd/message';
import {
  CheckOutline,
  DeleteOutline,
  EditOutline,
  CloseCircleOutline,
  CopyOutline,
  DownOutline,
  DownloadOutline,
  FilterOutline,
  LogoutOutline,
  PlusOutline,
  ReloadOutline,
  SearchOutline,
  UserOutline,
} from '@ant-design/icons-angular/icons';
import { routes } from './app.routes';
import { authInterceptor } from './core/auth.interceptor';
import { SessionService } from './core/session.service';

registerLocaleData(en);

export const appConfig: ApplicationConfig = {
  providers: [
    provideZoneChangeDetection({ eventCoalescing: true }),
    // ng-zorro's overlays — modal, dropdown, date picker — animate.
    provideAnimationsAsync(),
    // Dates, number formats and the built-in strings of the table pager and the range picker.
    provideNzI18n(en_US),
    // Icons are registered explicitly, one by one.
    //
    // Two reasons, and the first is not optional: an `<span nz-icon>` whose icon was never
    // registered THROWS, and the exception surfaces as a broken screen rather than a missing
    // glyph — which is exactly how the Projects screen broke the first time. The second is that
    // an unregistered icon is fetched at runtime from ant's CDN, and this console has to work on
    // a host with no route to the internet.
    //
    // Only the icons the console itself uses are here; the ones ng-zorro's own components draw —
    // the pager's arrows, the date picker's calendar, the modal's close — come with those
    // components. Adding an `nz-icon` to a template means adding it to this list.
    provideNzIcons([
      CheckOutline,
      DeleteOutline,
      EditOutline,
      CloseCircleOutline,
      CopyOutline,
      DownOutline,
      DownloadOutline,
      FilterOutline,
      LogoutOutline,
      PlusOutline,
      ReloadOutline,
      SearchOutline,
      UserOutline,
    ]),
    // NzModalService and NzMessageService are NOT `providedIn: 'root'` — they come with their
    // modules. A component that injects one without this gets a NullInjectorError at
    // construction, which presents as a blank screen rather than as a missing dialog, so this is
    // load-bearing rather than tidy. Provided once here so both are app-wide singletons: two
    // message containers would stack toasts on top of each other.
    importProvidersFrom(NzModalModule, NzMessageModule),
    provideRouter(routes, withComponentInputBinding(), withRouterConfig({ paramsInheritanceStrategy: 'always' })),
    provideHttpClient(
      // The backend issues an `XSRF-TOKEN` cookie (not HttpOnly, so this can read it) and
      // requires `X-XSRF-TOKEN` on state-changing requests — Angular's native convention, so
      // this is configuration rather than code (D10).
      withXsrfConfiguration({ cookieName: 'XSRF-TOKEN', headerName: 'X-XSRF-TOKEN' }),
      withInterceptors([authInterceptor]),
    ),
    // One `/api/v1/me` before the first screen renders, so role-gated navigation and guards
    // never have to reason about "roles not known yet".
    provideAppInitializer(() => inject(SessionService).load()),
  ],
};
