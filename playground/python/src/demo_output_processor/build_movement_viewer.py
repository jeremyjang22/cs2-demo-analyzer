"""Generate a standalone HTML movement viewer (legacy).

Superseded by export_movement.py plus packages/web, which keep the data and the
app that draws it separate. Kept working during the migration so there is a
fallback that needs no toolchain; delete once the web app is proven.

    python build_movement_viewer.py --demo anubispug
"""

import argparse
import base64
import json
from pathlib import Path

import movement_payload as mp
from movement_payload import (DEFAULT_HZ, OUT_ROOT, RADAR_DIR, REPO_ROOT, SERIES,
                              build_payload, resolve_demo_dirs)
from radar import RadarMap


def render_html(payload: dict, radar: RadarMap) -> str:
    images = {}
    for name in radar.section_names:
        path = (radar.image_path if name == "default"
                else radar.image_path.with_name(f"{radar.map_name}_{name}.png"))
        b64 = base64.b64encode(path.read_bytes()).decode()
        images[name] = f"data:image/png;base64,{b64}"
    return (TEMPLATE
            .replace("__DATA__", json.dumps(payload, separators=(",", ":")))
            .replace("__SERIES__", json.dumps(SERIES))
            .replace("__IMAGES__", json.dumps(images)))


TEMPLATE = r"""<!doctype html>
<meta charset="utf-8">
<title>Player movement</title>
<style>
  :root {
    --surface:#1a1a19; --plane:#0d0d0d; --ink:#fff; --ink-2:#c3c2b7;
    --muted:#898781; --line:#2c2c2a; --ring:rgba(255,255,255,.10);
    --t:#eda100; --ct:#3987e5;
  }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--plane); color:var(--ink); display:flex; gap:18px;
         padding:18px; align-items:flex-start;
         font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif; }
  #panel { width:300px; flex:none; background:var(--surface); border:1px solid var(--ring);
           border-radius:10px; padding:16px; max-height:calc(100vh - 36px); overflow-y:auto; }
  h1 { font-size:15px; margin:0; }
  .sub { color:var(--muted); font-size:12px; margin:2px 0 14px; }
  fieldset { border:0; padding:0; margin:0 0 14px; }
  legend { font-size:11px; text-transform:uppercase; letter-spacing:.07em;
           color:var(--muted); padding:0; margin-bottom:7px; }
  label.row { display:flex; align-items:center; gap:8px; padding:2px 0; cursor:pointer; }
  label.row.off { opacity:.35; cursor:not-allowed; }
  .swatch { width:11px; height:11px; border-radius:3px; flex:none; border:1px solid var(--ring); }
  .seg { display:flex; gap:4px; flex-wrap:wrap; }
  .seg button, .btn { padding:5px 9px; background:transparent; color:var(--ink-2);
        border:1px solid var(--line); border-radius:6px; cursor:pointer; font-size:12px; }
  .seg button { flex:1; }
  .seg button[aria-pressed=true] { background:#2c2c2a; color:var(--ink); border-color:var(--muted); }
  .btn:hover, .seg button:hover { border-color:var(--muted); }
  input[type=range] { width:100%; accent-color:#3987e5; }

  .score { display:flex; align-items:baseline; gap:8px; font-variant-numeric:tabular-nums; }
  .score b { font-size:26px; font-weight:600; }
  .score .vs { color:var(--muted); font-size:13px; }

  #chips { display:grid; grid-template-columns:repeat(8,1fr); gap:3px; margin-top:6px; }
  #chips button { padding:3px 0; font-size:11px; border-radius:4px; border:1px solid var(--line);
                  background:transparent; color:var(--muted); cursor:pointer;
                  font-variant-numeric:tabular-nums; }
  #chips button[aria-pressed=true] { color:var(--ink); border-color:var(--muted); background:#2c2c2a; }
  #chips button.t[aria-pressed=true] { border-color:var(--t); }
  #chips button.ct[aria-pressed=true] { border-color:var(--ct); }

  .play { display:flex; align-items:center; gap:10px; margin-bottom:6px; }
  .play .clock { font-variant-numeric:tabular-nums; font-size:22px; min-width:62px; }
  #pp { width:38px; height:32px; font-size:14px; }

  table { border-collapse:collapse; width:100%; font-size:12px; }
  th,td { text-align:left; padding:3px 5px 3px 0; color:var(--ink-2); font-weight:400;
          white-space:nowrap; }
  th { color:var(--muted); position:sticky; top:0; background:var(--surface); }
  td.n { text-align:right; font-variant-numeric:tabular-nums; }
  .scroll { max-height:190px; overflow-y:auto; }
  .dot { display:inline-block; width:7px; height:7px; border-radius:50%; }
  canvas { border-radius:10px; display:block; background:var(--surface); }
  .panel-label { display:flex; justify-content:space-between; align-items:baseline;
                 padding:0 4px 6px; font-size:12px; color:var(--ink-2);
                 text-transform:uppercase; letter-spacing:.07em; }
  .panel-label span:last-child { color:var(--muted); text-transform:none;
                                 letter-spacing:0; font-variant-numeric:tabular-nums; }
  .hint { color:var(--muted); font-size:11px; margin-top:5px; }
</style>

<div id="panel">
  <h1 id="title"></h1>
  <div class="sub" id="subtitle"></div>

  <fieldset><legend>Score</legend>
    <div class="score"><b id="sa">0</b><span class="vs">—</span><b id="sb">0</b>
      <span class="vs" id="scoreNote"></span></div>
    <div class="hint">Rosters tracked from round 1, so the count survives the halftime swap.</div>
  </fieldset>

  <fieldset><legend>Colours</legend>
    <div class="seg" id="palette">
      <button data-v="game" aria-pressed="true">Game</button>
      <button data-v="safe" aria-pressed="false">Accessible</button>
    </div>
    <div class="hint">Game = the five minimap colours. Accessible = a validated
      3-colour set for when hues must be told apart reliably.</div>
  </fieldset>

  <fieldset><legend>Players</legend>
    <div class="seg" style="margin-bottom:6px">
      <button class="btn" data-team="2">T side</button>
      <button class="btn" data-team="3">CT side</button>
      <button class="btn" data-team="none">Clear</button>
    </div>
    <div id="players"></div>
    <div class="hint">White ring = CT · bright white trail = airborne</div>
  </fieldset>

  <fieldset><legend>Side</legend>
    <div class="seg" id="side">
      <button data-v="all" aria-pressed="true">Both</button>
      <button data-v="2" aria-pressed="false">T</button>
      <button data-v="3" aria-pressed="false">CT</button>
    </div>
  </fieldset>

  <fieldset><legend>View</legend>
    <div class="seg" id="mode">
      <button data-v="dots" aria-pressed="true">At time</button>
      <button data-v="trail" aria-pressed="false">Trails</button>
      <button data-v="full" aria-pressed="false">All paths</button>
    </div>
    <div class="seg" id="cones" style="margin-top:4px">
      <button data-v="on" aria-pressed="true">View cones</button>
      <button data-v="off" aria-pressed="false">Off</button>
    </div>
    <div class="hint">90&deg; cone where they are looking; narrow and long when scoped.</div>
  </fieldset>

  <fieldset><legend>Playback</legend>
    <div class="play">
      <button class="btn" id="pp">▶</button>
      <div class="clock"><span id="sec">0.0</span>s</div>
      <div class="seg" style="flex:1">
        <button data-v="1" aria-pressed="true">1×</button>
        <button data-v="2" aria-pressed="false">2×</button>
        <button data-v="4" aria-pressed="false">4×</button>
      </div>
    </div>
    <input type="range" id="time" min="0" value="0" step="0.1">
    <div class="hint" id="hint"></div>
  </fieldset>

  <fieldset><legend>Rounds — <span id="roundCount"></span></legend>
    <div class="seg">
      <button class="btn" data-pick="all">All</button>
      <button class="btn" data-pick="none">None</button>
      <button class="btn" data-pick="win">Won</button>
      <button class="btn" data-pick="loss">Lost</button>
    </div>
    <div id="chips"></div>
  </fieldset>

  <fieldset><legend>Round detail</legend>
    <div class="scroll"><table>
      <thead><tr><th>#</th><th>Win</th><th>How</th><th class="n">Len</th><th class="n">Alive</th></tr></thead>
      <tbody id="tally"></tbody></table></div>
  </fieldset>
</div>

<div id="stage"></div>

<script>
const DATA = __DATA__, SERIES = __SERIES__, IMAGES = __IMAGES__;
const SURFACE = "#1a1a19", SIZE = 900, TRAIL_SEC = 8;

// The five CS2 minimap colours. Players recognise them, but they are a game
// palette, not an accessible one: blue/purple are near-identical under
// deuteranopia and orange/yellow are close even with full colour vision. The
// team outline and the name labels carry identity so hue never has to; the
// "Accessible" toggle swaps in a validated 3-colour set when it matters.
const GAME_COLORS = ["#F2C94C", "#B769D6", "#4CC94C", "#4E9BE8", "#F09A3E"];
const AIRBORNE = "#ffffff";   // a state, not an identity — same for every player

// View cone. CS2's default horizontal FOV is 90 degrees; scoped is far
// narrower, and a scoped player is watching a long angle, so that cone is drawn
// narrow and long. Only is_scoped is in the data, not the zoom level, so the
// scoped numbers stand for "zoomed" generally rather than a specific optic.
const FOV_DEG = 90, FOV_SCOPED_DEG = 22;
const CONE_PX = 30, CONE_SCOPED_PX = 62;

let selected = [DATA.players[0].id];
let side = "all", mode = "dots", sec = 0, speed = 1, playing = false;
let palette = "game", cones = true;
let roundSel = new Set(Object.keys(DATA.rounds));

// One canvas per floor. Multi-level maps (Nuke, Vertigo, Train) get them side
// by side rather than a toggle: a quarter of player-rounds on Nuke cross
// between floors, so hiding one would cut trails in half mid-path.
const MULTI = DATA.sections.length > 1;
const PANEL = MULTI ? Math.floor(SIZE / DATA.sections.length) - 10 : SIZE;
const dpr = window.devicePixelRatio || 1;
const CTX = {}, IMGS = {};
const LABELS = { default: "Upper", lower: "Lower" };

const stage = document.getElementById("stage");
stage.style.display = "flex"; stage.style.gap = "10px";
DATA.sections.forEach((name, i) => {
  const wrap = document.createElement("div");
  if (MULTI) {
    const h = document.createElement("div");
    h.className = "panel-label";
    h.innerHTML = `<span>${LABELS[name] || name}</span><span id="cnt${i}"></span>`;
    wrap.appendChild(h);
  }
  const cv = document.createElement("canvas");
  cv.width = PANEL * dpr; cv.height = PANEL * dpr;
  cv.style.width = cv.style.height = PANEL + "px";
  const c = cv.getContext("2d"); c.scale(dpr, dpr);
  CTX[i] = c;
  const img = new Image(); img.src = IMAGES[name]; IMGS[i] = img;
  wrap.appendChild(cv); stage.appendChild(wrap);
});

const k = PANEL / DATA.radar.size;
const tx = wx => ((wx - DATA.radar.pos_x) / DATA.radar.scale) * k;
const ty = wy => ((DATA.radar.pos_y - wy) / DATA.radar.scale) * k;
const colorOf = id => palette === "game"
  ? GAME_COLORS[DATA.players.find(p => p.id === id).col]
  : SERIES[selected.indexOf(id) % SERIES.length];
// Cap only applies to the validated 3-colour set; game colours are per-player
// and already distinct within a side, so all ten can be on screen at once.
const maxSelected = () => palette === "game" ? DATA.players.length : SERIES.length;
const $ = id => document.getElementById(id);

const lastRound = Object.values(DATA.rounds).sort((a,b) => (a.dm-b.dm)||(a.n-b.n)).at(-1);
$("title").textContent = DATA.map;
$("subtitle").textContent = `${DATA.demos.length} demo${DATA.demos.length>1?"s":""} · `
  + `${Object.keys(DATA.rounds).length} rounds · ${DATA.players.length} players`;
$("sa").textContent = lastRound.sa; $("sb").textContent = lastRound.sb;
$("scoreNote").textContent = DATA.demos.length > 1 ? "(last demo)" : "";
$("time").max = DATA.max_sec;

// --- players --------------------------------------------------------------
const list = $("players");
DATA.players.forEach(p => {
  const l = document.createElement("label");
  l.className = "row"; l.dataset.id = p.id;
  l.innerHTML = `<input type="checkbox"><span class="swatch"></span>`
              + `<span>${p.name}</span><span style="margin-left:auto;color:var(--muted);`
              + `font-size:11px">${p.rounds.length}</span>`;
  l.querySelector("input").addEventListener("change", e => {
    if (e.target.checked) {
      if (selected.length >= maxSelected()) { e.target.checked = false; return; }
      selected.push(p.id);
    } else selected = selected.filter(x => x !== p.id);
    syncPlayers(); draw();
  });
  list.appendChild(l);
});
function syncPlayers() {
  [...list.children].forEach(l => {
    const on = selected.includes(l.dataset.id);
    const p = DATA.players.find(x => x.id === l.dataset.id);
    l.querySelector("input").checked = on;
    const sw = l.querySelector(".swatch");
    sw.style.background = on ? colorOf(l.dataset.id) : "transparent";
    // Same outline rule as the map markers, so the legend teaches the encoding.
    sw.style.outline = p.side0 === 3 ? "1.5px solid #fff" : "none";
    sw.style.outlineOffset = "1px";
    l.classList.toggle("off", !on && selected.length >= maxSelected());
  });
}

// --- round chips ----------------------------------------------------------
const chips = $("chips");
Object.entries(DATA.rounds)
  .sort((a,b) => (a[1].dm-b[1].dm)||(a[1].n-b[1].n))
  .forEach(([key, r]) => {
    const b = document.createElement("button");
    b.textContent = r.n; b.dataset.key = key;
    b.className = r.w === 2 ? "t" : "ct";
    b.title = `Round ${r.n} — ${r.w===2?"T":"CT"} win, ${r.why}, ${r.dur}s`;
    b.addEventListener("click", () => {
      roundSel.has(key) ? roundSel.delete(key) : roundSel.add(key);
      syncChips(); draw();
    });
    chips.appendChild(b);
  });
function syncChips() {
  [...chips.children].forEach(b => b.setAttribute("aria-pressed", roundSel.has(b.dataset.key)));
  $("roundCount").textContent = `${roundSel.size} of ${Object.keys(DATA.rounds).length}`;
}
document.querySelectorAll("[data-pick]").forEach(b => b.addEventListener("click", () => {
  const pick = b.dataset.pick;
  roundSel = new Set(Object.entries(DATA.rounds).filter(([, r]) =>
    pick === "all" ? true : pick === "none" ? false : pick === "win" ? r.aw : !r.aw
  ).map(([kk]) => kk));
  syncChips(); draw();
}));

// --- segmented controls & playback ---------------------------------------
function seg(id, set) {
  $(id).addEventListener("click", e => {
    const b = e.target.closest("button"); if (!b) return;
    [...e.currentTarget.children].forEach(x => x.setAttribute("aria-pressed", x === b));
    set(b.dataset.v); draw();
  });
}
seg("side", v => side = v);
seg("mode", v => mode = v);
seg("cones", v => cones = v === "on");
seg("palette", v => {
  palette = v;
  if (selected.length > maxSelected()) selected = selected.slice(0, maxSelected());
  syncPlayers();
});

document.querySelectorAll("[data-team]").forEach(b => b.addEventListener("click", () => {
  const t = b.dataset.team;
  selected = t === "none" ? []
    : DATA.players.filter(p => p.side0 === +t).slice(0, maxSelected()).map(p => p.id);
  syncPlayers(); draw();
}));
$("pp").parentElement.querySelector(".seg").addEventListener("click", e => {
  const b = e.target.closest("button"); if (b) speed = +b.dataset.v;
});

$("time").addEventListener("input", e => { sec = +e.target.value; pause(); paint(); });
$("pp").addEventListener("click", () => playing ? pause() : play());

let raf = null, last = null;
function play() {
  if (mode === "full") return;           // nothing to animate in the overlay view
  playing = true; $("pp").textContent = "❚❚";
  // Deliberately NOT performance.now(): the rAF timestamp is the time the frame
  // began, which can precede a reading taken inside this click handler. Mixing
  // the two produced a negative first delta (measured at -14ms), which drove
  // sec below zero and NaN'd the whole render. Let the first frame set the
  // baseline from the same clock every later frame uses.
  last = null;
  raf = requestAnimationFrame(tick);
}
function pause() {
  playing = false; $("pp").textContent = "▶";
  if (raf) cancelAnimationFrame(raf), raf = null;
}
function tick(now) {
  if (!playing) return;
  if (last === null) last = now;
  const dt = Math.max(0, now - last);    // never run time backwards
  last = now;

  sec += dt / 1000 * speed;
  if (sec > DATA.max_sec) sec = 0;       // loop
  $("time").value = sec;
  paint();
  raf = requestAnimationFrame(tick);
}
function paint() { $("sec").textContent = sec.toFixed(1); draw(); }

// --- drawing --------------------------------------------------------------
const visible = p => p.rounds.filter(r =>
  roundSel.has(r.k) && (side === "all" || r.s === +side));

// Time is in seconds; the arrays are indexed by sample. Interpolate between
// samples so playback stays smooth at any rate. Stored positions are always
// real ticks — this lerp is display only and never written back.
const HZ = DATA.sample_hz;
const idxAt = t => t * HZ;
const lengthSec = r => (r.x.length - 1) / HZ;
const deadAt = (r, t) => r.d >= 0 && idxAt(t) >= r.d;

// Clamped at zero so a sample index can never go negative: r.x[-1] is
// undefined, which turns every downstream coordinate into NaN, and canvas
// gradients throw on non-finite input rather than skipping the draw.
function at(r, t) {
  const s = Math.max(0, idxAt(t)), i = Math.floor(s), f = s - i;
  if (i >= r.x.length - 1) return [r.x.at(-1), r.y.at(-1)];
  return [r.x[i] + (r.x[i+1]-r.x[i]) * f, r.y[i] + (r.y[i+1]-r.y[i]) * f];
}

// Yaw is [-180, 180] and wraps, so a plain lerp between +179 and -179 spins the
// cone 358 degrees the wrong way. Take the shortest signed arc instead — the
// same wrap rule as yaw_delta() on the Python side.
function yawAt(r, t) {
  const s = Math.max(0, idxAt(t)), i = Math.floor(s), f = s - i;
  if (i >= r.yaw.length - 1) return r.yaw.at(-1);
  const a = r.yaw[i], d = ((r.yaw[i+1] - a + 540) % 360) - 180;
  return a + d * f;
}

// World yaw runs counter-clockwise from +X; canvas y grows downward, so the
// screen angle is the negation. Everything else is a plain wedge.
function drawCone(c, X, Y, yaw, scoped, color) {
  const half = (scoped ? FOV_SCOPED_DEG : FOV_DEG) / 2 * Math.PI / 180;
  const reach = scoped ? CONE_SCOPED_PX : CONE_PX;
  const mid = -yaw * Math.PI / 180;

  // Hold the colour most of the way out and fade only near the tip. A gradient
  // that starts fading at the origin averages out to almost nothing and the
  // cone stops reading as a direction.
  const grad = c.createRadialGradient(X, Y, 0, X, Y, reach);
  grad.addColorStop(0, color);
  grad.addColorStop(0.62, color);
  grad.addColorStop(1, "transparent");

  c.beginPath();
  c.moveTo(X, Y);
  c.arc(X, Y, reach, mid - half, mid + half);
  c.closePath();
  c.globalAlpha = 0.42;
  c.fillStyle = grad;
  c.fill();
  c.globalAlpha = 1;
}
// Walk the path in runs where both the floor and the airborne state hold
// steady. The floor picks which canvas the run lands on; airborne picks the
// colour, so a jump reads as a bright break. Runs overlap by one point,
// otherwise the segment spanning a boundary would be dropped entirely — and on
// a multi-level map that boundary segment is exactly the ramp or the vents.
function drawPath(r, from, to, color, alpha) {
  to = Math.min(to, r.x.length - 1);
  if (to <= from) return;

  let start = from;
  while (start < to) {
    const lvl = r.lvl[start], air = r.air[start] === "1";
    let end = start;
    while (end < to && r.lvl[end + 1] === lvl && (r.air[end + 1] === "1") === air) end++;
    if (end === start) end++;                      // always advance

    const c = CTX[+lvl] || CTX[0];
    c.globalAlpha = alpha; c.lineWidth = 2; c.lineJoin = c.lineCap = "round";
    c.beginPath();
    c.moveTo(tx(r.x[start]), ty(r.y[start]));
    for (let i = start + 1; i <= end; i++) c.lineTo(tx(r.x[i]), ty(r.y[i]));
    c.strokeStyle = air ? AIRBORNE : color;
    c.stroke();
    c.globalAlpha = 1;
    start = end;
  }
}

function drawDot(r, t, color, ct) {
  const [wx, wy] = at(r, t), X = tx(wx), Y = ty(wy);
  const i = Math.max(0, Math.floor(idxAt(t)));
  const dead = deadAt(r, t), air = r.air[i] === "1";
  const lvl = +(r.lvl[Math.min(i, r.lvl.length - 1)] || 0);
  const c = CTX[lvl] || CTX[0];

  // Cones go under the marker, and only for the living — a corpse's last view
  // angle is frozen state, not something the player is doing.
  if (cones && !dead) {
    drawCone(c, X, Y, yawAt(r, t), r.sc[Math.min(i, r.sc.length - 1)] === "1", color);
  }

  c.beginPath(); c.arc(X, Y, 5, 0, 7);
  c.lineWidth = 2; c.strokeStyle = SURFACE; c.stroke();   // separates overlaps
  if (dead) { c.strokeStyle = color; c.stroke(); }
  else { c.fillStyle = color; c.fill(); }

  // CT gets a white ring, T gets none — identity that survives colour blindness.
  if (ct) {
    c.beginPath(); c.arc(X, Y, 7.5, 0, 7);
    c.lineWidth = 1.5; c.strokeStyle = "#fff"; c.stroke();
  }
  if (air && !dead) {   // mid-jump marker, same white as the trail
    c.beginPath(); c.arc(X, Y, 2, 0, 7);
    c.fillStyle = AIRBORNE; c.fill();
  }
  return lvl;
}

function draw() {
  DATA.sections.forEach((_, i) => {
    const c = CTX[i];
    c.clearRect(0, 0, PANEL, PANEL);
    c.globalAlpha = .5; c.drawImage(IMGS[i], 0, 0, PANEL, PANEL); c.globalAlpha = 1;
  });

  const rows = [], perLevel = DATA.sections.map(() => 0);
  for (const id of selected) {
    const p = DATA.players.find(x => x.id === id), color = colorOf(id);
    for (const r of visible(p)) {
      const meta = DATA.rounds[r.k];
      const ct = r.s === 3;
      if (mode === "full") { drawPath(r, 0, r.x.length - 1, color, .16); rows.push([r, meta, color, null]); continue; }
      if (sec > lengthSec(r)) continue;          // round already over
      if (mode === "trail")
        drawPath(r, Math.max(0, Math.floor(idxAt(sec - TRAIL_SEC))), Math.floor(idxAt(sec)), color, .45);
      perLevel[drawDot(r, sec, color, ct)]++;
      rows.push([r, meta, color, !deadAt(r, sec)]);
    }
  }
  if (MULTI) DATA.sections.forEach((_, i) => {
    const el = $("cnt" + i);
    if (el) el.textContent = mode === "full" ? "" : `${perLevel[i]} here`;
  });

  rows.sort((a, b) => (a[1].dm - b[1].dm) || (a[1].n - b[1].n));
  $("tally").innerHTML = rows.map(([r, m, color, alive]) =>
    `<tr><td><span class="dot" style="background:${color}"></span> ${m.n}</td>`
    + `<td style="color:${m.w===2?"var(--t)":"var(--ct)"}">${m.w===2?"T":"CT"}</td>`
    + `<td style="color:var(--muted)">${m.why}</td>`
    + `<td class="n">${m.dur}s</td>`
    + `<td class="n">${alive === null ? (r.sv ? "yes" : "no") : (alive ? "●" : "○")}</td></tr>`
  ).join("") || `<tr><td colspan="5" style="color:var(--muted)">nothing at this time</td></tr>`;

  $("hint").textContent = mode === "full"
    ? "Overlay view — playback disabled. Darkest lines are the routes taken every round."
    : `${rows.length} round${rows.length===1?"":"s"} on screen · ● alive, ○ dead`;
}

// Wait for every floor's image before the first paint, otherwise a slow one
// leaves that panel blank until the next redraw.
let pending = DATA.sections.length;
const ready = () => { if (--pending === 0) { syncPlayers(); syncChips(); paint(); } };
DATA.sections.forEach((_, i) => {
  IMGS[i].complete ? ready() : (IMGS[i].onload = ready, IMGS[i].onerror = ready);
});
</script>
"""


def check_demo_dirs(demo_dirs):
    """Fail with something actionable instead of a traceback from read_text().

    Paths here are relative to a directory four levels below the repo root, so
    an off-by-one in the ../ count is the likeliest mistake by a wide margin.
    """
    missing = [d for d in demo_dirs if not (d / "manifest.json").exists()]
    if not missing:
        return

    available = sorted(p.name for p in OUT_ROOT.glob("*") if (p / "manifest.json").exists()) \
        if OUT_ROOT.exists() else []
    lines = ["no manifest.json found in:"]
    lines += [f"    {d}  (resolved: {d.resolve()})" for d in missing]
    if available:
        lines += ["", "demos available in out/:"] + [f"    {n}" for n in available]
        lines += ["", f"try:  python build_movement_viewer.py --demo {available[0]}"]
    raise SystemExit("\n".join(lines))


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--demo", nargs="+", metavar="NAME",
                        help="demo folder name(s) under out/, e.g. --demo anubispug")
    parser.add_argument("--demo-dir", type=Path, nargs="+", metavar="PATH",
                        help="explicit path(s), if the output lives outside out/")
    parser.add_argument("--out", type=Path)
    parser.add_argument("--hz", type=float, default=DEFAULT_HZ,
                        help=f"samples per second (default {DEFAULT_HZ:g}); higher "
                             "hugs map geometry more closely but grows the file")
    args = parser.parse_args()

    demo_dirs = [OUT_ROOT / name for name in args.demo] if args.demo else args.demo_dir
    if not demo_dirs:
        demo_dirs = [OUT_ROOT / "nukepug"]
    check_demo_dirs(demo_dirs)

    first = json.loads((demo_dirs[0] / 'manifest.json').read_text())
    radar = RadarMap(first["map"], RADAR_DIR)

    payload = build_payload(demo_dirs, radar, args.hz)
    positions = sum(len(r["x"]) for p in payload["players"] for r in p["rounds"])
    print(f"{len(payload['demos'])} demo(s) | {len(payload['rounds'])} rounds | "
          f"{len(payload['players'])} players | {positions:,} positions @ {args.hz:g} Hz | "
          f"longest round {payload['max_sec']}s")

    out = args.out or (demo_dirs[0] / "movement_viewer.html")
    out.write_text(render_html(payload, radar), encoding="utf-8")
    print(f"wrote {out}  ({out.stat().st_size/1024:.0f} KB)")


if __name__ == "__main__":
    main()
