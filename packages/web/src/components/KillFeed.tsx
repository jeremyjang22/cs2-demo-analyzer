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
      <span style={{ color: colors[id] ?? "var(--ink)" }}>
        {teamOf[id] === 1 ? "▸ " : ""}{names[id] ?? id}
      </span>
    );

  return (
    <ul className="feed">
      {recent.map((k, i) => (
        <li key={`${k.t}-${k.v}-${i}`}>
          <span className="feed-t">{k.t.toFixed(0)}s</span>
          {who(k.k, "world")}
          <span className="feed-w">
            {k.w}
            {k.hs === 1 && <span className="tag hs" title="headshot">HS</span>}
            {k.wb === 1 && <span className="tag wb" title="wallbang">WB</span>}
          </span>
          {who(k.v, "?")}
        </li>
      ))}
    </ul>
  );
}
