/**
 * Canvas renderer for player movement.
 *
 * Deliberately not a React component. It owns the canvases, the playback clock,
 * and the draw loop, because a 60 fps animation must not run through React's
 * reconciler. React mounts it once, hands it state, and gets throttled
 * callbacks back for the parts of the UI that display time.
 */

import {
  arcsAt, bombAt, fadingAt, fuseProgress, kitsOnGround, tracerEnd,
  type Arc, type Fading,
} from "./effects";
import { makeProjector, radarImage, type Projector } from "./radar";
import { buildTimeline, chapterAt, clampTime, secWithin, type Chapter, type Timeline } from "./timeline";
import { deadAt, equipAt, flagAt, idxAt, lengthSec, posAt, runs, yawAt } from "./track";
import type {
  BombEvent, DamageEvent, KillEvent, KitEvent, Loadout, Payload, Player,
  RoundTrack, ShotEvent, TrajectoryEvent, UtilEvent,
} from "./types";

/** The five CS2 minimap colours, in slot order. */
export const GAME_COLORS = ["#F2C94C", "#B769D6", "#4CC94C", "#4E9BE8", "#F09A3E"];

/**
 * Validated 3-colour categorical set for the accessible palette. The game
 * colours are what players recognise but they are not colourblind-safe —
 * blue/purple are near-identical under deuteranopia — so this is the fallback
 * when hues must be told apart reliably. Three is the cap for scatter forms.
 */
export const SAFE_COLORS = ["#3987e5", "#d95926", "#199e70"];

const SURFACE = "#1a1a19";
const AIRBORNE = "#ffffff";
const TRAIL_SEC = 8;

/** Default horizontal FOV; scoped is much narrower and watches further. */
const FOV_DEG = 90;
const FOV_SCOPED_DEG = 22;
const CONE_PX = 30;
const CONE_SCOPED_PX = 62;

const FLOOR_LABELS: Record<string, string> = { default: "Upper", lower: "Lower" };

/** Utility fills, keyed by kind. Radii are Hammer units, drawn to scale. */
interface UtilStyle {
  fill: string;
  radius: number;
  ring?: string;
}

/**
 * Molotov and incendiary get near-identical treatment on purpose: on the
 * ground they are the same hazard, and giving them clearly different colours
 * would imply a distinction that does not exist in play. The real difference
 * is how long they last, which the timeline already shows. The slight tint
 * shift is only enough to tell them apart when you go looking.
 */
const UTIL_STYLE: Record<UtilEvent["kind"], UtilStyle> = {
  smoke: { fill: "rgba(214,222,230,0.34)", radius: 144, ring: "rgba(214,222,230,0.55)" },
  molotov: { fill: "rgba(232,120,42,0.32)", radius: 150, ring: "rgba(255,150,60,0.7)" },
  incendiary: { fill: "rgba(236,142,64,0.32)", radius: 110, ring: "rgba(255,172,96,0.7)" },
  decoy: { fill: "rgba(150,160,175,0.22)", radius: 60, ring: "rgba(170,180,195,0.5)" },
  he: { fill: "rgba(255,90,70,0.30)", radius: 70, ring: "rgba(255,120,90,0.85)" },
  flash: { fill: "rgba(255,255,220,0.30)", radius: 80, ring: "rgba(255,255,200,0.9)" },
};

/**
 * Anything the collector emits that this app has not been taught to draw. A
 * missing style used to skip the draw entirely, which is how 241 incendiaries
 * silently vanished when the collector learned to distinguish them. Rendering
 * something neutral means a new kind shows up as an unfamiliar blob rather
 * than as nothing at all.
 */
const UTIL_FALLBACK: UtilStyle = {
  fill: "rgba(180,180,190,0.28)",
  radius: 90,
  ring: "rgba(200,200,210,0.6)",
};

/** HE and flash are instants; hold them on screen briefly so they register. */
const BLAST_HOLD_SEC = 1.2;

/**
 * How long a tracer stays on screen. Short on purpose: a rifle empties thirty
 * rounds in three seconds, and a longer life turns a spray into a solid wedge
 * that says nothing about where the shooter was actually aiming over time.
 */
const TRACER_SEC = 0.35;

/**
 * How far a tracer runs when nothing recorded where it stopped. The demo does
 * not trace bullets — a shot only has an endpoint when it damaged somebody —
 * so this is the length of the "fired that way" ray. 900 Hammer units sits
 * between the median (607) and the p95 (1230) of shots that DID land in the
 * reference demos: long enough to read as a line of fire, short enough not to
 * imply a hit across the map.
 */
const TRACER_MISS_UNITS = 900;

/** How long a damage marker stays up. Long enough to read a number off it. */
const DAMAGE_SEC = 1.1;

/**
 * Damage marker colours, keyed by kind. Bullet damage is deliberately the
 * plainest: it is most of every round, and making the common case loud leaves
 * nothing to distinguish the rare cases with. Fire and HE borrow their hues
 * from the utility circles they come out of, so a burn marker reads as
 * belonging to the molotov it is sitting inside.
 */
interface DamageStyle {
  color: string;
  /** Drawn instead of the damage number when the number is not the point. */
  glyph?: string;
}

const DAMAGE_STYLE: Record<string, DamageStyle> = {
  bullet: { color: "#ff6b57" },
  he: { color: "#ff9d3e" },
  fire: { color: "#ff7a2f" },
  bomb: { color: "#ff3b30" },
  fall: { color: "#b3a894", glyph: "↓" },
  impact: { color: "#d6dee6" },
  knife: { color: "#e0e0e0" },
  zeus: { color: "#7fd6ff" },
};

/**
 * Same fallback rule as UTIL_FALLBACK: an unfamiliar kind must show up as
 * unfamiliar, not vanish. The collector's kind set is closed today and will
 * not stay that way.
 */
const DAMAGE_FALLBACK: DamageStyle = { color: "#c3c2b7" };

/** The bomb, in its five states. */
const BOMB_CARRIED = "#f0b429";
const BOMB_LOOSE = "#ffd166";
const BOMB_ARMED = "#ff3b30";
const BOMB_SAFE = "#4cc94c";

/**
 * A dropped defuse kit. CT blue, because that is the only side it means
 * anything to, and the same blue the app already uses for CT.
 */
/** A dropped defuse kit. CT blue, because that is the only side it means
 * anything to, and the same blue the app already uses for CT.
 */
const KIT_COLOR = "#5aa9f0";

/**
 * Grenade arc colours, matching the circles the same grenades leave behind and
 * the pips in the scoreboard. A flash arc and a flash bloom being the same
 * yellow is what lets you connect the throw to the effect without reading
 * anything.
 */
const ARC_COLOR: Record<UtilEvent["kind"], string> = {
  he: "#ff6b57",
  flash: "#ffe9a3",
  smoke: "#d6dee6",
  molotov: "#ff8a3d",
  incendiary: "#ffab60",
  decoy: "#aab4c2",
};

/** Where a player died. Deliberately not a player colour — see drawDeath. */
const DEATH_COLOR = "#8d8a82";

export interface ViewState {
  selected: string[];
  mode: "dots" | "trail" | "full";
  cones: boolean;
  util: boolean;
  labels: boolean;
  /** Bullet tracers and the damage they landed. One toggle: they are one story. */
  fire: boolean;
  bomb: boolean;
  /** Where everyone died, for the round on screen. */
  deaths: boolean;
  palette: "game" | "safe";
}

export interface VisibleRound {
  track: RoundTrack;
  color: string;
  /** null in "full" mode, where there is no single moment. */
  alive: boolean | null;
}

export interface Summary {
  /** Absolute match seconds. */
  t: number;
  /** Seconds into the round on screen. */
  sec: number;
  chapter: Chapter | null;
  /** Kills in this round up to now, newest last. */
  kills: KillEvent[];
  /** Players alive right now, by steamid. */
  alive: Set<string>;
  /** How many markers landed on each floor. */
  perFloor: number[];
  /** What each visible player is carrying right now, by steamid. */
  equip: Map<string, Loadout>;
  /** The bomb's state right now, or null before it is first seen. */
  bomb: BombEvent | null;
}

export interface RendererOptions {
  /** Called at most ~12x/sec with the current time and what is on screen. */
  onFrame?: (summary: Summary) => void;
  panelPx?: number;
}

export class MovementRenderer {
  private readonly payload: Payload;
  private readonly onFrame?: (s: Summary) => void;
  private readonly panelPx: number;
  private readonly contexts: CanvasRenderingContext2D[] = [];
  private readonly images: HTMLImageElement[] = [];
  private readonly project: Projector;
  private readonly floorCounts: HTMLElement[] = [];

  private readonly timeline: Timeline;
  private state: ViewState;
  /** Absolute match seconds. */
  private t = 0;
  private raf: number | null = null;
  private last: number | null = null;
  private lastEmit = 0;
  private speedFactor = 1;
  private destroyed = false;

  constructor(container: HTMLElement, payload: Payload, options: RendererOptions = {}) {
    this.payload = payload;
    this.onFrame = options.onFrame;
    this.panelPx = options.panelPx ??
      (payload.sections.length > 1 ? Math.floor(900 / payload.sections.length) - 10 : 900);
    this.project = makeProjector(payload.radar, this.panelPx);
    this.timeline = buildTimeline(payload);
    this.state = {
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

    const dpr = window.devicePixelRatio || 1;
    container.replaceChildren();
    for (const section of payload.sections) {
      const wrap = document.createElement("div");

      if (payload.sections.length > 1) {
        const label = document.createElement("div");
        label.className = "floor-label";
        const name = document.createElement("span");
        name.textContent = FLOOR_LABELS[section] ?? section;
        const count = document.createElement("span");
        label.append(name, count);
        wrap.append(label);
        this.floorCounts.push(count);
      }

      const canvas = document.createElement("canvas");
      canvas.width = this.panelPx * dpr;
      canvas.height = this.panelPx * dpr;
      canvas.style.width = `${this.panelPx}px`;
      canvas.style.height = `${this.panelPx}px`;
      const ctx = canvas.getContext("2d")!;
      ctx.scale(dpr, dpr);
      this.contexts.push(ctx);
      wrap.append(canvas);
      container.append(wrap);

      const img = new Image();
      img.src = `${import.meta.env.BASE_URL}radar/${radarImage(payload.map, section)}`;
      this.images.push(img);
    }
  }

  /** Resolve every floor's radar image before the first paint. */
  async load(): Promise<void> {
    await Promise.all(
      this.images.map(
        (img) =>
          new Promise<void>((resolve) => {
            if (img.complete) return resolve();
            img.onload = () => resolve();
            img.onerror = () => resolve(); // a missing floor draws empty, not blank
          }),
      ),
    );
    this.draw();
  }

  setState(next: ViewState): void {
    this.state = next;
    if (next.mode === "full") this.pause();
    this.draw();
  }

  setSpeed(factor: number): void {
    this.speedFactor = factor;
  }

  /** Move the match clock to an absolute time. */
  seek(t: number): void {
    this.t = clampTime(this.timeline, t);
    this.draw();
  }

  /** Move by a relative offset, for the +/- 5s buttons. */
  nudge(delta: number): void {
    this.seek(this.t + delta);
  }

  get time(): number {
    return this.t;
  }

  get matchDuration(): number {
    return this.timeline.duration;
  }

  get chapters(): Chapter[] {
    return this.timeline.chapters;
  }

  get playing(): boolean {
    return this.raf !== null;
  }

  play(): void {
    if (this.raf !== null || this.state.mode === "full" || this.destroyed) return;
    // Deliberately NOT performance.now(): the rAF timestamp is the time the
    // frame began, which can precede a reading taken here in the click handler.
    // Mixing the two produced a negative first delta, which drove time below
    // zero and NaN'd the render. Let the first frame set the baseline.
    this.last = null;
    this.raf = requestAnimationFrame(this.step);
  }

  pause(): void {
    if (this.raf !== null) cancelAnimationFrame(this.raf);
    this.raf = null;
    this.last = null;
  }

  destroy(): void {
    this.destroyed = true;
    this.pause();
  }

  private step = (now: number): void => {
    if (this.raf === null) return;
    if (this.last === null) this.last = now;
    const dt = Math.max(0, now - this.last); // never run time backwards
    this.last = now;

    this.t += (dt / 1000) * this.speedFactor;
    if (this.t > this.timeline.duration) {
      this.t = this.timeline.duration;
      this.pause();
    }

    this.draw();
    this.raf = requestAnimationFrame(this.step);
  };

  private colorOf(player: Player): string {
    if (this.state.palette === "game") return GAME_COLORS[player.col % GAME_COLORS.length];
    const i = this.state.selected.indexOf(player.id);
    return SAFE_COLORS[Math.max(0, i) % SAFE_COLORS.length];
  }

  /** Tracks for the round on screen, for the players that are selected. */
  private tracksFor(chapter: Chapter): Array<{ player: Player; track: RoundTrack }> {
    const out: Array<{ player: Player; track: RoundTrack }> = [];
    for (const id of this.state.selected) {
      const player = this.payload.players.find((p) => p.id === id);
      const track = player?.rounds.find((r) => r.k === chapter.key);
      if (player && track) out.push({ player, track });
    }
    return out;
  }

  draw(): void {
    const { payload, state } = this;
    const hz = payload.sample_hz;
    const chapter = chapterAt(this.timeline, this.t);

    this.contexts.forEach((ctx, i) => {
      ctx.clearRect(0, 0, this.panelPx, this.panelPx);
      ctx.globalAlpha = 0.5;
      ctx.drawImage(this.images[i], 0, 0, this.panelPx, this.panelPx);
      ctx.globalAlpha = 1;
    });

    const perFloor = payload.sections.map(() => 0);
    const alive = new Set<string>();
    const equip = new Map<string, Loadout>();
    let kills: KillEvent[] = [];
    let bomb: BombEvent | null = null;

    if (chapter) {
      const sec = secWithin(chapter, this.t);
      const live = state.mode !== "full";
      kills = (payload.kills[chapter.key] ?? []).filter((k) => k.t <= sec);

      // Utility under the players: it is context, not the subject.
      if (state.util) {
        for (const u of payload.util[chapter.key] ?? []) {
          const until = u.t1 > u.t ? u.t1 : u.t + BLAST_HOLD_SEC;
          if (sec >= u.t && sec <= until) this.drawUtil(u, sec);
        }
        // Arcs share the utility toggle: a throw and what it becomes are one
        // act, and splitting them across two switches would be pedantry.
        if (live) {
          for (const arc of arcsAt(this.trajectories(chapter), sec)) this.drawArc(arc);
        }
      }

      // Death markers below everyone still playing: they are where the round
      // has been, not what it is doing.
      if (state.deaths && live) this.drawDeaths(chapter, sec);

      // Tracers sit above utility and below the players: a bullet passes over
      // a smoke and under the person who fired it.
      if (state.fire && live) {
        for (const tracer of fadingAt(this.shots(chapter), sec, TRACER_SEC)) {
          this.drawTracer(tracer);
        }
      }

      const tracks = this.tracksFor(chapter);
      for (const { player, track } of tracks) {
        const color = this.colorOf(player);
        if (!live) {
          this.drawPath(track, 0, track.x.length - 1, color, 0.22);
          continue;
        }
        if (sec > lengthSec(track, hz)) continue;
        if (state.mode === "trail") {
          this.drawPath(
            track,
            Math.floor(idxAt(sec - TRAIL_SEC, hz)),
            Math.floor(idxAt(sec, hz)),
            color,
            0.45,
          );
        }
        const loadout = equipAt(track, sec, hz);
        perFloor[this.drawMarker(track, sec, color, player.name, loadout.hp)] += 1;
        if (!deadAt(track, sec, hz)) alive.add(player.id);
        equip.set(player.id, loadout);
      }

      // Drawn in every mode, unlike tracers and damage: those are instants
      // and mean nothing over a whole round, while "where the bomb was at this
      // point" is exactly the context a full-round path view is missing. Kits
      // ride the same toggle - both answer "what is on the floor right now".
      if (state.bomb) {
        for (const kit of kitsOnGround(this.kitEvents(chapter), sec)) {
          this.drawKit(kit);
        }
        bomb = bombAt(this.bombEvents(chapter), sec);
        if (bomb) this.drawBomb(bomb, sec, chapter);
      }

      // Damage last, over everything: it is the thing that just happened.
      if (state.fire && live) {
        for (const hit of fadingAt(this.damage(chapter), sec, DAMAGE_SEC)) {
          this.drawDamage(hit);
        }
      }
    }

    this.floorCounts.forEach((el, i) => {
      el.textContent = state.mode === "full" ? "" : `${perFloor[i]} here`;
    });

    const now = performance.now();
    if (this.onFrame && (now - this.lastEmit > 80 || !this.playing)) {
      this.lastEmit = now;
      this.onFrame({
        t: this.t,
        sec: chapter ? secWithin(chapter, this.t) : 0,
        chapter,
        kills,
        alive,
        perFloor,
        equip,
        bomb,
      });
    }
  }

  /** Shots for a round; empty on a payload written before they existed. */
  private shots(chapter: Chapter): ShotEvent[] {
    return this.payload.shots?.[chapter.key] ?? [];
  }

  private damage(chapter: Chapter): DamageEvent[] {
    return this.payload.damage?.[chapter.key] ?? [];
  }

  private bombEvents(chapter: Chapter): BombEvent[] {
    return this.payload.bomb?.[chapter.key] ?? [];
  }

  private kitEvents(chapter: Chapter): KitEvent[] {
    return this.payload.kits?.[chapter.key] ?? [];
  }

  /** World units to pixels, for anything sized in Hammer units. */
  private get scale(): number {
    return this.panelPx / this.payload.radar.size / this.payload.radar.scale;
  }

  private ctxFor(level: number): CanvasRenderingContext2D {
    return this.contexts[level] ?? this.contexts[0];
  }

  /**
   * Draw one bullet's path.
   *
   * A shot that hit somebody is drawn as a line that stops on them, capped
   * with a dot. A shot that hit a wall is drawn as a ray that fades out,
   * because the demo never recorded where it stopped — the fade is the honest
   * rendering of "this way, we do not know how far". Giving both the same
   * hard endpoint would put a fictional impact point on the map.
   *
   * Coloured by shooter so a firefight reads as two sides exchanging rather
   * than as anonymous lines, and drawn thin and brief so a full spray does not
   * black out the corridor it went down.
   */
  private drawTracer({ event, age }: Fading<ShotEvent>): void {
    const shooter = this.state.selected.includes(event.by);
    if (!shooter) return; // hiding a player hides their fire, like their dot

    const player = this.payload.players.find((p) => p.id === event.by);
    const color = player ? this.colorOf(player) : "#ffffff";
    const end = tracerEnd(event, TRACER_MISS_UNITS);

    const ctx = this.ctxFor(event.lv);
    const x0 = this.project.x(event.x);
    const y0 = this.project.y(event.y);
    const x1 = this.project.x(end.x);
    const y1 = this.project.y(end.y);

    ctx.save();
    ctx.globalAlpha = 0.85 * (1 - age);
    ctx.lineCap = "round";
    ctx.lineWidth = 1.4;

    if (end.hit) {
      ctx.strokeStyle = color;
    } else {
      // Fade along the ray rather than at a fixed alpha: the near end is where
      // we know the bullet was, the far end is a guess about direction only.
      const grad = ctx.createLinearGradient(x0, y0, x1, y1);
      grad.addColorStop(0, color);
      grad.addColorStop(1, "transparent");
      ctx.strokeStyle = grad;
    }
    ctx.beginPath();
    ctx.moveTo(x0, y0);
    ctx.lineTo(x1, y1);
    ctx.stroke();

    if (end.hit) {
      ctx.beginPath();
      ctx.arc(x1, y1, 2.2, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.fill();
    }
    ctx.restore();
  }

  /**
   * Draw one hit taken: a ring that expands off the victim and the damage
   * number beside it.
   *
   * The ring expands rather than pulsing in place so that several hits in a
   * second read as several events — a static marker redrawn at the same radius
   * looks like one long hit. The number is the point of the marker: "took 73"
   * is the fact, and its colour is the only thing carrying how.
   */
  private drawDamage({ event, age }: Fading<DamageEvent>): void {
    // The marker belongs to the victim, so it follows the victim's row in the
    // scoreboard: hide the player, hide what happened to them.
    if (!this.state.selected.includes(event.v)) return;

    const style = DAMAGE_STYLE[event.k] ?? DAMAGE_FALLBACK;
    const ctx = this.ctxFor(event.lv);
    const x = this.project.x(event.x);
    const y = this.project.y(event.y);

    // Bigger hits push a wider ring, so a 90-damage AWP hit is visibly not a
    // 9-damage leg clip. Capped, or the bomb's 500 would cover the site.
    const weight = Math.min(1, event.hp / 100);
    const radius = 5 + (6 + 9 * weight) * age;

    ctx.save();
    ctx.globalAlpha = 1 - age;
    ctx.beginPath();
    ctx.arc(x, y, radius, 0, Math.PI * 2);
    ctx.lineWidth = 1.5 + 1.5 * weight;
    ctx.strokeStyle = style.color;
    ctx.stroke();

    const label = style.glyph ?? String(event.hp);
    ctx.font = `700 ${11 + 3 * weight}px system-ui, -apple-system, sans-serif`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    // Rises as it fades, the way a damage number does in every game that has
    // ever shown one — it separates overlapping hits without moving the ring.
    const ty = y - 12 - 8 * age;
    ctx.lineWidth = 3;
    ctx.lineJoin = "round";
    ctx.strokeStyle = "rgba(0,0,0,0.85)";
    ctx.strokeText(label, x, ty);
    ctx.fillStyle = style.color;
    ctx.fillText(label, x, ty);
    ctx.restore();
  }

  private trajectories(chapter: Chapter): TrajectoryEvent[] {
    return this.payload.traj?.[chapter.key] ?? [];
  }

  /**
   * Draw one grenade's flight path.
   *
   * Only the flown part is drawn while it is in the air, so the line grows out
   * of the thrower's hand and arrives when the grenade does — drawing the
   * whole path from the moment of the throw would give away where it lands
   * before it lands, which is the one thing a viewer is watching to find out.
   *
   * The path can change floors mid-flight, so each segment goes on the canvas
   * for its own point's level. On a single-level map that is always canvas 0
   * and the loop costs nothing.
   */
  private drawArc({ event, upto, fade }: Arc): void {
    if (upto < 1) return; // a single point is not a line

    const color = ARC_COLOR[event.kind] ?? "#c3c2b7";
    const alpha = 0.85 * (1 - fade);

    for (let i = 1; i <= upto; i++) {
      const level = Number(event.lvl[i] ?? "0");
      const ctx = this.ctxFor(level);
      ctx.save();
      ctx.globalAlpha = alpha;
      ctx.strokeStyle = color;
      ctx.lineWidth = 1.6;
      ctx.lineCap = "round";
      ctx.setLineDash([4, 3]);
      ctx.beginPath();
      ctx.moveTo(this.project.x(event.x[i - 1]), this.project.y(event.y[i - 1]));
      ctx.lineTo(this.project.x(event.x[i]), this.project.y(event.y[i]));
      ctx.stroke();
      ctx.restore();
    }

    // The grenade itself, at the head of the line, while it is still moving.
    if (fade === 0) {
      const ctx = this.ctxFor(Number(event.lvl[upto] ?? "0"));
      ctx.save();
      ctx.globalAlpha = alpha;
      ctx.beginPath();
      ctx.arc(this.project.x(event.x[upto]), this.project.y(event.y[upto]), 3, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.fill();
      ctx.lineWidth = 1;
      ctx.strokeStyle = "rgba(0,0,0,0.7)";
      ctx.stroke();
      ctx.restore();
    }
  }

  /**
   * Mark where each dead player fell.
   *
   * Read off the tracks rather than from an event list: a track that ends in a
   * death stops at the position it ended at, so the last sample IS the death
   * position, exactly and without a join.
   *
   * Drawn in one grey rather than in player colours. Ten coloured crosses
   * compete with ten coloured dots for the same attention, and the useful
   * question a death marker answers — where did people die this round — is
   * about the shape of the set, not about who is in it. The name beside it
   * says who.
   */
  private drawDeaths(chapter: Chapter, sec: number): void {
    const hz = this.payload.sample_hz;

    for (const { player, track } of this.tracksFor(chapter)) {
      if (track.d < 0) continue;
      const diedAt = track.d / hz;
      if (sec < diedAt) continue;

      const last = track.x.length - 1;
      const ctx = this.ctxFor(Number(track.lvl[last] ?? "0"));
      const x = this.project.x(track.x[last]);
      const y = this.project.y(track.y[last]);

      // Fades in over its first half-second so the moment of death still
      // reads as an event rather than a marker that was always there.
      const age = Math.min(1, (sec - diedAt) / 0.5);
      ctx.save();
      ctx.globalAlpha = 0.25 + 0.3 * age;
      ctx.strokeStyle = DEATH_COLOR;
      ctx.lineWidth = 2;
      ctx.lineCap = "round";
      const r = 4;
      ctx.beginPath();
      ctx.moveTo(x - r, y - r);
      ctx.lineTo(x + r, y + r);
      ctx.moveTo(x + r, y - r);
      ctx.lineTo(x - r, y + r);
      ctx.stroke();

      if (this.state.labels) {
        ctx.globalAlpha = 0.4 * age;
        ctx.font = "500 9px system-ui, -apple-system, sans-serif";
        ctx.textAlign = "center";
        ctx.fillStyle = DEATH_COLOR;
        ctx.fillText(player.name, x, y + 15);
      }
      ctx.restore();
    }
  }

  /**
   * Arc around a player showing how much health is left.
   *
   * Only drawn below full. At ten players a permanent ring on everyone is ten
   * more circles competing with the dots, cones and labels already there —
   * whereas "this one is hurt" is exactly the thing worth making impossible to
   * miss, and it only means something if the unhurt are quiet.
   */
  private drawHealth(ctx: CanvasRenderingContext2D, x: number, y: number, hp: number): void {
    if (hp <= 0 || hp >= 100) return;

    const fraction = hp / 100;
    // Straight red at low health rather than a gradient across the whole
    // range: the only threshold that changes a decision is "one bullet".
    const color = hp <= 35 ? "#ff4d3d" : "#ffc148";
    const start = -Math.PI / 2;

    ctx.save();
    ctx.beginPath();
    ctx.arc(x, y, 9.5, start, start + Math.PI * 2 * fraction);
    ctx.lineWidth = 2;
    ctx.lineCap = "butt";
    ctx.strokeStyle = color;
    ctx.stroke();
    ctx.restore();
  }

  /**
   * Draw a defuse kit lying on the ground.
   *
   * A diamond, so the three things that can be on the floor are three
   * silhouettes: players are circles, the bomb is a square, a kit is a
   * diamond. Smaller and quieter than the bomb, which is the right relative
   * weight — a loose kit changes how a retake goes, a loose bomb changes who
   * wins the round.
   */
  private drawKit(kit: KitEvent): void {
    const ctx = this.ctxFor(kit.lv);
    const x = this.project.x(kit.x);
    const y = this.project.y(kit.y);
    const r = 5;

    ctx.save();
    ctx.beginPath();
    ctx.moveTo(x, y - r);
    ctx.lineTo(x + r, y);
    ctx.lineTo(x, y + r);
    ctx.lineTo(x - r, y);
    ctx.closePath();
    ctx.fillStyle = KIT_COLOR;
    ctx.fill();
    ctx.lineWidth = 1.5;
    ctx.strokeStyle = "rgba(0,0,0,0.75)";
    ctx.stroke();

    // The cutters' cross, at the size a 10px diamond can carry: two strokes.
    ctx.beginPath();
    ctx.moveTo(x - 2, y);
    ctx.lineTo(x + 2, y);
    ctx.moveTo(x, y - 2);
    ctx.lineTo(x, y + 2);
    ctx.lineWidth = 1.2;
    ctx.strokeStyle = "rgba(0,0,0,0.8)";
    ctx.stroke();
    ctx.restore();
  }

  /**
   * Draw the C4.
   *
   * A carried bomb follows its carrier's interpolated position rather than the
   * pickup coordinate stored on the event: the event says who has it, and the
   * carrier's own track says where they are this instant. Falling back to the
   * stored position covers a carrier who is hidden or absent from the round's
   * tracks, which is rare but should not make the bomb disappear.
   *
   * A planted bomb pulses, and the pulse accelerates with the fuse — see
   * fuseProgress. That is the one piece of bomb state a spectator actually
   * needs and cannot get from a static icon.
   */
  private drawBomb(event: BombEvent, sec: number, chapter: Chapter): void {
    let wx = event.x;
    let wy = event.y;
    let level = event.lv;

    if (event.st === "carried" && event.by) {
      // Deliberately not tracksFor(): that only returns SELECTED players, and
      // hiding the carrier's row would leave the bomb frozen at the pickup
      // coordinate rather than hidden — a wrong position, not a missing one.
      // The bomb is map state and has its own toggle.
      const carrier = this.payload.players
        .find((p) => p.id === event.by)?.rounds
        .find((r) => r.k === chapter.key);
      if (carrier) {
        const hz = this.payload.sample_hz;
        if (deadAt(carrier, sec, hz)) return; // a dead carrier drops it; the next event says where
        [wx, wy] = posAt(carrier, sec, hz);
        level = Number(flagAt(carrier.lvl, sec, hz));
      }
    }

    const ctx = this.ctxFor(level);
    const x = this.project.x(wx);
    const y = this.project.y(wy);

    if (event.st === "planted") {
      // The blast radius is real information: it is why the CT retake stands
      // where it stands. Drawn faintly, since it is a boundary, not an event.
      const urgency = fuseProgress(event, sec);
      const beat = 0.55 - 0.42 * urgency;
      const pulse = (sec % beat) / beat;
      ctx.save();
      ctx.globalAlpha = 0.55 * (1 - pulse);
      ctx.beginPath();
      ctx.arc(x, y, 7 + 12 * pulse, 0, Math.PI * 2);
      ctx.lineWidth = 2;
      ctx.strokeStyle = BOMB_ARMED;
      ctx.stroke();
      ctx.restore();
    }

    const color = {
      carried: BOMB_CARRIED,
      dropped: BOMB_LOOSE,
      planted: BOMB_ARMED,
      defused: BOMB_SAFE,
      exploded: BOMB_ARMED,
    }[event.st];

    // A square, so the bomb is never mistaken for a player at a glance — every
    // other marker on this map is a circle.
    const size = event.st === "carried" ? 4.5 : 6;
    ctx.save();
    ctx.beginPath();
    ctx.rect(x - size, y - size, size * 2, size * 2);
    ctx.fillStyle = color;
    ctx.fill();
    ctx.lineWidth = 1.5;
    ctx.strokeStyle = "rgba(0,0,0,0.75)";
    ctx.stroke();

    if (event.st !== "carried") {
      ctx.font = "700 9px system-ui, -apple-system, sans-serif";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillStyle = "rgba(0,0,0,0.85)";
      ctx.fillText("C4", x, y + 0.5);
    }
    // A loose bomb is easy to lose against the radar, and finding it is the
    // whole problem in that situation — so it gets a halo the others do not.
    if (event.st === "dropped") {
      ctx.globalAlpha = 0.5;
      ctx.beginPath();
      ctx.arc(x, y, 12, 0, Math.PI * 2);
      ctx.lineWidth = 1;
      ctx.strokeStyle = BOMB_LOOSE;
      ctx.stroke();
    }
    ctx.restore();
  }

  /**
   * Draw one grenade effect at map scale.
   *
   * Smokes and molotovs cover real area, so their radius is in world units and
   * scales with the projection rather than being a fixed pixel blob. HE and
   * flash have no duration, so they expand briefly instead of sitting static —
   * otherwise they are a single frame nobody sees.
   */
  private drawUtil(u: UtilEvent, sec: number): void {
    const style = UTIL_STYLE[u.kind] ?? UTIL_FALLBACK;

    const x = this.project.x(u.x);
    const y = this.project.y(u.y);

    // Prefer the spread the collector actually measured; the per-kind number
    // is only a fallback for effects the demo does not report a radius for.
    let radius = (u.r ?? style.radius) * this.scale;
    let alpha = 1;
    if (u.t1 <= u.t) {
      const age = Math.min(1, Math.max(0, (sec - u.t) / BLAST_HOLD_SEC));
      radius *= 0.35 + 0.65 * age;
      alpha = 1 - age;
    }

    const ctx = this.contexts[0];
    ctx.globalAlpha = alpha;
    ctx.beginPath();
    ctx.arc(x, y, radius, 0, Math.PI * 2);
    ctx.fillStyle = style.fill;
    ctx.fill();
    if (style.ring) {
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = style.ring;
      ctx.stroke();
    }
    ctx.globalAlpha = 1;
  }

  /**
   * Stroke a sample range, split so each run lands on the right floor's canvas
   * and airborne stretches read as a bright break.
   */
  private drawPath(track: RoundTrack, from: number, to: number, color: string, alpha: number): void {
    for (const run of runs(track, from, to)) {
      const ctx = this.contexts[run.level] ?? this.contexts[0];
      ctx.globalAlpha = alpha;
      ctx.lineWidth = 2;
      ctx.lineJoin = "round";
      ctx.lineCap = "round";
      ctx.beginPath();
      ctx.moveTo(this.project.x(track.x[run.from]), this.project.y(track.y[run.from]));
      for (let i = run.from + 1; i <= run.to; i++) {
        ctx.lineTo(this.project.x(track.x[i]), this.project.y(track.y[i]));
      }
      ctx.strokeStyle = run.airborne ? AIRBORNE : color;
      ctx.stroke();
      ctx.globalAlpha = 1;
    }
  }

  /** Draw one player marker; returns the floor index it landed on. */
  private drawMarker(
    track: RoundTrack, sec: number, color: string, name: string, hp: number,
  ): number {
    const hz = this.payload.sample_hz;
    const [wx, wy] = posAt(track, sec, hz);
    const x = this.project.x(wx);
    const y = this.project.y(wy);
    const level = Number(flagAt(track.lvl, sec, hz));
    const ctx = this.contexts[level] ?? this.contexts[0];
    const dead = deadAt(track, sec, hz);
    const airborne = flagAt(track.air, sec, hz) === "1";

    if (this.state.cones && !dead) {
      this.drawCone(ctx, x, y, yawAt(track, sec, hz), flagAt(track.sc, sec, hz) === "1", color);
    }

    ctx.beginPath();
    ctx.arc(x, y, 5, 0, Math.PI * 2);
    ctx.lineWidth = 2;
    ctx.strokeStyle = SURFACE; // separates overlapping markers
    ctx.stroke();
    if (dead) {
      ctx.strokeStyle = color;
      ctx.stroke();
    } else {
      ctx.fillStyle = color;
      ctx.fill();
    }

    // CT gets a white ring — identity that survives colour blindness.
    if (track.s === 3) {
      ctx.beginPath();
      ctx.arc(x, y, 7.5, 0, Math.PI * 2);
      ctx.lineWidth = 1.5;
      ctx.strokeStyle = "#fff";
      ctx.stroke();
    }
    if (airborne && !dead) {
      ctx.beginPath();
      ctx.arc(x, y, 2, 0, Math.PI * 2);
      ctx.fillStyle = AIRBORNE;
      ctx.fill();
    }
    if (!dead) this.drawHealth(ctx, x, y, hp);
    if (this.state.labels) this.drawLabel(ctx, x, y, name, color, dead);
    return level;
  }

  /**
   * Name beside the marker, so identity does not depend on remembering the
   * colour mapping. Drawn with a dark halo rather than a filled box: at ten
   * players the boxes tile into a wall, while a stroked outline stays legible
   * over both the pale and dark parts of a radar.
   */
  private drawLabel(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    name: string,
    color: string,
    dead: boolean,
  ): void {
    ctx.font = "600 11px system-ui, -apple-system, sans-serif";
    ctx.textAlign = "center";
    ctx.textBaseline = "bottom";
    ctx.globalAlpha = dead ? 0.45 : 1;

    const ty = y - 9;
    ctx.lineWidth = 3;
    ctx.strokeStyle = "rgba(0,0,0,0.85)";
    ctx.lineJoin = "round";
    ctx.strokeText(name, x, ty);
    ctx.fillStyle = dead ? "#c3c2b7" : color;
    ctx.fillText(name, x, ty);

    ctx.globalAlpha = 1;
    ctx.textAlign = "start";
    ctx.textBaseline = "alphabetic";
  }

  /**
   * World yaw runs counter-clockwise from +X; canvas y grows downward, so the
   * screen angle is its negation. The gradient holds colour most of the way out
   * and fades only near the tip — fading from the origin averages to nothing and
   * the cone stops reading as a direction.
   */
  private drawCone(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    yaw: number,
    scoped: boolean,
    color: string,
  ): void {
    const half = (((scoped ? FOV_SCOPED_DEG : FOV_DEG) / 2) * Math.PI) / 180;
    const reach = scoped ? CONE_SCOPED_PX : CONE_PX;
    const mid = (-yaw * Math.PI) / 180;

    const grad = ctx.createRadialGradient(x, y, 0, x, y, reach);
    grad.addColorStop(0, color);
    grad.addColorStop(0.62, color);
    grad.addColorStop(1, "transparent");

    ctx.beginPath();
    ctx.moveTo(x, y);
    ctx.arc(x, y, reach, mid - half, mid + half);
    ctx.closePath();
    ctx.globalAlpha = 0.42;
    ctx.fillStyle = grad;
    ctx.fill();
    ctx.globalAlpha = 1;

    this.drawFacing(ctx, x, y, mid, reach, color);
  }

  /**
   * Needle down the middle of the cone.
   *
   * The wedge shows the field of view, which at 90 degrees leaves the exact
   * facing ambiguous — two players looking 30 degrees apart have cones that
   * mostly overlap. The needle is the precise direction, and it is drawn over
   * a dark halo so it stays readable against its own fill.
   */
  private drawFacing(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    mid: number,
    reach: number,
    color: string,
  ): void {
    const cos = Math.cos(mid);
    const sin = Math.sin(mid);
    // Start clear of the marker and its 2px surface ring, not at the centre.
    const fromX = x + cos * 7;
    const fromY = y + sin * 7;
    const tipX = x + cos * reach * 0.8;
    const tipY = y + sin * reach * 0.8;

    ctx.lineCap = "round";
    ctx.beginPath();
    ctx.moveTo(fromX, fromY);
    ctx.lineTo(tipX, tipY);
    ctx.lineWidth = 3.5;
    ctx.strokeStyle = "rgba(0,0,0,0.55)";
    ctx.stroke();
    ctx.lineWidth = 1.6;
    ctx.strokeStyle = color;
    ctx.stroke();

    const head = 5.5;
    const spread = 0.45;
    ctx.beginPath();
    ctx.moveTo(tipX, tipY);
    ctx.lineTo(tipX - Math.cos(mid - spread) * head, tipY - Math.sin(mid - spread) * head);
    ctx.lineTo(tipX - Math.cos(mid + spread) * head, tipY - Math.sin(mid + spread) * head);
    ctx.closePath();
    ctx.lineWidth = 2.5;
    ctx.strokeStyle = "rgba(0,0,0,0.55)";
    ctx.stroke();
    ctx.fillStyle = color;
    ctx.fill();
  }
}
