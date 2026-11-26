import { useEffect, useMemo, useState, type ReactNode } from "react";
import DemoFilters from "./DemoFilters";
import DemoTable from "./DemoTable";
import NavBar from "./NavBar";
import {
  applyFilters, NO_FILTERS, proSubject, recordOf, sortDemos, type Filters, type Sort,
} from "../demoTable";
import { loadIndex, type DemoSummary } from "../payload";
import { demoLabel, demosFor, facets, mapLabel, PROS, proBySlug, type KindFilter } from "../pros";
import { LOGIN_HREF } from "../api";
import { proHref, PROS_HREF, startersHref } from "../route";
import { useMe } from "../useMe";

function useIndex(): { demos: DemoSummary[] | null; error: string | null } {
  const [demos, setDemos] = useState<DemoSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    loadIndex()
      .then((list) => !cancelled && setDemos(list))
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => { cancelled = true; };
  }, []);
  return { demos, error };
}

function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

const PROS_TAG = "Pro players' own point of view, from their pugs and their matches: how they move, where they start a round, and what they throw.";

/**
 * The Pros section is for accounts, for now. The Worker is the lock - signed
 * out, the index it serves holds the samples and nothing else, and every other
 * payload answers 404 - so this is only the page saying why it is empty.
 */
function SignedIn({ children }: { children: ReactNode }) {
  const auth = useMe();
  if (auth.status === "in") return <>{children}</>;
  return (
    <div className="home">
      <NavBar active="pros" />
      <header className="page-head">
        <h1 className="home-title">Pros</h1>
        <p className="home-tag">{PROS_TAG}</p>
      </header>
      {auth.status === "loading" && <p className="home-empty muted">Loading…</p>}
      {auth.status === "out" && (
        <div className="signin">
          <span>Sign in to see the pros' games.</span>
          <a className="steam-btn" href={LOGIN_HREF}>Sign in through Steam</a>
        </div>
      )}
      {auth.status === "down" && (
        <p className="home-empty">Sign-in is unavailable right now, and the Pros section needs it. {auth.message}</p>
      )}
    </div>
  );
}

/** The Pros section: every catalogued pro, and what is published of each. */
export function ProsIndex() {
  return <SignedIn><ProsList /></SignedIn>;
}

function ProsList() {
  const { demos, error } = useIndex();

  return (
    <div className="home">
      <NavBar active="pros" />
      <header className="page-head">
        <h1 className="home-title">Pros</h1>
        <p className="home-tag">{PROS_TAG}</p>
      </header>

      {error && <p className="home-empty">{error}</p>}
      {!error && demos === null && <p className="home-empty muted">Loading…</p>}

      <div className="demo-grid">
        {demos && PROS.map((pro) => {
          const f = facets(demosFor(demos, pro.slug));
          return (
            <a key={pro.slug} className={`demo-card pro-card${f.total ? "" : " empty"}`} href={proHref(pro.slug)}>
              <span className="pro-name">{pro.name}</span>
              {pro.team && <span className="demo-map">{pro.team}</span>}
              <span className="demo-meta">
                <span>{plural(f.kinds.pug, "pug")}</span>
                <span>{plural(f.kinds.match, "pro match", "pro matches")}</span>
                <span>{plural(f.rounds, "round")}</span>
              </span>
              <span className="pro-maps">
                {f.maps.length
                  ? f.maps.map(([m, n]) => <span key={m} className="pro-chip">{mapLabel(m)} {n}</span>)
                  : <span className="muted">Nothing published yet</span>}
              </span>
            </a>
          );
        })}
      </div>
    </div>
  );
}

interface ProProps {
  pro: string;
  map?: string;
  kind?: KindFilter;
}

/**
 * One pro's games, as that pro's table: their result, K/D/A and ADR in each.
 *
 * Grouped by map, because the study links are per map - round starts only
 * line up on one radar - and each map's heading carries the record across it.
 * Filters narrow every section at once, and studying a map's starters opens
 * the starters view over exactly the games left showing.
 */
export function ProPage(props: ProProps) {
  return <SignedIn><ProGames {...props} /></SignedIn>;
}

function ProGames({ pro: slug, map: wantMap, kind: wantKind }: ProProps) {
  const pro = proBySlug(slug);
  const { demos, error } = useIndex();
  const [filters, setFilters] = useState<Filters>({
    ...NO_FILTERS, map: wantMap ?? "", kind: wantKind && wantKind !== "all" ? wantKind : "",
  });
  const [sort, setSort] = useState<Sort | null>(null);
  const subject = useMemo(() => (pro ? proSubject(pro) : null), [pro]);

  // Map and kind stay in the URL, as before, so a filtered page can be shared.
  useEffect(() => {
    const kind = filters.kind === "pug" || filters.kind === "match" ? filters.kind : undefined;
    history.replaceState(null, "", proHref(slug, filters.map || undefined, kind));
  }, [slug, filters.map, filters.kind]);

  const theirs = useMemo(() => (demos ? demosFor(demos, slug) : []), [demos, slug]);
  const byMap = useMemo(() => {
    const shown = sortDemos(applyFilters(theirs, filters, subject), sort, subject, demoLabel);
    const out = new Map<string, DemoSummary[]>();
    for (const d of shown) {
      const list = out.get(d.map);
      if (list) list.push(d); else out.set(d.map, [d]);
    }
    return [...out.entries()].sort((a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]));
  }, [theirs, filters, sort, subject]);

  if (!pro || !subject) {
    return (
      <div className="fatal">
        <h1>No pro called {slug}</h1>
        <p><a className="back" href={PROS_HREF}>← All pros</a></p>
      </div>
    );
  }

  return (
    <div className="home pro-page">
      <NavBar active="pros" />
      <header className="page-head">
        <a className="back" href={PROS_HREF}>← All pros</a>
        <h1 className="home-title">{pro.name}</h1>
        {pro.team && <p className="home-tag">{pro.team}</p>}
      </header>

      {error && <p className="home-empty">{error}</p>}
      {!error && demos === null && <p className="home-empty muted">Loading…</p>}

      {demos && (
        <>
          <p className="subject">Result, K/D/A and ADR are <b>{pro.name}'s</b>, from their side of each game.</p>
          {theirs.length > 1 && <DemoFilters demos={theirs} value={filters} onChange={setFilters} />}

          {byMap.length === 0 && (
            <p className="home-empty muted">
              {theirs.length === 0 ? `Nothing of ${pro.name}'s is published yet.` : "Nothing matches these filters."}
            </p>
          )}

          {byMap.map(([m, list]) => {
            const names = list.map((d) => d.name);
            const rec = recordOf(list, subject);
            return (
              <section key={m} className="pro-map">
                <div className="pro-map-head">
                  <h2 className="home-h2">{mapLabel(m)}</h2>
                  <span className="muted">
                    {plural(list.length, "game")} · {rec.wins}–{rec.losses}
                    {rec.adr !== null && ` · ${Math.round(rec.adr)} ADR`}
                  </span>
                  <span className="pro-study">
                    Round starts:
                    <a href={startersHref(names, { pro: pro.slug, side: "T" })}>T side →</a>
                    <a href={startersHref(names, { pro: pro.slug, side: "CT" })}>CT side →</a>
                  </span>
                </div>
                <DemoTable
                  demos={list}
                  subject={subject}
                  sort={sort}
                  onSort={setSort}
                  showMap={false}
                  starters={(d) => startersHref(d.name, { pro: pro.slug })}
                />
              </section>
            );
          })}
        </>
      )}
    </div>
  );
}
