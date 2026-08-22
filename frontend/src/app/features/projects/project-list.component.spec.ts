import { fakeAsync, TestBed, tick } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { NzModalService } from 'ng-zorro-antd/modal';
import { ProjectListComponent } from './project-list.component';
import { Project } from '../models';

function project(over: Partial<Project> = {}): Project {
  return { id: 1, slug: 'shop', name: 'Shop', createdAt: '2026-01-01T00:00:00Z', ...over };
}

describe('ProjectListComponent', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ProjectListComponent],
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
        NzModalService,
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('requests limit/offset from the page state and reads X-Total-Count for the pager', () => {
    const fixture = TestBed.createComponent(ProjectListComponent);
    fixture.detectChanges();

    const req = http.expectOne((r) => r.url === '/api/v1/projects');
    expect(req.request.params.get('limit')).toBe('25');
    expect(req.request.params.get('offset')).toBe('0');
    req.flush([project()], { headers: { 'X-Total-Count': '137' } });

    expect(fixture.componentInstance.total()).toBe(137);
  });

  it('falls back to the row count when X-Total-Count is missing', () => {
    const fixture = TestBed.createComponent(ProjectListComponent);
    fixture.detectChanges();

    const req = http.expectOne((r) => r.url === '/api/v1/projects');
    req.flush([project(), project({ id: 2 })]);

    expect(fixture.componentInstance.total()).toBe(2);
  });

  it('changing the filter resets to page one', fakeAsync(() => {
    const fixture = TestBed.createComponent(ProjectListComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/projects').flush([project()]);

    // Page forward, as if the operator had paged into the old result set.
    fixture.componentInstance.onQueryParams({ pageIndex: 3, pageSize: 25, sort: [], filter: [] });
    const paged = http.expectOne((r) => r.url === '/api/v1/projects');
    expect(paged.request.params.get('offset')).toBe('50');
    // A total large enough that page 3 stays valid — otherwise nz-table corrects itself back to
    // page 1 on the next change detection and emits its own (nzQueryParams), landing a second,
    // spurious request.
    paged.flush([project()], { headers: { 'X-Total-Count': '100' } });

    // Now type a filter, the way an operator actually would.
    fixture.componentInstance.query = 'acme';
    fixture.componentInstance.onQueryChange();
    tick(250);
    const filtered = http.expectOne((r) => r.url === '/api/v1/projects');
    expect(filtered.request.params.get('offset')).toBe('0');
    expect(fixture.componentInstance.pageIndex()).toBe(1);
    filtered.flush([]);
  }));

  it('debounces typing instead of firing a request per keystroke', fakeAsync(() => {
    const fixture = TestBed.createComponent(ProjectListComponent);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/api/v1/projects').flush([project()]);

    fixture.componentInstance.query = 'a';
    fixture.componentInstance.onQueryChange();
    tick(100);
    fixture.componentInstance.query = 'ac';
    fixture.componentInstance.onQueryChange();
    tick(100);
    fixture.componentInstance.query = 'acme';
    fixture.componentInstance.onQueryChange();

    // Less than the 250ms debounce has elapsed since the last keystroke: nothing sent yet.
    tick(200);
    http.expectNone((r) => r.url === '/api/v1/projects');

    tick(50);
    const req = http.expectOne((r) => r.url === '/api/v1/projects');
    expect(req.request.params.get('q')).toBe('acme');
    req.flush([]);
  }));
});
