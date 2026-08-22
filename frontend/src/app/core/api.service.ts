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
 * Nothing but the `/api/v1` prefix and generics — the interceptor does auth, `HttpClient` does
 * XSRF. Wiring this to the real backend is a matter of naming paths at the call site.
 */
@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);
  private readonly base = '/api/v1';

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
