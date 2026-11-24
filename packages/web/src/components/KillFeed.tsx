import { GameIcon } from "./icons";
import { MARKS, weaponFile } from "../equipment";
import type { KillEvent } from "../types";

interface Props {
  kills: KillEvent[];
  names: Record<string, string>;
  colors: Record<string, string>;
  teamOf: Record<string, number>;
}

/** Most recent kills first, so the newest is always at the top of the panel. */
export default function KillFeed({ kills, names, colors, teamOf }: Props) {
  const recent = [...kills].reverse().slice(0, 8);
  if (!recent.length) return <div className="hint">No kills yet this round.</div>;

  const who = (id: string | null, fallback: string) =>
    id === null ? <span className="muted">{fallback}</span> : (
      <span className="feed-p" title={names[id] ?? id} style={{ color: colors[id] ?? "var(--ink)" }}>
        {teamOf[id] === 1 ? "▸ " : ""}{names[id] ?? id}
      </span>
    );

  return (
    <ul className="feed">
      {recent.map((k, i) => {
        const file = weaponFile(k.w);
        return (
          <li key={`${k.t}-${k.v}-${i}`} className={k.post === undefined ? "" : "post"}>
            <span className="feed-t">{k.t.toFixed(0)}s</span>
            {who(k.k, "world")}
            <span className="feed-w" title={k.w}>
              {/* The icon replaces the name, as in the game's own feed. A weapon
                  the lookup has not met, and "World" (a fall, the bomb), keep
                  their name, so nothing becomes a blank. */}
              {file ? <GameIcon file={file} height={12} title={k.w} /> : k.w}
              {k.wb === 1 && (
                <span className="mark wb" title="wallbang">
                  <GameIcon file={MARKS.wallbang} height={13} title="wallbang" />
                </span>
              )}
              {k.hs === 1 && (
                <span className="mark hs" title="headshot">
                  <GameIcon file={MARKS.headshot} height={13} title="headshot" />
                </span>
              )}
              {/* The round was already decided. Worth its own mark: it changes
                  nothing about who won, and everything about who kept a rifle. */}
              {k.post !== undefined && (
                <span className="tag post" title={`${k.post.toFixed(1)}s after the round was decided`}>
                  +{k.post.toFixed(1)}s
                </span>
              )}
            </span>
            {who(k.v, "?")}
          </li>
        );
      })}
    </ul>
  );
}
