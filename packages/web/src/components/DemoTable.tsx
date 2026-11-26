import { FIRST_DIR, lineOf, resultOf, type Sort, type SortKey, type Subject } from "../demoTable";
import type { DemoSummary } from "../payload";
import { demoLabel, KIND_LABEL, mapLabel } from "../pros";
import { analyticsHref, viewerHref } from "../route";

interface Props {
  demos: DemoSummary[];
  /** Whose numbers fill Result, K/D/A and ADR; null for none (score only). */
  subject: Subject | null;
  sort: Sort | null;
  onSort: (sort: Sort | null) => void;
  /** Off where the map is already the section heading. */
  showMap?: boolean;
  /** An extra study link per row, e.g. the starters view on a pro's page. */
  starters?: (d: DemoSummary) => string;
  empty?: string;
}

/** "1h 22m" / "29m" - a match length, not a stopwatch. */
function duration(seconds: number): string {
  const m = Math.round(seconds / 60);
  return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`;
}

const VISIBILITY_HINT = {
  private: "Only you can see this demo",
  link: "Not listed for others, but anyone with the link can open it",
} as const;

/**
 * One person's games as a table. The row opens the replay; the page around it
 * says whose numbers these are, so the cells never have to.
 *
 * Sorting is the caller's state so that a page with several tables - a pro's,
 * one per map - sorts them all by the same column at once.
 */
export default function DemoTable({ demos, subject, sort, onSort, showMap = true, starters, empty }: Props) {
  const header = (key: SortKey, label: string, num = false, secondary = false) => {
    const on = sort?.key === key;
    const next = (): Sort | null =>
      !on ? { key, dir: FIRST_DIR[key] } : sort!.dir === FIRST_DIR[key] ? { key, dir: -sort!.dir as 1 | -1 } : null;
    return (
      <th
        className={["sortable", num && "num", secondary && "sec"].filter(Boolean).join(" ")}
        aria-sort={on ? (sort!.dir > 0 ? "ascending" : "descending") : "none"}
      >
        {/* A button, so the header is reachable and operable from the keyboard. */}
        <button type="button" onClick={() => onSort(next())}>
          {label}<span className="arrow" aria-hidden>{on ? (sort!.dir > 0 ? "▲" : "▼") : ""}</span>
        </button>
      </th>
    );
  };

  const cols = (showMap ? 1 : 0) + 7;

  return (
    <div className="table-wrap">
      <table className="demos">
        <thead>
          <tr>
            {showMap && header("map", "Map")}
            {header("demo", "Demo")}
            <th className="sec">Type</th>
            {header("result", "Result")}
            {header("kda", "K/D/A", true, true)}
            {header("adr", "ADR", true, true)}
            {header("length", "Length", true)}
            <th className="sec"><span className="sr-only">Links</span></th>
          </tr>
        </thead>
        <tbody>
          {demos.length === 0 && (
            <tr className="empty-row"><td colSpan={cols}>{empty ?? "No demos match these filters."}</td></tr>
          )}
          {demos.map((d) => {
            const line = subject ? lineOf(d, subject) : null;
            const result = subject ? resultOf(d, subject) : d.score ? { wl: "", us: d.score[0], them: d.score[1] } : null;
            const href = viewerHref(d.name);
            return (
              <tr
                key={d.name}
                className="row"
                // The whole row is the target, as the card was. The demo name
                // inside is a real link, for the keyboard and for new tabs.
                onClick={(e) => {
                  if ((e.target as HTMLElement).closest("a, button")) return;
                  location.href = href;
                }}
              >
                {showMap && <td className="c-map">{mapLabel(d.map)}</td>}
                <td className="c-demo">
                  <a className="demo-label" href={href} title={d.name}>
                    <span className="phone-map">{mapLabel(d.map)} · </span>{demoLabel(d)}
                  </a>
                </td>
                <td className="c-tags sec">
                  <span className="tags">
                    {d.kind && d.kind !== "unknown" && (
                      <span className={`kind kind-${d.kind}`}>{KIND_LABEL[d.kind]}</span>
                    )}
                    {d.sample && <span className="kind kind-sample">Sample</span>}
                    {/* Only the owner is ever sent a non-public entry, so this
                        tells them what others cannot see. */}
                    {d.visibility && d.visibility !== "public" && (
                      <span className="kind kind-private" title={VISIBILITY_HINT[d.visibility]}>
                        {d.visibility === "private" ? "Private" : "Unlisted"}
                      </span>
                    )}
                  </span>
                </td>
                <td className="c-result result">
                  {result
                    ? <><span className={`wl wl-${result.wl || "none"}`}>{result.wl}</span>{result.us}–{result.them}</>
                    : <span className="muted">—</span>}
                </td>
                <td className="num stat sec">{line ? `${line[0]} / ${line[1]} / ${line[2]}` : <span className="muted">—</span>}</td>
                <td className="num stat sec">{line ? Math.round(line[4]) : <span className="muted">—</span>}</td>
                <td className="num c-length">{duration(d.seconds)}</td>
                <td className="actions sec">
                  {starters && <a href={starters(d)}>Starters</a>}
                  <a href={analyticsHref(d.name)}>Analytics</a>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
