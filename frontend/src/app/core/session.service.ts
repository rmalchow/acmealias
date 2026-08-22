import { computed, inject, Injectable, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Router } from '@angular/router';
import { ApiService } from './api.service';
import { firstValueFrom } from 'rxjs';

/** The four global roles (D10). There is no per-project scoping. */
export type Role = 'ADMIN' | 'CONFIG_ADMIN' | 'KEY_ROTATION' | 'READ_ONLY';

/** Shape of `GET /api/v1/me`. */
export interface Me {
  displayName: string;
  groups: string[];
  roles: Role[];
  /**
   * The account this session is acting in, or null when none is selected — which is a normal
   * state, not an error: someone who belongs to several accounts and has not yet said which.
   * A single-account deployment selects it at sign-in and never shows a picker.
   */
  selectedAccountId: string | null;
}

/** Where the browser-side of the BFF sign-in starts; the backend 302s on to the IdP. */
export const LOGIN_URL = '/api/v1/auth/login';
export const LOGOUT_URL = '/api/v1/auth/logout';

const RETURN_TO = 'acmeservice.returnTo';

function currentUrl(): string {
  return location.pathname + location.search + location.hash;
}

@Injectable({ providedIn: 'root' })
export class SessionService {
  private readonly http = inject(HttpClient);
  private readonly router = inject(Router);
  private readonly api = inject(ApiService);

  /** Null until `/api/v1/me` has answered. */
  readonly me = signal<Me | null>(null);
  /** Set by the auth interceptor when a session dies mid-use; the shell prompts on it. */
  readonly expired = signal(false);
  readonly roles = computed<readonly Role[]>(() => this.me()?.roles ?? []);

  /**
   * Client-side role checks HIDE controls. They are not the enforcement: every
   * management route re-checks the role server-side (D11).
   */
  hasAnyRole(allowed: readonly Role[]): boolean {
    return allowed.length === 0 || this.roles().some((r) => allowed.includes(r));
  }

  /** Loaded once at startup. A 401 is handled by the auth interceptor, which sends us to sign-in. */
  async load(): Promise<void> {
    try {
      const me = await firstValueFrom(this.http.get<Me>('/api/v1/me'));
      this.me.set(me);
      // Every account-scoped request names its account in the path, so the client has to know
      // which one before it can make any of them.
      this.api.setAccount(me.selectedAccountId);
    } catch {
      return;
    }
    this.expired.set(false);
    await this.returnToRemembered();
  }

  /** Remembers where the user wanted to be, then hands the browser to the backend's login route. */
  signIn(): void {
    remember(currentUrl());
    location.assign(LOGIN_URL);
  }

  signOut(): void {
    remember(null);
    location.assign(LOGOUT_URL);
  }

  /** After sign-in the backend redirects to `/`; this puts the user back on their deep link. */
  private async returnToRemembered(): Promise<void> {
    let url: string | null = null;
    try {
      url = sessionStorage.getItem(RETURN_TO);
      sessionStorage.removeItem(RETURN_TO);
    } catch {
      // ponytail: sessionStorage can be unavailable (privacy modes). Losing the deep link is
      // the acceptable failure here; sign-in itself still works.
    }
    if (url && url !== currentUrl()) {
      await this.router.navigateByUrl(url);
    }
  }
}

function remember(url: string | null): void {
  try {
    if (url === null) sessionStorage.removeItem(RETURN_TO);
    else sessionStorage.setItem(RETURN_TO, url);
  } catch {
    /* see above */
  }
}
