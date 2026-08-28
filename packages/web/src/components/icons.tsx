/**
 * Equipment icons, drawn rather than shipped.
 *
 * These are hand-built SVG silhouettes, not Valve's art. Two reasons: the game
 * files are not ours to redistribute, and at the size this panel gives them —
 * about 12 pixels — a faithful render of an MP9 and a faithful render of an
 * MP7 are the same grey smudge anyway.
 *
 * So weapons resolve to their CLASS: rifle, sniper, SMG, shotgun, machine gun,
 * pistol. The silhouette says what kind of gun at a glance, and the name text
 * beside it says which one, which is the division of labour that actually
 * works at this size. Grenades and gear get real per-item icons, because there
 * are only ten of them and their shapes are genuinely distinct.
 *
 * Everything inherits `currentColor`, so a caller sets colour with CSS.
 */

interface IconProps {
  size?: number;
  title?: string;
  className?: string;
}

function Svg({ size = 12, title, className, children }: IconProps & { children: React.ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      className={className}
      // Decorative unless the caller supplies a name — an icon repeated for
      // ten players is noise to a screen reader when the text is right there.
      role={title ? "img" : "presentation"}
      aria-hidden={title ? undefined : true}
    >
      {title && <title>{title}</title>}
      {children}
    </svg>
  );
}

/* ---------------------------------------------------------------- weapons */

/** Assault rifle: long barrel, angled magazine, stock. */
export const RifleIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M1 6h13v2H9l-1 2H6l0-2H1z" fill="currentColor" />
    <path d="M5 10l-1 3h2l1-3" fill="currentColor" opacity=".75" />
    <rect x="12" y="4" width="2" height="2" fill="currentColor" opacity=".6" />
  </Svg>
);

/** Sniper: the longest barrel, and the scope is the whole tell. */
export const SniperIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M0 7h15v1.6H8l-1 2H5.5l0-2H0z" fill="currentColor" />
    <rect x="6" y="3.5" width="6" height="2" rx=".6" fill="currentColor" />
    <path d="M4.5 10.6L3.5 13h1.8l.8-2.4" fill="currentColor" opacity=".75" />
  </Svg>
);

/** SMG: short, stubby, long magazine. */
export const SmgIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M3 6h10v2H8l-1 2H5.5l0-2H3z" fill="currentColor" />
    <rect x="6.5" y="9.5" width="1.8" height="4" rx=".4" fill="currentColor" opacity=".8" />
    <path d="M3 6l-2 .8v1.4L3 8z" fill="currentColor" opacity=".7" />
  </Svg>
);

/** Shotgun: fat twin-tube barrel, pump under it. */
export const ShotgunIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="1" y="5.5" width="13" height="1.6" rx=".5" fill="currentColor" />
    <rect x="1" y="7.4" width="13" height="1.6" rx=".5" fill="currentColor" opacity=".8" />
    <path d="M4 9.2h3v1.4H4z" fill="currentColor" opacity=".65" />
    <path d="M11 9l1 3h1.6l-1-3z" fill="currentColor" opacity=".75" />
  </Svg>
);

/** Machine gun: rifle plus a box magazine you cannot miss. */
export const MachineGunIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M1 5.5h13v1.8H1z" fill="currentColor" />
    <rect x="5" y="7.3" width="5" height="4.5" rx=".6" fill="currentColor" opacity=".85" />
    <path d="M11 7.3l1 3h1.5l-1-3z" fill="currentColor" opacity=".7" />
  </Svg>
);

/** Pistol: short slide, obvious grip. */
export const PistolIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M3 5h10v2.2H8L6.6 9H5.2V7.2H3z" fill="currentColor" />
    <path d="M4.6 9l-1.2 4h2.2L6.8 9z" fill="currentColor" opacity=".8" />
  </Svg>
);

/* ------------------------------------------------------------------- gear */

/** Kevlar: a vest outline. Filled when they also have the helmet. */
export const ArmorIcon = ({ helmet = false, ...p }: IconProps & { helmet?: boolean }) => (
  <Svg {...p}>
    {helmet && <path d="M4.5 3.2a3.5 3.5 0 017 0v.8h-7z" fill="currentColor" />}
    <path
      d="M8 4.6l4 1.2v3.4c0 2.2-1.7 3.9-4 4.8-2.3-.9-4-2.6-4-4.8V5.8z"
      fill={helmet ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinejoin="round"
    />
  </Svg>
);

/** Defuse kit: the wire cutters. */
export const KitIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M3 2.5l5.5 6M13 2.5L7.5 8.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    <circle cx="5" cy="12" r="2.2" stroke="currentColor" strokeWidth="1.4" fill="none" />
    <circle cx="11" cy="12" r="2.2" stroke="currentColor" strokeWidth="1.4" fill="none" />
  </Svg>
);

/** C4: a brick with a blinking light. */
export const BombIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="2" y="4" width="12" height="8" rx="1.2" fill="currentColor" />
    <circle cx="5" cy="8" r="1.3" fill="#1a1a19" />
    <path d="M8 6.6h4M8 9.4h4" stroke="#1a1a19" strokeWidth="1.2" strokeLinecap="round" />
  </Svg>
);

/* --------------------------------------------------------------- grenades */

/**
 * Grenades share a body so they read as one family, and differ in the one
 * detail that identifies them: the fluted casing of an HE, the flat top of a
 * flashbang, the wide can of a smoke, the bottle neck of a molotov.
 */
export const HeIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M9 1.5h3v1.5l-1.5.8" stroke="currentColor" strokeWidth="1.2" fill="none" strokeLinecap="round" />
    <ellipse cx="8" cy="9.5" rx="4" ry="5" fill="currentColor" />
    <path d="M5.2 7h5.6M5.2 10h5.6" stroke="#1a1a19" strokeWidth=".9" opacity=".55" />
  </Svg>
);

export const FlashIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="6.4" y="1.6" width="3.2" height="2.4" rx=".5" fill="currentColor" />
    <path d="M4.6 4.6h6.8l-.7 8.2a1 1 0 01-1 .9H6.3a1 1 0 01-1-.9z" fill="currentColor" />
  </Svg>
);

export const SmokeIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="6.6" y="1.4" width="2.8" height="2" rx=".5" fill="currentColor" />
    <rect x="4" y="3.6" width="8" height="10" rx="2.4" fill="currentColor" />
    <path d="M5.6 6.4h4.8M5.6 9h4.8M5.6 11.4h4.8" stroke="#1a1a19" strokeWidth=".9" opacity=".5" />
  </Svg>
);

export const MolotovIcon = (p: IconProps) => (
  <Svg {...p}>
    <path d="M7 1.4h2v2.2l2 2.6v6a1.4 1.4 0 01-1.4 1.4H6.4A1.4 1.4 0 015 12.2v-6l2-2.6z" fill="currentColor" />
    <path d="M8 .2c1.2 1 .4 1.8 0 2.2-.5-.5-1-1.1 0-2.2z" fill="currentColor" opacity=".8" />
  </Svg>
);

/** Same bottle as the molotov — it is the same object — in the CT tint. */
export const IncendiaryIcon = MolotovIcon;

export const DecoyIcon = (p: IconProps) => (
  <Svg {...p}>
    <rect x="6.6" y="1.4" width="2.8" height="2" rx=".5" fill="currentColor" />
    <rect x="4.4" y="3.6" width="7.2" height="10" rx="2" fill="currentColor" opacity=".85" />
    <path d="M8 6v3.4M8 11.2v.9" stroke="#1a1a19" strokeWidth="1.3" strokeLinecap="round" />
  </Svg>
);

/* ------------------------------------------------------------------- maps */

/**
 * Weapon name to icon. Keyed on the exact strings the collector writes, which
 * are demoinfocs' display names — the twenty-one observed across the reference
 * demos are all here.
 *
 * Anything unrecognised falls back by slot rather than vanishing, the same
 * rule the renderer's utility and damage styles follow: a weapon this table
 * has not met should look like an unfamiliar gun, not like no gun.
 */
const WEAPON_ICONS: Record<string, (p: IconProps) => React.JSX.Element> = {
  // Rifles
  "AK-47": RifleIcon,
  "M4A4": RifleIcon,
  "M4A1": RifleIcon,
  "M4A1-S": RifleIcon,
  "Galil AR": RifleIcon,
  "FAMAS": RifleIcon,
  "SG 553": RifleIcon,
  "AUG": RifleIcon,
  // Snipers
  "AWP": SniperIcon,
  "SSG 08": SniperIcon,
  "SCAR-20": SniperIcon,
  "G3SG1": SniperIcon,
  // SMGs
  "MP9": SmgIcon,
  "MAC-10": SmgIcon,
  "MP7": SmgIcon,
  "MP5-SD": SmgIcon,
  "UMP-45": SmgIcon,
  "P90": SmgIcon,
  "PP-Bizon": SmgIcon,
  // Shotguns and machine guns
  "Nova": ShotgunIcon,
  "XM1014": ShotgunIcon,
  "MAG-7": ShotgunIcon,
  "Sawed-Off": ShotgunIcon,
  "M249": MachineGunIcon,
  "Negev": MachineGunIcon,
  // Pistols
  "Glock-18": PistolIcon,
  "USP-S": PistolIcon,
  "P2000": PistolIcon,
  "P250": PistolIcon,
  "Five-SeveN": PistolIcon,
  "Tec-9": PistolIcon,
  "CZ75 Auto": PistolIcon,
  "Desert Eagle": PistolIcon,
  "R8 Revolver": PistolIcon,
  "Dual Berettas": PistolIcon,
  "Zeus x27": PistolIcon,
};

export function weaponIcon(name: string, slot: "primary" | "secondary") {
  return WEAPON_ICONS[name] ?? (slot === "primary" ? RifleIcon : PistolIcon);
}

/**
 * Grenade code to icon and colour, keyed by the characters the collector packs
 * into the `nades` string. Colours match the circles the map draws for the
 * same grenades, so a pip here and a bloom there agree.
 */
export const NADE_ICONS: Record<string, { Icon: (p: IconProps) => React.JSX.Element; color: string; title: string }> = {
  h: { Icon: HeIcon, color: "#ff6b57", title: "HE grenade" },
  f: { Icon: FlashIcon, color: "#ffe9a3", title: "Flashbang" },
  s: { Icon: SmokeIcon, color: "#d6dee6", title: "Smoke" },
  m: { Icon: MolotovIcon, color: "#ff8a3d", title: "Molotov" },
  i: { Icon: IncendiaryIcon, color: "#ffab60", title: "Incendiary" },
  d: { Icon: DecoyIcon, color: "#aab4c2", title: "Decoy" },
};
