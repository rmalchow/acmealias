import { TestBed } from '@angular/core/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { InfoBoxComponent } from './info-box.component';

function build(key: string, text = 'the body of the box') {
  TestBed.configureTestingModule({
    imports: [InfoBoxComponent],
    providers: [provideNoopAnimations(), provideNzIconsTesting()],
  });
  const fixture = TestBed.createComponent(InfoBoxComponent);
  fixture.componentRef.setInput('key', key);
  fixture.componentRef.setInput('heading', 'A heading');
  fixture.componentRef.setInput('text', text);
  fixture.detectChanges();
  return fixture;
}

describe('InfoBoxComponent', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => {
    localStorage.clear();
    TestBed.resetTestingModule();
  });

  it('starts open, because a box collapsed on first sight is a box nobody opens', () => {
    const fixture = build('first');
    expect(fixture.componentInstance.open()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain('the body of the box');
  });

  it('collapses to the heading, with the body out of the DOM rather than merely hidden', () => {
    const fixture = build('second');
    fixture.componentInstance.toggle();
    fixture.detectChanges();

    expect(fixture.componentInstance.open()).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain('A heading');
    expect(fixture.nativeElement.textContent).not.toContain('the body of the box');
  });

  it('remembers being closed, per box, in the next component that renders', () => {
    const first = build('remembered');
    first.componentInstance.toggle();
    expect(localStorage.getItem('acme.infobox.remembered')).toBe('closed');

    TestBed.resetTestingModule();
    expect(build('remembered').componentInstance.open()).toBeFalse();
    TestBed.resetTestingModule();
    // A different box is a different decision: closing one must not close the rest.
    expect(build('a-different-box').componentInstance.open()).toBeTrue();
  });

  it('stores nothing for a box that is open, so the default can change without stale keys', () => {
    const fixture = build('reopened');
    fixture.componentInstance.toggle();
    fixture.componentInstance.toggle();
    expect(fixture.componentInstance.open()).toBeTrue();
    expect(localStorage.getItem('acme.infobox.reopened')).toBeNull();
  });

  it('renders open when storage is unavailable, rather than failing to render', () => {
    // A private window, blocked site data, a thumbnail renderer: reading localStorage THROWS in
    // some contexts rather than returning null, and a box that took the console down with it
    // would be a preference costing a screen.
    const getItem = spyOn(Storage.prototype, 'getItem').and.throwError('SecurityError');
    const key = spyOn(Storage.prototype, 'key').and.throwError('SecurityError');
    expect(() => build('unstorable')).not.toThrow();
    getItem.and.callThrough();
    key.and.callThrough();
  });

  it('announces whether it is expanded, since the heading is the control', () => {
    const fixture = build('aria');
    const toggle: HTMLElement = fixture.nativeElement.querySelector('.toggle');
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    fixture.componentInstance.toggle();
    fixture.detectChanges();
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
  });
});
