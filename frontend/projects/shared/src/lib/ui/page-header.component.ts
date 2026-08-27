import { Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NzBreadCrumbModule } from 'ng-zorro-antd/breadcrumb';

/** One crumb in the trail. A null `link` is the current page — rendered as text, never a link. */
export interface Crumb {
  label: string;
  link?: string | unknown[] | null;
}

/**
 * The top of every screen: breadcrumb, headline, subheadline — in that order, always all three.
 *
 * The point is positional, not decorative. Every screen renders this component first and nothing
 * else sets a top margin, so the first row of real content — the filter row, the first table row
 * — begins at the same y coordinate on every page. Moving between Projects and the audit log
 * then does not shift the thing the operator is about to click.
 *
 * "Always all three" is enforced by reserving the space, not by demanding the text: a screen with
 * nothing to say in the subheadline still occupies the line, because collapsing it would move
 * everything below it up by exactly the amount this component exists to prevent.
 */
@Component({
  selector: 'app-page-header',
  imports: [RouterLink, NzBreadCrumbModule],
  template: `
    <header class="ac-page-header">
      <nz-breadcrumb>
        @for (c of crumbs(); track c.label; let last = $last) {
          <nz-breadcrumb-item>
            @if (c.link && !last) {
              <a [routerLink]="c.link">{{ c.label }}</a>
            } @else {
              <span>{{ c.label }}</span>
            }
          </nz-breadcrumb-item>
        }
      </nz-breadcrumb>

      <h1>{{ title() }}</h1>
      <p class="subtitle">{{ subtitle() }}</p>
    </header>
  `,
  styles: [
    `
      .ac-page-header {
        margin-bottom: 1.25rem;
      }
      nz-breadcrumb {
        display: block;
        height: 1.375rem;
        font-size: 0.8125rem;
      }
      h1 {
        margin: 0.25rem 0 0;
        font-size: 1.5rem;
        line-height: 2rem;
        font-weight: 500;
        color: rgb(255 255 255 / 85%);
      }
      .subtitle {
        /* Reserved whether or not there is text — see the class comment. */
        min-height: 1.375rem;
        margin: 0.125rem 0 0;
        font-size: 0.875rem;
        color: rgb(255 255 255 / 45%);
      }
    `,
  ],
})
export class PageHeaderComponent {
  readonly crumbs = input<Crumb[]>([]);
  readonly title = input.required<string>();
  readonly subtitle = input<string | null>(null);
}
