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
  });
  return firstValueFrom(ref.afterClose).then((result) => result === true);
}
