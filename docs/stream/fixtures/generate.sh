#!/usr/bin/env bash
# Re-record the stream fixtures from real litmus-lite runs.
#
#   docs/stream/fixtures/generate.sh [path/to/litmus-lite]
#   ONLY=ignite-load docs/stream/fixtures/generate.sh [path/to/litmus-lite]   # one recording
#
# Needs python3, curl, go, and free ports 8091 and 18080. Takes about two minutes.
# The launchpad runs go against LaunchPad when it is already listening on :8080,
# otherwise against src/stub.py, a stand-in. The cashier runs start
# examples/cashier from the repository.
#
# Each run writes NAME.report.json (what `run` wrote) and NAME.ndjson (what
# `stream from-report` made of it). run-failed.ndjson is written by hand: a run
# that fails before it finishes writes no report, so there is nothing to convert.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
bin="${1:-litmus-lite}"
tmp="$(mktemp -d)"
pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$tmp"; }
trap cleanup EXIT

record() { # NAME SCENARIO
  local name="$1" scenario="$2"
  [ -z "${ONLY:-}" ] || [ "$ONLY" = "$name" ] || return 0
  # Flags go before the file: the flag package stops at the first argument.
  "$bin" run -out "$here/$name.report.json" -html "$tmp/$name.html" "$scenario" >/dev/null || [ $? -eq 3 ]
  "$bin" stream from-report "$here/$name.report.json" -out "$here/$name.ndjson"
  "$bin" stream validate "$here/$name.ndjson"
}

# A live recording: the run streams itself (no report to convert), with vegaload
# as the load generator, so the file has real load.started, load.point and
# load.finished events. Needs vegaload (a build with -stream-file) on PATH.
record_live() { # NAME SCENARIO
  local name="$1" scenario="$2"
  [ -z "${ONLY:-}" ] || [ "$ONLY" = "$name" ] || return 0
  if ! command -v vegaload >/dev/null 2>&1; then
    echo "skipping $name: vegaload is not on PATH" >&2
    return 0
  fi
  rm -f "$here/$name.ndjson"
  LITMUS_LITE_CONFIG_DIR="$tmp/cfg" VEGALOAD_CONFIG_DIR="$tmp/vcfg" \
    "$bin" run -no-live -stream-file "$here/$name.ndjson" -run-id "$name" \
    -out "$tmp/$name.report.json" -html "$tmp/$name.html" "$scenario" >/dev/null || [ $? -eq 3 ]
  "$bin" stream validate "$here/$name.ndjson"
}

# Use LaunchPad when it is already up on :8080. Otherwise start the stand-in.
if ! curl -fsS -o /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; then
  python3 "$here/src/stub.py" & pids+=($!)
  sleep 1
fi

(cd "$root" && go build -o "$tmp/cashier" ./examples/cashier)
LITMUS_LITE_FAILPOINT=charge,charge-slow LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo "$tmp/cashier" >"$tmp/cashier.log" 2>&1 & pids+=($!)
sleep 2

record ignite "$root/examples/launchpad/ignite.chaos.yaml"
record ignite-load "$here/src/ignite-load.chaos.yaml"
record ignite-failed "$here/src/ignite-failed.chaos.yaml"
record cashier-charge "$root/examples/cashier/charge.chaos.yaml"
record cashier-charge-slow "$root/examples/cashier/charge-slow.chaos.yaml"
record_live ignite-load-live "$root/examples/launchpad/ignite-load.chaos.yaml"
