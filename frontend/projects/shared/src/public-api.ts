/*
 * What both consoles share.
 *
 * The split is by AUDIENCE, not by convenience: anything in here is reached by a
 * customer and by a member of staff alike, so a change to it is a change to both
 * surfaces. Feature screens live in the application that owns them, which is what
 * keeps the staff plane's screens out of the bundle a customer downloads.
 *
 * Consumed through the `@acme/shared` path mapping in tsconfig.json, which points
 * at this source rather than at a built package: one fewer build step, and a
 * change to a service is picked up by both applications' watchers immediately.
 */

// Talking to the service.
export * from './lib/core/api.service';
export * from './lib/core/session.service';
export * from './lib/core/auth.interceptor';

// Deciding whether a screen may be entered at all.
export * from './lib/core/account.guard';
export * from './lib/core/role.guard';

// The vocabulary both surfaces describe the same objects with.
export * from './lib/models';

// Small pieces of interface that would otherwise be written twice.
export * from './lib/core/message.component';
export * from './lib/core/wildcard.validator';
export * from './lib/ui/confirm';
export * from './lib/ui/page-header.component';
export * from './lib/ui/help-hint.component';
export * from './lib/ui/info-box.component';
export * from './lib/ui/name-slug.modal';

// The chrome around every screen. Both applications render it as their root component's body;
// what differs is what they put in it, not the frame itself.
export * from './lib/ui/shell.component';
export * from './lib/ui/signed-out.component';
