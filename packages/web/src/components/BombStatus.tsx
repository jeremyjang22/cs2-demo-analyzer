import type { BombEvent } from "../types";

interface Props {
  bomb: BombEvent | null;
  names: Record<string, string>;
}

/** What each state says, and the colour it says it in. */
const STATE: Record<BombEvent["st"], { label: string; className: string }> = {
  carried: { label: "Carried by", className: "carried" },
  dropped: { label: "Dropped", className: "dropped" },
  planted: { label: "Planted", className: "planted" },
  defused: { label: "Defused by", className: "defused" },
  exploded: { label: "Detonated", className: "planted" },
};

/**
 * The bomb in one line, because the marker on the map answers "where" and this
 * answers "who".
 *
 * A dropped C4 is the state worth calling out in words: it has no carrier to
 * name and it is the one a spectator most often loses track of.
 */
export default function BombStatus({ bomb, names }: Props) {
  if (!bomb) {
    return <div className="hint">Not seen yet this round.</div>;
  }

  const { label, className } = STATE[bomb.st];
  const who = bomb.by ? names[bomb.by] ?? bomb.by : null;

  return (
    <div className={`bomb-status ${className}`}>
      <span className="bomb-chip">C4</span>
      <span className="bomb-what">{label}</span>
      {who && <span className="bomb-who">{who}</span>}
      {bomb.site && <span className="bomb-site">site {bomb.site}</span>}
    </div>
  );
}
