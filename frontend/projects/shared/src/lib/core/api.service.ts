import { inject, Injectable } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { map } from 'rxjs';

type Params = Record<string, string | number | boolean>;

/** One page of a list endpoint: the rows, and how many rows the filter matched in total. */
export interface Page<T> {
  items: T[];
  total: number;
}

/**
 * Nothing but the path prefix and generics — the interceptor does auth, `HttpClient` does XSRF.
 *
 * There are two prefixes now, and the difference matters. Account-scoped resources live under
 * `/api/v1/accounts/{id}/…`, and the id is named in the path even though the session already
 * knows it: if the target came only from the session, a forged request would act in whichever
 * account happened to be selected and the caller would never have had to know which. The server
 * refuses a mismatch. Everything reachable with NO account selected — the profile, the list of
 * accounts, creating one, selecting one — uses `root()` instead.
 */
@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);
  private readonly root = '/api/v1';

  /**
   * The account every scoped call addresses. Set from `/api/v1/me` at start-up and on every
   * switch; null until then, and a scoped call made while it is null is a bug in the caller
   * rather than something to paper over with a guess.
   */
  private accountId: string | null = null;

  setAccount(id: string | null): void {
    this.accountId = id;
  }

  currentAccount(): string | null {
    return this.accountId;
  }

  private get base(): string {
    if (this.accountId === null) {
      throw new Error(
        'ApiService: an account-scoped request was made with no account selected. ' +
          'Use the root* methods for /me, /accounts and account selection.',
      );
    }
    return `${this.root}/accounts/${this.accountId}`;
  }

  /** Requests that are not about anything inside an account. */
  rootGet<T>(path: string, params?: Params) {
    return this.http.get<T>(this.root + path, { params: new HttpParams({ fromObject: params ?? {} }) });
  }
  rootPost<T>(path: string, body?: unknown) {
    return this.http.post<T>(this.root + path, body ?? {});
  }

  get<T>(path: string, params?: Params) {
    return this.http.get<T>(this.base + path, { params: new HttpParams({ fromObject: params ?? {} }) });
  }
  /**
   * A list endpoint with paging. The body is the plain array it has always been; the size of the
   * full filtered result set arrives in `X-Total-Count`, which is why this reads the whole
   * response rather than just its body.
   *
   * A missing header means an endpoint that does not page — then the page IS the total, and the
   * pager below the table correctly shows a single page.
   */
  list<T>(path: string, params?: Params) {
    return this.http
      .get<T[]>(this.base + path, {
        params: new HttpParams({ fromObject: params ?? {} }),
        observe: 'response',
      })
      .pipe(
        map((res): Page<T> => {
          const items = res.body ?? [];
          const header = res.headers.get('X-Total-Count');
          const total = header === null ? items.length : Number(header);
          return { items, total: Number.isFinite(total) ? total : items.length };
        }),
      );
  }

  post<T>(path: string, body?: unknown) {
    return this.http.post<T>(this.base + path, body ?? {});
  }
  put<T>(path: string, body?: unknown) {
    return this.http.put<T>(this.base + path, body ?? {});
  }
  patch<T>(path: string, body?: unknown) {
    return this.http.patch<T>(this.base + path, body ?? {});
  }
  delete<T>(path: string) {
    return this.http.delete<T>(this.base + path);
  }
}

/**
 * The one error-surfacing path: whatever reason the service gave for a rejection reaches the
 * user verbatim, because the server is the authority on validation and its message names the
 * rule (or the conflicting load balancer) that the client cannot know about.
 *
 * The management API answers with one JSON error shape; `message` is read first, `error` second
 * so a plain-string or Echo-default body still shows something useful.
 */
export function apiErrorMessage(err: unknown): string {
  if (!(err instanceof HttpErrorResponse)) return String(err);
  const body: unknown = err.error;
  const reason =
    typeof body === 'string'
      ? body
      : ((body as { message?: unknown; error?: unknown } | null)?.message ??
        (body as { message?: unknown; error?: unknown } | null)?.error);
  if (typeof reason === 'string' && reason.trim()) return reason.trim();
  return err.status ? `${err.status} ${err.statusText}`.trim() : 'The service could not be reached.';
}
