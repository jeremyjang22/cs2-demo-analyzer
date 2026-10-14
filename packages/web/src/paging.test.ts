import { describe, expect, it } from "vitest";
import { paginate } from "./paging";

const rows = Array.from({ length: 23 }, (_, i) => i + 1);

describe("paginate", () => {
  it("slices a page and says where it sits", () => {
    expect(paginate(rows, 0, 10)).toMatchObject({ rows: rows.slice(0, 10), pages: 3, first: 1, last: 10, total: 23 });
    expect(paginate(rows, 2, 10)).toMatchObject({ rows: [21, 22, 23], page: 2, first: 21, last: 23 });
  });

  // The page number outlives the list: a filter shrinks the rows while the
  // caller still remembers being on page 3. That must not show an empty table.
  it("falls back to the last page that exists", () => {
    expect(paginate(rows.slice(0, 4), 2, 10)).toMatchObject({ rows: [1, 2, 3, 4], page: 0, pages: 1 });
    expect(paginate(rows, 99, 10).page).toBe(2);
    expect(paginate(rows, -1, 10).page).toBe(0);
  });

  it("shows everything on one page for 'all'", () => {
    expect(paginate(rows, 5, "all")).toMatchObject({ rows, page: 0, pages: 1, first: 1, last: 23 });
  });

  // Still one page, so the controls have something coherent to say.
  it("handles an empty list", () => {
    expect(paginate([], 0, 10)).toEqual({ rows: [], page: 0, pages: 1, first: 0, last: 0, total: 0 });
    expect(paginate([], 0, "all").pages).toBe(1);
  });

  it("does not leave a trailing empty page on an exact fit", () => {
    expect(paginate(rows.slice(0, 20), 0, 10).pages).toBe(2);
  });
});
