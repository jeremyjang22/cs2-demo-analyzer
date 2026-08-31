import { useEffect, useState } from "react";
import { fetchMe, logout, steamProfileHref, LOGIN_HREF, type Me } from "../api";

type State =
  | { status: "loading" }
  | { status: "out" }
  | { status: "in"; me: Me }
  /** The API could not be reached at all — distinct from being signed out. */
  | { status: "down"; message: string };

/**
 * Sign in through Steam, or say who is signed in.
 *
 * Being signed out and being unable to ask are deliberately different states.
 * Collapsing them would show a working login button while the backend is down,
 * which sends the user to Steam and back for nothing.
 */
export default function SteamAuth() {
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    const ac = new AbortController();
    fetchMe(ac.signal)
      .then((me) => setState(me ? { status: "in", me } : { status: "out" }))
      .catch((e: Error) => {
        if (e.name === "AbortError") return;
        setState({ status: "down", message: e.message });
      });
    return () => ac.abort();
  }, []);

  if (state.status === "loading") {
    return <div className="auth muted">…</div>;
  }

  if (state.status === "down") {
    return (
      <div className="auth">
        <span className="auth-down" title={state.message}>Sign-in unavailable</span>
      </div>
    );
  }

  if (state.status === "out") {
    return (
      <div className="auth">
        {/* A link, not a button with fetch(): Steam answers the login route
            with a redirect to its own page, so the browser has to navigate. */}
        <a className="steam-btn" href={LOGIN_HREF}>
          <SteamMark />
          Sign in through Steam
        </a>
      </div>
    );
  }

  const { me } = state;
  return (
    <div className="auth">
      {me.avatar
        ? <img className="auth-avatar" src={me.avatar} alt="" width={32} height={32} />
        : <span className="auth-avatar auth-avatar-blank"><SteamMark /></span>}
      <span className="auth-who">
        <a href={steamProfileHref(me.steamid)} target="_blank" rel="noreferrer noopener">
          {me.name}
        </a>
        <button
          type="button"
          className="auth-out"
          onClick={async () => {
            await logout();
            // Reload rather than just clearing state: anything else on the
            // page that was rendered for a signed-in user goes with it.
            location.reload();
          }}
        >
          Sign out
        </button>
      </span>
    </div>
  );
}

/** The Steam logo, simplified to a single path at button size. */
function SteamMark() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M11.98 2a10 10 0 00-9.96 9.02l5.36 2.22a2.8 2.8 0 011.6-.5l2.39-3.46v-.05a3.78 3.78 0 113.78 3.78h-.09l-3.4 2.43c0 .05.01.1.01.14a2.84 2.84 0 01-5.63.5L2.2 14.5A10 10 0 1011.98 2zm-3.1 15.17l-1.22-.51a2.13 2.13 0 001.1 1.04 2.13 2.13 0 002.78-1.14 2.11 2.11 0 00-1.15-2.77 2.12 2.12 0 00-1.53-.03l1.27.53a1.57 1.57 0 11-1.21 2.89zm9.4-8.05a2.52 2.52 0 10-5.03 0 2.52 2.52 0 005.03 0zm-4.4-.01a1.89 1.89 0 113.78.01 1.89 1.89 0 01-3.78-.01z" />
    </svg>
  );
}
