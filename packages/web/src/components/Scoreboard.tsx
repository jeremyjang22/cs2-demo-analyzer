import {
  ArmorIcon, BombIcon, KitIcon, NADE_ICONS, weaponIcon,
} from "./icons";
import type { Loadout, Payload, Player } from "../types";

interface Props {
  payload: Payload;
  colors: Record<string, string>;
  selected: string[];
  alive: Set<string>;
  /** What each player is carrying right now. Empty in "Full round" mode. */
  equip: Map<string, Loadout>;
  onToggle: (id: string) => void;
  onToggleTeam: (teamId: number) => void;
}

/** A weapon name short enough for a 300px panel. */
function short(name: string): string {
  return name
    .replace("Desert Eagle", "Deagle")
    .replace("Dual Berettas", "Duals")
    .replace(" Grenade", "");
}

/**
 * Health as a number and a bar.
 *
 * Both, not one: the bar is what you read at a glance across ten rows, and the
 * number is what you need when the question is whether one more bullet does
 * it. The colour changes only at 35 — the single threshold that changes a
 * decision — rather than sliding continuously, which would make every value
 * look slightly different and no value look urgent.
 */
function Health({ hp }: { hp: number }) {
  const state = hp <= 0 ? "dead" : hp <= 35 ? "low" : hp < 100 ? "hurt" : "full";
  return (
    <span className={`hp ${state}`} title={`${hp} health`}>
      <span className="hp-bar">
        <span className="hp-fill" style={{ width: `${Math.max(0, Math.min(100, hp))}%` }} />
      </span>
      <span className="hp-n">{hp}</span>
    </span>
  );
}

function Equipment({ loadout }: { loadout: Loadout }) {
  const PrimaryIcon = loadout.primary ? weaponIcon(loadout.primary, "primary") : null;
  const SecondaryIcon = loadout.secondary ? weaponIcon(loadout.secondary, "secondary") : null;

  return (
    <div className="kit">
      <span
        className={`armor ${loadout.armor > 0 ? "on" : ""}`}
        title={
          loadout.armor > 0
            ? `${loadout.armor} armour${loadout.helmet ? " + helmet" : ""}`
            : "No armour"
        }
      >
        <ArmorIcon helmet={loadout.helmet} size={11} />
        {loadout.armor > 0 && <span className="armor-n">{loadout.armor}</span>}
      </span>

      {loadout.kit && (
        <span className="tag-kit" title="Defuse kit"><KitIcon size={11} /></span>
      )}
      {loadout.bomb && (
        <span className="tag-c4" title="Carrying the C4"><BombIcon size={11} /></span>
      )}

      <span className="guns">
        {PrimaryIcon
          ? <span className="gun" title={loadout.primary}>
              <PrimaryIcon size={13} />{short(loadout.primary)}
            </span>
          : <span className="muted">—</span>}
        {SecondaryIcon && (
          <span className="gun sidearm" title={loadout.secondary}>
            <SecondaryIcon size={12} />{short(loadout.secondary)}
          </span>
        )}
      </span>

      <span className="nades">
        {[...loadout.nades].map((code, i) => {
          const nade = NADE_ICONS[code];
          if (!nade) return <span key={`${code}-${i}`} className="nade muted">?</span>;
          const { Icon, color, title } = nade;
          return (
            <span key={`${code}-${i}`} className="nade" style={{ color }} title={title}>
              <Icon size={11} />
            </span>
          );
        })}
      </span>
    </div>
  );
}

/**
 * Both rosters with K/D/A, grouped by team rather than by side so a player
 * stays in the same block across the halftime swap.
 *
 * Doubles as the player filter: a row is a toggle, and dimmed rows are hidden
 * from the map. Health, money and equipment all come from the current moment,
 * so they update as playback runs.
 */
export default function Scoreboard({
  payload, colors, selected, alive, equip, onToggle, onToggleTeam,
}: Props) {
  const byId = new Map(payload.players.map((p) => [p.id, p]));

  const row = (player: Player) => {
    const stats = payload.stats[player.id] ?? { k: 0, d: 0, a: 0 };
    const on = selected.includes(player.id);
    const isAlive = alive.has(player.id);
    const loadout = equip.get(player.id);
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
          {loadout && isAlive && <Health hp={loadout.hp} />}
          {loadout && (
            <span className="cash" title="Cash in hand">${loadout.money}</span>
          )}
          {/* Equipment on a dead player is what they died holding, which is
              rarely the question being asked — so it goes with them. */}
          {loadout && isAlive && <Equipment loadout={loadout} />}
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
