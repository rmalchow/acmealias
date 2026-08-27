import { Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzDropDownModule } from 'ng-zorro-antd/dropdown';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { AccountSwitcherComponent } from './features/accounts/account-switcher.component';
import { SessionService } from '@acme/shared';

interface NavItem {
  path: string;
  label: string;
}

/**
 * The screens of D11. Sessions and effective-configuration are deliberately NOT here: both are
 * still served by the management API (`/sessions`, `/config`) for the CLI and for support, but
 * neither is part of an operator's work — see the note in app.routes.ts.
 */
export const NAV: NavItem[] = [
  { path: '/projects', label: 'Projects' },
  { path: '/challenges', label: 'Live challenges' },
  { path: '/audit', label: 'Audit log' },
  // Roles and membership are an account administering itself. Reading the roster
  // needs no grant; changing it is refused by the service for anyone who is not
  // an unrestricted member, so the link is offered to everyone and the screen
  // says what they may do.
  { path: '/roles', label: 'Roles' },
];

@Component({
  selector: 'app-root',
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    NzButtonModule,
    NzDropDownModule,
    NzIconModule,
    NzMenuModule,
    NzModalModule,
    AccountSwitcherComponent,
  ],
  templateUrl: './app.component.html',
  styleUrl: './app.component.css',
})
export class AppComponent {
  readonly session = inject(SessionService);
  // Every item, for everybody. Navigation is not filtered by permission: a link
  // that vanishes teaches nothing, and a screen that refuses with a reason teaches
  // what to ask for. Each destination says what this caller may do once there.
  readonly nav = NAV;
}
