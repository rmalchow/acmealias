import { ChangeDetectionStrategy, Component, TemplateRef, computed, input, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzIconModule } from 'ng-zorro-antd/icon';

/**
 * Where an info box remembers whether it is open.
 *
 * One key per box, namespaced, so two boxes on one screen are two decisions and a box that is
 * renamed starts open again rather than inheriting a stranger's state.
 */
const STORAGE_PREFIX = 'acme.infobox.';

/**
 * An info box that can be folded away to its heading, and stays that way.
 *
 * These boxes are all the same shape: a sentence or six explaining what a screen is, which is
 * exactly what somebody needs on their first visit and exactly what is in their way on their
 * fiftieth. The console had no answer to that — the text was either present for everybody forever
 * or written for nobody — so the boxes grew shorter than they should be, which is the wrong fix.
 * Collapsing is the right one: it lets the copy be as long as the subject needs while costing a
 * returning reader one line.
 *
 * OPEN BY DEFAULT, and that is the whole argument for which default. A box collapsed on first
 * sight is a box nobody opens, and some of these carry things a customer genuinely has to read
 * once — what a project key reaches, what a CAA record with an account URI prevents. The reader
 * closes it when they have read it; the console does not decide they have.
 *
 * The state is per browser and per box in localStorage, which is the right storage for exactly
 * this: a preference about presentation, worth nothing to anybody else, and free to be lost. A
 * private window, cleared site data or a second machine simply shows the box open again, which is
 * the same thing a first visit does. Every access is wrapped, because reading it THROWS rather
 * than returning null in some contexts.
 *
 * The body arrives as a TemplateRef rather than through <ng-content>, so it is only instantiated
 * while the box is open — projected content in a conditional template is the one Angular corner
 * where "is it in the DOM" stops being obvious, and a collapsed box must not be paying for markup
 * nobody can see. Callers that only have a sentence pass `text` instead.
 */
@Component({
  selector: 'app-info-box',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgTemplateOutlet, NzAlertModule, NzIconModule],
  template: `
    <nz-alert
      class="box"
      [class.collapsed]="!open()"
      [nzType]="type()"
      nzShowIcon
      [nzMessage]="head"
      [nzDescription]="open() ? content : null"
    />

    <ng-template #head>
      <!--
        The heading IS the control. A separate chevron button beside it would be a second target
        for one action, and the heading of a box that folds is the thing everybody clicks anyway.
        aria-expanded is what makes that legible to a screen reader; the chevron is decoration and
        says so.
      -->
      <button type="button" class="toggle" [attr.aria-expanded]="open()" (click)="toggle()">
        <span class="heading">{{ heading() }}</span>
        <span class="chevron" nz-icon nzType="down" aria-hidden="true"></span>
      </button>
    </ng-template>

    <ng-template #content>
      @if (body(); as tpl) {
        <ng-container *ngTemplateOutlet="tpl" />
      } @else {
        {{ text() }}
      }
    </ng-template>
  `,
  styles: [
    `
      /* THE HOST IS BLOCK, and this is the line whose absence broke the spacing on two screens.
         A component element has no default display, so it is INLINE — and a margin-block on an
         inline box is ignored outright. Every call site that had asked for room around its alert
         (.intro on the project list, .panel-note on the keys panel) went on asking, correctly, and
         got nothing, so the box sat flush against the table under it. Setting it here rather than
         at each call site is what stops the next one rediscovering it. */
      :host {
        display: block;
      }
      .box {
        display: block;
      }
      /* The alert's own description padding is what would leave a collapsed box a few pixels
         taller than its heading. With no description rendered there is nothing to pad, so this
         only tightens the message row itself. */
      .collapsed ::ng-deep .ant-alert-message {
        margin-bottom: 0;
      }
      .toggle {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        width: 100%;
        padding: 0;
        border: 0;
        background: none;
        font: inherit;
        color: inherit;
        text-align: left;
        cursor: pointer;
      }
      .heading {
        font-weight: 500;
      }
      /* One icon for both states, turned over. Registering a second glyph in every application
         that uses this — the staff console registers three icons in total — to say "up" when the
         one already there says "down" upside down is not worth the import. */
      .chevron {
        margin-inline-start: auto;
        font-size: 0.75rem;
        opacity: 0.65;
        transition: transform 0.15s ease;
      }
      .open .chevron,
      .toggle[aria-expanded='true'] .chevron {
        transform: rotate(180deg);
      }
    `,
  ],
})
export class InfoBoxComponent {
  /** Distinguishes this box's remembered state from every other box's. Stable across releases. */
  readonly key = input.required<string>();
  readonly heading = input.required<string>();
  /** The body, for a caller that has markup. */
  readonly body = input<TemplateRef<unknown> | null>(null);
  /** The body, for a caller that has a sentence. Ignored when `body` is set. */
  readonly text = input('');
  /** `info` unless a caller has a reason; the type is the alert's, not a second vocabulary. */
  readonly type = input<'info' | 'warning' | 'error' | 'success'>('info');

  private readonly closed = signal<Set<string>>(readClosed());

  readonly open = computed(() => !this.closed().has(this.key()));

  toggle(): void {
    const key = this.key();
    const next = new Set(this.closed());
    if (next.has(key)) next.delete(key);
    else next.add(key);
    this.closed.set(next);
    write(key, !next.has(key));
  }
}

/**
 * Which boxes this browser has closed.
 *
 * Read once per component rather than kept in a service: there is no shared state to coordinate —
 * two boxes never disagree about one key, because a key belongs to one box — and a service would
 * be an injection every caller has to remember for a preference that fits in a string.
 */
function readClosed(): Set<string> {
  const out = new Set<string>();
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k?.startsWith(STORAGE_PREFIX) && localStorage.getItem(k) === 'closed') {
        out.add(k.slice(STORAGE_PREFIX.length));
      }
    }
  } catch {
    // Storage can be unavailable or throw on access — a private window, blocked site data, a
    // thumbnail renderer. Every box then reads as open, which is what a first visit looks like.
  }
  return out;
}

function write(key: string, open: boolean): void {
  try {
    if (open) localStorage.removeItem(STORAGE_PREFIX + key);
    else localStorage.setItem(STORAGE_PREFIX + key, 'closed');
  } catch {
    // Nothing is remembered; the box still opens and closes for this visit.
  }
}
