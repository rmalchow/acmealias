import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';

/** One destination in the navigation. `path` is a router link; `label` is what the person reads. */
export interface NavItem {
  path: string;
  label: string;
}

/**
 * The chrome both consoles wear: a bar across the top of the window, and — under it, inside the
 * page — the navigation.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * WHY THE NAVIGATION IS NOT IN THE BAR
 * ─────────────────────────────────────────────────────────────────────────────
 * It used to be, wedged between the wordmark and the account switcher, and that put three
 * unrelated jobs on one line: who you are, which account you are in, and where in the product
 * you are standing. The first two are properties of the SESSION and do not change as you move
 * about; the third is a property of the PAGE and changes every time you click. Mixing them meant
 * the only thing that ever moved sat between two things that never do.
 *
 * So the bar keeps identity and context — the mark, the account, the person — and the navigation
 * moves down into the content column, sharing its left edge with the page heading beneath it.
 * `Projects` now sits directly above the breadcrumb whose first crumb is Projects, which is the
 * relationship the old layout drew no line between. It also means the nav scrolls away with the
 * page it belongs to, while the bar stays: the thing you need while reading a long table is which
 * account you are reading it in, not the list of screens you are not on.
 *
 * The bar itself still spans the window and its contents sit at the window's edges, which is what
 * the marketing site's header does (site/src/styles.css `.bar`). Only the navigation and the page
 * are held to `--ac-page-width`. The alternative — constraining the bar's contents too, so the
 * mark lines up with the page — was what the customer console did before, and it lost precisely
 * because it made the bar look like part of the page rather than part of the window.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ONE FRAME, TWO CONSOLES
 * ─────────────────────────────────────────────────────────────────────────────
 * A member of staff and a customer look at the same chrome. What differs is what is IN it: the
 * navigation's contents (an account's kind decides those, and the caller passes them), whatever
 * the caller projects into the bar's right-hand side, and the accent — which the staff console
 * makes amber in its own theme.less, so every underline and button below this bar follows without
 * this file knowing anything about it.
 *
 * `tone` is the one thing the frame itself varies, and it is deliberately small: the staff bar
 * takes an amber-cast ground. It exists because the difference between the two surfaces has to be
 * legible in the first quarter-second, before a word has been read — that is the whole argument
 * in `projects/admin/src/theme.less`, and a shared shell that dropped it would have traded a real
 * safeguard for tidiness. It is a ground, not a second layout: the two shells are the same shape.
 *
 * The providers are NOT shared, and that is a separate decision — see each application's
 * app.config.ts. This is a component; it brings nothing into the injector.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * WHAT THE CALLER PROVIDES
 * ─────────────────────────────────────────────────────────────────────────────
 *   [shellIdentity]  the bar's right-hand group — the account switcher and user menu in the
 *                    customer console, a name and a sign-out link in the staff one
 *   [shellBanner]    a full-width strip under the bar, for something that must be seen on every
 *                    screen (the low-balance warning). Absent, nothing occupies the space.
 *   (default)        the page — in practice the router outlet
 */
@Component({
  selector: 'app-shell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, RouterLinkActive],
  template: `
    <header class="bar" [class.staff]="tone() === 'staff'">
      <a class="brand" [routerLink]="home()" [attr.aria-label]="product() + ' — home'">
        <span class="brand-box"><img src="brand/logo.svg" alt="" /></span>
        <span class="brand-word">{{ product() }}</span>
        @if (badge(); as text) {
          <span class="brand-badge">{{ text }}</span>
        }
      </a>
      <div class="identity">
        <ng-content select="[shellIdentity]" />
      </div>
    </header>

    <ng-content select="[shellBanner]" />

    <!--
      The navigation lives in the content column, not in the bar. It is rendered only when there
      is somewhere to go: the staff console has one screen and passes nothing, and an account that
      is unselected or suspended has no navigation either (the customer console decides that). An
      empty rule with a border under it would be a line across the page meaning nothing.
    -->
    @if (nav().length) {
      <nav class="ac-page nav">
        @for (item of nav(); track item.path) {
          <a [routerLink]="item.path" routerLinkActive="active">{{ item.label }}</a>
        }
      </nav>
    }

    <main class="ac-page">
      <ng-content />
    </main>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        min-height: 100vh;
      }

      /* Flat and opaque, the same rule the marketing site's bar follows: the bar is chrome, and
         chrome that the page's lighting shows through reads as part of the content. So it is the
         ground, not the raised surface the panels use.

         'space-between' with the gutter as padding is what puts the contents at the window's
         edges while the bar's own bottom border still reaches both of them. */
      .bar {
        position: sticky;
        top: 0;
        z-index: 10;
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 1rem;
        height: 3rem;
        padding-inline: var(--ac-page-gutter);
        background: var(--ac-ground);
        border-block-end: 1px solid var(--ac-line);
      }

      /* The staff ground. Falls back to the customer's rather than to nothing, so a console that
         has not defined it gets a plain bar instead of a transparent one. */
      .bar.staff {
        background: var(--ac-staff-bar, var(--ac-ground));
      }

      .brand {
        display: flex;
        align-items: center;
        gap: 0.55rem;
        text-decoration: none;
      }

      /* The product name is the wordmark, so it is set in the wordmark's face: Barlow Condensed at
         weight 700, the same cut as material/word.svg. Running text stays Barlow.

         White beside the mark, not the accent — the same call the site's '.mark-word' makes, and
         for the same reason: the mark is what the eye should land on. */
      .brand-word {
        font-family: 'Barlow Condensed', Barlow, sans-serif;
        font-weight: 700;
        font-size: 1.15rem;
        letter-spacing: 0.01em;
        color: var(--ac-ink);
      }

      /* The mark sits in a box of its own rather than floating against the bar. The logo is a
         stroked outline with no fill, so at bar size it needs a ground to read against — and the
         box is what gives the corner a fixed shape whatever the mark inside it becomes. */
      .brand-box {
        display: grid;
        place-items: center;
        inline-size: 1.85rem;
        block-size: 1.85rem;
        border: 1px solid var(--ac-accent-line);
        border-radius: 4px;
        background: var(--ac-accent-soft);
      }

      .brand-box img {
        inline-size: 1.05rem;
        block-size: auto;
        display: block;
      }

      /* Set in the accent, which is amber on the staff console — so the word 'staff' is itself
         the colour that says which console this is. */
      .brand-badge {
        margin-inline-start: 0.15rem;
        font-size: 0.75rem;
        letter-spacing: 0.12em;
        text-transform: uppercase;
        color: var(--ac-accent);
        border: 1px solid var(--ac-accent-line);
        border-radius: 3px;
        padding: 0 0.35rem;
      }

      .identity {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        min-width: 0;
      }

      /* The nav shares the page's left edge and the page's rule: its underline is the same line
         that separates it from the content, so the active tab appears to sit on the page rather
         than on a strip of its own. 'margin-block-end: -1px' is what makes the active marker
         cover that line instead of stacking below it. */
      .nav {
        display: flex;
        gap: 1.5rem;
        border-block-end: 1px solid var(--ac-line);
      }

      .nav a {
        color: rgb(255 255 255 / 65%);
        text-decoration: none;
        padding-block: 0.75rem;
        border-block-end: 2px solid transparent;
        margin-block-end: -1px;
      }

      .nav a:hover {
        color: rgb(255 255 255 / 85%);
      }

      /* The accent by TOKEN, not by value. This rule once named '#177ddc' — the old theme's blue,
         pasted in by hand — which made it the one piece of the console no theme change could
         reach. By token it is also what makes the staff console's active tab amber without
         anybody editing this file. */
      .nav a.active {
        color: var(--ac-accent);
        border-block-end-color: var(--ac-accent);
      }

      /* The page. One measure for both consoles, from one token: it was 70rem in the staff shell
         and 78rem in the customer's, which was not a decision — it was two hand-written copies of
         one frame. The consoles differ in colour, deliberately; they do not differ in how wide a
         table is allowed to be. */
      main {
        flex: 1;
      }
    `,
  ],
})
export class ShellComponent {
  /** Where the mark leads. The customer console's front screen is `/projects`; staff has one. */
  readonly home = input<string>('/');
  /** The product name, set as the wordmark. */
  readonly product = input<string>('acmeservice');
  /** A word beside the mark naming this surface — `staff`. Null on the customer console. */
  readonly badge = input<string | null>(null);
  /** The bar's ground. Only `staff` differs, and only in the ground; see the class comment. */
  readonly tone = input<'customer' | 'staff'>('customer');
  /**
   * Where this person can go from here. A signal input, so an account switch that changes the
   * navigation re-renders it in place — the switcher used to call `location.reload()` for this,
   * which threw away the very state the switch existed to change.
   */
  readonly nav = input<readonly NavItem[]>([]);
}
