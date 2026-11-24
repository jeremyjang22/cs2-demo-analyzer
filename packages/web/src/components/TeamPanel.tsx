import { GameIcon } from "./icons";
import { GEAR, NADES, weaponFile } from "../equipment";
import { byScoreboard, type LiveStat } from "../stats";
import type { Loadout, Payload, Player, Team } from "../types";

interface Props {
  team: Team;
  /** Side this round: 2 = T, 3 = CT. Undefined before the first round loads. */
  side?: number;
  payload: Payload;
  colors: Record<string, string>;
  selected: string[];
  alive: Set<string>;
  /** What each player is carrying right now. Empty in "Full round" mode. */
  equip: Map<string, Loadout>;
  /** K/D/A and ADR as of the current moment, not for the whole match. */
  stats: Map<string, LiveStat>;
  /**
   * Mirror the layout for the team on the right of the map, so both rosters
   * read outward from the centre the way a broadcast lays them out.
   */
  mirror?: boolean;
  onToggle: (id: string) => void;
  onToggleTeam: (teamId: number) => void;
}

/** A weapon name short enough for a 240px column. */
function short(name: string): string {
  return name
    .replace("Desert Eagle", "Deagle")
    .replace("Dual Berettas", "Duals")
    .replace(" Grenade", "");
}

/**
 * Health as a bar and a number.
 *
 * Both, because they answer different questions: the bar is what you read
 * across five players at a glance, and the number is what you need when the
 * question is whether one more bullet does it. The colour changes only at 35 —
 * the single threshold that changes a decision — rather than sliding
 * continuously, which would make every value look slightly different and no
 * value look urgent.
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
  // A weapon the lookup has not met still shows its name; it only loses the
  // picture, which is the failure that stays readable.
  const primaryFile = loadout.primary ? weaponFile(loadout.primary) : undefined;
  const secondaryFile = loadout.secondary ? weaponFile(loadout.secondary) : undefined;

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
        <GameIcon file={loadout.helmet ? GEAR.armorHelmet : GEAR.armor} height={11} />
      </span>
      {loadout.kit && <span className="tag-kit" title="Defuse kit"><GameIcon file={GEAR.kit} height={11} /></span>}
      {loadout.bomb && <span className="tag-c4" title="Carrying the C4"><GameIcon file={GEAR.bomb} height={11} /></span>}

      <span className="guns">
        {loadout.primary
          ? <span className="gun" title={loadout.primary}>
              {primaryFile && <GameIcon file={primaryFile} height={11} />}
              {short(loadout.primary)}
            </span>
          : <span className="muted">—</span>}
        {loadout.secondary && (
          <span className="gun sidearm" title={loadout.secondary}>
            {secondaryFile
              ? <GameIcon file={secondaryFile} height={11} />
              : short(loadout.secondary)}
          </span>
        )}
      </span>

      <span className="nades">
        {[...loadout.nades].map((code, i) => {
          const nade = NADES[code];
          if (!nade) return <span key={`${code}-${i}`} className="nade muted">?</span>;
          return (
            <span key={`${code}-${i}`} className="nade" style={{ color: nade.color }} title={nade.title}>
              <GameIcon file={nade.file} height={12} />
            </span>
          );
        })}
      </span>
    </div>
  );
}

/**
 * One team's roster, laid out down one side of the map.
 *
 * Broadcast puts the two teams either side of the minimap because it makes
 * "who is left" answerable without reading — five rows one side, three the
 * other, and you know the state of the round. A single stacked list loses
 * that, which is why this is a component per team rather than one table of
 * ten players.
 *
 * A row is still the player filter it always was: clicking hides them on the
 * map.
 */
export default function TeamPanel({
  team, side, payload, colors, selected, alive, equip, stats, mirror = false,
  onToggle, onToggleTeam,
}: Props) {
  // The map colours by side and these rows colour by player, so the header
  // is where the two meet: it says which side this roster is on the map.
  const sideName = side === 2 ? "T" : side === 3 ? "CT" : null;
  const byId = new Map(payload.players.map((p) => [p.id, p]));
  const nameOf = (id: string) => byId.get(id)?.name ?? id;
  // Re-sorted every frame, so the board reorders itself as the match plays.
  const members = byScoreboard(team.players, stats, nameOf)
    .map((id) => byId.get(id))
    .filter((p): p is Player => !!p);

  const living = members.filter((p) => alive.has(p.id)).length;

  return (
    <section className={`team ${mirror ? "mirror" : ""} ${sideName ? `side-${sideName.toLowerCase()}` : ""}`}>
      <header className="team-head">
        {sideName && (
          <span className="side-badge" title={sideName === "T" ? "Terrorists" : "Counter-Terrorists"}>
            {sideName}
          </span>
        )}
        <button type="button" className="team-btn" onClick={() => onToggleTeam(team.id)}>
          {team.name}
        </button>
        {/* The number that matters most in a live round, so it gets its own
            slot rather than being counted off the rows. */}
        <span className="team-alive" title={`${living} alive`}>{living}</span>
      </header>

      <ul className="roster">
        {members.map((player) => {
          const stat = stats.get(player.id);
          const on = selected.includes(player.id);
          const isAlive = alive.has(player.id);
          const loadout = equip.get(player.id);

          return (
            <li
              key={player.id}
              className={`pcard ${on ? "" : "hidden-row"} ${isAlive ? "" : "dead-row"}`}
              onClick={() => onToggle(player.id)}
              title={on ? "Hide on map" : "Show on map"}
            >
              <div className="pcard-top">
                <span
                  className="pip"
                  style={{ background: colors[player.id], opacity: isAlive ? 1 : 0.3 }}
                />
                <span className="pname">{player.name}</span>
                <span className="pkd" title="kills / deaths / assists">
                  {stat?.kills ?? 0}<i>/</i>{stat?.deaths ?? 0}<i>/</i>{stat?.assists ?? 0}
                </span>
                <span className="padr" title="average damage per round so far">
                  {Math.round(stat?.adr ?? 0)}
                </span>
              </div>

              {loadout && isAlive && (
                <>
                  <div className="pcard-mid">
                    <Health hp={loadout.hp} />
                    <span className="cash" title="Cash in hand">${loadout.money}</span>
                  </div>
                  {/* Equipment on a dead player is what they died holding,
                      which is rarely the question being asked. */}
                  <Equipment loadout={loadout} />
                </>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
