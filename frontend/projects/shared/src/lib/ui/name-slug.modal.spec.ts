import { TestBed } from "@angular/core/testing";
import { provideHttpClient } from "@angular/common/http";
import { provideHttpClientTesting } from "@angular/common/http/testing";
import { provideNoopAnimations } from "@angular/platform-browser/animations";
import { provideNzIconsTesting } from "ng-zorro-antd/icon/testing";
import { NZ_MODAL_DATA, NzModalRef } from "ng-zorro-antd/modal";
import { NameSlugModalComponent, slugify } from "./name-slug.modal";

describe("slugify", () => {
  it("lower cases and hyphenates the words of a name", () => {
    expect(slugify("ACME Shop")).toBe("acme-shop");
  });

  it("collapses runs of punctuation into a single hyphen", () => {
    expect(slugify("ACME Shop (EU) — staging")).toBe("acme-shop-eu-staging");
  });

  it("leaves no leading or trailing hyphen, so a half-typed name is still a valid slug", () => {
    expect(slugify(" Shop ")).toBe("shop");
    expect(slugify("Shop -")).toBe("shop");
  });

  it("keeps digits and existing hyphens", () => {
    expect(slugify("edge-01")).toBe("edge-01");
  });

  it("does not transliterate: the operator is meant to see that and fix it", () => {
    expect(slugify("München")).toBe("m-nchen");
  });

  it("gives an empty slug for a name with nothing sluggable in it", () => {
    expect(slugify("!!!")).toBe("");
  });
});

describe("NameSlugModalComponent", () => {
  function build(): NameSlugModalComponent {
    TestBed.configureTestingModule({
      imports: [NameSlugModalComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideNoopAnimations(),
        provideNzIconsTesting(),
        { provide: NZ_MODAL_DATA, useValue: { path: "/projects" } },
        { provide: NzModalRef, useValue: { close: () => undefined } },
      ],
    });
    return TestBed.createComponent(NameSlugModalComponent).componentInstance;
  }

  /** What the operator does: one keystroke at a time, and the slug keeps up with each one. */
  function type(c: NameSlugModalComponent, name: string): void {
    for (let i = 1; i <= name.length; i++) {
      c.name = name.slice(0, i);
      c.onNameChange(c.name);
    }
  }

  it("derives the slug from the name as it is typed", () => {
    const c = build();
    type(c, "ACME Shop");
    expect(c.slug).toBe("acme-shop");
  });

  it("stops deriving once the operator edits the slug themselves", () => {
    const c = build();
    type(c, "ACME");
    c.slug = "shop-eu";
    c.onSlugChange(c.slug);

    type(c, "ACME Shop");
    expect(c.slug).toBe("shop-eu");
  });

  it("resumes deriving after the slug is emptied, without refilling it on the spot", () => {
    const c = build();
    type(c, "ACME");
    c.slug = "shop";
    c.onSlugChange(c.slug);
    c.slug = "";
    c.onSlugChange("");
    expect(c.slug).toBe("");

    type(c, "ACME Shop");
    expect(c.slug).toBe("acme-shop");
  });
});
