/**
 * Canvas renderer for player movement.
 *
 * Deliberately not a React component. It owns the canvases, the playback clock,
 * and the draw loop, because a 60 fps animation must not run through React's
 * reconciler. React mounts it once, hands it state, and gets throttled
 * callbacks back for the parts of the UI that display time.
 */

import { makeProjector, radarImage, type Projector } from "./radar";
import { buildTimeline, chapterAt, clampTime, secWithin, type Chapter, type Timeline } from "./timeline";
import { deadAt, flagAt, idxAt, lengthSec, posAt, runs, yawAt } from "./track";
import type { KillEvent, Payload, Player, RoundTrack, UtilEvent } from "./types";

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

export interface ViewState {
  selected: string[];
  mode: "dots" | "trail" | "full";
  cones: boolean;
  util: boolean;
  labels: boolean;
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
    let kills: KillEvent[] = [];

    if (chapter) {
      const sec = secWithin(chapter, this.t);
      kills = (payload.kills[chapter.key] ?? []).filter((k) => k.t <= sec);

      // Utility under the players: it is context, not the subject.
      if (state.util) {
        for (const u of payload.util[chapter.key] ?? []) {
          const until = u.t1 > u.t ? u.t1 : u.t + BLAST_HOLD_SEC;
          if (sec >= u.t && sec <= until) this.drawUtil(u, sec);
        }
      }

      for (const { player, track } of this.tracksFor(chapter)) {
        const color = this.colorOf(player);
        if (state.mode === "full") {
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
        perFloor[this.drawMarker(track, sec, color, player.name)] += 1;
        if (!deadAt(track, sec, hz)) alive.add(player.id);
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
      });
    }
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
    const scale = this.panelPx / this.payload.radar.size / this.payload.radar.scale;

    // Prefer the spread the collector actually measured; the per-kind number
    // is only a fallback for effects the demo does not report a radius for.
    let radius = (u.r ?? style.radius) * scale;
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
  private drawMarker(track: RoundTrack, sec: number, color: string, name: string): number {
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
