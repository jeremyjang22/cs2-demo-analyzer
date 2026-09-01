#!/usr/bin/env bash
# Run the whole site on localhost: the Go API and the web app, wired together.
#
#   ./scripts/dev.sh              api on :8080, web on :5173
#   ./scripts/dev.sh --web-only   just the web app, no database needed
#   ./scripts/dev.sh --api-only   just the API
#
# The web app talks to /api on its own origin and vite proxies that to the API,
# stripping the prefix — exactly what the Cloudflare Worker does in production.
# That is the point of running both together: the same relative URLs, the same
# first-party cookie, the same Steam redirect path. A local setup that reached
# the API on a second origin would work right up until it was deployed.
#
# Ctrl-C stops both.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

API_PORT="${API_PORT:-8080}"
# Not configurable, and deliberately so: PUBLIC_URL and WEB_ORIGIN below are
# built from it, and Steam checks the return URL against the realm. A vite that
# quietly moved to 5174 because 5173 was busy would hand Steam an address that
# no longer matches, and login would fail with nothing useful said about why.
# --strict-port below turns that drift into an error instead.
WEB_PORT=5173

want_api=1
want_web=1
for arg in "$@"; do
  case "$arg" in
    --web-only) want_api=0 ;;
    --api-only) want_web=0 ;;
    -h|--help) sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

api_pid=""
web_pid=""

# Stop a process and everything it spawned.
#
# Walked with ps rather than handed to taskkill: under MSYS the pid bash knows
# is not the pid Windows knows - `ps` lists both, and taskkill given the MSYS
# one silently fails, after which waiting on the still-live child hangs the
# script forever. Recursing over the PID/PPID columns and using MSYS `kill`
# keeps every pid in the same namespace, and works unchanged off Windows.
#
# It has to be a tree: the web server is bash -> npx -> node, so killing only
# the pid we hold leaves the server running and the port taken, which is the
# entire failure this exists to prevent.
kill_tree() {
  local pid="$1" child
  for child in $(ps 2>/dev/null | awk -v p="$pid" '$2 == p { print $1 }'); do
    kill_tree "$child"
  done
  kill -TERM "$pid" 2>/dev/null || true
}

stop() {
  local pid="$1" label="$2" i
  [[ -z "$pid" ]] && return 0
  # `return 0`, not a bare `return`: a bare one returns the status of the last
  # command, so this guard handed back the failure of `kill -0` on an
  # already-dead process. Under `set -e` that aborted cleanup here, and the
  # api survived every shutdown with the port still held.
  kill -0 "$pid" 2>/dev/null || return 0
  echo "stopping $label"
  kill_tree "$pid"
  # Bounded, never `wait`: if something declines to die we say so and move on
  # rather than hanging on the way out.
  for i in 1 2 3 4 5 6 7 8 9 10; do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep 0.3
  done
  kill -9 "$pid" 2>/dev/null || true
}

cleanup() {
  # Shutdown runs to the end even if a step fails. With `set -e` still on, one
  # non-zero status part way through would leave whatever comes after it
  # running - which for a cleanup handler defeats the point of having one.
  set +e
  echo
  stop "$web_pid" "the web server"
  stop "$api_pid" "the api"
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------- the api
if (( want_api )); then
  if [[ ! -f packages/api/.env ]]; then
    echo "packages/api/.env not found." >&2
    echo "Copy packages/api/.env.example and fill in DATABASE_URL, or run with --web-only." >&2
    exit 1
  fi

  # Parsed, not sourced. `source .env` executes the file as shell, and a Neon
  # connection string contains "&" — so the line
  #
  #     DATABASE_URL=postgresql://...?sslmode=require&channel_binding=require
  #
  # is read as an assignment BACKGROUNDED by the &, which runs it in a subshell
  # and leaves the variable unset in this one. It fails as "DATABASE_URL is
  # empty", pointing at the file rather than at the parsing. Reading the value
  # as a literal string avoids that and every other metacharacter with it.
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"                     # the file is CRLF
    case "$line" in ''|'#'*) continue ;; esac
    [[ "$line" != *=* ]] && continue
    key="${line%%=*}"
    value="${line#*=}"
    key="${key// /}"
    # Strip one layer of surrounding quotes if a value has them, so a
    # quoted connection string does not carry its quotes into pgx.
    sq="'"
    case "$value" in
      '"'*'"') value="${value:1:${#value}-2}" ;;
      "$sq"*"$sq") value="${value:1:${#value}-2}" ;;
    esac
    export "$key=$value"
  done < packages/api/.env

  if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "DATABASE_URL is empty in packages/api/.env" >&2
    exit 1
  fi

  # The api refuses to boot without these three, and none of them belong in
  # .env: they describe where this instance is reachable, which is a property
  # of how it is being run, not of the project.
  #
  # PUBLIC_URL ends in /api because that is the address the BROWSER uses —
  # vite's proxy strips the prefix before the api sees it. Steam sends the user
  # back there after login, so it has to be the public address, not :8080.
  export PUBLIC_URL="${PUBLIC_URL:-http://localhost:$WEB_PORT/api}"
  export WEB_ORIGIN="${WEB_ORIGIN:-http://localhost:$WEB_PORT}"
  export PORT="$API_PORT"

  # Regenerated per run unless one is supplied, which means restarting the
  # script signs you out. That is the right default for a dev loop: a key
  # committed to a file is a key that eventually ships.
  if [[ -z "${SESSION_KEY:-}" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      SESSION_KEY="$(openssl rand -hex 32)"
    else
      SESSION_KEY="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
    export SESSION_KEY
  fi

  # Built rather than `go run`: go run spawns the compiled binary as a child,
  # so killing it on Ctrl-C can leave the actual server holding the port. A
  # binary we started ourselves is a pid we can actually stop.
  bin="$ROOT/.dev/api"
  [[ "$OSTYPE" == msys* || "$OSTYPE" == cygwin* ]] && bin="$bin.exe"
  mkdir -p "$ROOT/.dev"

  echo "building the api"
  (cd packages && go build -o "$bin" ./api/cmd/api)

  echo "starting the api on :$API_PORT"
  "$bin" &
  api_pid=$!

  # Wait for it to answer before starting the web server, so the first page
  # load does not race a database connection that is still opening.
  for _ in $(seq 1 40); do
    if curl -fsS --max-time 2 "http://localhost:$API_PORT/health" >/dev/null 2>&1; then
      break
    fi
    if ! kill -0 "$api_pid" 2>/dev/null; then
      echo "the api exited during startup — see the error above" >&2
      exit 1
    fi
    sleep 0.5
  done

  health="$(curl -fsS --max-time 2 "http://localhost:$API_PORT/health" 2>/dev/null || true)"
  if [[ -z "$health" ]]; then
    echo "the api never answered /health on :$API_PORT" >&2
    exit 1
  fi
  echo "api ready: $health"
fi

# ---------------------------------------------------------------- the web app
if (( want_web )); then
  if [[ ! -d packages/web/node_modules ]]; then
    echo "installing web dependencies"
    (cd packages/web && npm install)
  fi

  echo
  echo "  web    http://localhost:$WEB_PORT"
  if (( want_api )); then
    echo "  api    http://localhost:$API_PORT   (proxied at /api)"
    echo
    echo "  Steam login works here: Steam redirects the browser, and the"
    echo "  verification call goes from this machine out to Steam, so nothing"
    echo "  needs to reach localhost from outside."
  else
    echo "  api    not running — sign-in will show as unavailable"
  fi
  echo

  # Deliberately NOT `exec`. exec replaces this shell with vite, and a
  # replaced shell takes its EXIT trap with it — so Ctrl-C would stop vite and
  # leave the api it started running, holding :8080 until someone hunted down
  # the pid. Staying in the foreground costs one process and means cleanup
  # actually happens.
  #
  # --strict-port so a busy 5173 is an error rather than a silent move to 5174,
  # which would leave PUBLIC_URL and WEB_ORIGIN pointing at the wrong place.
  #
  # Backgrounded and waited on rather than run in the foreground: bash defers
  # a signal until the current foreground child finishes, so a Ctrl-C that
  # reached only this shell would be queued behind a dev server that never
  # exits. `wait` is interruptible, so the trap runs immediately.
  ( cd packages/web && npx vite --port "$WEB_PORT" --strict-port ) &
  web_pid=$!
  wait "$web_pid"
elif (( want_api )); then
  echo
  echo "  api    http://localhost:$API_PORT"
  echo "  Ctrl-C to stop."
  wait "$api_pid"
fi
