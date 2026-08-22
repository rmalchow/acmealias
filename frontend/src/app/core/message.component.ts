import { Component, inject } from '@angular/core';
import { ActivatedRoute } from '@angular/router';

/** The shell's own two dead ends: an unknown route, and a route the user's roles do not cover. */
@Component({
  selector: 'app-message',
  template: `<h2>{{ heading }}</h2>
    <p>{{ detail }}</p>`,
})
export class MessageComponent {
  private readonly data = inject(ActivatedRoute).snapshot.data;
  readonly heading: string = this.data['heading'] ?? 'Not found';
  readonly detail: string = this.data['detail'] ?? 'That page does not exist.';
}
