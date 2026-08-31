import { useEffect, useState } from "react";
import Home from "./components/Home";
import Viewer from "./components/Viewer";
import { routeFrom, type Route } from "./route";

/**
 * The whole router.
 *
 * Two views and no library. A router earns its weight when there are nested
 * routes, guards, or data loading per route; here there is one query parameter
 * deciding between a list and a canvas, and react-router would be more code
 * than it replaces.
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

  // Keyed by demo so switching demos remounts rather than trying to reconcile
  // a whole new payload, renderer and canvas set into the existing tree.
  return <Viewer key={route.demo} demo={route.demo} start={route.start} />;
}
