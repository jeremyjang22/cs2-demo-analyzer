interface Props {
  playing: boolean;
  speed: number;
  t: number;
  duration: number;
  roundLabel: string;
  onToggle: () => void;
  onNudge: (delta: number) => void;
  onRound: (delta: number) => void;
  onEdge: (which: "start" | "end") => void;
  onSpeed: (factor: number) => void;
}

const SPEEDS = [1, 2, 4, 8];

function clock(sec: number): string {
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${String(s).padStart(2, "0")}`;
}

export default function PlaybackBar({
  playing, speed, t, duration, roundLabel,
  onToggle, onNudge, onRound, onEdge, onSpeed,
}: Props) {
  return (
    <div className="transport">
      <div className="tbtns">
        <button type="button" onClick={() => onEdge("start")} title="Start of match">⏮</button>
        <button type="button" onClick={() => onRound(-1)} title="Previous round">⏪</button>
        <button type="button" onClick={() => onNudge(-5)} title="Back 5 seconds">−5s</button>
        <button type="button" className="primary" onClick={onToggle} title={playing ? "Pause" : "Play"}>
          {playing ? "❚❚" : "▶"}
        </button>
        <button type="button" onClick={() => onNudge(5)} title="Forward 5 seconds">+5s</button>
        <button type="button" onClick={() => onRound(1)} title="Next round">⏩</button>
        <button type="button" onClick={() => onEdge("end")} title="End of match">⏭</button>
      </div>
      <div className="treadout">
        <span className="tclock">{clock(t)}</span>
        <span className="muted"> / {clock(duration)}</span>
        <span className="tround">{roundLabel}</span>
        <span className="tspeed">
          {SPEEDS.map((s) => (
            <button
              key={s}
              type="button"
              aria-pressed={s === speed}
              onClick={() => onSpeed(s)}
            >
              {s}×
            </button>
          ))}
        </span>
      </div>
    </div>
  );
}
