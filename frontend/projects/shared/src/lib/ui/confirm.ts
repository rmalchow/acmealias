import { NzModalService } from 'ng-zorro-antd/modal';
import { firstValueFrom } from 'rxjs';

export interface ConfirmOptions {
  title: string;
  message: string;
  /** Label of the button that goes through with it. Name the ACT — "Delete", not "OK". */
  okText?: string;
  /** Destructive actions get the danger colour; everything else stays neutral. */
  danger?: boolean;
}

/**
 * The one confirmation prompt in the console, replacing `window.confirm`.
 *
 * Not cosmetic. A native `confirm()` blocks the page, cannot show which object it is about in
 * more than a line of unstyled text, and — the reason it had to go — is suppressible per origin
 * by the browser: a user who once ticked "don't show me these again" would delete a load balancer on
 * the first click, with no prompt at all.
 */
export function confirmAction(modal: NzModalService, o: ConfirmOptions): Promise<boolean> {
  const ref = modal.confirm({
    nzTitle: o.title,
    nzContent: o.message,
    nzOkText: o.okText ?? 'Confirm',
    nzOkDanger: o.danger ?? false,
    nzCancelText: 'Cancel',
    // THESE TWO HANDLERS ARE LOAD-BEARING, AND THEIR ABSENCE BROKE EVERY
    // CONFIRMATION IN BOTH CONSOLES.
    //
    // ng-zorro's modal resolves the OK button through `trigger('ok')`, which
    // reads `nzOnOk` and then does one of exactly three things: emit, if it is
    // an EventEmitter; call it and close with what it returned, if it is a
    // function; or — with no handler at all — FALL OFF THE END OF THE IF AND DO
    // NOTHING. Not close, not resolve, not throw. So pressing Delete left the
    // dialog open, `afterClose` never emitted, and the promise this function
    // returns never settled: no request, no error, nothing in the console and
    // nothing in the network tab. Every delete and every revocation in the
    // console failed that way, silently, and looked like a dead button.
    //
    // THE ONE VALUE A HANDLER MUST NOT RETURN IS `false`, and it is the obvious
    // thing to write for Cancel. `closeWhitResult` reads `false` as "stay open"
    // — it is the veto, not the answer — so `nzOnCancel: () => false` produced a
    // Cancel button that did nothing at all, which is the same dead-button
    // symptom one step to the left.
    //
    // So: OK closes with `true`, Cancel closes with `null`. Both are "close",
    // and only one of them is the confirmation.
    nzOnOk: () => true,
    // Void, not `null` — the callback's type allows `false | void | {}`, and of
    // those `false` is the veto. Returning nothing closes the modal with
    // `undefined`, which the comparison below reads as "not confirmed".
    nzOnCancel: () => {
      /* close, carrying no result */
    },
  });
  // `afterClose` carries whatever the modal closed WITH, and a dismissal — mask
  // click, Escape, the X — closes with `undefined`. Comparing against `true`
  // rather than truthiness is what makes those three paths mean "no" instead of
  // "no answer".
  return firstValueFrom(ref.afterClose).then((result) => result === true);
}
