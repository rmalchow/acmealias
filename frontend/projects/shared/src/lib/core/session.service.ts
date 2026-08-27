import { computed, inject, Injectable, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Router } from '@angular/router';
import { ApiService } from './api.service';
import { firstValueFrom } from 'rxjs';

/**
 * The name of a role held IN THE SELECTED ACCOUNT.
 *
 * It was a union of four literals — the global vocabulary configured by
 * `ACME_ADMIN_GROUP` and its siblings. There is no such vocabulary any more
 * (design C10): the four names survive as the default roles seeded into each new
 * team account, where the account's owner may rename, edit or delete them, so
 * any fixed set of literals here would be a claim the service no longer makes.
 * A personal account has no roles at all (C9) and this is empty there.
 */
export type Role = string;

/** Shape of `GET /api/v1/me`. */
export interface Me {
  /**
   * The identity provider's own identifier for this person — stable across a
   * display-name change, and the value the audit trail attributes actions to.
   * The service has always answered with it; only the staff console shows it.
   */
  subject: string;
  displayName: string;
  groups: string[];
  roles: Role[];
  /**
   * What this person may do where no object is in hand — creating the first project in an empty
   * account, for instance. Per-object questions are answered per object, on the row.
   */
  permissions: string[];
  /**
   * The account this session is acting in, or null when none is selected.
   *
   * That is now the state EVERY session starts in (design P19, C10): sign-in no longer selects
   * an account, so the picker is the front door rather than an edge case — and it is never
   * empty, because a user's personal account is materialized on first sign-in (C1). "No account
   * selected" therefore means "you have not yet said which", never "you have nothing".
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
