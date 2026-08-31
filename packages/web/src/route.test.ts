import { describe, expect, it } from "vitest";
import { routeFrom, viewerHref } from "./route";

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
