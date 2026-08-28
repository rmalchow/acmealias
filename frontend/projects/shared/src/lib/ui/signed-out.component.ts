import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { SessionService } from '../core/session.service';

/**
 * What somebody sees when they are not signed in.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * WHY THIS EXISTS RATHER THAN A REDIRECT
 * ─────────────────────────────────────────────────────────────────────────────
 * Until now an unauthenticated first load called `location.assign('/api/v1/auth/login')` from the
 * HTTP interceptor, so a stranger who typed the console's address was thrown, without a word, onto
 * a DIFFERENT HOSTNAME running DIFFERENT SOFTWARE that looks like a login page.
 *
 * That is the exact shape of a phishing redirect, and a careful person is right to hesitate at it.
 * The people most likely to stop are the ones who have been trained to check the address bar —
 * which is to say the security-minded operators this product is for.
 *
 * One screen fixes it. It is served from the address they typed, it is wearing the product's own
 * chrome, it says what is about to happen, and the hand-off is something THEY press. The identity
 * provider is then a step they chose rather than a page they were thrown at.
 *
 * It also gives sign-up somewhere to live. A redirect has exactly one destination and no room for a
 * second sentence, so "or create an account" had nowhere to be said.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ONE BUTTON, NOT TWO
 * ─────────────────────────────────────────────────────────────────────────────
 * "Log in" and "Sign up" are two buttons everywhere else in the world, and here they would be two
 * buttons that do the identical thing: the identity provider decides which form to show, and a
 * person who has an account and pressed "Sign up" ends up signed in anyway. Two controls with one
 * behaviour is a promise the page cannot keep — so it is one control, labelled with both, which is
 * also literally what happens next.
 *
 * The marketing site's "Sign Up Now" does NOT land here; it links straight to the redirect. Somebody
 * who has already pressed a button saying what they want should not be shown a second button
 * saying it again.
 */
@Component({
  selector: 'app-signed-out',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NzButtonModule, NzIconModule],
  template: `
    <main class="out">
      <img class="mark" src="brand/logo.svg" alt="" />
      <h1>{{ product() }}</h1>
      <p>{{ blurb() }}</p>
      <button nz-button nzType="primary" nzSize="large" (click)="session.signIn()">
        <span nz-icon nzType="login"></span>
        Log in or sign up
      </button>
      <p class="where">You will continue at {{ idpLabel() }}.</p>
    </main>
  `,
  styles: [
    `
      .out {
        max-width: 26rem;
        margin: max(8vh, 3rem) auto;
        padding-inline: var(--ac-page-gutter);
        text-align: center;
        display: grid;
        justify-items: center;
        gap: 0.75rem;
      }

      /* NO BOX. The mark draws its own rectangle — it is a stroked outline around the
         letterforms, and that outline IS the box. Putting a second one around it gives the corner
         a box inside a box, which is what the shell's bar used to do and why it stopped. */
      .mark {
        block-size: 3rem;
        inline-size: auto;
        display: block;
      }

      h1 {
        margin: 0.5rem 0 0;
        font-family: 'Barlow Condensed', Barlow, sans-serif;
        font-weight: 400;
        font-size: 2rem;
        line-height: 1;
        color: var(--ac-ink);
      }

      p {
        margin: 0;
        color: var(--ac-muted);
      }

      /* Named, and named quietly. Somebody about to be sent to another hostname should be able to
         read which one BEFORE they press, not discover it in the address bar afterwards — that is
         the whole difference between a hand-off and a redirect. It is small because it is
         reassurance, not an instruction. */
      .where {
        font-size: 0.8125rem;
        opacity: 0.75;
      }
    `,
  ],
})
export class SignedOutComponent {
  protected readonly session = inject(SessionService);

  readonly product = input<string>('AcmeAlias');
  readonly blurb = input<string>('Certificates for domains whose DNS stays where it is.');

  /** The hostname the hand-off lands on, so the page can name it. */
  readonly idpLabel = input<string>('our sign-in page');
}
