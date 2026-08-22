import { fakeAsync, TestBed, tick } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { ChallengesComponent } from './challenges.component';
import { ChallengeEntry } from '../models';

function entry(over: Partial<ChallengeEntry> = {}): ChallengeEntry {
  return {
    id: 1,
    aliasLabel: 'alias-1',
    challengeName: '_acme-challenge.shop.example.com',
    value: 'abc',
    publishedBy: 'key-1',
    createdAt: '2026-01-01T00:00:00Z',
    expiresAt: '2026-01-01T00:05:00Z',
    ageSeconds: 12,
    ...over,
  };
}

describe('ChallengesComponent', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ChallengesComponent],
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
    const fixture = TestBed.createComponent(ChallengesComponent);
    fixture.detectChanges();

    const req = http.expectOne((r) => r.url === '/api/v1/challenges');
    expect(req.request.params.get('limit')).toBe('25');
    expect(req.request.params.get('offset')).toBe('0');
    req.flush([entry()], { headers: { 'X-Total-Count': '9' } });

    expect(fixture.componentInstance.total()).toBe(9);
  });

  it('falls back to the row count when X-Total-Count is missing', () => {
    const fixture = TestBed.createComponent(ChallengesComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/challenges').flush([entry(), entry({ id: 2 })]);
    expect(fixture.componentInstance.total()).toBe(2);
  });

  it('changing the filter resets to page one', fakeAsync(() => {
    const fixture = TestBed.createComponent(ChallengesComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/challenges').flush([entry()]);

    fixture.componentInstance.onQueryParams({ pageIndex: 2, pageSize: 25, sort: [], filter: [] });
    const paged = http.expectOne((r) => r.url === '/api/v1/challenges');
    expect(paged.request.params.get('offset')).toBe('25');
    // A total large enough that page 2 stays valid — otherwise nz-table corrects itself back to
    // page 1 on the next change detection and emits its own (nzQueryParams), which would land a
    // second, spurious request and make this test about the table widget instead of the component.
    paged.flush([entry()], { headers: { 'X-Total-Count': '100' } });

    fixture.componentInstance.query = 'shop';
    fixture.componentInstance.onQueryChange();
    tick(250);
    const filtered = http.expectOne((r) => r.url === '/api/v1/challenges');
    expect(filtered.request.params.get('offset')).toBe('0');
    expect(fixture.componentInstance.pageIndex()).toBe(1);
    filtered.flush([]);
  }));

  it('debounces typing instead of firing a request per keystroke', fakeAsync(() => {
    const fixture = TestBed.createComponent(ChallengesComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/challenges').flush([entry()]);

    fixture.componentInstance.query = 's';
    fixture.componentInstance.onQueryChange();
    tick(100);
    fixture.componentInstance.query = 'shop';
    fixture.componentInstance.onQueryChange();
    tick(200);
    http.expectNone((r) => r.url === '/api/v1/challenges');

    tick(50);
    const req = http.expectOne((r) => r.url === '/api/v1/challenges');
    expect(req.request.params.get('q')).toBe('shop');
    req.flush([]);
  }));

  it('shows the quiet empty state when nothing is answerable and no filter is set', () => {
    const fixture = TestBed.createComponent(ChallengesComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/challenges').flush([]);
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('No challenge is currently answerable');
    expect(fixture.nativeElement.textContent).not.toContain('No live challenge matches this filter');
  });

  it('shows the filtered empty state, distinct from the quiet one, when a filter matches nothing', () => {
    const fixture = TestBed.createComponent(ChallengesComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/challenges').flush([entry()]);
    fixture.detectChanges();

    fixture.componentInstance.query = 'nomatch';
    fixture.componentInstance.load();
    http.expectOne((r) => r.url === '/api/v1/challenges').flush([]);
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('No live challenge matches this filter');
    expect(fixture.nativeElement.textContent).not.toContain('No challenge is currently answerable');
  });
});
