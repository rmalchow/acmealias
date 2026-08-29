import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { Router, provideRouter } from '@angular/router';
import { NZ_MODAL_DATA, NzModalRef, NzModalService } from 'ng-zorro-antd/modal';
import { ApiService, CAN_ADMIN_PROJECT, DelegationStatus, Domain, Project, RecordCheck, VerificationResult } from '@acme/shared';
import { DelegationHelpModalComponent } from './delegation-help.modal';
import { ProjectDetailComponent } from './project-detail.component';

const ACCOUNT = '00000000-0000-7000-8000-0000000000a1';
const BASE = `/api/v1/accounts/${ACCOUNT}`;

function domain(over: Partial<Domain> = {}): Domain {
  return {
    id: 'd-1',
    projectId: '7',
    name: 'shop.example.com',
    challengeName: '_acme-challenge.shop.example.com',
    aliasId: 'a-1',
    aliasLabel: 'shop-example-com',
    createdAt: '2026-01-01T00:00:00Z',
    // Activated, which is what a domain in a working deployment looks like. The activation chip
    // reads these two directly, so a fixture that left them out would test the "waiting" state
    // on every row without meaning to.
    lastCheckedAt: '2026-01-02T00:00:00Z',
    lastSuccessAt: '2026-01-02T00:00:00Z',
    // What the DNS plan used to carry, on the domain itself (design G2). cnameTarget is what the
    // delegation dialog prints; expectedCaa is what decides whether there is a CAA dot at all,
    // and null here would silently test the "no issuer configured" row instead.
    observeTls: false,
    tlsTestHost: null,
    cnameTarget: 'shop-example-com.svc.example.net',
    // Where the CAA rdata belongs. The DNS panel needs an owner name to print a record at, and a
    // fixture without one silently tests the "no issuer configured" table instead.
    caaName: 'example.com',
    expectedCaa: ['0 issue "letsencrypt.org"', '0 issuewild "letsencrypt.org"'],
    ...over,
  };
}

function project(domains: Domain[] = [domain()]): Project {
  return {
    id: '7',
    slug: 'shop',
    name: 'Shop',
    createdAt: '2026-01-01T00:00:00Z',
    permittedActions: [],
    nodes: [],
    domains,
  } as unknown as Project;
}

function verification(over: Partial<VerificationResult> = {}): VerificationResult {
  return {
    resolver: '10.0.0.1:53',
    domains: [
      {
        domainId: 'd-1',
        name: 'shop.example.com',
        challengeName: '_acme-challenge.shop.example.com',
        aliasId: 'a-1',
        delegation: { name: '_acme-challenge.shop.example.com', status: 'correct' },
        caa: { name: 'example.com', status: 'blocking', found: 'someone.else' },
      },
    ],
    ...over,
  };
}

describe('ProjectDetailComponent', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ProjectDetailComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
        // The header renders breadcrumb links, so the component needs a router even though this
        // spec never navigates.
        provideRouter([]),
        // The component holds it for controls this spec never opens; ng-zorro provides it
        // through modules the test bed does not import.
        NzModalService,
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    TestBed.inject(ApiService).setAccount(ACCOUNT);
  });

  afterEach(() => http.verify());

  /**
   * Opens the screen and answers everything but the verification, which the caller drives.
   *
   * ONE read of the project where there used to be two requests — the load balancer, then its
   * parent for a breadcrumb label. The screen and the object are the same thing now, so there is
   * no parent to look up, and this helper is where that shows.
   *
   * The two reporting panels folded in from the screen this one absorbed ask for their own
   * ranges as soon as they render, so they are answered here — emptily. They are not what this
   * spec is about, and an unanswered request fails http.verify() in afterEach.
   */
  function open(p: Project = project()) {
    const fixture = TestBed.createComponent(ProjectDetailComponent);
    fixture.componentRef.setInput('id', '7');
    fixture.detectChanges();
    http.expectOne(`${BASE}/projects/7`).flush(p);
    // NO PLAN READ. The endpoint it used to call no longer exists, and every call site of it was
    // a 404 that painted an error banner over an operation that had actually succeeded — which
    // is what "the delete button does not work" turned out to be (task 11.1).
    //
    // AND NO VERIFICATION READ, which is the newer half. Opening the screen asks DNS nothing at
    // all; the records a domain needs come from the project read above, and what is published at
    // those names is asked for by a button. A test that wants answers presses it with runCheck().
    reporting();
    return fixture;
  }

  /**
   * Presses the DNS panel's button and answers it: the GET that stores nothing.
   *
   * It takes the fixture rather than reaching for the component so the call site reads like the
   * thing a person does — press, then the answer arrives — which is now the only way any row on
   * this screen acquires a colour.
   */
  function runCheck(fixture: ComponentFixture<ProjectDetailComponent>, answer = verification()) {
    fixture.componentInstance.verify();
    const req = http.expectOne(`${BASE}/projects/7/verification`);
    req.flush(answer);
    return req;
  }

  /**
   * The usage panel's own read, answered with "nothing recorded".
   *
   * There used to be a second, for the inventory-history panel. That panel is gone: it plotted how
   * many nodes and domains the project held over time, which is a chart of numbers this page states
   * exactly a few centimetres above for the only moment that matters.
   */
  function reporting() {
    http.expectOne((r) => r.url === `${BASE}/projects/7/usage`).flush({ recorded: false, buckets: [] });
  }

  it('asks DNS nothing at all until somebody asks it to', () => {
    // This REVERSES design D1, which had the screen check on open. The argument for D1 was that a
    // customer with a mistyped CNAME would otherwise read "never validated" and learn nothing; the
    // records panel now prints the exact line to publish without asking anybody, so the empty
    // screen that argument was about does not exist. http.verify() in afterEach is what enforces
    // it: an automatic pass would show up as an unanswered request.
    const fixture = open();
    expect(fixture.componentInstance.verification()).toBeNull();
    expect(fixture.componentInstance.verifying()).toBeFalse();
  });

  it('banners a failure and degrades the rows, because every check is one somebody pressed', () => {
    // The old split — no banner for the automatic pass, a banner for the press — had no automatic
    // pass left to draw itself against. A failure now always has somebody waiting on it.
    const fixture = open();
    fixture.componentInstance.verify();
    expect(fixture.componentInstance.verifying()).toBeTrue();
    http.expectOne(`${BASE}/projects/7/verification`).flush('boom', { status: 500, statusText: 'Server Error' });

    expect(fixture.componentInstance.verifyError()).not.toBeNull();
    expect(fixture.componentInstance.verifying()).toBeFalse();
    expect(fixture.componentInstance.checks(domain()).delegation.label).toBe('could not check');
  });

  it('degrades both chips on a failed re-check rather than dropping CAA or keeping a stale verdict', () => {
    // A row that said "blocks us" must not lose its CAA line because a later request failed, and
    // must not go on asserting the old answer either: the banner and the chips would then
    // disagree about whether anything is known.
    const fixture = open();
    runCheck(fixture);
    expect(fixture.componentInstance.checks(domain()).caa?.label).toBe('blocks us');

    fixture.componentInstance.verify();
    http.expectOne(`${BASE}/projects/7/verification`).flush('boom', { status: 503, statusText: 'Unavailable' });

    const chips = fixture.componentInstance.checks(domain());
    expect(chips.caa?.label).toBe('could not check');
    expect(chips.delegation.label).toBe('could not check');
  });

  // Check now (design domain-activation task 4.3): the same lookup, against the route that
  // RECORDS what it saw, followed by a reload — because the stamps on screen are what just
  // changed and the person pressing it is watching for exactly that.
  it('records what a check-now saw and reloads the rows it changed', () => {
    const fixture = open();
    fixture.componentInstance.checkNow();
    const req = http.expectOne(`${BASE}/projects/7/verification`);
    expect(req.request.method).toBe('POST');
    req.flush(verification());

    // The reload. Without it the row would still read "waiting for delegation" for a domain
    // the service has just activated.
    http.expectOne(`${BASE}/projects/7`).flush(project());
  });

  // The panel's own button stays a GET. It is the check that only LOOKS, and the difference from
  // Recheck beside it is exactly that: pressing one asks a question, pressing the other answers it
  // and writes the answer down against the domains. A page that activated domains because somebody
  // wanted to see what was published would be a page with one button too few.
  it('asks the panel check as a read, and Recheck as the write', () => {
    const fixture = open();
    expect(runCheck(fixture).request.method).toBe('GET');
    expect(fixture.componentInstance.verifyError()).toBeNull();

    fixture.componentInstance.checkNow();
    const recorded = http.expectOne(`${BASE}/projects/7/verification`);
    expect(recorded.request.method).toBe('POST');
    recorded.flush(verification());
    http.expectOne(`${BASE}/projects/7`).flush(project());
  });

  // Waiting is not broken (task 4.1). A domain added five minutes ago has never been seen, and
  // the chip has to say so as an expectation rather than as a fault.
  it('reads a never-seen delegation as waiting, and one that stopped resolving as still active', () => {
    const fixture = open();
    runCheck(fixture);
    const c: ProjectDetailComponent = fixture.componentInstance;

    expect(c.activation(domain({ lastCheckedAt: null, lastSuccessAt: null })).label).toBe('waiting for delegation');
    expect(c.activation(domain({ lastCheckedAt: '2026-02-01T00:00:00Z', lastSuccessAt: null })).colour).toBe('blue');
    expect(c.activation(domain()).label).toBe('active');

    // Checked after it was last seen: we looked and it was gone. It stays active — activation is
    // one-way (A3) — and the row says since when rather than counting down to anything.
    const broken = domain({ lastSuccessAt: '2026-02-01T00:00:00Z', lastCheckedAt: '2026-02-03T00:00:00Z' });
    expect(c.activation(broken).label).toBe('not resolving');
    expect(c.stoppedResolving(broken)).toBeTrue();
    expect(c.stoppedResolving(domain())).toBeFalse();

    // Check now is offered in both states a check can change (task 4.3). The second one is the
    // person who has just REPAIRED a delegation: gating on "never seen" alone left exactly them
    // with no way to ask, waiting a full settled interval for the chip to go green.
  });

  it('offers one Recheck for the whole project, not one per row (reverses G18)', () => {
    // G18 put a Check now on every row and argued the condition away. The reversal is not a
    // rollback to the gated version: the control POSTs /projects/:id/verification, which looks up
    // EVERY domain in the project and records what it saw for all of them. Per row it read as
    // "check this domain" — a promise the request does not keep — and it spun a spinner on one
    // row while the others silently changed underneath it.
    //
    // What G18 was protecting survives: there is no condition. Recheck is offered whenever the
    // project has a domain at all, including when every one of them is active, because an active
    // domain whose certificate expires in nine days is exactly what somebody re-checks.
    const fixture = open();
    runCheck(fixture);
    fixture.detectChanges();
    // Rendering the rows renders the two panels below them, which ask for their own contents.
    // They are not what this spec is about, and an unanswered request fails http.verify().
    http.expectOne((r) => r.url.endsWith('/projects/7/keys')).flush([]);
    http.expectOne((r) => r.url.startsWith(`${BASE}/projects/7/interactions`)).flush([]);

    expect(fixture.componentInstance.activation(domain()).label).toBe('active');
    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Recheck');
    // And exactly once, beside Add domain, rather than once per row.
    expect(text.match(/Recheck/g)?.length).toBe(1);
    expect(text).not.toContain('Check now');
  });

  it('keeps the timestamp, in the chip tooltip rather than in every row', () => {
    // The "Last looked" line under the chip cost a line of vertical space in every row for a date
    // that matters in the few rows where something is wrong — and it made row height depend on
    // whether a domain had ever been checked, so a table of ten had two row heights for no reason
    // a reader could name. The wording and the reasoning are unchanged (design A1): since when,
    // never how many failures, and no deadline because nothing is ever switched off for it (A3).
    const fixture = open();
    runCheck(fixture);
    const c: ProjectDetailComponent = fixture.componentInstance;

    expect(c.lookedAt(domain({ lastCheckedAt: null, lastSuccessAt: null }))).toContain('Not looked at yet');
    expect(c.lookedAt(domain())).toContain('Last looked');
    // Checked after it was last seen: the row says since when it stopped, not that we looked.
    const broken = domain({ lastSuccessAt: '2026-02-01T00:00:00Z', lastCheckedAt: '2026-02-03T00:00:00Z' });
    expect(c.lookedAt(broken)).toContain('Last seen');
  });

  it('shows a dot per record type this domain HAS, and none for the ones it does not', () => {
    // A type this domain does not have gets NO dot rather than a grey one (task 7.3): a grey dot
    // would claim we meant to look. TLS is off here and no issuer is configured on the second
    // row, so each is absent for its own reason.
    const fixture = open(project([domain(), domain({ id: 'd-2', name: 'blog.example.com', expectedCaa: null })]));
    runCheck(fixture);
    const c: ProjectDetailComponent = fixture.componentInstance;

    expect(c.checks(domain()).caa).not.toBeNull();
    expect(c.checks(domain()).tls).toBeNull();
    expect(c.checks(domain({ expectedCaa: null })).caa).toBeNull();
  });

  it('gives a wildcard and its apex one CNAME dot and separate TLS dots (task 12.1)', () => {
    // They share one alias and therefore one delegation, which is why both CNAME dots read the
    // same state. They are different names very often served by different certificates, so the
    // switch, the hostname and the TLS dot are each their own.
    const apex = domain({ id: 'd-1', name: 'example.com' });
    const wild = domain({ id: 'd-2', name: '*.example.com', observeTls: true, tlsTestHost: 'www.example.com' });
    const fixture = open(project([apex, wild]));
    // Only the observed one is asked about, which is what keeps the switch being off free.
    http.expectOne(`${BASE}/domains/d-2/certificate`).flush({
      serverName: 'www.example.com',
      band: 'urgent',
      addresses: [],
    });
    runCheck(
      fixture,
      verification({
        domains: [
          {
            domainId: 'd-1',
            name: 'example.com',
            challengeName: '_acme-challenge.example.com',
            aliasId: 'a-1',
            delegation: { name: '_acme-challenge.example.com', status: 'correct' },
          },
          {
            domainId: 'd-2',
            name: '*.example.com',
            challengeName: '_acme-challenge.example.com',
            aliasId: 'a-1',
            delegation: { name: '_acme-challenge.example.com', status: 'correct' },
          },
        ],
      }),
    );
    const c: ProjectDetailComponent = fixture.componentInstance;

    expect(c.checks(apex).delegation.colour).toBe('green');
    expect(c.checks(wild).delegation.colour).toBe('green');
    expect(c.checks(apex).tls).toBeNull();
    expect(c.checks(wild).tls?.colour).toBe('red');
  });

  // --- deleting the project ------------------------------------------------

  /**
   * Stands in for the confirmation dialog, which this spec is not testing.
   *
   * confirmAction resolves on the modal ref's `afterClose`, so a ref carrying `of(answer)` is the
   * whole dialog as far as the component is concerned. Spying on `confirm` also keeps a real modal
   * out of the DOM, which would otherwise outlive the fixture.
   */
  function answerConfirm(answer: boolean): jasmine.Spy {
    const spy = spyOn(TestBed.inject(NzModalService), 'confirm');
    spy.and.returnValue({ afterClose: of(answer) } as unknown as NzModalRef);
    return spy;
  }

  function adminProject(): Project {
    return { ...project(), permittedActions: [CAN_ADMIN_PROJECT] } as Project;
  }

  /**
   * The keys and interactions panels, answered emptily.
   *
   * They ask as soon as they render, and in these tests they DO render: awaiting the confirmation
   * lets change detection run after the project arrives, which the synchronous tests above never
   * reach. Neither panel is what this is about, and an unanswered request fails http.verify().
   */
  function panels(): void {
    http.expectOne(`${BASE}/projects/7/keys`).flush([]);
    http.expectOne((r) => r.url === `${BASE}/projects/7/interactions`).flush([]);
  }

  it('deletes the project and leaves for the list', async () => {
    const fixture = open(adminProject());
    runCheck(fixture);
    const navigate = spyOn(TestBed.inject(Router), 'navigate').and.resolveTo(true);
    answerConfirm(true);

    await fixture.componentInstance.deleteProject(adminProject());
    panels();
    const req = http.expectOne(`${BASE}/projects/7`);
    expect(req.request.method).toBe('DELETE');
    req.flush(null, { status: 204, statusText: 'No Content' });

    // The screen is about an object that no longer exists, so it must not stay on it — and it
    // must not reload it either, which would paint a 404 over a delete that worked.
    expect(navigate).toHaveBeenCalledWith(['/projects']);
    expect(fixture.componentInstance.deleting()).toBeFalse();
  });

  it('asks first, and does nothing at all when the answer is no', async () => {
    const fixture = open(adminProject());
    runCheck(fixture);
    const confirm = answerConfirm(false);

    await fixture.componentInstance.deleteProject(adminProject());
    panels();

    expect(confirm).toHaveBeenCalled();
    // No DELETE: http.verify() in afterEach is what asserts it, and a request made here would
    // fail that instead of this line.
    expect(fixture.componentInstance.deleting()).toBeFalse();
  });

  it('does not ask, let alone send, without project.admin', async () => {
    // The button is disabled for this session, but a disabled button is a courtesy and this is
    // the handler behind it. The server refuses regardless; the point here is that a screen
    // without the grant never gets as far as the confirmation.
    const fixture = open(project());
    runCheck(fixture);
    const confirm = answerConfirm(true);

    await fixture.componentInstance.deleteProject(project());
    panels();

    expect(confirm).not.toHaveBeenCalled();
    expect(fixture.componentInstance.canAdmin()).toBeFalse();
  });

  it('shows a refusal beside the button rather than at the top of the page', async () => {
    // The refusal that actually happens: the API declines to delete a project that still holds
    // domains. It belongs next to the control that produced it — `error` renders five screens up.
    const fixture = open(adminProject());
    runCheck(fixture);
    const navigate = spyOn(TestBed.inject(Router), 'navigate').and.resolveTo(true);
    answerConfirm(true);

    await fixture.componentInstance.deleteProject(adminProject());
    panels();
    http
      .expectOne(`${BASE}/projects/7`)
      .flush({ error: 'project "shop" still has 1 domain(s), delete them first' }, { status: 409, statusText: 'Conflict' });

    expect(fixture.componentInstance.deleteError()).toContain('delete them first');
    expect(fixture.componentInstance.error()).toBeNull();
    expect(navigate).not.toHaveBeenCalled();
    expect(fixture.componentInstance.deleting()).toBeFalse();
  });

});


/**
 * The one rule carried over from the dialog this one was split out of (task 8.9): a lookup that
 * FAILED never has its `found` printed. That field holds our resolver's error text, addressed to
 * an operator, and showing it reads as a verdict on the customer's zone when it is a verdict on
 * our lookup.
 */
describe('DelegationHelpModalComponent', () => {
  function render(delegation: RecordCheck<DelegationStatus>) {
    // The data differs per case and is an injection token, so the module is rebuilt each time
    // rather than configured once in a beforeEach.
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      imports: [DelegationHelpModalComponent],
      providers: [
        provideNoopAnimations(),
        provideNzIconsTesting(),
        { provide: NzModalRef, useValue: { close: () => undefined } },
        { provide: NZ_MODAL_DATA, useValue: { domain: domain(), delegation } },
      ],
    });
    const fixture = TestBed.createComponent(DelegationHelpModalComponent);
    fixture.detectChanges();
    return fixture.nativeElement.textContent as string;
  }

  it('never prints what a failed lookup found', () => {
    const text = render({
      name: '_acme-challenge.shop.example.com',
      status: 'undetermined',
      found: 'query CNAME _acme-challenge.shop.example.com: dial udp 10.0.0.1:53: i/o timeout',
    });
    expect(text).not.toContain('i/o timeout');
    expect(text).not.toContain('10.0.0.1');
  });

  it('prints the record in every state, green included, from the domain and not from a check', () => {
    // Which record a domain needs is knowable without asking DNS anything (G2), so this is the
    // same sentence on a deployment that never configured a verification resolver.
    for (const status of ['correct', 'missing', 'not-checked'] as DelegationStatus[]) {
      const text = render({ name: '_acme-challenge.shop.example.com', status });
      expect(text).toContain('_acme-challenge.shop.example.com. IN CNAME shop-example-com.svc.example.net.');
    }
  });

  it('keeps the deleted-and-re-added explanation for a record that points elsewhere', () => {
    const text = render({ name: '_acme-challenge.shop.example.com', status: 'incorrect', found: 'old-alias.svc.example.net' });
    expect(text).toContain('deleted and added again');
    expect(text).toContain('old-alias.svc.example.net');
  });
});
