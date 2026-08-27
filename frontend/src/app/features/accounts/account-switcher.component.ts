import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzMessageService } from 'ng-zorro-antd/message';
import { AccountsService } from './accounts.service';
import { apiErrorMessage } from '../../core/api.service';

/**
 * The account this session is acting in, and a way to change it.
 *
 * It renders NOTHING when there is one account to choose from, which is the whole internal
 * edition — constraint 1 says that deployment must not observably change, and a switcher with one
 * entry is a change. It is not hidden by configuration; it is absent because there is no choice
 * to offer.
 */
@Component({
  selector: 'app-account-switcher',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NzButtonModule, NzDropDownModule, NzIconModule],
  template: `
    @if (visible()) {
      <button nz-button nz-dropdown [nzDropdownMenu]="menu" nzPlacement="bottomRight" class="ac-account">
        <span nz-icon nzType="cluster"></span>
        {{ currentName() }}
        <span nz-icon nzType="down"></span>
      </button>
      <nz-dropdown-menu #menu="nzDropdownMenu">
        <ul nz-menu>
          @for (a of accounts.accounts(); track a.id) {
            <li nz-menu-item [nzSelected]="a.selected" (click)="choose(a.id)">
              {{ a.name }}
              <small class="ac-muted">{{ a.slug }}</small>
            </li>
          }
          <li nz-menu-divider></li>
          <!--
            Leaving is not signing out. "No account selected" is a state the service understands
            and answers distinctly, so a person who has finished with one account can get back to
            the picker without ending their session.
          -->
          <li nz-menu-item (click)="leave()">Leave this account</li>
        </ul>
      </nz-dropdown-menu>
    }
  `,
  styles: [
    `
      .ac-account {
        margin-inline-end: 0.5rem;
      }
      .ac-muted {
        opacity: 0.6;
        margin-inline-start: 0.5rem;
      }
    `,
  ],
})
export class AccountSwitcherComponent implements OnInit {
  protected readonly accounts = inject(AccountsService);
  private readonly message = inject(NzMessageService);

  protected readonly switching = signal(false);

  protected readonly visible = computed(() => this.accounts.hasChoice());
  protected readonly currentName = computed(() => this.accounts.current()?.name ?? 'No account');

  ngOnInit(): void {
    void this.accounts.load();
  }

  protected async leave(): Promise<void> {
    if (this.switching()) return;
    this.switching.set(true);
    try {
      await this.accounts.leave();
      location.assign('/accounts');
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    } finally {
      this.switching.set(false);
    }
  }

  protected async choose(id: string): Promise<void> {
    if (this.switching()) return;
    this.switching.set(true);
    try {
      await this.accounts.select(id);
      // Everything on screen belongs to the account that just changed, so the
      // honest thing is to start again rather than to leave half of it stale.
      location.reload();
    } catch (err) {
      this.message.error(apiErrorMessage(err));
    } finally {
      this.switching.set(false);
    }
  }
}
