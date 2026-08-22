#!/usr/bin/env bash
# Run atlas with the variables from .env loaded.
#
# Atlas has no --env-file flag and does not read .env on its own: the getenv()
# calls in atlas.hcl read the real process environment, so without this the
# config silently resolves to empty strings and atlas reports the unhelpful
# "required flag(s) \"dev-url\" not set".
#
#   ./migrate.sh migrate diff initial
#   ./migrate.sh migrate apply
#   ./migrate.sh migrate status
set -euo pipefail

cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
  echo "no .env here — copy .env.example and fill in both Neon URLs" >&2
  exit 1
fi

# set -a exports every variable defined while it is on, which is what makes
# them visible to the atlas child process.
set -a
# shellcheck disable=SC1091
source .env
set +a

for v in DATABASE_URL DATABASE_DEV_URL; do
  if [[ -z "${!v:-}" ]]; then
    echo "$v is empty in .env" >&2
    exit 1
  fi
done

exec atlas "$@" --env neon
