import { TestBed } from '@angular/core/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { NzModalModule, NzModalService } from 'ng-zorro-antd/modal';

import { confirmAction } from '@acme/shared';

/**
 * The confirmation prompt, driven the way a person drives it.
 *
 * THIS EXISTS BECAUSE THE FAILURE IT CATCHES IS SILENT. `confirmAction` shipped without an
 * `nzOnOk`, and ng-zorro's `trigger('ok')` does nothing at all when there is no handler — it does
 * not close, does not resolve and does not throw. Every delete and every key revocation in the
 * console therefore did nothing when confirmed: no request, no error, no console output, nothing
 * in the network tab. It looked like a dead button, it was reported as one twice, and it was
 * diagnosed twice as something else entirely.
 *
 * A unit test of the helper's return value would not have caught it — the promise never settles,
 * so an `await` in a spec hangs rather than fails. What catches it is pressing the button and
 * requiring an answer within the test's own timeout, which is what this does.
 */
describe('confirmAction', () => {
  beforeEach(() =>
    TestBed.configureTestingModule({
      // NzModalModule brings the service and the overlay wiring it needs; the animations provider
      // is what lets a dialog finish opening and closing inside a synchronous test.
      imports: [NzModalModule],
      providers: [provideNoopAnimations()],
    }),
  );

  afterEach(() => {
    TestBed.inject(NzModalService).closeAll();
    document.querySelectorAll('.cdk-overlay-container').forEach((n) => n.remove());
  });

  function press(selector: string): void {
    const button = document.querySelector<HTMLButtonElement>(`.ant-modal-confirm-btns ${selector}`);
    if (!button) throw new Error(`the confirmation has no ${selector} button`);
    button.click();
  }

  it('resolves true when the act is confirmed', async () => {
    const modal = TestBed.inject(NzModalService);
    const answer = confirmAction(modal, { title: 'Delete domain?', message: 'It stops resolving.', okText: 'Delete', danger: true });
    await new Promise((r) => setTimeout(r, 0));

    press('.ant-btn-dangerous, .ant-btn-primary');
    // The assertion is that this settles AT ALL. Before the fix it never did, and the spec would
    // fail on Jasmine's timeout rather than on the value.
    await expectAsync(answer).toBeResolvedTo(true);
  });

  it('resolves false when it is cancelled', async () => {
    const modal = TestBed.inject(NzModalService);
    const answer = confirmAction(modal, { title: 'Delete domain?', message: 'It stops resolving.' });
    await new Promise((r) => setTimeout(r, 0));

    press('.ant-btn:not(.ant-btn-primary):not(.ant-btn-dangerous)');
    await expectAsync(answer).toBeResolvedTo(false);
  });
});
