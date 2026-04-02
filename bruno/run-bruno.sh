#!/usr/bin/env bash
set -euo pipefail
# Helper wrapper to run bru with DRONER_REPO_PATH set to the parent
# of this collection. This makes execution robust whether the caller
# invokes the script from the repo root, the bruno/ dir, or elsewhere.

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -z "${DRONER_REPO_PATH:-}" ]; then
  DRONER_REPO_PATH="$DIR/.."
fi
DRONER_REPO_PATH="$(realpath "$DRONER_REPO_PATH")"
export DRONER_REPO_PATH

exec bru run --env-file "$DIR/environments/local.bru" "$@"
