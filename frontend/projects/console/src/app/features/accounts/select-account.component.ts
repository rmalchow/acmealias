import { ChangeDetectionStrategy, Component, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzFormModule } from 'ng-zorro-antd/form';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { AccountsService } from './accounts.service';
import { apiErrorMessage } from '@acme/shared';

/**
 * The shell a session with no account selected sees (design P19, task 9.0).
 *
 * From this state exactly four things are reachable: the profile, this list, creating an account,
 * and accepting an invitation. Everything else is refused by the service with a statement that no
 * account is selected — not with an empty result, which would read as "you have nothing".
 *
 * So this screen is not decoration around an error. It is the only place a person in that state
 * can do anything at all, and it has to make the way out obvious.
 */
@Component({
  selector: 'app-select-account',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, NzButtonModule, NzCardModule, NzFormModule, NzInputModule, NzAlertModule],
  template: `
    <div class="ac-select">
      <h1>Choose an account</h1>

      @if (accounts.accounts().length > 0) {
        <p class="ac-lead">Everything in this console belongs to one account. Pick the one you want to work in.</p>
        <div class="ac-list">
          @for (a of accounts.accounts(); track a.id) {
            <nz-card [nzHoverable]="true" (click)="choose(a.id)">
              <h3>{{ a.name }}</h3>
              <code>{{ a.slug }}</code>
            </nz-card>
          }
        </div>
      } @else {
        <nz-alert
          nzType="info"
          nzMessage="You do not belong to an account yet"
          nzDescription="Ask someone who does to invite you, or create one below if this deployment allows it."
        />
      }

      <h2>Create an account</h2>
      <p class="ac-lead">
        A new account starts empty and you become its administrator. Deployments that authenticate a
        whole organization usually have this switched off.
      </p>
      <form nz-form nzLayout="inline" (ngSubmit)="create()">
        <nz-form-item>
          <nz-form-control>
            <input nz-input name="slug" [(ngModel)]="slug" placeholder="slug" required />
          </nz-form-control>
        </nz-form-item>
        <nz-form-item>
          <nz-form-control>
            <input nz-input name="name" [(ngModel)]="name" placeholder="Name" required />
          </nz-form-control>
        </nz-form-item>
        <button nz-button nzType="primary" [disabled]="!slug() || !name()">Create</button>
      </form>

      @if (error(); as e) {
        <nz-alert nzType="error" [nzMessage]="e" />
      }
    </div>
  `,
  styles: [
    `
      .ac-select {
        max-width: 44rem;
        margin: 3rem auto;
      }
      .ac-lead {
        opacity: 0.75;
      }
      .ac-list {
        display: grid;
        gap: 0.75rem;
        grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
        margin-block-end: 2rem;
      }
      .ac-list h3 {
        margin: 0;
      }
    `,
  ],
})
export class SelectAccountComponent implements OnInit {
  protected readonly accounts = inject(AccountsService);
  private readonly router = inject(Router);

  protected readonly slug = signal('');
  protected readonly name = signal('');
  protected readonly error = signal<string | null>(null);

  ngOnInit(): void {
    void this.accounts.load();
  }

  protected async choose(id: string): Promise<void> {
    this.error.set(null);
    try {
      await this.accounts.select(id);
      await this.router.navigateByUrl('/projects');
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    }
  }

  protected async create(): Promise<void> {
    this.error.set(null);
    try {
      await this.accounts.create(this.slug(), this.name());
      await this.router.navigateByUrl('/projects');
    } catch (err) {
      this.error.set(apiErrorMessage(err));
    }
  }
}
