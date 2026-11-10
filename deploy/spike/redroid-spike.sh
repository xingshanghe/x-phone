#!/bin/sh
# Run from the repository root on an Ubuntu 24.04 host with Docker and binderfs.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
PROFILE=${1:-"$ROOT/profiles/pixel_8.json"}
export PYTHONPATH="$ROOT/tools/phone${PYTHONPATH:+:$PYTHONPATH}"
exec python3 -m phone spike --profile "$PROFILE" --execute
