import type { Payload, Player } from "../types";

interface Props {
  payload: Payload;
  colors: Record<string, string>;
  selected: string[];
  alive: Set<string>;
  onToggle: (id: string) => void;
  onToggleTeam: (teamId: number) => void;
}

/**
 * Both rosters with K/D/A, grouped by team rather than by side so a player
 * stays in the same block across the halftime swap.
 *
 * Doubles as the player filter: a row is a toggle, and dimmed rows are hidden
 * from the map. Alive/dead comes from the current moment, so it updates as
 * playback runs.
 */
export default function Scoreboard({
  payload, colors, selected, alive, onToggle, onToggleTeam,
}: Props) {
  const byId = new Map(payload.players.map((p) => [p.id, p]));

  const row = (player: Player) => {
    const stats = payload.stats[player.id] ?? { k: 0, d: 0, a: 0 };
    const on = selected.includes(player.id);
    const isAlive = alive.has(player.id);
    return (
      <tr
        key={player.id}
        className={`${on ? "" : "hidden-row"} ${isAlive ? "" : "dead-row"}`}
        onClick={() => onToggle(player.id)}
        title={on ? "Hide on map" : "Show on map"}
      >
        <td className="who">
          <span className="pip" style={{ background: colors[player.id], opacity: isAlive ? 1 : 0.3 }} />
          <span className="pname">{player.name}</span>
        </td>
        <td className="n">{stats.k}</td>
        <td className="n">{stats.d}</td>
        <td className="n">{stats.a}</td>
      </tr>
    );
  };

  return (
    <>
      {payload.teams.map((team) => (
        <table key={team.id} className="board">
          <thead>
            <tr>
              <th>
                <button type="button" className="team-btn" onClick={() => onToggleTeam(team.id)}>
                  {team.name}
                </button>
              </th>
              <th className="n">K</th><th className="n">D</th><th className="n">A</th>
            </tr>
          </thead>
          <tbody>
            {team.players.map((id) => byId.get(id)).filter((p): p is Player => !!p).map(row)}
          </tbody>
        </table>
      ))}
    </>
  );
}
