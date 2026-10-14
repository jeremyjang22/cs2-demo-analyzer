import { useEffect, useMemo, useState } from "react";
import { usePager } from "./Pager";
import Segmented from "./Segmented";
import {
  assertAnalytics, clutchesByEnemies, clutchRows, conversionGrid, countKey, fraction,
  openingRows, openingSummary, percent, playerRows, rate, record, roundPath, roundsFor,
  sideOf, weaponPlayerRows, weaponRows,
  type Analytics as Data, type Cell, type ClutchRow, type Filter, type OpeningRow,
  type PlantFilter, type ResultFilter, type SideFilter, type Tally,
} from "../analytics";
import { HOME_HREF, viewerHref } from "../route";

interface Props {
  /** Folder name under public/data (locally) or under data/ in R2. */
  demo: string;
}

/**
 * How far before a moment its "watch" link lands. A link that opens on the
 * exact tick of a kill shows a body; a few seconds earlier shows the fight.
 */
const LEAD_IN = 4;

const COUNTS = [5, 4, 3, 2, 1];

/**
 * The grid's geometry, for drawing a round's path over it. These mirror
 * .an-grid in styles.css — 4px spacing, a 20px header row and column, 76x52
 * cells — and have to move with it.
 */
const GRID = { left: 28, top: 28, stepX: 80, stepY: 56, inset: 5 };

/**
 * Where a count sits on the grid, as the top-left corner of its cell — the
 * corner, so the path does not run through the numbers. A side with nobody
 * left has no cell, so it lands just past the last one.
 */
function pathPoint(own: number, enemy: number): { x: number; y: number } {
  const col = enemy > 0 ? 5 - enemy : 4.72;
  const row = own > 0 ? 5 - own : 4.74;
  return { x: GRID.left + col * GRID.stepX + GRID.inset, y: GRID.top + row * GRID.stepY + GRID.inset };
}

/** "0:41" — seconds into the round, the way the replay's clock reads. */
function clock(sec: number): string {
  const s = Math.max(0, Math.round(sec));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** One headline number with the fraction it came from underneath. */
function Tile({ label, tally, note }: { label: string; tally: Tally; note?: string }) {
  return (
    <div className="an-tile">
      <span className="an-tile-l">{label}</span>
      <span className="an-tile-v">{percent(tally)}</span>
      <span className="an-tile-s">{fraction(tally)}{note ? ` ${note}` : ""}</span>
    </div>
  );
}

function Result({ won }: { won: boolean }) {
  return <span className={`an-res ${won ? "won" : "lost"}`}>{won ? "Won" : "Lost"}</span>;
}

/** What every table below needs from the page to name things and link out. */
interface Ctx {
  teamName: string;
  nameOf: (id: string) => string;
  list: (ids: readonly string[]) => string;
  /** Link into the replay, a few seconds before a moment. */
  watch: (round: number, at: number) => string;
}

/*
 * Each table is its own component because each one pages independently, and a
 * hook cannot be called after the page's early returns for "loading" and
 * "no analytics".
 */

/** The rounds behind one cell of the grid. */
function CellTable({ cell, ctx }: { cell: Cell; ctx: Ctx }) {
  const { rows, controls } = usePager(cell.rows);
  return (
    <section className="an-card">
      <h2 className="feed-h">{cell.own}v{cell.enemy} — won {fraction(cell)}</h2>
      <table className="an-table">
        <thead>
          <tr>
            <th className="n">Round</th><th>Side</th><th>Bomb</th>
            <th>{ctx.teamName} alive</th><th>Opponents alive</th><th>Result</th><th />
          </tr>
        </thead>
        <tbody>
          {rows.map((s) => (
            <tr key={s.round}>
              <td className="n">{s.round}</td>
              <td>{s.side}</td>
              <td className="muted">{s.planted ? "planted" : "—"}</td>
              <td>{ctx.list(s.own)}</td>
              <td className="muted">{ctx.list(s.enemy)}</td>
              <td><Result won={s.won} /></td>
              <td><a className="an-watch" href={ctx.watch(s.round, s.at)}>Watch ▸</a></td>
            </tr>
          ))}
        </tbody>
      </table>
      {controls}
    </section>
  );
}

function ClutchTable({ rows: all, byEnemies, ctx }: { rows: ClutchRow[]; byEnemies: Tally[]; ctx: Ctx }) {
  const { rows, controls } = usePager(all);
  return (
    <section className="an-card">
      <h2 className="feed-h">Clutches</h2>
      {/* Totals for every clutch the filters allow, not just the page shown. */}
      <div className="an-strip">
        {byEnemies.map((t, i) => (
          <span key={i} className={t.n ? "" : "muted"}>
            <b>1v{i + 1}:</b> {t.n ? fraction(t) : "—"}
          </span>
        ))}
      </div>
      {all.length === 0
        ? <p className="hint">None with these filters.</p>
        : (
          <table className="an-table">
            <thead>
              <tr>
                <th className="n">Round</th><th>Player</th><th>Side</th><th>Clutch</th>
                <th className="n">Kills</th><th>Bomb</th><th>Result</th><th />
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.round} title={`against ${ctx.list(c.enemies)}`}>
                  <td className="n">{c.round}</td>
                  <td>{ctx.nameOf(c.player)}</td>
                  <td>{c.side}</td>
                  <td>1v{c.enemies.length}</td>
                  <td className="n">{c.kills}</td>
                  <td className="muted">{c.planted ? "planted" : "—"}</td>
                  <td>
                    <Result won={c.won} />
                    {c.won && !c.survived && <span className="muted"> · died</span>}
                  </td>
                  <td><a className="an-watch" href={ctx.watch(c.round, c.at)}>Watch ▸</a></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      {controls}
    </section>
  );
}

function OpeningTable({ rows: all, ctx }: { rows: OpeningRow[]; ctx: Ctx }) {
  const { rows, controls } = usePager(all);
  const summary = openingSummary(all);
  return (
    <section className="an-card">
      <h2 className="feed-h">First kills</h2>
      {/* Counts every first kill the filters allow, not just the page shown -
          and sits where the clutch card has its 1vN strip, so the two tables
          beside each other start on the same line. */}
      <div className="an-strip">
        <span><b>Won:</b> {summary.got.n}</span>
        <span><b>Lost:</b> {summary.conceded.n}</span>
      </div>
      {all.length === 0
        ? <p className="hint">None with these filters.</p>
        : (
          <table className="an-table">
            <thead>
              <tr>
                <th className="n">Round</th><th>Side</th><th>Killer</th><th>Victim</th>
                <th>Weapon</th><th>Result</th><th />
              </tr>
            </thead>
            <tbody>
              {rows.map((o) => (
                <tr key={o.round}>
                  <td className="n">{o.round}</td>
                  <td>{o.side}</td>
                  {/* Which name is ours is the fact the row exists for,
                      so theirs is the one that recedes. */}
                  <td className={o.got ? "" : "muted"}>{ctx.nameOf(o.killer)}</td>
                  <td className={o.got ? "muted" : ""}>{ctx.nameOf(o.victim)}</td>
                  <td className="muted">{o.weapon}{o.headshot ? " · HS" : ""}</td>
                  <td><Result won={o.won} /></td>
                  <td><a className="an-watch" href={ctx.watch(o.round, o.at)}>Watch ▸</a></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      {controls}
    </section>
  );
}

/**
 * Kills by weapon, as bars on one shared scale.
 *
 * For a whole team each weapon opens to show who got those kills. With a
 * player selected the list is already one person's, so there is nothing to
 * open and the rows are not buttons.
 */
function WeaponBars({ data, team, filter, ctx }: { data: Data; team: number; filter: Filter; ctx: Ctx }) {
  const [open, setOpen] = useState<string | null>(null);
  const weapons = weaponRows(data, team, filter);
  const { rows, controls } = usePager(weapons);
  // The scale is the busiest weapon overall, not the busiest on this page, so
  // a bar means the same length on every page and in every breakdown.
  const top = weapons[0]?.kills ?? 0;
  const width = (kills: number) => `${top ? (kills / top) * 100 : 0}%`;
  const whole = filter.player === null;

  const bar = (label: string, kills: number, headshots: number) => (
    <>
      <span className="an-bar-l">{label}</span>
      <span className="an-bar-track"><span className="an-bar" style={{ width: width(kills) }} /></span>
      <span className="an-bar-v">{kills}</span>
      <span className="an-bar-s muted">{headshots} HS</span>
    </>
  );

  return (
    <section className="an-card">
      <h2 className="feed-h">
        Kills by weapon — {filter.player ? ctx.nameOf(filter.player) : ctx.teamName}
      </h2>
      {whole && weapons.length > 0 && <p className="hint">Click a weapon to see who got the kills.</p>}
      {weapons.length === 0
        ? <p className="hint">No kills.</p>
        : (
          <ul className="an-bars">
            {rows.map((w) => {
              const isOpen = whole && open === w.weapon;
              return (
                <li key={w.weapon}>
                  {whole
                    ? (
                      <button
                        type="button"
                        className="an-bar-row"
                        aria-expanded={isOpen}
                        onClick={() => setOpen(isOpen ? null : w.weapon)}
                      >
                        <span className="an-bar-caret">{isOpen ? "▾" : "▸"}</span>
                        {bar(w.weapon, w.kills, w.headshots)}
                      </button>
                    )
                    : (
                      <div className="an-bar-row">
                        <span className="an-bar-caret" />
                        {bar(w.weapon, w.kills, w.headshots)}
                      </div>
                    )}
                  {isOpen && (
                    <ul className="an-bar-sub">
                      {weaponPlayerRows(data, team, filter, w.weapon).map((p) => (
                        <li key={p.player} className="an-bar-row">
                          <span className="an-bar-caret" />
                          {bar(ctx.nameOf(p.player), p.kills, p.headshots)}
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      {controls}
    </section>
  );
}

/**
 * One game's analytics, read alongside its replay.
 *
 * Every table here is a view of the same few fact lists, narrowed by the
 * filters at the top, and every row that names a moment links into the replay
 * a few seconds before it. That link is the point of the page: a number says
 * the 4v5s went badly, and the replay says why.
 */
export default function Analytics({ demo }: Props) {
  const [data, setData] = useState<Data | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [team, setTeam] = useState(0);
  const [side, setSide] = useState<SideFilter>("all");
  const [plant, setPlant] = useState<PlantFilter>("all");
  const [player, setPlayer] = useState<string | null>(null);
  const [cell, setCell] = useState<string | null>(null);
  const [pathRound, setPathRound] = useState<number | null>(null);
  const [result, setResult] = useState<ResultFilter>("all");

  useEffect(() => {
    const ac = new AbortController();
    fetch(`${import.meta.env.BASE_URL}data/${demo}/analytics.json`, { signal: ac.signal })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((raw: unknown) => {
        assertAnalytics(raw);
        setData(raw);
      })
      .catch((e: Error) => e.name !== "AbortError" && setError(e.message));
    return () => ac.abort();
  }, [demo]);

  const filter: Filter = useMemo(() => ({ side, plant, player }), [side, plant, player]);
  const names = useMemo(
    () => Object.fromEntries((data?.players ?? []).map((p) => [p.id, p.name])),
    [data],
  );

  if (error) {
    return (
      <div className="fatal">
        <h1>No analytics for {demo}</h1>
        <p className="muted">
          This demo has no analytics published. The replay may still be available.
        </p>
        <p>
          <a className="back" href={viewerHref(demo)}>Open the replay</a>
          {" · "}
          <a className="back" href={HOME_HREF}>← All demos</a>
        </p>
      </div>
    );
  }
  if (!data) return <div className="fatal muted">Loading {demo}…</div>;

  const nameOf = (id: string) => names[id] ?? id;
  const list = (ids: readonly string[]) => ids.map(nameOf).join(", ");
  const watch = (round: number, at: number) =>
    viewerHref(demo, undefined, { round, sec: Math.max(0, at - LEAD_IN) });

  const score = [record(data, 0, "all"), record(data, 1, "all")];
  const rec = record(data, team, side);
  const players = playerRows(data, team, filter);
  const openings = openingRows(data, team, filter);
  const opening = openingSummary(openings);
  const clutches = clutchRows(data, team, filter);
  const clutchAll: Tally = { n: clutches.length, wins: clutches.filter((c) => c.won).length };
  const byEnemies = clutchesByEnemies(clutches);
  const grid = conversionGrid(data, team, filter);
  const picked = cell ? grid.get(cell) : undefined;
  const ctx: Ctx = { teamName: data.teams[team], nameOf, list, watch };
  // The round list follows the team and side filters at the top, plus its own
  // won/lost one. A traced round that a filter removes from the list is no
  // longer traced: a dropdown showing nothing while a path stays drawn would
  // leave no way to tell which round it was.
  const pickable = roundsFor(data, team, side, result);
  const traced = pickable.some((r) => r.n === pathRound) ? pathRound : null;
  const path = traced === null ? null : roundPath(data, team, traced);
  const points = path?.steps.map((st) => pathPoint(st.own, st.enemy)) ?? [];

  // Changing team changes who a player id and a grid cell refer to.
  const pickTeam = (value: string) => {
    setTeam(Number(value));
    setPlayer(null);
    setCell(null);
  };

  return (
    <div className="an">
      <header className="bcast-top">
        <div className="bcast-id">
          <a className="back" href={HOME_HREF}>← All demos</a>
          <span className="bcast-map">{data.map}</span>
          <span className="bcast-demo">{demo}</span>
          <nav className="view-tabs">
            <a href={viewerHref(demo)}>Replay</a>
            <a aria-current="page">Analytics</a>
          </nav>
        </div>
        <div className="bcast-score">
          <span className="bs-team left">{data.teams[0]}</span>
          <b>{score[0].wins}</b>
          <span className="bs-round">Final</span>
          <b>{score[1].wins}</b>
          <span className="bs-team right">{data.teams[1]}</span>
        </div>
        <div />
      </header>

      {/* One row of filters above everything they narrow. */}
      <div className="an-filters">
        <Segmented
          grow={false}
          options={data.teams.map((name, i) => ({ value: String(i), label: name }))}
          value={String(team)}
          onChange={pickTeam}
        />
        <Segmented<SideFilter>
          grow={false}
          options={[
            { value: "all", label: "Both sides" },
            { value: "T", label: "T side" },
            { value: "CT", label: "CT side" },
          ]}
          value={side}
          onChange={setSide}
        />
        <Segmented<PlantFilter>
          grow={false}
          options={[
            { value: "all", label: "Any time" },
            { value: "pre", label: "Before plant" },
            { value: "post", label: "After plant" },
          ]}
          value={plant}
          onChange={setPlant}
        />
        {player && (
          <button type="button" className="an-chip" onClick={() => setPlayer(null)}>
            {nameOf(player)} ✕
          </button>
        )}
      </div>

      <div className="an-tiles">
        <Tile label="Rounds won" tally={rec} />
        <Tile label="Won after first kill" tally={opening.got} note="rounds" />
        <Tile label="Won after conceding it" tally={opening.conceded} note="rounds" />
        <Tile label="Clutches won" tally={clutchAll} />
      </div>

      <div className="an-cols">
        <section className="an-card">
          <h2 className="feed-h">Players</h2>
          <p className="hint">
            Click a player to narrow every table to the moments they were in.
          </p>
          <table className="an-table">
            <thead>
              <tr>
                <th>Player</th>
                <th className="n" title="kills / deaths / assists">K / D / A</th>
                <th className="n" title="share of kills that were headshots">HS</th>
                <th className="n" title="opening kills – opening deaths">First K–D</th>
                <th className="n" title="clutches won / attempted">Clutch</th>
                <th>Most kills with</th>
              </tr>
            </thead>
            <tbody>
              {players.map((p) => (
                <tr
                  key={p.id}
                  className={`an-pick ${player === p.id ? "on" : ""}`}
                  onClick={() => setPlayer(player === p.id ? null : p.id)}
                >
                  <td>{p.name}</td>
                  <td className="n">{p.kills} / {p.deaths} / {p.assists}</td>
                  <td className="n">{percent({ n: p.kills, wins: p.headshots })}</td>
                  <td className="n">{p.openingKills}–{p.openingDeaths}</td>
                  <td className="n">{p.clutches.n ? fraction(p.clutches) : "—"}</td>
                  <td className="muted">
                    {p.topWeapon ? `${p.topWeapon.weapon} (${p.topWeapon.kills})` : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>

        <section className="an-card">
          <h2 className="feed-h">
            Rounds won from each player count
            {player ? ` — with ${nameOf(player)} alive` : ""}
          </h2>
          <p className="hint">
            {data.teams[team]} down the side, opponents across. Click a count to list its rounds,
            or pick a round to trace how it went.
          </p>
          <div className="an-round-pick">
            <label htmlFor="an-round">Trace a round</label>
            <select
              id="an-round"
              value={traced ?? ""}
              onChange={(e) => setPathRound(e.target.value === "" ? null : Number(e.target.value))}
            >
              <option value="">{pickable.length ? `Choose from ${pickable.length} round${pickable.length === 1 ? "" : "s"}` : "No rounds match"}</option>
              {pickable.map((r) => (
                <option key={r.n} value={r.n}>
                  Round {r.n} · {data.teams[team]} {r.winner === team ? "won" : "lost"} ({sideOf(r, team)})
                </option>
              ))}
            </select>
            <Segmented<ResultFilter>
              grow={false}
              options={[
                { value: "all", label: "All" },
                { value: "won", label: "Won" },
                { value: "lost", label: "Lost" },
              ]}
              value={result}
              onChange={setResult}
            />
          </div>
          <div className={`an-grid-wrap ${path ? "tracing" : ""}`}>
          <table className="an-grid">
            <thead>
              <tr>
                <th />
                {COUNTS.map((e) => <th key={e}>v{e}</th>)}
              </tr>
            </thead>
            <tbody>
              {COUNTS.map((own) => (
                <tr key={own}>
                  <th>{own}</th>
                  {COUNTS.map((enemy) => {
                    const key = countKey(own, enemy);
                    const c = grid.get(key);
                    const r = c ? rate(c) : null;
                    if (!c || r === null) return <td key={key}><span className="an-cell empty">·</span></td>;
                    return (
                      <td key={key}>
                        <button
                          type="button"
                          className={`an-cell ${cell === key ? "on" : ""}`}
                          // One hue, stronger as the rate rises: the cell's
                          // job is magnitude. The numbers stay in ink.
                          style={{ background: `rgba(124, 108, 240, ${(0.06 + 0.6 * r).toFixed(2)})` }}
                          title={`${own}v${enemy}: won ${c.wins} of ${c.n} round${c.n === 1 ? "" : "s"}`}
                          onClick={() => setCell(cell === key ? null : key)}
                        >
                          <b>{percent(c)}</b>
                          <span>{fraction(c)}</span>
                        </button>
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          {/* Drawn over the table rather than in it: the path is one round,
              and the cells underneath keep saying what every round did. An
              enemy dying moves it right, one of ours moves it down. */}
          {path && (
            <svg className="an-path" aria-hidden="true">
              <polyline points={points.map((p) => `${p.x},${p.y}`).join(" ")} />
              {points.map((p, i) => (
                <g key={i}>
                  <circle cx={p.x} cy={p.y} r={9} />
                  <text x={p.x} y={p.y}>{i + 1}</text>
                </g>
              ))}
            </svg>
          )}
          </div>
          {traced !== null && !path && (
            <p className="hint">Round {traced} has no player counts recorded, so there is no path to trace.</p>
          )}
          {path && (
            <div className="an-trace">
              <div className="an-trace-h">
                <b>Round {path.round}</b>
                <span className="muted">{path.side} side</span>
                <Result won={path.won} />
                <a className="an-watch" href={viewerHref(demo, undefined, { round: path.round, sec: 0 })}>
                  Watch round ▸
                </a>
              </div>
              <ol>
                {path.steps.map((st, i) => (
                  <li key={i}>
                    <span className="an-step-n">{i + 1}</span>
                    <span className="an-step-t muted">{clock(st.at)}</span>
                    <b className="an-step-c">{st.own}v{st.enemy}</b>
                    <span className="an-step-d">
                      {st.deaths.length === 0 && <span className="muted">Round starts</span>}
                      {st.deaths.map((d) => (
                        // The arrow is the direction the path just moved, so
                        // the list and the drawing say the same thing twice.
                        <span key={d.victim} className={d.own ? "own" : ""}>
                          <i>{d.own ? "↓" : "→"}</i>
                          <b>{nameOf(d.victim)}</b>
                          <span className="muted">
                            {d.killer
                              ? ` killed by ${nameOf(d.killer)} · ${d.weapon}${d.headshot ? " · HS" : ""}`
                              : ` died · ${d.weapon || "no killer"}`}
                          </span>
                        </span>
                      ))}
                      {st.planted && i > 0 && !path.steps[i - 1].planted && (
                        <span className="muted">bomb planted by now</span>
                      )}
                    </span>
                    {i > 0 && <a className="an-watch" href={watch(path.round, st.at)}>Watch ▸</a>}
                  </li>
                ))}
              </ol>
            </div>
          )}
          {data.rounds_uncertain > 0 && (
            <p className="hint">
              {data.rounds_uncertain} round{data.rounds_uncertain === 1 ? " is" : "s are"} left
              out here and under clutches: a player left mid-round, so the counts are not known.
            </p>
          )}
        </section>
      </div>

      {picked && <CellTable key={cell} cell={picked} ctx={ctx} />}

      <div className="an-cols">
        <ClutchTable rows={clutches} byEnemies={byEnemies} ctx={ctx} />
        <OpeningTable rows={openings} ctx={ctx} />
      </div>

      <WeaponBars data={data} team={team} filter={filter} ctx={ctx} />
    </div>
  );
}
