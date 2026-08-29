import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';
import { provideNoopAnimations } from '@angular/platform-browser/animations';
import { provideNzIconsTesting } from 'ng-zorro-antd/icon/testing';
import { NzI18nService } from 'ng-zorro-antd/i18n';
import { appConfig } from '../../app.config';
import { ApiService } from '@acme/shared';
import { ProjectUsagePanelComponent } from './project-usage-panel.component';

/**
 * The range picker's placeholders, which arrived in Chinese on a deployed console.
 *
 * ng-zorro's default locale is zh_CN and it is compiled into the date-picker bundle. `en_US` has
 * to be provided or every unset placeholder in the console falls back to it — this asserts the
 * provider actually reaches the control, rather than that the file mentions it.
 */
describe('the usage panel’s range picker', () => {
  it('renders its placeholders in the configured locale', () => {
    TestBed.configureTestingModule({
      imports: [ProjectUsagePanelComponent],
      // THE APPLICATION'S OWN PROVIDERS, not a hand-built set. A test that provides en_US itself
      // proves only that ng-zorro works; what broke here is whether the console's configuration
      // reaches the control, and only appConfig can answer that.
      providers: [
        ...appConfig.providers,
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
      ],
    });
    TestBed.inject(ApiService).setAccount('00000000-0000-7000-8000-0000000000a1');
    const fixture = TestBed.createComponent(ProjectUsagePanelComponent);
    fixture.componentRef.setInput('projectId', '7');
    fixture.detectChanges();
    TestBed.inject(HttpTestingController)
      .expectOne((r) => r.url.includes('/usage'))
      .flush({ recorded: false, buckets: [] });
    fixture.detectChanges();

    const inputs: HTMLInputElement[] = Array.from(fixture.nativeElement.querySelectorAll('input'));
    const placeholders = inputs.map((i) => i.placeholder).filter(Boolean);
    expect(placeholders).toEqual(['From', 'To']);
    // The placeholders above are spelled out, so they would pass even with the wrong locale. This
    // is the assertion that actually catches it: everything else the picker renders — the month
    // names, Today, the weekday headers — comes from here.
    expect(TestBed.inject(NzI18nService).getLocaleId()).toBe('en');
  });
});
