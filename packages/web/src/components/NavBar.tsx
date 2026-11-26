import SteamAuth from "./SteamAuth";
import { PROS } from "../pros";
import { HOME_HREF, PROS_HREF } from "../route";
import { useMe } from "../useMe";

export type Section = "mine" | "pros";

/**
 * The bar across the top of the list pages: your games and the pros' games as
 * two separate places, and who is signed in.
 *
 * Only the list pages carry it. The replay, starters and compare views are
 * full-screen tools that spend every pixel on the map, and they keep their
 * own back link instead.
 *
 * Signed out, the first tab is the sample - there is no "yours" to show - and
 * the Pros tab stays visible so a visitor learns it exists; its page asks
 * them to sign in.
 */
export default function NavBar({ active }: { active: Section }) {
  const auth = useMe();
  const mineLabel = auth.status === "out" || auth.status === "down" ? "Sample" : "Your demos";

  const tab = (section: Section, href: string, label: string) => (
    <a href={href} aria-current={active === section ? "page" : undefined}>{label}</a>
  );

  return (
    <nav className="nav" aria-label="Sections">
      <a className="nav-brand" href={HOME_HREF}>CS2 Demo Analyzer</a>
      <div className="nav-tabs">
        {tab("mine", HOME_HREF, mineLabel)}
        {PROS.length > 0 && tab("pros", PROS_HREF, "Pros")}
      </div>
      <SteamAuth />
    </nav>
  );
}
