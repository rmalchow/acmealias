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
  ClusterOutline,
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
  ShoppingCartOutline,
  TeamOutline,
  UserOutline,
  WalletOutline,
} from '@ant-design/icons-angular/icons';
import { routes } from './app.routes';
import { SessionService, authInterceptor } from '@acme/shared';
import { AccountsService } from './features/accounts/accounts.service';
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
      // `cluster` was never registered here — which never threw only because the switcher used to
      // render nothing below two accounts. It renders always now (design C12), so this line is
      // what stands between the first sign-in and a broken screen. It is no longer the account
      // glyph (that is `team`); what still draws it is "Leave this account" in the user menu.
      ClusterOutline,
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
      ShoppingCartOutline,
      // The team account's glyph, in the picker's cards and the switcher (accountIcon in shared).
      TeamOutline,
      UserOutline,
      WalletOutline,
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
    // Two requests before the first screen renders, in this order and both blocking.
    //
    // `/api/v1/me` says who is signed in and which account is selected, and the guards would
    // otherwise have to reason about "not known yet". `/api/v1/accounts` says what that account
    // IS — its kind, its balance, whether it is suspended — and the navigation is computed from
    // exactly that (design C11): a personal account gets Projects and Upgrade, a team account
    // gets Projects, Audit and Settings. Loading the list from a screen's ngOnInit, as the
    // switcher used to, would render the bar in one shape and then change it under the pointer.
    //
    // Neither is allowed to stop the application from starting. A signed-out caller gets a 401
    // here, which the auth interceptor turns into sign-in; a failure to reach the service is a
    // screen that says so, not a blank page.
    provideAppInitializer(() => {
      const session = inject(SessionService);
      const accounts = inject(AccountsService);
      return session.load().then(
        () => accounts.load().catch(() => undefined),
        () => undefined,
      );
    }),
  ],
};
