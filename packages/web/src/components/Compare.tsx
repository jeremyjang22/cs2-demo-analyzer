import { useEffect, useMemo, useRef, useState } from "react";
import KillFeed from "./KillFeed";
import Segmented from "./Segmented";
import ViewControls from "./ViewControls";
import {
  ANCHOR_LABEL, clockRange, neighbour, paneClock, paneSec, relClock, roundTitle,
  type Anchor, type RoundRef,
} from "../compare";
import { loadIndex, loadPayload, type DemoSummary } from "../payload";
import { GAME_COLORS, MovementRenderer, SAFE_COLORS, type Summary, type ViewState } from "../renderer";
import { compareHref, HOME_HREF, viewerHref } from "../route";
import { buildTimeline, scoreBefore, timeInRound, type Chapter, type Timeline } from "../timeline";
import type { Payload } from "../types";

interface Props {
  a?: RoundRef;
  b?: RoundRef;
  align?: Anchor;
  t?: number;
}

/** A pane's payload with its timeline, built once per payload. */
interface Loaded {
  payload: Payload;
  timeline: Timeline;
}

const SPEEDS = ["1", "2", "4", "8"] as const;

const VIEW: ViewState = {
  // Per pane: each pane shows every player of its own payload.
  selected: [],
  mode: "dots",
  cones: true,
  util: true,
  labels: true,
  fire: true,
  bomb: true,
  deaths: true,
  palette: "game",
};

function sideName(side: number): string {
  return side === 2 ? "T" : side === 3 ? "CT" : "?";
}

/** Each pane's radar edge, in CSS pixels, per floor. */
function panelSize(floors: number): number {
  const perPane = (window.innerWidth - 36 - 18) / 2;
  const byHeight = window.innerHeight - 300;
  return Math.max(260, Math.floor(Math.min(perPane / floors - (floors > 1 ? 10 : 0), byHeight)));
}

function useLoaded(demo: string | undefined): { loaded: Loaded | null; error: string | null } {
  const [state, setState] = useState<{ demo?: string; loaded: Loaded | null; error: string | null }>(
    { loaded: null, error: null },
  );
  useEffect(() => {
    if (!demo) return;
    let cancelled = false;
    setState({ demo, loaded: null, error: null });
    loadPayload(demo)
      .then((payload) => !cancelled && setState({ demo, loaded: { payload, timeline: buildTimeline(payload) }, error: null }))
      .catch((e: Error) => !cancelled && setState({ demo, loaded: null, error: e.message }));
    return () => { cancelled = true; };
  }, [demo]);
  return state.demo === demo ? state : { loaded: null, error: null };
}

export default function Compare({ a: wantA, b: wantB, align: wantAlign, t: wantT }: Props) {
  const [demos, setDemos] = useState<DemoSummary[] | null>(null);
  const [a, setA] = useState<RoundRef | undefined>(wantA);
  const [b, setB] = useState<RoundRef | undefined>(wantB);
  const [align, setAlign] = useState<Anchor>(wantAlign ?? "start");
  const [rel, setRel] = useState(wantT ?? 0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<(typeof SPEEDS)[number]>("1");
  const [view, setView] = useState<ViewState>(VIEW);

  useEffect(() => { void loadIndex().then(setDemos).catch(() => setDemos([])); }, []);

  const A = useLoaded(a?.demo ?? demos?.[0]?.name);
  const B = useLoaded(b?.demo ?? a?.demo ?? demos?.[0]?.name);

  // Fill in whatever the URL left out: A defaults to the first round of the
  // first demo, B to the round after A. Comparing a round with its neighbour is
  // the most common ask ("what did they do differently next round"), and an
  // empty pane teaches nothing.
  useEffect(() => {
    if (!a && A.loaded) {
      const first = A.loaded.timeline.chapters[0];
      if (first) setA({ demo: demos?.[0]?.name ?? "", key: first.key });
    }
  }, [a, A.loaded, demos]);
  useEffect(() => {
    if (!b && a && B.loaded && B.loaded.payload === A.loaded?.payload) {
      const next = neighbour(B.loaded.timeline.chapters, a.key);
      if (next) setB({ demo: a.demo, key: next.key });
    }
  }, [a, b, A.loaded, B.loaded]);

  const chA = A.loaded && a ? A.loaded.timeline.chapters.find((c) => c.key === a.key) ?? null : null;
  const chB = B.loaded && b ? B.loaded.timeline.chapters.find((c) => c.key === b.key) ?? null : null;
  const clockA = A.loaded && chA ? paneClock(A.loaded.payload, chA, align) : null;
  const clockB = B.loaded && chB ? paneClock(B.loaded.payload, chB, align) : null;
  const range = clockRange([clockA, clockB].filter((c) => c !== null));

  // A new anchor or round moves where zero is; keep the clock inside the range.
  // Not before a round has loaded: the range is 0..0 until then, and clamping
  // to it threw away the time the URL asked for.
  const known = clockA !== null || clockB !== null;
  useEffect(() => {
    if (!known) return;
    setRel((r) => Math.max(range.min, Math.min(r, range.max)));
  }, [known, range.min, range.max]);

  useEffect(() => {
    if (!playing) return;
    let last: number | null = null;
    let raf = 0;
    const step = (now: number) => {
      // The first frame only sets the baseline; see MovementRenderer.play.
      const dt = last === null ? 0 : Math.max(0, Math.min(0.1, (now - last) / 1000));
      last = now;
      setRel((r) => {
        const next = r + dt * Number(speed);
        if (next >= range.max) { setPlaying(false); return range.max; }
        return next;
      });
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [playing, speed, range.max]);

  // The URL follows what is shown, so a comparison can be sent as a link. The
  // time is written only while paused: rewriting history sixty times a second
  // buys nothing.
  useEffect(() => {
    if (playing) return;
    history.replaceState(null, "", compareHref(a, b, align, rel));
  }, [a, b, align, rel, playing]);

  const swap = () => { setA(b); setB(a); };

  return (
    <div className="bcast cmp">
      <header className="bcast-top">
        <div className="bcast-id">
          <a className="back" href={HOME_HREF}>← All demos</a>
          <span className="bcast-map">Compare rounds</span>
        </div>
        <div className="cmp-align">
          <span className="muted">Line up by</span>
          <Segmented
            grow={false}
            options={(Object.keys(ANCHOR_LABEL) as Anchor[]).map((k) => ({ value: k, label: ANCHOR_LABEL[k] }))}
            value={align}
            onChange={(v) => { setAlign(v); setRel(0); }}
          />
        </div>
        <div className="cmp-actions">
          <button type="button" className="cmp-btn" onClick={swap} disabled={!a || !b}>⇄ Swap</button>
        </div>
      </header>

      <div className="cmp-panes">
        <Pane
          label="A"
          refd={a}
          onPick={setA}
          demos={demos}
          state={A}
          chapter={chA}
          sec={clockA ? paneSec(clockA, rel) : 0}
          missing={clockA?.missing ?? false}
          align={align}
          view={view}
        />
        <Pane
          label="B"
          refd={b}
          onPick={setB}
          demos={demos}
          state={B}
          chapter={chB}
          sec={clockB ? paneSec(clockB, rel) : 0}
          missing={clockB?.missing ?? false}
          align={align}
          view={view}
        />
      </div>

      <div className="cmp-bottom">
        <div className="st-play">
          <button
            type="button"
            className="pp"
            aria-label={playing ? "Pause" : "Play"}
            onClick={() => {
              if (!playing && rel >= range.max) setRel(range.min);
              setPlaying(!playing);
            }}
          >
            {playing ? "❚❚" : "▶"}
          </button>
          <button type="button" className="pp" aria-label="Back 5 seconds"
            onClick={() => setRel((r) => Math.max(range.min, r - 5))}>−5</button>
          <button type="button" className="pp" aria-label="Forward 5 seconds"
            onClick={() => setRel((r) => Math.min(range.max, r + 5))}>+5</button>
          <input
            type="range"
            min={range.min}
            max={range.max || 1}
            step={0.05}
            value={rel}
            onChange={(e) => { setPlaying(false); setRel(Number(e.target.value)); }}
            aria-label={`Seconds from ${ANCHOR_LABEL[align].toLowerCase()}`}
          />
          <span className="st-clock">
            {relClock(rel)} <span className="muted">from {ANCHOR_LABEL[align].toLowerCase()}</span>
          </span>
          <Segmented
            grow={false}
            options={SPEEDS.map((s) => ({ value: s, label: `${s}×` }))}
            value={speed}
            onChange={setSpeed}
          />
        </div>
        <ViewControls view={view} patch={(next) => setView((v) => ({ ...v, ...next }))} />
      </div>
    </div>
  );
}

interface PaneProps {
  label: "A" | "B";
  refd: RoundRef | undefined;
  onPick: (ref: RoundRef) => void;
  demos: DemoSummary[] | null;
  state: { loaded: Loaded | null; error: string | null };
  chapter: Chapter | null;
  /** Seconds into this pane's round. */
  sec: number;
  /** The anchor did not happen in this round; it is aligned at its start. */
  missing: boolean;
  align: Anchor;
  view: ViewState;
}

/**
 * One round, drawn by the replay's own renderer.
 *
 * The renderer runs a whole match clock; a pane never lets it play. It is
 * only ever seeked, through timeInRound, which holds it inside this round -
 * so everything the replay draws (tracers, damage, the bomb, deaths, the
 * view toggles) comes along without a second renderer to keep in step.
 */
function Pane({ label, refd, onPick, demos, state, chapter, sec, missing, align, view }: PaneProps) {
  const stage = useRef<HTMLDivElement>(null);
  const renderer = useRef<MovementRenderer | null>(null);
  const [summary, setSummary] = useState<Summary | null>(null);
  const loaded = state.loaded;
  const payload = loaded?.payload ?? null;

  useEffect(() => {
    if (!payload || !stage.current) return;
    const r = new MovementRenderer(stage.current, payload, {
      onFrame: setSummary,
      panelPx: panelSize(payload.sections.length),
    });
    renderer.current = r;
    void r.load();
    return () => { r.destroy(); renderer.current = null; };
  }, [payload]);

  useEffect(() => {
    if (!payload) return;
    renderer.current?.setState({ ...view, selected: payload.players.map((p) => p.id) });
  }, [payload, view]);

  useEffect(() => {
    if (chapter) renderer.current?.seek(timeInRound(chapter, sec));
  }, [chapter, sec, payload]);

  const colors = useMemo(() => {
    const out: Record<string, string> = {};
    payload?.players.forEach((p, i) => {
      out[p.id] = view.palette === "game" ? GAME_COLORS[p.col % GAME_COLORS.length] : SAFE_COLORS[i % SAFE_COLORS.length];
    });
    return out;
  }, [payload, view.palette]);
  const names = useMemo(() => Object.fromEntries((payload?.players ?? []).map((p) => [p.id, p.name])), [payload]);
  const teamOf = useMemo(() => Object.fromEntries((payload?.players ?? []).map((p) => [p.id, p.team])), [payload]);

  // Rounds grouped by game, for the picker.
  const games = useMemo(() => {
    const out = new Map<number, Chapter[]>();
    for (const c of loaded?.timeline.chapters ?? []) {
      const list = out.get(c.meta.dm);
      if (list) list.push(c); else out.set(c.meta.dm, [c]);
    }
    return [...out.entries()];
  }, [loaded]);

  // A demo just picked arrives with a placeholder key; once its rounds are
  // known, move to its first real one rather than showing nothing.
  useEffect(() => {
    if (!loaded || !refd) return;
    const chapters = loaded.timeline.chapters;
    if (chapters.length && !chapters.some((c) => c.key === refd.key)) {
      onPick({ demo: refd.demo, key: chapters[0].key });
    }
  }, [loaded, refd, onPick]);

  const demoName = refd?.demo ?? "";
  const [before0, before1] = chapter && loaded ? scoreBefore(loaded.timeline, chapter) : [0, 0];
  const alive = summary?.alive.size ?? 0;

  return (
    <section className="cmp-pane">
      <div className="cmp-pick">
        <span className={`cmp-badge cmp-${label.toLowerCase()}`}>{label}</span>
        <select
          aria-label={`Demo for pane ${label}`}
          value={demoName}
          onChange={(e) => onPick({ demo: e.target.value, key: "0:1" })}
        >
          {!demoName && <option value="">Choose a demo…</option>}
          {(demos ?? []).map((d) => (
            <option key={d.name} value={d.name}>{d.name} ({d.map.replace(/^(de|cs)_/, "")})</option>
          ))}
        </select>
        <select
          aria-label={`Round for pane ${label}`}
          value={refd?.key ?? ""}
          onChange={(e) => onPick({ demo: demoName, key: e.target.value })}
          disabled={!loaded}
        >
          {games.map(([dm, chapters]) => (
            <optgroup key={dm} label={games.length > 1 ? `Game ${dm + 1}` : "Rounds"}>
              {chapters.map((c) => (
                <option key={c.key} value={c.key}>
                  R{c.meta.n} · {sideName(c.meta.w)} win · {c.meta.sa}–{c.meta.sb}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
        {refd && chapter && (
          <a className="cmp-open" href={viewerHref(refd.demo, undefined, { round: chapter.meta.n, sec, game: chapter.meta.dm })}
            title="Open this round in the replay">Replay ↗</a>
        )}
      </div>

      {chapter && payload ? (
        <div className="cmp-info">
          <b>{roundTitle(payload, chapter)}</b>
          <span className="muted">{payload.map}</span>
          <span>{sideName(chapter.meta.w)} won · {chapter.meta.why}</span>
          <span className="muted">score before {before0}–{before1}</span>
          <span className="cmp-alive">{alive} alive</span>
          {missing && <span className="cmp-warn">no {align === "plant" ? "plant" : "kill"} this round: lined up at round start</span>}
        </div>
      ) : (
        <div className="cmp-info muted">{state.error ?? "Loading…"}</div>
      )}

      <div className="stage" ref={stage} />

      <div className="cmp-feed">
        <KillFeed kills={summary?.kills ?? []} names={names} colors={colors} teamOf={teamOf} />
      </div>
    </section>
  );
}
