import { useEffect, useState } from "react";
import SteamAuth from "./SteamAuth";
import { viewerHref } from "../route";

/** One entry from /data/index.json, written by scripts/index-demos.mjs. */
interface DemoSummary {
  name: string;
  map: string;
  rounds: number;
  players: number;
  kb: number;
  seconds: number;
}

/** "1h 22m" / "29m" — a match length, not a stopwatch. */
function duration(seconds: number): string {
  const m = Math.round(seconds / 60);
  return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`;
}

/** de_anubis → Anubis. The de_ prefix is noise once they are all in a row. */
function mapLabel(map: string): string {
  const bare = map.replace(/^(de|cs)_/, "");
  return bare.charAt(0).toUpperCase() + bare.slice(1);
}

export default function Home() {
  const [demos, setDemos] = useState<DemoSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    fetch(`${import.meta.env.BASE_URL}data/index.json`, { signal: ac.signal })
      .then((r) => {
        // A missing index is the normal state before anything is published,
        // not a failure — an empty list says that better than an error does.
        if (r.status === 404) return { demos: [] };
        if (!r.ok) throw new Error(`could not load the demo list (HTTP ${r.status})`);
        return r.json();
      })
      .then((data: { demos?: DemoSummary[] }) => setDemos(data.demos ?? []))
      .catch((e: Error) => e.name !== "AbortError" && setError(e.message));
    return () => ac.abort();
  }, []);

  return (
    <div className="home">
      <header className="home-top">
        <div>
          <h1 className="home-title">CS2 Demo Analyzer</h1>
          <p className="home-tag">
            Parsed demos, played back on the radar — movement, utility, the
            bomb, and every shot that landed.
          </p>
        </div>
        <SteamAuth />
      </header>

      <section>
        <h2 className="home-h2">Demos</h2>

        {error && <p className="home-empty">{error}</p>}
        {!error && demos === null && <p className="home-empty muted">Loading…</p>}

        {demos?.length === 0 && (
          <div className="home-empty">
            <p>No demos published yet.</p>
            <p className="muted">
              Parse one with <code>go run ./round-collector -demo &lt;file&gt;.dem</code>,
              export it with <code>python export_movement.py --demo &lt;name&gt;</code>,
              then publish with <code>npm run upload-data</code>.
            </p>
          </div>
        )}

        <div className="demo-grid">
          {demos?.map((d) => (
            <a key={d.name} className="demo-card" href={viewerHref(d.name)}>
              <span className="demo-map">{mapLabel(d.map)}</span>
              <span className="demo-name">{d.name}</span>
              <span className="demo-meta">
                <span>{d.rounds} rounds</span>
                <span>{d.players} players</span>
                <span>{duration(d.seconds)}</span>
              </span>
              {/* Worth showing before someone opens one on a phone: the
                  payload is fetched whole, and they run to a few megabytes. */}
              <span className="demo-size">{(d.kb / 1024).toFixed(1)} MB</span>
            </a>
          ))}
        </div>
      </section>

      <footer className="home-foot muted">
        Demo data is parsed offline and published separately from the app — see{" "}
        <code>docs/deployment.md</code>.
      </footer>
    </div>
  );
}
