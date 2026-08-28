import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { LOGOUT_URL, SessionService, ShellComponent } from '@acme/shared';

/**
 * The staff console's shell.
 *
 * It exists before the screens it will hold, and that is the point: the hostname, the
 * identity-provider application and the ingress restriction that protect this surface are worth
 * having in place and exercised while there is nothing behind them to lose. `staff-plane` fills
 * it in.
 *
 * THE FRAME IS THE CUSTOMER CONSOLE'S, and that is a change from what stood here before. This
 * file used to hand-draw its own bar — its own mark box, its own wordmark rule, its own measure
 * for `main` (70rem, against the customer's 78rem, which was not a decision but a second
 * hand-written copy). A member of staff and a customer now look at the same chrome, from
 * projects/shared/src/lib/ui/shell.component.ts.
 *
 * The visible difference is still deliberate and still there — a member of staff will have both
 * open, and two consoles that look identical is how somebody suspends the wrong account. It is
 * carried by the two things that actually differ: the accent, which theme.less makes amber, so
 * every button and focus ring below the bar is amber rather than green; and the bar's ground,
 * which `tone="staff"` darkens to amber-cast black. Making the LAYOUT differ as well was what we
 * had, and it bought nothing: nobody tells two consoles apart by where the sign-out link sits.
 *
 * There is no navigation, because there is one screen. The shell renders none rather than a rule
 * across an empty row.
 */
@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterOutlet, ShellComponent, NzButtonModule, NzIconModule],
  template: `
    <app-shell home="/" badge="staff" tone="staff">
      <div shellIdentity class="who">
        @if (name(); as n) {
          <span class="name">{{ n }}</span>
        }
        <a nz-button nzType="text" [href]="logoutUrl">
          <span nz-icon nzType="logout"></span>
          Sign out
        </a>
      </div>
      <router-outlet />
    </app-shell>
  `,
  styles: [
    `
      /* All that is left of this console's own chrome: the name beside the sign-out link. The bar,
         the mark, the wordmark and the page's measure come from the shared shell. */
      .who {
        display: flex;
        align-items: center;
        gap: 0.75rem;
      }
      .name {
        opacity: 0.75;
      }
    `,
  ],
})
export class AppComponent {
  private readonly session = inject(SessionService);

  protected readonly logoutUrl = LOGOUT_URL;
  protected readonly name = computed(() => this.session.me()?.displayName ?? null);
}
