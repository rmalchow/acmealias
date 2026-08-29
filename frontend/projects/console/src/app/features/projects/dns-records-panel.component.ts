import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTableModule } from 'ng-zorro-antd/table';
import { NzToolTipModule } from 'ng-zorro-antd/tooltip';
import { CAAStatus, DelegationStatus, Domain, DomainCheck, HelpHintComponent, InfoBoxComponent } from '@acme/shared';
import { StatusChip, caaChip, delegationChip, dotTitle } from './domain-status';

/** Which of the two tables a check is about. It is the `records` parameter the route takes. */
export type RecordKind = 'delegation' | 'caa';

/**
 * One row of a record table: the name, what belongs there, and — once somebody has asked — what is
 * actually there.
 *
 * `expected` is a list because a CAA row is two lines, `issue` and `issuewild`; joining them into
 * one string would produce a zone file that does not parse.
 *
 * `chip` is null until a check has run. It is not "unknown as a status": there is a genuine
 * difference between a row nobody has looked at and a row we looked at and could not resolve, and
 * a fifth grey state would have hidden it.
 */
interface RecordRow {
  name: string;
  expected: string[];
  found: string;
  chip: StatusChip | null;
  /** The domains this one record is for, so a table of two rows explains eight domains. */
  covers: string[];
}

/**
 * THE DNS PANEL, BACK — for two record types, and answering DNS only when asked.
 *
 * A panel like this existed and was deleted (design G2). Most of it should stay deleted: it
 * rendered a whole PLAN, address records for every node included, and those could only ever
 * discover that a customer disagreed with addresses the same customer had typed into the Nodes
 * panel — a check whose only possible finding is that somebody contradicted themselves. It also
 * carried a verdict column restating the dots on the domain rows above it.
 *
 * What went with it was the one thing nothing else does: PRINT THE RECORD. A dot says a name is
 * wrong; it does not say what the line should read. That was tolerable while the answer was one
 * CNAME whose target already sits on the domain row, and it is not tolerable now that the CAA
 * answer is a two-line record carrying a parameter most people have never published.
 *
 * So: no A, no AAAA, no zone download, no verdict column. Two tables, each printing the record a
 * name needs beside the record it has.
 *
 * WHAT IS SHOWN WITHOUT ASKING DNS ANYTHING. Both tables render in full from the project read
 * alone — every name, every line to publish. That is the half a customer setting a domain up
 * actually needs, and it is knowable without a single lookup, so it must not wait behind one. The
 * Found column and the dots are the other half, they cost a query per name against somebody's
 * recursor, and they appear when the button is pressed and not before. Nothing on this screen
 * queries DNS on its own any more.
 *
 * The two tables are separate because the two questions are. A delegation is per DOMAIN and is
 * judged by whether it is there. CAA is per REGISTRABLE NAME — one set covers every host under it,
 * so eight domains under example.com are one row — and is judged by how little it leaves open. One
 * table with a Type column would have made those look like one list with a filter on it.
 */
@Component({
  selector: 'app-dns-records-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HelpHintComponent, NzAlertModule, NzButtonModule, NzIconModule, NzTableModule, NzToolTipModule, InfoBoxComponent],
  template: `
    <div class="panel-head">
      <h2>DNS records</h2>
      <app-help-hint
        heading="DNS records"
        [paragraphs]="[
          'What each name needs to have published, and — once you ask — what a lookup finds there. The records are known without asking anybody; the Found column costs a query per name, so it waits until you press the button on that table.',
          'A button per table, because they are separate questions asked at separate moments: the delegation is what you publish first and check once, and the CAA record is a decision about who may issue that most people come back to later. Each button asks only about its own table — checking CAA sends no CNAME queries.',
          'Nothing here is stored. Every value in the Found column was read live, and these checks record nothing against your domains.'
        ]"
      />
    </div>

    @if (!rowCount()) {
      <p class="lede">Add a domain and the records it needs will be listed here.</p>
    }

    @if (delegations().length) {
      <div class="section-head">
        <h3>Delegation</h3>
        <app-help-hint
          heading="Delegation"
          [paragraphs]="[
            'One CNAME per challenge name, published in your own DNS, pointing at the alias we issued. Publish it once; it never changes again, renewals included.',
            'A wildcard and its base name validate at the same challenge name, because the *. label is stripped before the name is formed — so one row here can be the record for two domains, and the names it covers are listed under it.',
            'The trailing dot on the target is part of the record. Without it a provider that appends your zone turns the alias into something under your own name, which is the commonest way this gets published wrong.'
          ]"
        />
        <button
          class="check"
          nz-button
          nzSize="small"
          [nzLoading]="checkingDelegation()"
          nz-tooltip
          nzTooltipTitle="Look up each challenge name now and show what resolves there. Stores nothing."
          (click)="check.emit('delegation')"
        >
          <span nz-icon nzType="reload"></span>
          {{ delegationChecked() ? 'Check again' : 'Check DNS' }}
        </button>
      </div>
      <table class="records">
        <thead>
          <tr>
            <th class="col-name">Name</th>
            <th class="col-record">Record needed</th>
            <th class="col-found">Found</th>
          </tr>
        </thead>
        <tbody>
          @for (r of delegations(); track r.name) {
            <tr>
              <td class="col-name">
                <code>{{ r.name }}</code>
                <div class="covers">{{ r.covers.join(', ') }}</div>
              </td>
              <td><code class="rdata">{{ r.expected[0] }}</code></td>
              <td class="col-found">
                @if (r.chip; as chip) {
                  <span class="verdict">
                    <span
                      class="dot"
                      [class]="'dot-' + chip.colour"
                      nz-tooltip
                      [nzTooltipTitle]="dotTitle(chip)"
                    ></span>
                    @if (r.found) {
                      <code class="rdata">{{ r.found }}</code>
                    } @else {
                      <span class="nothing">{{ chip.label }}</span>
                    }
                  </span>
                } @else {
                  <span class="unasked">not checked yet</span>
                }
              </td>
            </tr>
          }
        </tbody>
      </table>
    }

    @if (caa().length) {
      <!--
        The paragraph that used to sit here is now behind the question mark, and what it says is
        the reason this panel needed one rather than a shorter sentence: publishing the delegation
        above lets this service complete a DNS-01 challenge for the name and its wildcard, and
        accounturi is the one record that takes that back. That is not a thing to leave unsaid, and
        it is also not four lines of prose to put between somebody and a table they came here to
        read. It stays one click away here, it is step 4 of the setup on the marketing site, and
        the dot on every row carries the short form in its tooltip.
      -->
      <div class="section-head">
        <h3>CAA</h3>
        <app-help-hint
          heading="CAA"
          [paragraphs]="[
            'One record set per registrable name, covering every host beneath it. It says which certificate authority may issue for these names — and, with the accounturi parameter, which ACME account at that authority.',
            'Publishing the delegation above lets us answer the one question a certificate authority asks about that name, which is the service. It also means whoever operates this service could ask an authority for a certificate for it. Every hosted DNS-01 responder holds that over the names delegated to it.',
            'accounturi is what bounds it: it pins issuance to a single ACME account, and an order from any other account is refused by the authority itself, whatever we do. Your account URL is not visible to us, so the record below carries a placeholder — the box under the table says where to read your own.',
            'Both tags, because a present issuewild governs wildcard issuance instead of issue: authorising only the first leaves wildcards blocked.'
          ]"
        />
        <button
          class="check"
          nz-button
          nzSize="small"
          [nzLoading]="checkingCaa()"
          nz-tooltip
          nzTooltipTitle="Look up the CAA set at each registrable name now. Stores nothing."
          (click)="check.emit('caa')"
        >
          <span nz-icon nzType="reload"></span>
          {{ caaChecked() ? 'Check again' : 'Check DNS' }}
        </button>
      </div>
      <table class="records">
        <thead>
          <tr>
            <th class="col-name">Name</th>
            <th class="col-record">Record needed</th>
            <th class="col-found">Found</th>
          </tr>
        </thead>
        <tbody>
          @for (r of caa(); track r.name) {
            <tr>
              <td class="col-name">
                <code>{{ r.name }}</code>
                <div class="covers">{{ r.covers.join(', ') }}</div>
              </td>
              <td>
                @for (line of r.expected; track line) {
                  <code class="rdata block">{{ line }}</code>
                }
              </td>
              <td class="col-found">
                @if (r.chip; as chip) {
                  <span class="verdict">
                    <span
                      class="dot"
                      [class]="'dot-' + chip.colour"
                      nz-tooltip
                      [nzTooltipTitle]="dotTitle(chip)"
                    ></span>
                    @if (r.found) {
                      <code class="rdata">{{ r.found }}</code>
                    } @else {
                      <span class="nothing">{{ chip.label }}</span>
                    }
                  </span>
                } @else {
                  <span class="unasked">not checked yet</span>
                }
              </td>
            </tr>
          }
        </tbody>
      </table>

      <!--
        BELOW BOTH TABLES, not repeated inside the CAA one. It answers the question the CAA row
        raises — "where do I get an account URL" — and that is one answer for the whole project,
        not a fact about any single row.

        Traefik gets the worked example, and is named as such. It is the client this service is
        most often paired with, its account URI is the hardest of the lot to find (it is inside a
        JSON file most people only ever chmod 600), and one command a reader can run beats a list
        of five clients with a shrug beside each. The others get a pointer, because for them it is
        a line in their own documentation.
      -->
      <app-info-box
        class="accounturi"
        key="acme-account-url"
        heading="Finding your ACME account URL"
        [body]="accountUri"
      />
      <ng-template #accountUri>
        <!--
          THE FIRST PARAGRAPH SAYS WHAT THE RECORD IS FOR, and it names us as the party it defends
          against. "Prevents unauthorised issuance" would be the same sentence with the subject
          removed, and the subject is the point: the delegation this service asked the customer to
          publish is precisely what would make such issuance possible, so we are the party with the
          means. A customer who completes this record is protected from us, and saying so plainly is
          worth more than the discomfort of writing it down.
        -->
        <p>
          A complete record here is what stops <strong>us</strong> from obtaining certificates for
          your names. Holding your delegation, we can answer the challenge for any name pointed at
          our zone — and for its wildcard, which shares that name — so we could order a certificate
          for it in your name without asking you. Pinning issuance to your own ACME account is what
          makes that impossible: the authority refuses an order from any other account, ours
          included, and nothing we do changes that refusal.
        </p>
        <p>
          Your certificate authority mints one account URL when your ACME client first registers,
          and it does not change afterwards. With Traefik it is in the <code>acme.json</code> your
          certificate resolver writes:
        </p>
        <pre class="cmd">jq -r '.[].Account.Registration.uri' acme.json</pre>
        <p>
          That prints one line per resolver, like
          <code>https://acme-v02.api.letsencrypt.org/acme/acct/3680831095</code>. Put it where the
          record above reads <code>YOUR-ACME-ACCOUNT-URL</code> and leave the rest of the line as it
          is. Other clients keep the same value elsewhere — lego in
          <code>accounts/&hellip;/account.json</code>, certbot under
          <code>/etc/letsencrypt/accounts/</code>, cert-manager in the Issuer's status as
          <code>acme.uri</code>.
        </p>
      </ng-template>
    }
  `,
  styles: [
    `
      .panel-head {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin-bottom: 0.75rem;
      }
      /* The button goes right; the help icon does NOT — it belongs beside the heading it explains.
         The other panel heads on this page put the auto margin on the second child, and copying
         that rule here put the margin on the HELP ICON, parking a question mark at the far right of
         the row with nothing next to the words it was about. This head has three children where
         theirs have two, so the rule names the button. */
      .panel-head .check,
      .section-head .check {
        margin-inline-start: auto;
      }
      h2 {
        margin: 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      /* The same font as the panel heading above it. These are the headings a reader actually
         navigates by — "the CNAME table" and "the CAA table" — and shrinking them to signal a level
         of nesting nobody is counting only made them harder to find. */
      h3 {
        margin: 0;
        font-size: 1.125rem;
        font-weight: 500;
      }
      /* Heading and question mark on one line, the same pairing the panel head uses. The
         explanation lives behind the icon: it runs to four paragraphs on CAA, and four paragraphs
         between a heading and the table they introduce is a wall somebody scrolls past to reach
         the thing they came for. */
      .section-head {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin: 1.5rem 0 0.5rem;
      }
      .section-head:first-of-type {
        margin-top: 0;
      }
      /* The empty state, and the only prose left in the body of this panel. */
      .lede {
        margin: 0;
        color: var(--ac-muted);
      }
      /* A plain table rather than nz-table: there is no paging, no sorting and no row selection
         here, and the wrapper's own scroll container fights the wrapping the rdata column needs. */
      .records {
        width: 100%;
        border-collapse: collapse;
      }
      .records th,
      .records td {
        text-align: left;
        vertical-align: top;
        padding: 0.5rem 0.75rem 0.5rem 0;
        border-bottom: 1px solid var(--ac-line, #f0f0f0);
      }
      .records th {
        font-weight: 500;
        color: var(--ac-muted);
        font-size: 0.8125rem;
      }
      /* TWELFTHS, like the two tables above: 3 / 6 / 3 here against the Domains table's
         2 / 3 / 2 / 2 / 2 / 1, which puts a column boundary at the same place on both at 9. The
         boundary at 3 is this table's own — the Name column has to hold a challenge name, which is
         longer than anything in the first column up there.

         Percentages and not rem, for the reason the tables above give: these are the full width of
         the page and rem stops tracking it the moment the window changes. */
      .col-name {
        width: 25%;
      }
      .col-record {
        width: 50%;
      }
      .col-found {
        width: 25%;
      }
      /* The rdata is the point of the table, so it gets the room and it WRAPS rather than
         scrolling: a CAA line with an account URL in it is longer than any column anybody has, and
         a value clipped at the edge is a value somebody pastes half of. */
      .rdata {
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
      .block {
        display: block;
      }
      /* Which domains a row is the record for. A CAA row covers every host under one registrable
         name, and without this a customer with eight domains sees one row and wonders which. */
      .covers {
        margin-top: 0.25rem;
        font-size: 0.75rem;
        color: var(--ac-muted);
      }
      .verdict {
        display: flex;
        align-items: baseline;
        gap: 0.5rem;
      }
      /* The same four colours the domain rows use, with the same literal fallbacks: a deployment
         that has not defined the tokens would otherwise render invisible dots. */
      .dot {
        flex: none;
        width: 0.625rem;
        height: 0.625rem;
        border-radius: 50%;
        display: inline-block;
        transform: translateY(-0.0625rem);
      }
      .dot-green {
        background: var(--ac-ok, #52c41a);
      }
      .dot-orange {
        background: var(--ac-warn, #fa8c16);
      }
      .dot-red {
        background: var(--ac-error, #f5222d);
      }
      .dot-default {
        background: var(--ac-line, #d9d9d9);
      }
      /* What stands in for rdata when there is none: the chip's own word, in the body colour
         rather than the code colour, so "nothing published" never reads as a published value. */
      .nothing {
        color: var(--ac-muted);
      }
      /* Before anybody has pressed the button. Deliberately not a grey dot — a dot is a verdict,
         and there is no verdict here yet. */
      .unasked {
        color: var(--ac-muted);
        font-style: italic;
      }
      .accounturi {
        display: block;
        margin-top: 1rem;
      }
      .cmd {
        margin: 0.5rem 0;
        padding: 0.5rem 0.75rem;
        border-radius: 4px;
        background: var(--ac-code-bg, rgba(0, 0, 0, 0.04));
        overflow-x: auto;
      }
    `,
  ],
})
export class DnsRecordsPanelComponent {
  /** The project's domains. Everything in the "Record needed" column comes from here. */
  readonly domains = input<Domain[]>([]);
  /**
   * The last answers, merged: a delegation-only check leaves the CAA answers alone and vice versa,
   * which is what makes two buttons possible without one wiping the other's column.
   */
  readonly checks = input<DomainCheck[] | null>(null);
  /** Which check is in flight, if any. The two buttons spin independently because they are two
   *  requests, and a spinner on both would claim work that is not happening. */
  readonly checking = input<RecordKind | null>(null);
  /** Pressed, naming which table asked. The parent owns the request: the domain rows above read
   *  the delegation half of the same answer. */
  readonly check = output<RecordKind>();

  protected readonly dotTitle = dotTitle;

  readonly checkingDelegation = computed(() => this.checking() === 'delegation');
  readonly checkingCaa = computed(() => this.checking() === 'caa');

  /**
   * Whether each table has an answer yet, asked per table rather than per panel.
   *
   * A row's chip is the honest source: it is null exactly when no answer covers that name, which is
   * true both before any check and for the table whose button has not been pressed. Reading a panel
   * -wide flag instead would have put "Check again" on a table nobody had checked.
   */
  readonly delegationChecked = computed(() => this.delegations().some((r) => r.chip !== null));
  readonly caaChecked = computed(() => this.caa().some((r) => r.chip !== null));
  readonly rowCount = computed(() => this.delegations().length + this.caa().length);

  /**
   * One row per challenge NAME, not per domain: a wildcard and its base derive the same
   * `_acme-challenge` name, so `*.example.com` and `example.com` are one record. Two rows saying
   * so would be two places to publish it and one of them wrong.
   *
   * A domain whose alias has gone missing carries an empty `cnameTarget` and is skipped — there is
   * no record to print, and a row saying to publish a CNAME to nothing would be an instruction the
   * customer cannot follow for a fault that is ours.
   */
  readonly delegations = computed<RecordRow[]>(() => {
    const answers = this.byName((c) => c.delegation);
    const rows = new Map<string, RecordRow>();
    for (const d of this.domains()) {
      if (!d.cnameTarget) continue;
      const existing = rows.get(d.challengeName);
      if (existing) {
        existing.covers.push(d.name);
        continue;
      }
      const answer = answers.get(d.challengeName);
      rows.set(d.challengeName, {
        name: d.challengeName,
        expected: [zoneLine(d.challengeName, 'CNAME', `${d.cnameTarget}.`)],
        found: answer?.found ?? '',
        chip: answer ? delegationChip(answer.status as DelegationStatus) : null,
        covers: [d.name],
      });
    }
    return [...rows.values()];
  });

  /**
   * One row per registrable name. Empty, and so absent from the screen entirely, when the
   * deployment configures no issuer: there is then nothing to authorise, and a table asking a
   * customer to publish a record naming no authority would be worse than no table.
   */
  readonly caa = computed<RecordRow[]>(() => {
    const answers = this.byName((c) => c.caa);
    const rows = new Map<string, RecordRow>();
    for (const d of this.domains()) {
      if (!d.caaName || !d.expectedCaa?.length) continue;
      const existing = rows.get(d.caaName);
      if (existing) {
        existing.covers.push(d.name);
        continue;
      }
      const answer = answers.get(d.caaName);
      rows.set(d.caaName, {
        name: d.caaName,
        expected: d.expectedCaa.map((rdata) => zoneLine(d.caaName, 'CAA', rdata)),
        found: answer?.found ?? '',
        chip: answer ? caaChip(answer.status as CAAStatus) : null,
        covers: [d.name],
      });
    }
    return [...rows.values()];
  });

  /**
   * The last check's answers, indexed by the NAME they are about.
   *
   * By name and not by domain id because that is the key the rows are grouped on: a check answers
   * per domain, several domains share one challenge name and every domain under one registrable
   * name shares one CAA set, and the answer for each of those is the same answer. Indexing by name
   * is what lets a row hold one verdict instead of picking one of eight identical ones.
   */
  private byName(pick: (c: DomainCheck) => { name: string; status: string; found?: string } | undefined) {
    const out = new Map<string, { name: string; status: string; found?: string }>();
    for (const c of this.checks() ?? []) {
      const answer = pick(c);
      // `not-checked` IS THE ABSENCE OF AN ANSWER, and dropping it here is what makes two buttons
      // work. The service builds its reply from what a domain needs rather than from what was
      // asked, so a delegation-only pass still carries a CAA row for every domain — reading
      // not-checked, because nobody looked. Keeping those would give the CAA table a grey dot and
      // a "Check again" button for a check that never ran.
      //
      // The same status arrives when the deployment configured no verification resolver, and it
      // collapses into the same thing correctly: nobody looked, and the row says so.
      if (answer && answer.status !== 'not-checked') out.set(answer.name, answer);
    }
    return out;
  }
}

/**
 * A whole zone-file line rather than the rdata alone.
 *
 * Rdata by itself is what an API returns; a line carrying the owner name, a TTL and the type is
 * what somebody publishes. Most DNS interfaces ask for the parts in separate boxes, and splitting
 * a full line into a form is easy — where a reader handed `0 issue "…"` and four empty boxes has
 * to know that CAA rdata has three fields and which is which.
 *
 * The trailing dot on the CNAME target is load-bearing. Without it a provider that appends the
 * zone turns the alias into `k7f2q.edgedns.cloud.shop.example.com`, which is the commonest way a
 * delegation gets published wrong.
 */
function zoneLine(name: string, type: string, rdata: string): string {
  return `${name}.  300  IN  ${type}  ${rdata}`;
}
