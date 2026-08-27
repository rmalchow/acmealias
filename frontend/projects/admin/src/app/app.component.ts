import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { LOGOUT_URL, SessionService } from '@acme/shared';

/**
 * The staff console's shell.
 *
 * It exists before the screens it will hold, and that is the point: the hostname,
 * the identity-provider application and the ingress restriction that protect this
 * surface are worth having in place and exercised while there is nothing behind
 * them to lose. `staff-plane` fills it in.
 *
 * The visible difference from the customer console is deliberate and should stay.
 * A member of staff will have both open, and two consoles that look identical is
 * how somebody suspends the wrong account.
 */
@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterOutlet, NzButtonModule, NzIconModule],
  template: `
    <header class="bar">
      <div class="mark">
        <span class="dot"></span>
        <strong>acmeservice</strong>
        <span class="tag">staff</span>
      </div>
      <div class="who">
        @if (name(); as n) {
          <span class="name">{{ n }}</span>
        }
        <a nz-button nzType="text" [href]="logoutUrl">
          <span nz-icon nzType="logout"></span>
          Sign out
        </a>
      </div>
    </header>
    <main>
      <router-outlet />
    </main>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        min-height: 100vh;
      }
      /* Amber, and not the console's blue. Two surfaces that look the same are
         how somebody acts on the wrong one. */
      .bar {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 1rem;
        padding: 0.6rem 1.5rem;
        border-block-end: 1px solid rgb(255 255 255 / 12%);
        background: #1f1a0e;
      }
      /* The product name is the wordmark, so it is set in the wordmark's face — Barlow Condensed
         at weight 700, the same cut as material/word.svg. Running text stays Barlow. */
      .mark strong {
        font-family: 'Barlow Condensed', Barlow, sans-serif;
        font-weight: 700;
        font-size: 1.15rem;
      }
      .mark {
        display: flex;
        align-items: center;
        gap: 0.6rem;
      }
      .dot {
        inline-size: 0.6rem;
        block-size: 0.6rem;
        border-radius: 50%;
        background: #faad14;
      }
      .tag {
        font-size: 0.75rem;
        letter-spacing: 0.12em;
        text-transform: uppercase;
        color: #faad14;
        border: 1px solid rgb(250 173 20 / 40%);
        border-radius: 3px;
        padding: 0 0.35rem;
      }
      .who {
        display: flex;
        align-items: center;
        gap: 0.75rem;
      }
      .name {
        opacity: 0.75;
      }
      main {
        flex: 1;
        padding: 2rem 1.5rem;
        max-inline-size: 70rem;
        inline-size: 100%;
        margin-inline: auto;
      }
    `,
  ],
})
export class AppComponent {
  private readonly session = inject(SessionService);

  protected readonly logoutUrl = LOGOUT_URL;
  protected readonly name = computed(() => this.session.me()?.displayName ?? null);
}
