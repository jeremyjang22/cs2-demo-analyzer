import { describe, expect, it } from "vitest";
import {
  analyticsHref, compareHref, proHref, PROS_HREF, routeFrom, startersHref, viewerHref,
} from "./route";

describe("routeFrom", () => {
  // The home page is what the app shows when no demo is named. Before this
  // existed the viewer opened on a hardcoded default, so an empty URL loaded
  // somebody's Anubis PUG at whoever happened to be first in the roster.
  it("routes an empty URL to the home page", () => {
    expect(routeFrom("")).toEqual({ view: "home" });
    expect(routeFrom("?")).toEqual({ view: "home" });
  });

  it("routes a named demo to the viewer", () => {
    expect(routeFrom("?demo=nukepug")).toEqual({
      view: "viewer", demo: "nukepug", start: 0,
    });
  });

  it("carries a start time through", () => {
    expect(routeFrom("?demo=nukepug&t=973.5")).toEqual({
      view: "viewer", demo: "nukepug", start: 973.5,
    });
  });

  // Every ?demo= and ?t= link shared before the home page existed has to keep
  // opening what it opened. That compatibility is the reason routing is by
  // query parameter at all.
  it("accepts the parameters in either order", () => {
    expect(routeFrom("?t=12&demo=anubispug")).toEqual({
      view: "viewer", demo: "anubispug", start: 12,
    });
  });

  // A bad ?t= should open the demo at the beginning, not refuse to open it.
  it("falls back to the start on a malformed time", () => {
    for (const bad of ["?demo=x&t=abc", "?demo=x&t=", "?demo=x&t=-40", "?demo=x&t=NaN"]) {
      expect(routeFrom(bad)).toEqual({ view: "viewer", demo: "x", start: 0 });
    }
  });

  it("ignores parameters it does not know", () => {
    expect(routeFrom("?utm_source=x&demo=nukepug")).toEqual({
      view: "viewer", demo: "nukepug", start: 0,
    });
  });

  // An empty ?demo= names nothing, so there is nothing to open.
  it("treats an empty demo as no demo", () => {
    expect(routeFrom("?demo=")).toEqual({ view: "home" });
  });

  it("decodes a name that needed escaping", () => {
    expect(routeFrom("?demo=05-11-2026_mirage_44-32-10")).toMatchObject({
      demo: "05-11-2026_mirage_44-32-10",
    });
  });
});

describe("analytics and round-relative links", () => {
  it("routes ?view=analytics to the analytics page", () => {
    expect(routeFrom(analyticsHref("nukepug"))).toEqual({ view: "analytics", demo: "nukepug" });
  });

  // A link that names a real demo should always open that demo.
  it("opens the replay for a view it does not know", () => {
    expect(routeFrom("?demo=nukepug&view=heatmap")).toMatchObject({ view: "viewer" });
  });

  // The analytics page links to "round 7, 41 seconds in" because it does not
  // have the replay's clock. That has to survive the trip through the URL.
  it("round-trips a moment within a round", () => {
    const href = viewerHref("nukepug", undefined, { round: 7, sec: 41.3 });
    expect(routeFrom(href)).toEqual({
      view: "viewer", demo: "nukepug", start: 0, at: { round: 7, sec: 41.3 },
    });
  });

  it("opens the round at its start when the offset is missing or bad", () => {
    expect(routeFrom("?demo=x&round=7")).toMatchObject({ at: { round: 7, sec: 0 } });
    expect(routeFrom("?demo=x&round=7&rt=abc")).toMatchObject({ at: { round: 7, sec: 0 } });
  });

  it("ignores a round that is not a round", () => {
    for (const bad of ["?demo=x&round=0", "?demo=x&round=abc", "?demo=x&round=1.5", "?demo=x&round="]) {
      expect(routeFrom(bad)).toEqual({ view: "viewer", demo: "x", start: 0 });
    }
  });
});

describe("viewerHref", () => {
  it("links to a demo", () => {
    expect(viewerHref("nukepug")).toBe("?demo=nukepug");
  });

  it("includes a time only when there is one to include", () => {
    expect(viewerHref("nukepug", 0)).toBe("?demo=nukepug");
    expect(viewerHref("nukepug", 973.5)).toBe("?demo=nukepug&t=974");
  });

  // Whatever it produces has to parse back to what went in, or a "link to this
  // moment" button quietly produces a link somewhere else.
  it("round-trips through routeFrom", () => {
    const href = viewerHref("05-11-2026_mirage_44-32-10", 1482);
    expect(routeFrom(href)).toEqual({
      view: "viewer", demo: "05-11-2026_mirage_44-32-10", start: 1482,
    });
  });
});

describe("starters route", () => {
  it("round-trips a player and side on one demo", () => {
    const href = startersHref("nukepug", { player: "76561198074762801", side: "CT" });
    expect(routeFrom(href)).toEqual({
      view: "starters", demos: ["nukepug"], player: "76561198074762801", side: "CT",
    });
  });

  // A pro's set: several demos, followed as the pro rather than one account.
  it("round-trips a set of demos and a pro", () => {
    const href = startersHref(["a", "b", "c"], { pro: "m0nesy", side: "T", t: 15 });
    expect(routeFrom(href)).toEqual({
      view: "starters", demos: ["a", "b", "c"], pro: "m0nesy", side: "T", t: 15,
    });
  });

  // Links written before sets existed named one demo with ?demo=.
  it("still reads the single-demo form", () => {
    expect(routeFrom("?demo=x&view=starters")).toEqual({ view: "starters", demos: ["x"] });
  });

  // A bare link, or a side spelled oddly, still opens the page on its defaults.
  it("leaves out what it cannot read", () => {
    expect(routeFrom("?demo=x&view=starters&side=spec")).toEqual({ view: "starters", demos: ["x"] });
    expect(routeFrom("?demo=x&view=starters&side=t")).toEqual({ view: "starters", demos: ["x"], side: "T" });
    expect(routeFrom("?demo=x&view=starters&t=abc")).toEqual({ view: "starters", demos: ["x"] });
    expect(routeFrom("?view=starters")).toEqual({ view: "home" });
  });
});

describe("pros routes", () => {
  it("routes the section and a filtered pro page", () => {
    expect(routeFrom(PROS_HREF)).toEqual({ view: "pros" });
    expect(routeFrom(proHref("m0nesy", "de_mirage", "pug"))).toEqual({
      view: "pro", pro: "m0nesy", map: "de_mirage", kind: "pug",
    });
  });

  it("drops an unknown kind and leaves 'all' implicit", () => {
    expect(routeFrom("?view=pro&pro=x&kind=scrim")).toEqual({ view: "pro", pro: "x" });
    expect(proHref("x", undefined, "all")).toBe("?view=pro&pro=x");
  });

  it("sends a pro page with no pro to the list", () => {
    expect(routeFrom("?view=pro")).toEqual({ view: "pros" });
  });
});

describe("compare route", () => {
  const a = { demo: "m0nesy-mirage", key: "0:5" };
  const b = { demo: "nukepug", key: "0:12" };

  it("round-trips two rounds, an anchor and a negative time", () => {
    expect(routeFrom(compareHref(a, b, "plant", -12.5))).toEqual({
      view: "compare", a, b, align: "plant", t: -12.5,
    });
  });

  // Compare has no ?demo=; it must not fall through to the home page.
  it("opens with nothing chosen", () => {
    expect(routeFrom("?view=compare")).toEqual({ view: "compare" });
  });

  it("drops what it cannot read", () => {
    expect(routeFrom("?view=compare&a=junk&align=sideways&t=x")).toEqual({ view: "compare" });
  });
});

describe("round links into merged payloads", () => {
  // Every game of a merged payload has a round 5; the link has to say which.
  it("carries the game", () => {
    expect(routeFrom(viewerHref("m", undefined, { round: 5, sec: 3, game: 2 }))).toEqual({
      view: "viewer", demo: "m", start: 0, at: { round: 5, sec: 3, game: 2 },
    });
  });

  it("leaves game one implicit, so older links read the same", () => {
    expect(viewerHref("m", undefined, { round: 5, sec: 0, game: 0 })).toBe("?demo=m&round=5");
  });
});
