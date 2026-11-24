import { useEffect, useMemo, useRef, useState } from "react";
import Segmented from "./Segmented";
import { ARC_COLOR, GAME_COLORS } from "../renderer";
import { makeProjector, radarImage } from "../radar";
import {
  analyticsHref, compareHref, HOME_HREF, proHref, startersHref, viewerHref,
} from "../route";
import {
  CT, T, defaultPlayer, flownTo, longest, playersIn, snapshot, startsFor, summarize, trailTo,
  type Side, type Source, type Start,
} from "../starters";
import { posAt, yawAt } from "../track";
import { loadPayload } from "../payload";
import { mapLabel, PROS, proBySlug } from "../pros";
import type { Payload } from "../types";

interface Props {
  /** One demo, or a set: a pro's Mirage pugs is five payloads, not one file. */
  demos: string[];
  player?: string;
  /** A catalogued pro, followed on whichever of their accounts each game has. */
  pro?: string;
  side?: "T" | "CT";
  /** Seconds into the round to open at. */
  t?: number;
}

/**
 * Who is followed: one account, or a pro across all of theirs. Kept as one
 * string so a <select> can hold either.
 */
type Who = string;
const PRO_PREFIX = "pro:";

type ColorBy = "outcome" | "game";
type TrailMode = "full" | "tail";

const WON = "#4cc94c";
const LOST = "#ff5a4a";
const DEAD = "#8d8a82";
/** Seconds of trail kept in "tail" mode: enough to read a direction of travel. */
const TAIL_SEC = 4;
/** CS2's round timer, so the clock can be read the way it reads in game. */
const ROUND_TIME = 115;

function mmss(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** Canvas edge in CSS pixels: as large as fits beside the two side columns. */
function panelSize(): number {
  const byHeight = window.innerHeight - 170;
  const byWidth = window.innerWidth - 2 * 236 - 2 * 14 - 36;
  return Math.max(420, Math.min(880, byHeight, byWidth));
}

export default function Starters({ demos, player: wantPlayer, pro: wantPro, side: wantSide, t: wantT }: Props) {
  const [sources, setSources] = useState<Source[] | null>(null);
  /** Demos left out of the set, and why: a different map, or a failed load. */
  const [skipped, setSkipped] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [radar, setRadar] = useState<HTMLImageElement | null>(null);

  const [who, setWho] = useState<Who>("");
  const [side, setSide] = useState<Side>(wantSide === "CT" ? CT : T);
  const [colorBy, setColorBy] = useState<ColorBy>("outcome");
  const [trail, setTrail] = useState<TrailMode>("full");
  const [nades, setNades] = useState(true);
  const [kills, setKills] = useState(true);
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [focus, setFocus] = useState<string | null>(null);
  /** Up to two rounds picked to open side by side in the compare view. */
  const [pick, setPick] = useState<string[]>([]);

  const [t, setT] = useState(wantT ?? 0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<"1" | "2" | "4">("1");

  const canvas = useRef<HTMLCanvasElement>(null);
  const [panel] = useState(panelSize);

  const demoKey = demos.join(",");
  useEffect(() => {
    let cancelled = false;
    Promise.allSettled(demos.map((name) => loadPayload(name).then((payload) => ({ name, payload }))))
      .then((results) => {
        if (cancelled) return;
        const ok = results.flatMap((r) => (r.status === "fulfilled" ? [r.value] : []));
        if (ok.length === 0) {
          setError(`none of the demos could be loaded (${demos.join(", ")})`);
          return;
        }
        // Positions only mean something on one radar. The set's map is the
        // first demo's; anything on another map is left out and listed.
        const map = ok[0].payload.map;
        const same = ok.filter((s) => s.payload.map === map);
        setSources(same);
        setSkipped([
          ...results.flatMap((r, i) => (r.status === "rejected" ? [`${demos[i]} (did not load)`] : [])),
          ...ok.filter((s) => s.payload.map !== map).map((s) => `${s.name} (${s.payload.map})`),
        ]);

        const present = new Set(playersIn(same).map((p) => p.id));
        const pro = proBySlug(wantPro);
        if (pro && pro.steamids.some((id) => present.has(id))) setWho(PRO_PREFIX + pro.slug);
        else setWho(wantPlayer && present.has(wantPlayer) ? wantPlayer : defaultPlayer(same));

        // Only one floor is drawn. Every map with a lower level would need the
        // replay's per-floor canvases; Mirage has none.
        const img = new Image();
        img.onload = () => !cancelled && setRadar(img);
        img.src = `${import.meta.env.BASE_URL}radar/${radarImage(map, "default")}`;
      });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [demoKey]);

  const base: Payload | null = sources?.[0]?.payload ?? null;
  const hz = base?.sample_hz ?? 4;
  const pro = who.startsWith(PRO_PREFIX) ? proBySlug(who.slice(PRO_PREFIX.length)) : undefined;
  const ids = useMemo(() => (pro ? pro.steamids : who ? [who] : []), [pro, who]);
  const people = useMemo(() => (sources ? playersIn(sources) : []), [sources]);
  const starts = useMemo(
    () => (sources && ids.length ? startsFor(sources, ids, side) : []),
    [sources, ids, side],
  );
  const visible = useMemo(() => starts.filter((s) => !hidden.has(s.id)), [starts, hidden]);
  const end = useMemo(() => longest(visible, hz), [visible, hz]);
  const totals = useMemo(() => summarize(starts), [starts]);
  const now = snapshot(visible, t, hz);
  const games = sources ? sources.reduce((n, s) => n + Math.max(1, s.payload.demos.length), 0) : 0;
  const name = pro?.name ?? people.find((p) => p.id === who)?.name ?? "";

  // Keep the URL on what is shown, so the page can be linked to as-is.
  useEffect(() => {
    if (!who || !sources) return;
    history.replaceState(null, "", startersHref(sources.map((s) => s.name), {
      ...(pro ? { pro: pro.slug } : { player: who }),
      side: side === T ? "T" : "CT",
    }));
  }, [sources, who, pro, side]);

  // A new player or side is a new set of rounds: start it from the top. The
  // first set is the one the URL asked for, so it keeps the URL's time.
  const firstSet = useRef(true);
  useEffect(() => {
    if (!who) return;
    if (firstSet.current) { firstSet.current = false; return; }
    setHidden(new Set()); setFocus(null); setT(0); setPick([]);
  }, [who, side]);

  useEffect(() => {
    if (!playing) return;
    let last = performance.now();
    let raf = 0;
    const step = (now: number) => {
      const dt = Math.min(0.1, (now - last) / 1000);
      last = now;
      setT((prev) => {
        const next = prev + dt * Number(speed);
        if (next >= end) { setPlaying(false); return end; }
        return next;
      });
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [playing, speed, end]);

  useEffect(() => {
    const el = canvas.current;
    if (!el || !base || !radar) return;
    draw(el, base, radar, panel, visible, t, {
      colorBy, trail, nades, kills, focus,
    });
  }, [base, radar, panel, visible, t, colorBy, trail, nades, kills, focus]);

  if (error) {
    return (
      <div className="fatal">
        <h1>Could not load these demos</h1>
        <p className="muted">{error}</p>
        <p><a className="back" href={HOME_HREF}>← All demos</a></p>
      </div>
    );
  }
  if (!sources || !base || !who) {
    return <div className="fatal muted">Loading {demos.length} demo{demos.length === 1 ? "" : "s"}…</div>;
  }

  // Counted the way startsFor filters, so the button and the page agree.
  const counts = {
    [T]: startsFor(sources, ids, T).length,
    [CT]: startsFor(sources, ids, CT).length,
  };
  const present = new Set(people.map((p) => p.id));
  const prosHere = PROS.filter((p) => p.steamids.some((id) => present.has(id)));
  const single = sources.length === 1 ? sources[0].name : null;
  const toggle = (key: string) =>
    setHidden((h) => {
      const next = new Set(h);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    });
  const togglePick = (key: string) =>
    setPick((p) => (p.includes(key) ? p.filter((k) => k !== key) : [...p, key].slice(-2)));
  const byId = (id: string) => starts.find((x) => x.id === id);
  const label = (id: string) => {
    const s = byId(id);
    return s ? `G${s.game + 1} R${s.round}` : id;
  };
  const refOf = (id: string) => {
    const s = byId(id);
    return s ? { demo: s.demoName, key: s.key } : undefined;
  };
  const showOnly = (keep: (s: Start) => boolean) =>
    setHidden(new Set(starts.filter((s) => !keep(s)).map((s) => s.id)));

  return (
    <div className="bcast st">
      <header className="bcast-top">
        <div className="bcast-id">
          {pro ? (
            <a className="back" href={proHref(pro.slug, base.map)}>← {pro.name}</a>
          ) : (
            <a className="back" href={HOME_HREF}>← All demos</a>
          )}
          <span className="bcast-map">{mapLabel(base.map)}</span>
          {single ? (
            <>
              <span className="bcast-demo">{single}</span>
              <nav className="view-tabs">
                <a href={viewerHref(single)}>Replay</a>
                <a href={analyticsHref(single)}>Analytics</a>
                <a aria-current="page">Starters</a>
              </nav>
            </>
          ) : (
            <span className="bcast-demo">{sources.length} demos</span>
          )}
        </div>
        <div className="st-title">
          <b>{name}</b>
          <span className={side === T ? "st-t" : "st-ct"}>{side === T ? "T side" : "CT side"}</span>
          <span className="muted">
            {totals.rounds} rounds · {games} game{games === 1 ? "" : "s"}
          </span>
        </div>
        <div className="st-totals">
          <span>won <b>{totals.won}/{totals.rounds}</b></span>
          <span>opening duels <b>{totals.openingsWon}–{totals.openingsLost}</b></span>
          <span>K/D <b>{totals.kills}/{totals.deaths}</b></span>
        </div>
      </header>

      <div className="bcast-body">
        <aside className="st-side">
          <label className="st-field">
            <span>Player</span>
            <select value={who} onChange={(e) => setWho(e.target.value)}>
              {prosHere.length > 0 && (
                <optgroup label="Pros, on every account">
                  {prosHere.map((p) => (
                    <option key={p.slug} value={PRO_PREFIX + p.slug}>{p.name}</option>
                  ))}
                </optgroup>
              )}
              <optgroup label="Players">
                {people.map((p) => (
                  <option key={p.id} value={p.id}>{p.name} ({p.rounds} rounds)</option>
                ))}
              </optgroup>
            </select>
          </label>
          <div className="st-field">
            <span>Side</span>
            <Segmented
              options={[
                { value: "T", label: `T (${counts[T]})` },
                { value: "CT", label: `CT (${counts[CT]})` },
              ]}
              value={side === T ? "T" : "CT"}
              onChange={(v) => setSide(v === "T" ? T : CT)}
            />
          </div>
          <div className="st-field">
            <span>Colour by</span>
            <Segmented
              options={[{ value: "outcome", label: "Round won/lost" }, { value: "game", label: "Game" }]}
              value={colorBy}
              onChange={setColorBy}
            />
          </div>
          <div className="st-field">
            <span>Trails</span>
            <Segmented
              options={[{ value: "full", label: "Whole route" }, { value: "tail", label: `Last ${TAIL_SEC}s` }]}
              value={trail}
              onChange={setTrail}
            />
          </div>
          <div className="st-field">
            <span>Show</span>
            <Segmented
              options={[{ value: "on", label: "Grenades" }, { value: "off", label: "off" }]}
              value={nades ? "on" : "off"}
              onChange={(v) => setNades(v === "on")}
            />
            <Segmented
              options={[{ value: "on", label: "Kills" }, { value: "off", label: "off" }]}
              value={kills ? "on" : "off"}
              onChange={(v) => setKills(v === "on")}
            />
          </div>

          <div className="st-now">
            <h2 className="feed-h">At {mmss(t)} into the round</h2>
            <dl>
              <dt>alive</dt><dd>{now.alive}/{now.rounds}</dd>
              <dt>kills so far</dt><dd>{now.kills}</dd>
              <dt>deaths so far</dt><dd>{now.deaths}</dd>
              <dt>grenades thrown</dt><dd>{now.thrown}</dd>
            </dl>
          </div>

          {skipped.length > 0 && (
            <p className="hint cmp-warn">Left out: {skipped.join(", ")}</p>
          )}
          <p className="hint">
            Every round is lined up from the moment it went live (end of freeze
            time), so the same second means the same point in every round.
            Positions are sampled {hz}× a second.
          </p>
        </aside>

        <main className="bcast-stage">
          <canvas
            ref={canvas}
            className="st-canvas"
            style={{ width: panel, height: panel }}
          />
          <div className="st-play">
            <button
              type="button"
              className="pp"
              aria-label={playing ? "Pause" : "Play"}
              onClick={() => {
                if (!playing && t >= end) setT(0);
                setPlaying(!playing);
              }}
            >
              {playing ? "❚❚" : "▶"}
            </button>
            <input
              type="range"
              min={0}
              max={end || 1}
              step={0.05}
              value={Math.min(t, end)}
              onChange={(e) => { setPlaying(false); setT(Number(e.target.value)); }}
              aria-label="Seconds into the round"
            />
            <span className="st-clock">
              {mmss(t)} <span className="muted">({mmss(ROUND_TIME - t)} on the clock)</span>
            </span>
            <Segmented
              grow={false}
              options={[{ value: "1", label: "1×" }, { value: "2", label: "2×" }, { value: "4", label: "4×" }]}
              value={speed}
              onChange={setSpeed}
            />
          </div>
        </main>

        <aside className="st-rounds">
          <div className="st-rounds-head">
            <h2 className="feed-h">Rounds</h2>
            <div className="seg">
              <button type="button" onClick={() => setHidden(new Set())}>All</button>
              <button type="button" onClick={() => showOnly((s) => s.won)}>Won</button>
              <button type="button" onClick={() => showOnly((s) => !s.won)}>Lost</button>
            </div>
          </div>
          {pick.length > 0 && (
            <div className="st-pick">
              <a href={compareHref(refOf(pick[0]), pick[1] ? refOf(pick[1]) : undefined)}>
                Compare {label(pick[0])}{pick[1] ? ` vs ${label(pick[1])}` : " with the next round"} →
              </a>
              <button type="button" onClick={() => setPick([])} aria-label="Clear picked rounds">✕</button>
            </div>
          )}
          <ul onMouseLeave={() => setFocus(null)}>
            {starts.map((s) => (
              <li
                key={s.id}
                className={hidden.has(s.id) ? "off" : focus === s.id ? "focus" : undefined}
                onMouseEnter={() => setFocus(s.id)}
              >
                <button type="button" onClick={() => toggle(s.id)} aria-pressed={!hidden.has(s.id)}>
                  <span
                    className="dot"
                    style={{ background: colorBy === "game" ? GAME_COLORS[s.game % GAME_COLORS.length] : s.won ? WON : LOST }}
                  />
                  <span className="st-rl">G{s.game + 1} R{s.round}</span>
                  <span className={s.won ? "st-w" : "st-l"}>{s.won ? "W" : "L"}</span>
                  <span className="st-k">{s.kills.length ? `${s.kills.length}K` : ""}</span>
                  <span className="st-o">
                    {s.opening === "won" ? "opened" : s.opening === "lost" ? "opened, died" : ""}
                  </span>
                  <span className="st-d">{s.diedAt !== null ? `† ${mmss(s.diedAt)}` : ""}</span>
                </button>
                <button
                  type="button"
                  className="st-cmp"
                  aria-pressed={pick.includes(s.id)}
                  title="Pick for side-by-side compare"
                  onClick={() => togglePick(s.id)}
                >
                  ⇄
                </button>
              </li>
            ))}
          </ul>
          <p className="hint">
            Hover a round to pick it out; click to hide it. ⇄ picks up to two
            rounds to watch side by side, with everyone in them.
          </p>
        </aside>
      </div>
    </div>
  );
}

interface DrawOptions {
  colorBy: ColorBy;
  trail: TrailMode;
  nades: boolean;
  kills: boolean;
  focus: string | null;
}

/**
 * Paint one moment. Plain canvas rather than the replay's renderer: that one
 * is built around one match clock and ten players, and this is one player
 * across many clocks - sharing it would mean teaching it both.
 */
function draw(
  el: HTMLCanvasElement,
  payload: Payload,
  radar: HTMLImageElement,
  panel: number,
  starts: Start[],
  sec: number,
  opt: DrawOptions,
): void {
  const dpr = window.devicePixelRatio || 1;
  if (el.width !== Math.round(panel * dpr)) {
    el.width = Math.round(panel * dpr);
    el.height = Math.round(panel * dpr);
  }
  const ctx = el.getContext("2d");
  if (!ctx) return;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, panel, panel);
  ctx.drawImage(radar, 0, 0, panel, panel);
  // Darken the radar a little: fifty faint trails need the contrast.
  ctx.fillStyle = "rgba(0,0,0,0.28)";
  ctx.fillRect(0, 0, panel, panel);

  const hz = payload.sample_hz;
  const proj = makeProjector(payload.radar, panel);
  const px = ([x, y]: [number, number]): [number, number] => [proj.x(x), proj.y(y)];
  const colorOf = (s: Start) =>
    opt.colorBy === "game" ? GAME_COLORS[s.game % GAME_COLORS.length] : s.won ? WON : LOST;

  // The focused round last, so it is drawn over the rest.
  const ordered = opt.focus
    ? [...starts.filter((s) => s.id !== opt.focus), ...starts.filter((s) => s.id === opt.focus)]
    : starts;

  for (const s of ordered) {
    const focused = s.id === opt.focus;
    const faded = opt.focus !== null && !focused;
    const color = colorOf(s);
    ctx.globalAlpha = faded ? 0.12 : 1;

    let pts = trailTo(s.track, sec, hz);
    if (opt.trail === "tail") pts = pts.slice(-Math.ceil(TAIL_SEC * hz) - 1);
    if (pts.length > 1) {
      ctx.beginPath();
      const [x0, y0] = px(pts[0]);
      ctx.moveTo(x0, y0);
      for (let i = 1; i < pts.length; i++) {
        const [x, y] = px(pts[i]);
        ctx.lineTo(x, y);
      }
      ctx.lineJoin = "round";
      ctx.lineCap = "round";
      ctx.lineWidth = focused ? 3 : 1.6;
      ctx.strokeStyle = color;
      ctx.globalAlpha = faded ? 0.08 : focused ? 1 : 0.55;
      ctx.stroke();
      ctx.globalAlpha = faded ? 0.12 : 1;
    }

    if (opt.nades) {
      for (const e of s.throws) {
        const flown = flownTo(e, sec);
        if (flown.length === 0) continue;
        const nc = ARC_COLOR[e.kind] ?? "#c3c2b7";
        ctx.beginPath();
        flown.map(px).forEach(([x, y], i) => (i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)));
        ctx.setLineDash([4, 4]);
        ctx.lineWidth = focused ? 2 : 1.2;
        ctx.strokeStyle = nc;
        ctx.stroke();
        ctx.setLineDash([]);
        if (sec >= e.t1) {
          const [lx, ly] = px([e.x[e.x.length - 1], e.y[e.y.length - 1]]);
          ctx.beginPath();
          ctx.arc(lx, ly, e.kind === "smoke" ? 6 : 4, 0, Math.PI * 2);
          ctx.fillStyle = nc;
          ctx.fill();
          ctx.lineWidth = 1;
          ctx.strokeStyle = "rgba(0,0,0,0.7)";
          ctx.stroke();
        }
      }
    }

    if (opt.kills) {
      for (const k of s.kills) {
        if (k.t > sec) continue;
        const [kx, ky] = px([k.x, k.y]);
        ctx.lineWidth = 2;
        ctx.strokeStyle = "#ffffff";
        ctx.beginPath();
        ctx.moveTo(kx - 4, ky - 4); ctx.lineTo(kx + 4, ky + 4);
        ctx.moveTo(kx + 4, ky - 4); ctx.lineTo(kx - 4, ky + 4);
        ctx.stroke();
      }
    }

    const dead = s.diedAt !== null && sec >= s.diedAt;
    if (dead) {
      const [dx, dy] = px(posAt(s.track, s.diedAt as number, hz));
      ctx.lineWidth = 2.5;
      ctx.strokeStyle = DEAD;
      ctx.beginPath();
      ctx.moveTo(dx - 5, dy - 5); ctx.lineTo(dx + 5, dy + 5);
      ctx.moveTo(dx + 5, dy - 5); ctx.lineTo(dx - 5, dy + 5);
      ctx.stroke();
    } else {
      const [x, y] = px(posAt(s.track, sec, hz));
      const ang = (-yawAt(s.track, sec, hz) * Math.PI) / 180;
      const reach = focused ? 26 : 16;
      ctx.lineCap = "round";
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x + Math.cos(ang) * reach, y + Math.sin(ang) * reach);
      ctx.lineWidth = 3.5;
      ctx.strokeStyle = "rgba(0,0,0,0.55)";
      ctx.stroke();
      ctx.lineWidth = 1.6;
      ctx.strokeStyle = color;
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(x, y, focused ? 6 : 4.5, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.fill();
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = "#1a1a19";
      ctx.stroke();
    }
    ctx.globalAlpha = 1;
  }
}
