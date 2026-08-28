import { Component, input, signal } from '@angular/core';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzPopoverModule } from 'ng-zorro-antd/popover';

/**
 * The question mark beside a headline, and the box it opens.
 *
 * A POPOVER RATHER THAN A TOOLTIP, because of what it has to hold. A tooltip is one line about
 * one control — "Look up every domain's delegation now" — and this is two or three sentences
 * explaining what a whole panel IS. Put that in a tooltip and it renders as a grey ribbon the
 * width of the page; the popover is a box with a title, which is the shape the content already
 * has.
 *
 * A BUTTON rather than a span with an icon in it, so the thing is reachable by keyboard and
 * announced as something that can be operated. ng-zorro's popover takes exactly one trigger and
 * `hover` does not fire on focus, so focus and blur drive the visibility directly alongside it —
 * the two coexist because `nzPopoverVisible` is two-way and hovering writes the same signal.
 *
 * The copy itself lives at the call site, not here. Every one of these paragraphs is about the
 * panel it sits on, and a component holding a dictionary of them would put the words a long way
 * from the thing they describe — which is how help text goes stale without anybody noticing.
 */
@Component({
  selector: 'app-help-hint',
  imports: [NzIconModule, NzPopoverModule],
  template: `
    <button
      type="button"
      class="hint"
      nz-popover
      nzPopoverTrigger="hover"
      nzPopoverPlacement="bottomLeft"
      nzPopoverOverlayClassName="ac-help-popover"
      [nzPopoverTitle]="heading()"
      [nzPopoverContent]="body"
      [(nzPopoverVisible)]="visible"
      [attr.aria-label]="'What is this? ' + heading()"
      (focus)="visible.set(true)"
      (blur)="visible.set(false)"
    >
      <span nz-icon nzType="question-circle"></span>
    </button>

    <ng-template #body>
      @for (p of paragraphs(); track $index) {
        <p>{{ p }}</p>
      }
    </ng-template>
  `,
  styles: [
    `
      :host {
        /* Inline, so it flows with the headline's text baseline instead of becoming another item
         * in the panel-head flex row — where the margin-inline-start:auto on the second child
         * would have thrown it against the right edge, next to the buttons. */
        display: inline;
      }
      .hint {
        border: 0;
        padding: 0 0 0 0.375rem;
        background: none;
        color: inherit;
        line-height: 1;
        cursor: help;
        /* It sits INSIDE the h2, so it would otherwise inherit the headline's size and weight and
         * read as punctuation in the title. Normal weight and a fraction of the size keep it a
         * mark beside the words rather than one of them. */
        font-size: 0.8em;
        font-weight: 400;
        /* Visible enough to be found without being read first. The headline is still what the eye
         * lands on, but at 0.4 this was a smudge nobody would think to point at. Short of full
         * strength on purpose — an icon as bright as the heading competes with it. */
        opacity: 0.65;
      }
      .hint:hover,
      .hint:focus-visible {
        opacity: 0.95;
      }
      /* The box is prose, so it gets a measure. Without one it grows to the widest paragraph and
       * becomes a single long line across the screen. */
      ::ng-deep .ac-help-popover .ant-popover-inner-content {
        max-width: 26rem;
      }
      ::ng-deep .ac-help-popover p {
        margin: 0 0 0.5rem;
      }
      ::ng-deep .ac-help-popover p:last-child {
        margin-bottom: 0;
      }
    `,
  ],
})
export class HelpHintComponent {
  /** The box's title — normally the headline the icon sits beside. */
  readonly heading = input.required<string>();

  /** One string per paragraph. Rendered in order, and nothing else is rendered. */
  readonly paragraphs = input.required<string[]>();

  protected readonly visible = signal(false);
}
