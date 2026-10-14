/**
 * One game's analytics: the payload `demo-analytics -web` writes, and the
 * groupings the analytics page draws from it.
 *
 * The payload is facts, not finished tables — every opening kill, clutch and
 * player count, once. That is what lets the page filter by side, by player and
 * by plant without a table for each combination. Everything here is pure, so
 * the numbers on the page are testable without rendering it.
 *
 * Teams are 0 and 1, 0 being whoever started on T: the same order as
 * `Payload.teams` in movement.json, so the replay and this page agree.
 */

export interface GamePlayer {
  /** SteamID as a string — exceeds Number.MAX_SAFE_INTEGER. */
  id: string;
  name: string;
  team: number;
  rounds: number;
}

export interface GameRound {
  n: number;
  /** Team index that played T side this round. */
  t: number;
  /** Team index that won it. */
  winner: number;
}

/**
 * One death. K, D and A are each a count of the rows naming a player, which is
 * what lets the scoreboard be re-summed for one side.
 */
export interface GameKill {
  round: number;
  at: number;
  /** The VICTIM's team. A credited killer or assister is on the other one. */
  team: number;
  victim: string;
  /** Empty when nobody is credited: the world, a suicide, a team kill. */
  killer: string;
  /** Empty when nobody helped. */
  assister: string;
  weapon: string;
  headshot: boolean;
  /** The bomb was already down. */
  planted: boolean;
  /** After the round was won. Absent on payloads written before it existed. */
  post?: boolean;
}

export interface GameOpening {
  round: number;
  /** Seconds into the round's live play. Same on every fact. */
  at: number;
  /** The killer's team. */
  team: number;
  killer: string;
  victim: string;
  weapon: string;
  headshot: boolean;
}

export interface GameClutch {
  round: number;
  at: number;
  team: number;
  player: string;
  enemies: string[];
  planted: boolean;
  kills: number;
  survived: boolean;
}

export interface GameSituation {
  round: number;
  at: number;
  /** Who each team had alive, indexed by team. */
  alive: [string[], string[]];
  planted: boolean;
}

export interface Analytics {
  version: number;
  demo: string;
  map: string;
  /** Rounds left out of clutches and situations; see docs/demo-analytics.md. */
  rounds_uncertain: number;
  teams: [string, string];
  players: GamePlayer[];
  rounds: GameRound[];
  kills: GameKill[];
  openings: GameOpening[];
  clutches: GameClutch[];
  situations: GameSituation[];
}

/** The payload version this code reads. Mirrors webVersion in web.go. */
export const ANALYTICS_VERSION = 2;

const REQUIRED = ["teams", "players", "rounds", "kills", "openings", "clutches", "situations"] as const;

/**
 * Refuse a payload this code does not understand, by name.
 *
 * The alternative is `undefined is not iterable` from somewhere inside a
 * table, which says nothing about the actual problem: a stale file written by
 * an older demo-analytics.
 */
export function assertAnalytics(data: unknown): asserts data is Analytics {
  if (typeof data !== "object" || data === null) {
    throw new Error("analytics.json is not an object");
  }
  const a = data as Record<string, unknown>;
  if (a.version !== ANALYTICS_VERSION) {
    throw new Error(`analytics.json is version ${String(a.version)}; this app reads ${ANALYTICS_VERSION}`);
  }
  const missing = REQUIRED.filter((key) => !Array.isArray(a[key]));
  if (missing.length) throw new Error(`analytics.json is missing ${missing.join(", ")}`);
}

export type Side = "T" | "CT";
export type SideFilter = "all" | Side;
export type PlantFilter = "all" | "pre" | "post";

export interface Filter {
  side: SideFilter;
  /** Only moments this player was part of. Null for the whole team. */
  player: string | null;
  plant: PlantFilter;
}

export const NO_FILTER: Filter = { side: "all", player: null, plant: "all" };

/** Wins out of attempts. The unit every table on the page is made of. */
export interface Tally {
  n: number;
  wins: number;
}

/** A tally as a 0–1 rate, or null when there is nothing to divide by. */
export function rate(t: Tally): number | null {
  return t.n > 0 ? t.wins / t.n : null;
}

function add(t: Tally, won: boolean): void {
  t.n++;
  if (won) t.wins++;
}

/** Rounds by number, for the side and winner every fact leaves to the round. */
export function roundIndex(a: Analytics): Map<number, GameRound> {
  return new Map(a.rounds.map((r) => [r.n, r]));
}

export function sideOf(round: GameRound, team: number): Side {
  return round.t === team ? "T" : "CT";
}

function plantOk(planted: boolean, plant: PlantFilter): boolean {
  return plant === "all" || (plant === "post") === planted;
}

export type ResultFilter = "all" | "won" | "lost";

/**
 * The rounds a team played on a side and with a result, in order. Won and
 * lost are that team's: its opponent's list for "won" is this one for "lost".
 */
export function roundsFor(
  a: Analytics, team: number, side: SideFilter, result: ResultFilter,
): GameRound[] {
  return a.rounds.filter((r) => {
    if (side !== "all" && sideOf(r, team) !== side) return false;
    return result === "all" || (result === "won") === (r.winner === team);
  });
}

/** A team's rounds played and won, on one side or both. */
export function record(a: Analytics, team: number, side: SideFilter): Tally {
  const out: Tally = { n: 0, wins: 0 };
  for (const r of a.rounds) {
    if (side !== "all" && sideOf(r, team) !== side) continue;
    add(out, r.winner === team);
  }
  return out;
}

export interface OpeningRow extends GameOpening {
  side: Side;
  /** This team got the opening kill, rather than conceding it. */
  got: boolean;
  /** This team won the round. */
  won: boolean;
}

/**
 * Every round's opening kill from one team's point of view.
 *
 * With a player filter it is the openings that player was in, as killer or as
 * victim — both are "their" opening duel, and `got` says which way it went.
 */
export function openingRows(a: Analytics, team: number, f: Filter): OpeningRow[] {
  const rounds = roundIndex(a);
  const out: OpeningRow[] = [];
  for (const o of a.openings) {
    const round = rounds.get(o.round);
    if (!round) continue;
    const side = sideOf(round, team);
    if (f.side !== "all" && side !== f.side) continue;
    if (f.player && o.killer !== f.player && o.victim !== f.player) continue;
    out.push({ ...o, side, got: o.team === team, won: round.winner === team });
  }
  return out;
}

export interface OpeningSummary {
  /** Rounds won, out of the rounds this team got the first kill. */
  got: Tally;
  /** Rounds won, out of the rounds it conceded the first kill. */
  conceded: Tally;
}

export function openingSummary(rows: readonly OpeningRow[]): OpeningSummary {
  const out: OpeningSummary = { got: { n: 0, wins: 0 }, conceded: { n: 0, wins: 0 } };
  for (const r of rows) add(r.got ? out.got : out.conceded, r.won);
  return out;
}

export interface ClutchRow extends GameClutch {
  side: Side;
  won: boolean;
}

/** One team's clutch attempts, in round order. */
export function clutchRows(a: Analytics, team: number, f: Filter): ClutchRow[] {
  const rounds = roundIndex(a);
  const out: ClutchRow[] = [];
  for (const c of a.clutches) {
    const round = rounds.get(c.round);
    if (!round || c.team !== team) continue;
    const side = sideOf(round, team);
    if (f.side !== "all" && side !== f.side) continue;
    if (f.player && c.player !== f.player) continue;
    if (!plantOk(c.planted, f.plant)) continue;
    out.push({ ...c, side, won: round.winner === team });
  }
  return out;
}

/** Clutches as 1vN tallies, index N-1. Always five long, so a row never shifts. */
export function clutchesByEnemies(rows: readonly ClutchRow[]): Tally[] {
  const out: Tally[] = Array.from({ length: 5 }, () => ({ n: 0, wins: 0 }));
  for (const r of rows) {
    const slot = out[Math.min(r.enemies.length, 5) - 1];
    if (slot) add(slot, r.won);
  }
  return out;
}

export interface SituationRow {
  round: number;
  at: number;
  side: Side;
  own: string[];
  enemy: string[];
  planted: boolean;
  won: boolean;
}

export interface Cell extends Tally {
  own: number;
  enemy: number;
  rows: SituationRow[];
}

/** The key a player count is stored under: "4v5" is four of ours, five of theirs. */
export function countKey(own: number, enemy: number): string {
  return `${own}v${enemy}`;
}

/**
 * How often one team won from each player count it passed through.
 *
 * A round reaches a given count at most once, so a cell's `n` is rounds. With
 * a player filter, only the counts that player was ALIVE in are kept — which
 * is the question being asked: what happens when they are one of the four.
 */
export function conversionGrid(a: Analytics, team: number, f: Filter): Map<string, Cell> {
  const rounds = roundIndex(a);
  const out = new Map<string, Cell>();
  for (const s of a.situations) {
    const round = rounds.get(s.round);
    if (!round) continue;
    const side = sideOf(round, team);
    if (f.side !== "all" && side !== f.side) continue;
    if (!plantOk(s.planted, f.plant)) continue;

    const own = s.alive[team] ?? [];
    const enemy = s.alive[1 - team] ?? [];
    if (f.player && !own.includes(f.player)) continue;

    const key = countKey(own.length, enemy.length);
    let cell = out.get(key);
    if (!cell) out.set(key, (cell = { own: own.length, enemy: enemy.length, n: 0, wins: 0, rows: [] }));
    const won = round.winner === team;
    add(cell, won);
    cell.rows.push({ round: s.round, at: s.at, side, own, enemy, planted: s.planted, won });
  }
  return out;
}

export interface WeaponRow {
  weapon: string;
  kills: number;
  headshots: number;
}

export interface PlayerRow extends GamePlayer {
  kills: number;
  deaths: number;
  assists: number;
  headshots: number;
  openingKills: number;
  openingDeaths: number;
  clutches: Tally;
  /** The weapon they got the most kills with, if they got any. */
  topWeapon: WeaponRow | null;
}

/**
 * The deaths that pass the side and plant filters, from one team's point of
 * view. The player filter is not applied: callers differ on what it means.
 */
function killsFor(a: Analytics, team: number, f: Filter): GameKill[] {
  const rounds = roundIndex(a);
  return a.kills.filter((k) => {
    const round = rounds.get(k.round);
    if (!round) return false;
    if (f.side !== "all" && sideOf(round, team) !== f.side) return false;
    return plantOk(k.planted, f.plant);
  });
}

function tallyWeapons(kills: readonly GameKill[]): WeaponRow[] {
  const byWeapon = new Map<string, WeaponRow>();
  for (const k of kills) {
    let row = byWeapon.get(k.weapon);
    if (!row) byWeapon.set(k.weapon, (row = { weapon: k.weapon, kills: 0, headshots: 0 }));
    row.kills++;
    if (k.headshot) row.headshots++;
  }
  // Name breaks ties so equal weapons do not trade places between renders.
  return [...byWeapon.values()].sort((x, y) => y.kills - x.kills || x.weapon.localeCompare(y.weapon));
}

/**
 * One team's scoreboard with the analytics columns beside it, best first.
 *
 * Every column follows the side and plant filters: on "T side" a player's
 * K / D / A is what they did in the rounds their team played T. The player
 * filter is ignored — this is the table a player is picked from, and it would
 * blank every other row.
 */
export function playerRows(a: Analytics, team: number, f: Filter): PlayerRow[] {
  const everyone: Filter = { ...f, player: null };
  const kills = killsFor(a, team, everyone);
  const openings = openingRows(a, team, everyone);
  const clutches = clutchRows(a, team, everyone);

  return a.players
    .filter((p) => p.team === team)
    .map((p) => {
      const mine = clutches.filter((c) => c.player === p.id);
      const got = kills.filter((k) => k.killer === p.id);
      return {
        ...p,
        kills: got.length,
        deaths: kills.filter((k) => k.victim === p.id).length,
        assists: kills.filter((k) => k.assister === p.id).length,
        headshots: got.filter((k) => k.headshot).length,
        openingKills: openings.filter((o) => o.killer === p.id).length,
        openingDeaths: openings.filter((o) => o.victim === p.id).length,
        clutches: { n: mine.length, wins: mine.filter((c) => c.won).length },
        topWeapon: tallyWeapons(got)[0] ?? null,
      };
    })
    // Name last so equal rows do not swap as the filters change.
    .sort((x, y) => y.kills - x.kills || x.deaths - y.deaths || x.name.localeCompare(y.name));
}

/**
 * Kills by weapon for a team, or for the one player the filter names. Most
 * used first. Follows the side and plant filters like everything else.
 */
export function weaponRows(a: Analytics, team: number, f: Filter): WeaponRow[] {
  return tallyWeapons(
    killsFor(a, team, f).filter(
      // A credited kill on the other team's player is this team's kill.
      (k) => k.killer !== "" && k.team !== team && (!f.player || k.killer === f.player),
    ),
  );
}

export interface WeaponPlayerRow {
  player: string;
  kills: number;
  headshots: number;
}

/**
 * Who on a team got the kills with one weapon, most first. The player filter
 * is ignored: this is the breakdown ACROSS players, and it is only asked for
 * when the weapon list is showing the whole team.
 */
export function weaponPlayerRows(
  a: Analytics, team: number, f: Filter, weapon: string,
): WeaponPlayerRow[] {
  const byPlayer = new Map<string, WeaponPlayerRow>();
  for (const k of killsFor(a, team, f)) {
    if (k.killer === "" || k.team === team || k.weapon !== weapon) continue;
    let row = byPlayer.get(k.killer);
    if (!row) byPlayer.set(k.killer, (row = { player: k.killer, kills: 0, headshots: 0 }));
    row.kills++;
    if (k.headshot) row.headshots++;
  }
  // Id breaks ties so equal players do not trade places between renders.
  return [...byPlayer.values()].sort((x, y) => y.kills - x.kills || x.player.localeCompare(y.player));
}

export interface PathDeath {
  victim: string;
  /** Empty when nobody is credited with it. */
  killer: string;
  weapon: string;
  headshot: boolean;
  /** One of the chosen team's players died, rather than one of theirs. */
  own: boolean;
}

export interface PathStep {
  own: number;
  enemy: number;
  at: number;
  planted: boolean;
  /** Who died to get here from the step before. Empty on the first step. */
  deaths: PathDeath[];
}

export interface RoundPath {
  round: number;
  side: Side;
  won: boolean;
  steps: PathStep[];
}

/**
 * One round as a walk across the player-count grid: where it started, and
 * each count it moved to as people died.
 *
 * Who died at each step is read off the rosters — whoever was alive at the
 * last count and is not at this one — rather than matched to kills by time.
 * Two deaths a tick apart round to the same tenth of a second, and a time
 * match would hand both to the first step.
 *
 * The last step can have nobody left on one side. Those counts are not in
 * `situations`, which records only contested ones, so it is rebuilt here from
 * the round's remaining live deaths. A round with no situations at all — one
 * the collector could not trust the counts of — has no path.
 */
export function roundPath(a: Analytics, team: number, n: number): RoundPath | null {
  const round = roundIndex(a).get(n);
  const states = a.situations.filter((s) => s.round === n);
  if (!round || states.length === 0) return null;

  const kills = a.kills.filter((k) => k.round === n && !k.post);
  const death = (victim: string, own: boolean): PathDeath => {
    const k = kills.find((x) => x.victim === victim);
    return { victim, killer: k?.killer ?? "", weapon: k?.weapon ?? "", headshot: k?.headshot ?? false, own };
  };

  const steps: PathStep[] = [];
  let prev: [string[], string[]] | null = null;
  for (const s of states) {
    const own = s.alive[team] ?? [];
    const enemy = s.alive[1 - team] ?? [];
    const deaths = prev
      ? [
          ...prev[0].filter((id) => !own.includes(id)).map((id) => death(id, true)),
          ...prev[1].filter((id) => !enemy.includes(id)).map((id) => death(id, false)),
        ]
      : [];
    steps.push({ own: own.length, enemy: enemy.length, at: s.at, planted: s.planted, deaths });
    prev = [own, enemy];
  }

  if (prev) {
    const [own, enemy] = prev;
    const rest = kills.filter((k) => own.includes(k.victim) || enemy.includes(k.victim));
    if (rest.length) {
      const deaths = rest.map((k) => death(k.victim, own.includes(k.victim)));
      steps.push({
        own: own.length - deaths.filter((d) => d.own).length,
        enemy: enemy.length - deaths.filter((d) => !d.own).length,
        at: Math.max(...rest.map((k) => k.at)),
        planted: rest.some((k) => k.planted),
        deaths,
      });
    }
  }
  return { round: n, side: sideOf(round, team), won: round.winner === team, steps };
}

/** "7/12" — wins over attempts, the way every number on the page is written. */
export function fraction(t: Tally): string {
  return `${t.wins}/${t.n}`;
}

/** "58%", or an em dash when there were no attempts to rate. */
export function percent(t: Tally): string {
  const r = rate(t);
  return r === null ? "—" : `${Math.round(r * 100)}%`;
}
