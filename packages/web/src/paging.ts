/**
 * Slicing a list into pages. Pure, so the arithmetic that decides which rows
 * are on screen is tested without rendering a table.
 */

/** Rows per page, or "all" for no paging. */
export type PageSize = number | "all";

export const PAGE_SIZES: readonly PageSize[] = [5, 10, 20, "all"];

export interface Page<T> {
  rows: T[];
  /** 0-based, and always a page that exists. */
  page: number;
  pages: number;
  /** 1-based positions of the first and last row shown; both 0 when empty. */
  first: number;
  last: number;
  total: number;
}

/**
 * The rows on a page.
 *
 * `page` is clamped rather than trusted. The caller's page number outlives the
 * list it was chosen for — a filter can shrink 30 rows to 4 while the caller
 * still says "page 3" — and the right answer then is the last page that
 * exists, not an empty table.
 */
export function paginate<T>(rows: readonly T[], page: number, size: PageSize): Page<T> {
  const total = rows.length;
  const per = size === "all" ? Math.max(total, 1) : Math.max(1, Math.floor(size));
  const pages = Math.max(1, Math.ceil(total / per));
  const at = Math.min(Math.max(0, Math.floor(page) || 0), pages - 1);
  const start = at * per;
  const shown = rows.slice(start, start + per);
  return {
    rows: shown,
    page: at,
    pages,
    first: shown.length ? start + 1 : 0,
    last: start + shown.length,
    total,
  };
}
