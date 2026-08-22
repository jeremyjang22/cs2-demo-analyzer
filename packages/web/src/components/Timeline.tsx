import type { Chapter } from "../timeline";

interface Props {
  chapters: Chapter[];
  duration: number;
  t: number;
  current: Chapter | null;
  onSeek: (t: number) => void;
}

/**
 * Match scrubber with one chapter block per round, tinted by who won.
 *
 * Blocks are laid out by real elapsed time rather than evenly, so the gaps
 * between them are freeze time and post-round — the shape of the bar matches
 * the shape of the match.
 */
export default function Timeline({ chapters, duration, t, current, onSeek }: Props) {
  const pct = (v: number) => (duration ? (v / duration) * 100 : 0);

  return (
    <div className="timeline">
      <div
        className="tl-track"
        onPointerDown={(e) => {
          const box = e.currentTarget.getBoundingClientRect();
          onSeek(((e.clientX - box.left) / box.width) * duration);
        }}
      >
        {chapters.map((c) => (
          <button
            key={c.key}
            type="button"
            className={`tl-chapter ${c.meta.w === 2 ? "t" : "ct"}${c === current ? " on" : ""}`}
            style={{ left: `${pct(c.start)}%`, width: `${pct(c.end - c.start)}%` }}
            title={`Round ${c.meta.n} — ${c.meta.w === 2 ? "T" : "CT"} win, ${c.meta.why}`}
            onPointerDown={(e) => { e.stopPropagation(); onSeek(c.start); }}
          >
            <span>{c.meta.n}</span>
          </button>
        ))}
        <div className="tl-head" style={{ left: `${pct(t)}%` }} />
      </div>
    </div>
  );
}
