import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import BombStatus from "./BombStatus";
import KillFeed from "./KillFeed";
import PlaybackBar from "./PlaybackBar";
import TeamPanel from "./TeamPanel";
import Timeline from "./Timeline";
import ViewControls from "./ViewControls";
import { GAME_COLORS, MovementRenderer, SAFE_COLORS, type Summary, type ViewState } from "../renderer";
import { HOME_HREF } from "../route";
import { isPostround, scoreBefore, stepRound } from "../timeline";
import { assertPayload, type Payload } from "../types";

interface Props {
  /** Folder name under public/data (locally) or under data/ in R2. */
  demo: string;
  /** Seconds into the match to open at, so a moment can be linked to. */
  start: number;
}

function initialView(payload: Payload): ViewState {
  return {
    selected: payload.players.map((p) => p.id),
    mode: "dots",
    cones: true,
    util: true,
    labels: true,
    fire: true,
    bomb: true,
    deaths: true,
    palette: "game",
  };
}

const EMPTY: Summary = {
  t: 0, sec: 0, chapter: null, kills: [], alive: new Set<string>(), perFloor: [],
  equip: new Map(), bomb: null,
};

export default function Viewer({ demo, start }: Props) {
  const [payload, setPayload] = useState<Payload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [summary, setSummary] = useState<Summary>(EMPTY);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [view, setView] = useState<ViewState | null>(null);

  const stage = useRef<HTMLDivElement>(null);
  const renderer = useRef<MovementRenderer | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch(`${import.meta.env.BASE_URL}data/${demo}/movement.json`)
      .then((r) => {
        if (!r.ok) throw new Error(`movement.json for ${demo} not found (HTTP ${r.status})`);
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
  }, [demo]);

  useEffect(() => {
    if (!payload || !stage.current) return;
    const r = new MovementRenderer(stage.current, payload, { onFrame: setSummary });
    renderer.current = r;
    void r.load().then(() => {
      if (start > 0) r.seek(start);
    });
    return () => { r.destroy(); renderer.current = null; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
        <h1>Could not load {demo}</h1>
        <p className="muted">
          That demo is not available. It may have been unpublished, or the link
          may be wrong.
        </p>
        <p><a className="back" href={HOME_HREF}>← All demos</a></p>
      </div>
    );
  }
  if (!payload || !view) return <div className="fatal muted">Loading {demo}…</div>;

  const r = renderer.current;
  const chapter = summary.chapter;
  const timeline = { chapters: r?.chapters ?? [], duration: r?.matchDuration ?? 0 };
  const [sa, sb] = chapter ? scoreBefore(timeline, chapter) : [0, 0];
  const roundLabel = chapter ? `Round ${chapter.meta.n}` : "—";
  // Seconds past the win condition, plus why the round ended — the two facts
  // that turn "why is nobody shooting" into "this is a save".
  const postround = chapter && isPostround(chapter, summary.t)
    ? `${chapter.meta.why} · +${(summary.t - chapter.decided).toFixed(1)}s`
    : undefined;

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
    <div className="bcast">
      <header className="bcast-top">
        <div className="bcast-id">
          <a className="back" href={HOME_HREF}>← All demos</a>
          <span className="bcast-map">{payload.map}</span>
          <span className="bcast-demo">{demo}</span>
        </div>

        {/* The score reads outward from the centre, each team's number on its
            own side, so it matches which side of the map that team is on. */}
        <div className="bcast-score">
          <span className="bs-team left">{payload.teams[0]?.name}</span>
          <b>{sa}</b>
          <span className="bs-round">
            {roundLabel}
            {postround && <span className="tpost">{postround}</span>}
          </span>
          <b>{sb}</b>
          <span className="bs-team right">{payload.teams[1]?.name}</span>
        </div>

        <div className="bcast-bomb">
          <BombStatus bomb={summary.bomb} names={names} />
        </div>
      </header>

      <div className="bcast-body">
        {payload.teams[0] && (
          <TeamPanel
            team={payload.teams[0]}
            payload={payload}
            colors={colors}
            selected={view.selected}
            alive={summary.alive}
            equip={summary.equip}
            onToggle={toggle}
            onToggleTeam={toggleTeam}
          />
        )}

        <main className="bcast-stage">
          <div className="stage" ref={stage} />

          <PlaybackBar
            playing={playing}
            speed={speed}
            t={summary.t}
            duration={timeline.duration}
            roundLabel={roundLabel}
            postround={postround}
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

          <ViewControls view={view} patch={patch} />
        </main>

        {/* The feed shares the right column with the team above it: that is
            where a broadcast puts it, and it is the only column with room
            once a roster has thinned out. */}
        <div className="bcast-side">
          {payload.teams[1] && (
            <TeamPanel
              team={payload.teams[1]}
              payload={payload}
              colors={colors}
              selected={view.selected}
              alive={summary.alive}
              equip={summary.equip}
              mirror
              onToggle={toggle}
              onToggleTeam={toggleTeam}
            />
          )}

          <div className="bcast-feed">
            <h2 className="feed-h">Kill feed</h2>
            <KillFeed kills={summary.kills} names={names} colors={colors} teamOf={teamOf} />
          </div>
        </div>
      </div>
    </div>
  );
}
