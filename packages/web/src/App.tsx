import { useEffect, useState } from "react";
import Analytics from "./components/Analytics";
import Home from "./components/Home";
import Viewer from "./components/Viewer";
import { routeFrom, type Route } from "./route";

/**
 * The whole router.
 *
 * Three views and no library. A router earns its weight when there are nested
 * routes, guards, or data loading per route; here a couple of query parameters
 * decide between a list, a canvas and a page of tables, and react-router would
 * be more code than it replaces.
 *
 * Links are plain anchors, so a click is a real navigation and the browser
 * handles history, middle-click and "open in new tab" for free. popstate is
 * still handled because the viewer pushes state when it wants a moment to be
 * linkable, and back from there should land on the home page without a reload.
 */
export default function App() {
  const [route, setRoute] = useState<Route>(() => routeFrom(location.search));

  useEffect(() => {
    const onPop = () => setRoute(routeFrom(location.search));
    addEventListener("popstate", onPop);
    return () => removeEventListener("popstate", onPop);
  }, []);

  if (route.view === "home") return <Home />;
  if (route.view === "analytics") return <Analytics key={route.demo} demo={route.demo} />;

  // Keyed by demo so switching demos remounts rather than trying to reconcile
  // a whole new payload, renderer and canvas set into the existing tree.
  return <Viewer key={route.demo} demo={route.demo} start={route.start} at={route.at} />;
}
