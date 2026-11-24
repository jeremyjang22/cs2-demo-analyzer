import { useEffect, useMemo, useState } from "react";
import Segmented from "./Segmented";
import { loadIndex, type DemoSummary } from "../payload";
import {
  demoLabel, demosFor, facets, KIND_LABEL, mapLabel, PROS, proBySlug, type KindFilter,
} from "../pros";
import { analyticsHref, HOME_HREF, proHref, PROS_HREF, startersHref, viewerHref } from "../route";

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

/** The Pros section: every catalogued pro, and what is published of each. */
export function ProsIndex() {
  const { demos, error } = useIndex();

  return (
    <div className="home">
      <header className="home-top">
        <div>
          <a className="back" href={HOME_HREF}>← All demos</a>
          <h1 className="home-title">Pros</h1>
          <p className="home-tag">
            Pro players' own point of view, from their pugs and their matches:
            how they move, where they start a round, and what they throw.
          </p>
        </div>
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
 * One pro, filtered by kind of game and map.
 *
 * The filters narrow which published demos are in play, and everything below
 * them - the study links and the list - is built from that set. Studying a
 * map's starters opens the starters view over exactly those demos, so "his
 * Mirage pugs" or "his Mirage pro matches" is a click, not an export.
 */
export function ProPage({ pro: slug, map: wantMap, kind: wantKind }: ProProps) {
  const pro = proBySlug(slug);
  const { demos, error } = useIndex();
  const [kind, setKind] = useState<KindFilter>(wantKind ?? "all");
  const [map, setMap] = useState<string | undefined>(wantMap);

  useEffect(() => {
    history.replaceState(null, "", proHref(slug, map, kind));
  }, [slug, map, kind]);

  const mine = useMemo(() => (demos ? demosFor(demos, slug) : []), [demos, slug]);
  // Each filter's counts are taken with the OTHER filter applied, so a chip's
  // number is what you would get by clicking it.
  const kindCounts = useMemo(() => facets(demosFor(mine, slug, { map })).kinds, [mine, slug, map]);
  const mapCounts = useMemo(() => facets(demosFor(mine, slug, { kind })).maps, [mine, slug, kind]);
  const shown = useMemo(() => demosFor(mine, slug, { kind, map }), [mine, slug, kind, map]);
  const byMap = useMemo(() => {
    const out = new Map<string, DemoSummary[]>();
    for (const d of shown) {
      const list = out.get(d.map);
      if (list) list.push(d); else out.set(d.map, [d]);
    }
    return [...out.entries()].sort((a, b) => b[1].length - a[1].length);
  }, [shown]);

  if (!pro) {
    return (
      <div className="fatal">
        <h1>No pro called {slug}</h1>
        <p><a className="back" href={PROS_HREF}>← All pros</a></p>
      </div>
    );
  }

  const allKinds = kindCounts.pug + kindCounts.match + kindCounts.unknown;

  return (
    <div className="home pro-page">
      <header className="home-top">
        <div>
          <a className="back" href={PROS_HREF}>← All pros</a>
          <h1 className="home-title">{pro.name}</h1>
          {pro.team && <p className="home-tag">{pro.team}</p>}
        </div>
      </header>

      {error && <p className="home-empty">{error}</p>}
      {!error && demos === null && <p className="home-empty muted">Loading…</p>}

      {demos && (
        <>
          <div className="pro-filters">
            <Segmented
              grow={false}
              options={[
                { value: "all", label: `All games (${allKinds})` },
                { value: "pug", label: `Pugs (${kindCounts.pug})` },
                { value: "match", label: `Pro matches (${kindCounts.match})` },
              ]}
              value={kind}
              onChange={setKind}
            />
            <div className="seg">
              <button type="button" aria-pressed={!map} onClick={() => setMap(undefined)}>All maps</button>
              {mapCounts.map(([m, n]) => (
                <button key={m} type="button" aria-pressed={map === m} onClick={() => setMap(m)}>
                  {mapLabel(m)} ({n})
                </button>
              ))}
            </div>
          </div>

          {shown.length === 0 && (
            <p className="home-empty muted">
              {mine.length === 0
                ? `Nothing of ${pro.name}'s is published yet.`
                : "Nothing matches these filters."}
            </p>
          )}

          {/* Starters is a per-map view - positions only line up on one radar -
              so the study links come one map at a time. */}
          {byMap.map(([m, list]) => {
            const names = list.map((d) => d.name);
            const rounds = list.reduce((n, d) => n + d.rounds, 0);
            return (
              <section key={m} className="pro-map">
                <div className="pro-map-head">
                  <h2 className="home-h2">{mapLabel(m)}</h2>
                  <span className="muted">
                    {plural(list.length, "game")} · {plural(rounds, "round")}
                    {kind !== "all" && ` · ${KIND_LABEL[kind].toLowerCase()}${list.length === 1 ? "" : "s"} only`}
                  </span>
                  <span className="pro-study">
                    Round starts:
                    <a href={startersHref(names, { pro: pro.slug, side: "T" })}>T side →</a>
                    <a href={startersHref(names, { pro: pro.slug, side: "CT" })}>CT side →</a>
                  </span>
                </div>
                <table className="pro-table">
                  <thead>
                    <tr>
                      <th>Game</th>
                      <th>Kind</th>
                      <th>Date</th>
                      <th className="n">Rounds</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {list.map((d) => (
                      <tr key={d.name}>
                        <td title={d.name}>{demoLabel(d)}</td>
                        <td><span className={`kind kind-${d.kind ?? "unknown"}`}>{KIND_LABEL[d.kind ?? "unknown"]}</span></td>
                        <td className="muted">{d.date ?? "—"}</td>
                        <td className="n">{d.rounds}</td>
                        <td className="pro-links">
                          <a href={viewerHref(d.name)}>Replay</a>
                          <a href={analyticsHref(d.name)}>Analytics</a>
                          <a href={startersHref(d.name, { pro: pro.slug })}>Starters</a>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </section>
            );
          })}
        </>
      )}
    </div>
  );
}
