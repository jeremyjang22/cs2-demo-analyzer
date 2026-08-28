/**
 * The edge in front of everything: one origin serving three things.
 *
 *   /api/*   →  proxied to the Go API on Northflank
 *   /data/*  →  demo payloads out of R2
 *   /*       →  the built SPA, from Workers static assets
 *
 * The proxy is not a convenience. The API sets its session cookie with
 * SameSite=Lax, and Steam login finishes with a cross-site redirect from
 * steamcommunity.com back to us — on a Lax cookie a browser sends that only
 * when the request lands on the same site that set it. Point the browser
 * straight at the Northflank hostname and login silently fails to stick, with
 * nothing in any log to say why. Behind this Worker the cookie is first-party
 * and the whole class of problem disappears, CORS included.
 */

interface Env {
  /** Static assets binding, populated by wrangler from `dist/`. */
  ASSETS: Fetcher;
  /** Demo payloads. Optional so a deploy works before the bucket has anything. */
  DEMOS?: R2Bucket;
  /** Origin of the API on Northflank, e.g. https://api--x.code.run */
  API_ORIGIN: string;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/api" || url.pathname.startsWith("/api/")) {
      return proxyToApi(request, url, env);
    }
    if (url.pathname.startsWith("/data/")) {
      return serveDemo(request, url, env);
    }
    return env.ASSETS.fetch(request);
  },
} satisfies ExportedHandler<Env>;

/**
 * Forward a request to the API, minus the /api prefix.
 *
 * `redirect: "manual"` is load-bearing twice over. Both the Steam login kickoff
 * and the post-callback bounce answer with a 302 the BROWSER has to follow —
 * one to steamcommunity.com, one back to the app. Left on the default the
 * Worker would chase them itself and hand back Steam's HTML as if it were our
 * response.
 */
async function proxyToApi(request: Request, url: URL, env: Env): Promise<Response> {
  if (!env.API_ORIGIN) {
    return json({ error: "API_ORIGIN is not configured on this Worker" }, 503);
  }

  const target = new URL(env.API_ORIGIN);
  // "/api/me" → "/me"; bare "/api" → "/".
  target.pathname = url.pathname.slice("/api".length) || "/";
  target.search = url.search;

  const headers = new Headers(request.headers);
  // The API is behind this Worker, so its own view of Host is meaningless.
  // Hand it the public host instead: anything it builds a URL from — and the
  // Steam realm check in particular — has to agree with what the browser used.
  headers.set("Host", target.host);
  headers.set("X-Forwarded-Host", url.host);
  headers.set("X-Forwarded-Proto", url.protocol.replace(":", ""));

  const upstream = new Request(target, {
    method: request.method,
    headers,
    body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
    redirect: "manual",
  });

  try {
    return await fetch(upstream);
  } catch (err) {
    // Northflank scales to zero; a cold start that outruns the edge timeout
    // should read as "try again", not as a broken deploy.
    return json({ error: "api unreachable", detail: String(err) }, 502);
  }
}

/**
 * Serve a demo payload from R2.
 *
 * The key is the request path with its leading slash removed, so an object
 * stored at `data/nukepug/movement.json` answers `/data/nukepug/movement.json`
 * — the same URL the app already fetches in local dev, where Vite serves it
 * off disk from `public/`. Nothing in the app has to know which it is talking
 * to.
 */
async function serveDemo(request: Request, url: URL, env: Env): Promise<Response> {
  if (!env.DEMOS) {
    return json({ error: "no demo bucket bound to this Worker" }, 503);
  }

  const key = decodeURIComponent(url.pathname.slice(1));
  const object = await env.DEMOS.get(key, {
    onlyIf: request.headers,
    range: request.headers,
  });

  if (object === null) {
    return json({ error: `no such demo payload: ${key}` }, 404);
  }

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set("etag", object.httpEtag);
  // A payload is rewritten only when its demo is re-exported, and the etag
  // catches that. An hour of browser cache plus revalidation keeps a re-export
  // from being invisible while still not refetching megabytes on every load.
  headers.set("cache-control", "public, max-age=3600, must-revalidate");
  if (!headers.has("content-type")) {
    headers.set("content-type", "application/json; charset=utf-8");
  }

  // A body is absent when the conditional request matched (304) or on HEAD.
  const body = "body" in object ? object.body : null;
  if (body === null) {
    return new Response(null, { status: 304, headers });
  }
  return new Response(body, { headers });
}

function json(payload: unknown, status: number): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "content-type": "application/json; charset=utf-8" },
  });
}
