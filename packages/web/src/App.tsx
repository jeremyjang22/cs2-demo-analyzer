import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import KillFeed from "./components/KillFeed";
import PlaybackBar from "./components/PlaybackBar";
import Scoreboard from "./components/Scoreboard";
import Segmented from "./components/Segmented";
import Timeline from "./components/Timeline";
import { GAME_COLORS, MovementRenderer, SAFE_COLORS, type Summary, type ViewState } from "./renderer";
import { scoreBefore, stepRound } from "./timeline";
import { assertPayload, type Payload } from "./types";

/** Which demo to load. ?demo=<name> matches a folder under public/data. */
function demoName(): string {
  return new URLSearchParams(location.search).get("demo") ?? "anubispug";
}

/** Optional ?t=<seconds> start position, so a moment can be linked to. */
function startTime(): number {
  const raw = new URLSearchParams(location.search).get("t");
  const n = raw === null ? NaN : Number(raw);
  return Number.isFinite(n) ? n : 0;
}

function initialView(payload: Payload): ViewState {
  return {
    selected: payload.players.map((p) => p.id),
    mode: "dots",
    cones: true,
    util: true,
    labels: true,
    palette: "game",
  };
}

const EMPTY: Summary = {
  t: 0, sec: 0, chapter: null, kills: [], alive: new Set<string>(), perFloor: [],
};

export default function App() {
  const [payload, setPayload] = useState<Payload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [summary, setSummary] = useState<Summary>(EMPTY);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [view, setView] = useState<ViewState | null>(null);

  const stage = useRef<HTMLDivElement>(null);
  const renderer = useRef<MovementRenderer | null>(null);
  const name = useMemo(demoName, []);

  useEffect(() => {
    let cancelled = false;
    fetch(`${import.meta.env.BASE_URL}data/${name}/movement.json`)
      .then((r) => {
        if (!r.ok) throw new Error(`movement.json for ${name} not found (HTTP ${r.status})`);
        return r.json();
      })
      .then((data: unknown) => {
        assertPayload(data);
        if (cancelled) return;
        // Set together: the renderer needs the <main> element, which only
        // renders once both exist. Deriving the view inside the renderer effect
        // instead deadlocks — no view means no element, and no element means
        // the effect bails before ever setting the view.
        setPayload(data);
        setView(initialView(data));
      })
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => { cancelled = true; };
  }, [name]);

  useEffect(() => {
    if (!payload || !stage.current) return;
    const r = new MovementRenderer(stage.current, payload, { onFrame: setSummary });
    renderer.current = r;
    void r.load().then(() => {
      const t = startTime();
      if (t > 0) r.seek(t);
    });
    return () => { r.destroy(); renderer.current = null; };
  }, [payload]);

  useEffect(() => { if (view) renderer.current?.setState(view); }, [view]);

  // The renderer stops itself at the end of the match; keep the button honest.
  useEffect(() => {
    if (playing && renderer.current && !renderer.current.playing) setPlaying(false);
  }, [summary, playing]);

  const patch = (next: Partial<ViewState>) => setView((v) => (v ? { ...v, ...next } : v));
  const seek = useCallback((t: number) => renderer.current?.seek(t), []);

  const colors = useMemo(() => {
    const out: Record<string, string> = {};
    if (!payload || !view) return out;
    for (const p of payload.players) {
      out[p.id] = view.palette === "game"
        ? GAME_COLORS[p.col % GAME_COLORS.length]
        : SAFE_COLORS[Math.max(0, view.selected.indexOf(p.id)) % SAFE_COLORS.length];
    }
    return out;
  }, [payload, view]);

  const names = useMemo(
    () => Object.fromEntries((payload?.players ?? []).map((p) => [p.id, p.name])),
    [payload],
  );
  const teamOf = useMemo(
    () => Object.fromEntries((payload?.players ?? []).map((p) => [p.id, p.team])),
    [payload],
  );

  if (error) {
    return (
      <div className="fatal">
        <h1>Could not load {name}</h1>
        <p>{error}</p>
        <p className="muted">
          Generate it with <code>python export_movement.py --demo {name}</code>
        </p>
      </div>
    );
  }
  if (!payload || !view) return <div className="fatal muted">Loading {name}…</div>;

  const r = renderer.current;
  const chapter = summary.chapter;
  const timeline = { chapters: r?.chapters ?? [], duration: r?.matchDuration ?? 0 };
  const [sa, sb] = chapter ? scoreBefore(timeline, chapter) : [0, 0];
  const roundLabel = chapter ? `Round ${chapter.meta.n}` : "—";

  const toggle = (id: string) =>
    patch({
      selected: view.selected.includes(id)
        ? view.selected.filter((x) => x !== id)
        : [...view.selected, id],
    });

  const toggleTeam = (teamId: number) => {
    const ids = payload.teams.find((t) => t.id === teamId)?.players ?? [];
    const allOn = ids.every((id) => view.selected.includes(id));
    patch({
      selected: allOn
        ? view.selected.filter((id) => !ids.includes(id))
        : [...new Set([...view.selected, ...ids])],
    });
  };

  return (
    <>
      <aside className="panel">
        <h1>{payload.map}</h1>
        <div className="sub">
          {Object.keys(payload.rounds).length} rounds · {payload.players.length} players
        </div>

        <div className="livescore">
          <span className="ls-name">{payload.teams[0]?.name}</span>
          <b>{sa}</b><span className="vs">–</span><b>{sb}</b>
          <span className="ls-name r">{payload.teams[1]?.name}</span>
        </div>
        <div className="hint" style={{ marginTop: 0 }}>
          Score entering {roundLabel.toLowerCase()}.
        </div>

        <fieldset>
          <legend>Scoreboard</legend>
          <Scoreboard
            payload={payload}
            colors={colors}
            selected={view.selected}
            alive={summary.alive}
            onToggle={toggle}
            onToggleTeam={toggleTeam}
          />
          <div className="hint">Click a row to hide it on the map. Dimmed = dead right now.</div>
        </fieldset>

        <fieldset>
          <legend>Kill feed — {roundLabel}</legend>
          <KillFeed kills={summary.kills} names={names} colors={colors} teamOf={teamOf} />
        </fieldset>

        <fieldset>
          <legend>View</legend>
          <Segmented
            options={[
              { value: "dots", label: "Live" },
              { value: "trail", label: "Trails" },
              { value: "full", label: "Full round" },
            ]}
            value={view.mode}
            onChange={(mode) => patch({ mode })}
          />
          <div style={{ marginTop: 4 }}>
            <Segmented
              options={[{ value: "on", label: "Cones" }, { value: "off", label: "No cones" }]}
              value={view.cones ? "on" : "off"}
              onChange={(v) => patch({ cones: v === "on" })}
            />
          </div>
          <div style={{ marginTop: 4 }}>
            <Segmented
              options={[{ value: "on", label: "Utility" }, { value: "off", label: "No utility" }]}
              value={view.util ? "on" : "off"}
              onChange={(v) => patch({ util: v === "on" })}
            />
          </div>
          <div style={{ marginTop: 4 }}>
            <Segmented
              options={[{ value: "on", label: "Names" }, { value: "off", label: "No names" }]}
              value={view.labels ? "on" : "off"}
              onChange={(v) => patch({ labels: v === "on" })}
            />
          </div>
        </fieldset>

        <fieldset>
          <legend>Colours</legend>
          <Segmented
            options={[{ value: "game", label: "Game" }, { value: "safe", label: "Accessible" }]}
            value={view.palette}
            onChange={(palette) => patch({ palette })}
          />
          <div className="hint">
            Game = the five minimap colours; not colourblind-safe, which is what
            the accessible set is for.
          </div>
        </fieldset>
      </aside>

      <main className="viewer">
        <div className="stage" ref={stage} />

        <PlaybackBar
          playing={playing}
          speed={speed}
          t={summary.t}
          duration={timeline.duration}
          roundLabel={roundLabel}
          onToggle={() => {
            if (!r) return;
            if (r.playing) { r.pause(); setPlaying(false); }
            else { r.play(); setPlaying(true); }
          }}
          onNudge={(d) => r?.nudge(d)}
          onRound={(d) => { if (r) seek(stepRound(timeline, r.time, d)); }}
          onEdge={(which) => seek(which === "start" ? 0 : timeline.duration)}
          onSpeed={(f) => { setSpeed(f); r?.setSpeed(f); }}
        />

        <Timeline
          chapters={timeline.chapters}
          duration={timeline.duration}
          t={summary.t}
          current={chapter}
          onSeek={seek}
        />
      </main>
    </>
  );
}
