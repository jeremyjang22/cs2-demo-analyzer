import { useState, type ReactNode } from "react";
import { PAGE_SIZES, paginate, type PageSize } from "../paging";

/**
 * Page a list, and get the controls to go with it.
 *
 * A hook rather than a wrapper component because the caller owns the table
 * markup: it needs the rows to put in its own <tbody> and somewhere to drop
 * the controls, not a component that renders rows for it.
 *
 * The page number is clamped by `paginate` on every render instead of being
 * reset by an effect when the rows change. A filter that shrinks the list just
 * lands on the last page that still exists, with no frame of empty table in
 * between.
 */
export function usePager<T>(all: readonly T[], initialSize: PageSize = 10): { rows: T[]; controls: ReactNode } {
  const [page, setPage] = useState(0);
  const [size, setSize] = useState<PageSize>(initialSize);
  const p = paginate(all, page, size);

  // Nothing to page and nothing to choose: fewer rows than the smallest size.
  const smallest = Math.min(...PAGE_SIZES.filter((s): s is number => s !== "all"));
  const controls = p.total <= smallest ? null : (
    <div className="pager">
      <label>
        Rows
        <select
          value={String(size)}
          onChange={(e) => {
            setSize(e.target.value === "all" ? "all" : Number(e.target.value));
            setPage(0);
          }}
        >
          {PAGE_SIZES.map((s) => (
            <option key={String(s)} value={String(s)}>{s === "all" ? "All" : s}</option>
          ))}
        </select>
      </label>
      <span className="pager-at">{p.first}–{p.last} of {p.total}</span>
      <button type="button" disabled={p.page === 0} onClick={() => setPage(p.page - 1)} aria-label="Previous page">
        ‹
      </button>
      <button
        type="button"
        disabled={p.page >= p.pages - 1}
        onClick={() => setPage(p.page + 1)}
        aria-label="Next page"
      >
        ›
      </button>
    </div>
  );

  return { rows: p.rows, controls };
}
