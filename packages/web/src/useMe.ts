/**
 * Who is signed in, asked once per page load and shared.
 *
 * The header's sign-in control and the home page's tables both need the
 * answer, and each asking /api/me on its own would be two requests for one
 * fact - and two moments where they could briefly disagree.
 */

import { useEffect, useState } from "react";
import { fetchMe, type Me } from "./api";

export type MeState =
  | { status: "loading" }
  | { status: "out" }
  | { status: "in"; me: Me }
  /** The API could not be reached at all - distinct from being signed out. */
  | { status: "down"; message: string };

let pending: Promise<MeState> | null = null;

function ask(): Promise<MeState> {
  pending ??= fetchMe()
    .then((me): MeState => (me ? { status: "in", me } : { status: "out" }))
    .catch((e: Error): MeState => ({ status: "down", message: e.message }));
  return pending;
}

export function useMe(): MeState {
  const [state, setState] = useState<MeState>({ status: "loading" });
  useEffect(() => {
    let live = true;
    ask().then((s) => live && setState(s));
    return () => { live = false; };
  }, []);
  return state;
}
