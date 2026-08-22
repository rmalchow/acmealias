import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { AuditComponent, isoDate } from './audit.component';
import { AuditEntry } from '../models';

function entry(over: Partial<AuditEntry> = {}): AuditEntry {
  return {
    id: 1,
    occurredAt: '2026-01-01T00:00:00Z',
    actorSubject: 'user-1',
    actorName: 'Reader',
    actorGroups: [],
    actorRoles: ['READ_ONLY'],
    actorSource: 'oidc',
    requestId: null,
    clientIp: null,
    action: 'project.create',
    objectType: 'project',
    objectId: 1,
    objectLabel: 'shop',
    projectId: 1,
    ...over,
  };
}

describe('isoDate', () => {
  // Pinned against toISOString() regressing this into a UTC shift: a late-evening local time
  // must not roll over to the next UTC day, and a near-midnight one must not roll back.
  it('formats the local calendar date, not the UTC one', () => {
    expect(isoDate(new Date(2026, 0, 5, 23, 30))).toBe('2026-01-05');
    expect(isoDate(new Date(2026, 11, 31, 0, 5))).toBe('2026-12-31');
  });

  it('pads single-digit months and days', () => {
    expect(isoDate(new Date(2026, 2, 4))).toBe('2026-03-04');
  });
});

describe('AuditComponent', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AuditComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('requests limit/offset and reads X-Total-Count for the pager', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();

    const req = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(req.request.params.get('limit')).toBe('50');
    expect(req.request.params.get('offset')).toBe('0');
    req.flush([entry()], { headers: { 'X-Total-Count': '412' } });

    expect(fixture.componentInstance.total()).toBe(412);
  });

  it('falls back to the row count when X-Total-Count is missing', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/audit').flush([entry(), entry({ id: 2 })]);
    expect(fixture.componentInstance.total()).toBe(2);
  });

  it('sends the date range as local YYYY-MM-DD from/to, not toISOString()', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/audit').flush([]);

    fixture.componentInstance.range = [new Date(2026, 0, 1, 23, 0), new Date(2026, 0, 31, 1, 0)];
    fixture.componentInstance.reload();

    const req = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(req.request.params.get('from')).toBe('2026-01-01');
    expect(req.request.params.get('to')).toBe('2026-01-31');
    req.flush([]);
  });

  it('omits from/to when no range is set', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    const req = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(req.request.params.has('from')).toBeFalse();
    expect(req.request.params.has('to')).toBeFalse();
    req.flush([]);
  });

  it('applying a filter resets to page one', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/audit').flush([entry()]);

    fixture.componentInstance.onQueryParams({ pageIndex: 5, pageSize: 50, sort: [], filter: [] });
    const paged = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(paged.request.params.get('offset')).toBe('200');
    paged.flush([entry()], { headers: { 'X-Total-Count': '1000' } });

    fixture.componentInstance.actor = 'someone';
    fixture.componentInstance.reload();
    const filtered = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(filtered.request.params.get('offset')).toBe('0');
    expect(filtered.request.params.get('actor')).toBe('someone');
    expect(fixture.componentInstance.pageIndex()).toBe(1);
    filtered.flush([]);
  });

  // The two selects call reload() from (ngModelChange), so they must land on page one
  // for the same reason the text filters do: page 5 of the old result set means nothing
  // in the new one, and an empty page reads as "nothing happened".
  it('sends objectType and actionClass, from page one', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/audit').flush([entry()]);

    fixture.componentInstance.onQueryParams({ pageIndex: 5, pageSize: 50, sort: [], filter: [] });
    http.expectOne((r) => r.url === '/api/v1/audit').flush([entry()], {
      headers: { 'X-Total-Count': '1000' },
    });

    fixture.componentInstance.objectType = 'api_key';
    fixture.componentInstance.reload();
    const byType = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(byType.request.params.get('objectType')).toBe('api_key');
    expect(byType.request.params.get('offset')).toBe('0');
    byType.flush([entry()], { headers: { 'X-Total-Count': '1000' } });

    fixture.componentInstance.onQueryParams({ pageIndex: 3, pageSize: 50, sort: [], filter: [] });
    http.expectOne((r) => r.url === '/api/v1/audit').flush([entry()], {
      headers: { 'X-Total-Count': '1000' },
    });

    fixture.componentInstance.actionClass = 'credential';
    fixture.componentInstance.reload();
    const byClass = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(byClass.request.params.get('actionClass')).toBe('credential');
    expect(byClass.request.params.get('offset')).toBe('0');
    expect(fixture.componentInstance.pageIndex()).toBe(1);
    byClass.flush([]);
  });

  it('omits both new filters until they are set, and clear() drops them again', () => {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    const first = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(first.request.params.has('objectType')).toBeFalse();
    expect(first.request.params.has('actionClass')).toBeFalse();
    first.flush([]);

    fixture.componentInstance.objectType = 'domain';
    fixture.componentInstance.actionClass = 'config';
    fixture.componentInstance.clear();
    const cleared = http.expectOne((r) => r.url === '/api/v1/audit');
    expect(cleared.request.params.has('objectType')).toBeFalse();
    expect(cleared.request.params.has('actionClass')).toBeFalse();
    cleared.flush([]);
  });
});
