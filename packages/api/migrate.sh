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
  echo "no .env here — copy .env.example and fill in both URLs" >&2
  exit 1
fi

# Parsed, not sourced, for the reason scripts/dev.sh gives: `source .env` runs
# the file as shell, and the "&" in a connection string's query
# (?search_path=public&sslmode=require) backgrounds the assignment into a
# subshell, leaving the variable empty here. Each value is read as a literal.
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%$'\r'}"                     # a CRLF .env, from a Windows editor
  case "$line" in ''|'#'*) continue ;; esac
  [[ "$line" != *=* ]] && continue
  key="${line%%=*}"
  key="${key// /}"
  value="${line#*=}"
  sq="'"
  case "$value" in
    '"'*'"') value="${value:1:${#value}-2}" ;;
    "$sq"*"$sq") value="${value:1:${#value}-2}" ;;
  esac
  export "$key=$value"
done < .env

for v in DATABASE_URL DATABASE_DEV_URL; do
  if [[ -z "${!v:-}" ]]; then
    echo "$v is empty in .env" >&2
    exit 1
  fi
done

exec atlas "$@" --env db
