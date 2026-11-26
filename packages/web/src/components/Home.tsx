import { useEffect, useMemo, useState } from "react";
import DemoFilters from "./DemoFilters";
import DemoTable from "./DemoTable";
import NavBar from "./NavBar";
import { LOGIN_HREF } from "../api";
import {
  applyFilters, NO_FILTERS, sampleDemos, sampleSubject, sortDemos, yourDemos,
  type Filters, type Sort, type Subject,
} from "../demoTable";
import { loadIndex, type DemoSummary } from "../payload";
import { demoLabel, PROS } from "../pros";
import { PROS_HREF } from "../route";
import { useMe } from "../useMe";

/**
 * The home page: your games, or - signed out - a sample to try the viewer on.
 *
 * Signed in, the table's subject is you: every published demo you played in
 * or own, with your result, K/D/A and ADR, and the way into the Pros section.
 * Signed out there is nobody to show games for, so the page shows the declared
 * samples instead, seen through the pro they were chosen for - which is all
 * the Worker sends a signed-out visitor anyway (worker/access.ts). This
 * decides only how to show what the index holds.
 */
export default function Home() {
  const [index, setIndex] = useState<DemoSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const auth = useMe();

  useEffect(() => {
    let cancelled = false;
    loadIndex()
      .then((list) => !cancelled && setIndex(list))
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => { cancelled = true; };
  }, []);

  const steamid = auth.status === "in" ? auth.me.steamid : null;

  return (
    <div className="home">
      <NavBar active="mine" />

      {error && <p className="home-empty">{error}</p>}
      {!error && (index === null || auth.status === "loading") && <p className="home-empty muted">Loading…</p>}

      {index && auth.status !== "loading" && (
        steamid
          ? <YourDemos index={index} steamid={steamid} />
          : <Samples index={index} />
      )}

      <footer className="home-foot muted">
        Demos are parsed from <code>.dem</code> files offline and published here
        as read-only replays.
      </footer>
    </div>
  );
}

function YourDemos({ index, steamid }: { index: DemoSummary[]; steamid: string }) {
  const subject: Subject = useMemo(() => ({ name: "You", ids: [steamid] }), [steamid]);
  const mine = useMemo(() => yourDemos(index, steamid), [index, steamid]);
  const [filters, setFilters] = useState<Filters>(NO_FILTERS);
  const [sort, setSort] = useState<Sort | null>(null);
  const shown = useMemo(
    () => sortDemos(applyFilters(mine, filters, subject), sort, subject, demoLabel),
    [mine, filters, sort, subject],
  );

  return (
    <>
      <section>
        <div className="list-head">
          <h2 className="home-h2">Your demos</h2>
          <span className="count">{shown.length === mine.length ? mine.length : `${shown.length} of ${mine.length}`}</span>
        </div>
        {mine.length > 0 ? (
          <>
            <p className="subject">Result, K/D/A and ADR are <b>yours</b>, from your side of each game.</p>
            {mine.length > 1 && <DemoFilters demos={mine} value={filters} onChange={setFilters} />}
            <DemoTable demos={shown} subject={subject} sort={sort} onSort={setSort} />
          </>
        ) : (
          <p className="home-empty muted">
            None of the published demos include you yet.
            {PROS.length > 0 && <> Meanwhile, <a href={PROS_HREF}>the pros' games</a> are open to you.</>}
          </p>
        )}
      </section>
    </>
  );
}

function Samples({ index }: { index: DemoSummary[] }) {
  const samples = useMemo(() => sampleDemos(index), [index]);
  const [sort, setSort] = useState<Sort | null>(null);
  // Each sample is seen through its own featured pro; with one subject per
  // table, a single shared one is the first sample's.
  const subject = samples.length ? sampleSubject(samples[0]) : null;
  const shown = useMemo(() => sortDemos(samples, sort, subject, demoLabel), [samples, sort, subject]);

  return (
    <section>
      <p className="home-tag intro">
        Parsed CS2 demos, played back on the radar — movement, utility, the
        bomb, and every shot that landed.
      </p>
      <div className="signin">
        <span>
          Sign in to see the games you've played, with your result, K/D/A and
          ADR in each{PROS.length > 0 && <>, and the pros' games to study</>}.
        </span>
        <a className="steam-btn" href={LOGIN_HREF}>Sign in through Steam</a>
      </div>
      <div className="list-head">
        <h2 className="home-h2">{samples.length === 1 ? "Sample demo" : "Sample demos"}</h2>
      </div>
      {samples.length === 0 ? (
        <p className="home-empty muted">No sample is published yet.</p>
      ) : (
        <>
          <p className="subject">
            A fully processed match to try the viewer on, no account needed.
            {subject && <> Result, K/D/A and ADR are <b>{subject.name}'s</b>.</>}
          </p>
          <DemoTable demos={shown} subject={subject} sort={sort} onSort={setSort} />
        </>
      )}
    </section>
  );
}
