# Stream fixtures

Recorded event streams for building and testing a consumer. The event format is described in [`../../streaming.md`](../../reference/streaming.md) and by [`../schema/v1.json`](../schema/v1.json).

| Fixture | What it shows |
| --- | --- |
| `ignite.ndjson` | `http.latency` with a 5% status override on LaunchPad. Passed. A `warning` event (Prometheus is not running). |
| `ignite-load.ndjson` | The same fault with a load generator (`command` tool). Passed. `run.finished.timeseries` carries the merged load series. |
| `ignite-load-live.ndjson` | A live recording, not converted from a report: the same fault with vegaload as the generator. It has real `load.started`, about one `load.point` a second (rps, error rate, p50/p95/p99), and `load.finished`, and no `synthetic` field. Has no `.report.json`. Needs vegaload on `PATH` to re-record. |
| `ignite-failed.ndjson` | Completed with a failed hypothesis (`stays-fast`). `status` is `failed`. |
| `cashier-charge.ndjson` | A `failpoint` fault. The fault `token` is redacted to `***`. |
| `cashier-charge-slow.ndjson` | A `failpoint` fault that adds latency instead of errors. |
| `run-failed.ndjson` | A run that did not complete: `run.started` then `run.failed`. Written by hand, in the shape a live run produces. |

Each recording except `run-failed` has the report it came from next to it (`NAME.report.json`). The `.ndjson` is exactly what `litmus-lite stream from-report NAME.report.json` writes. A test checks that.

## About the data

- These streams come from finished reports, so every event is marked `"synthetic": true`. A live run does not set that field, and its `run.started` has more detail (target, probe list).
- A `point` event is complete when its one-second bucket closes, so `offsetS` on the envelope is one second after `data.offsetS`.
- `point.rps` is the probe rate until a load series is merged. In `ignite-load`, `run.finished.timeseries` has the generator's rate during the inject window and the probe rate elsewhere. A consumer should not read that as one continuous measurement.
- `errorRate` and `availability` are fractions between 0 and 1.

## Replay them

```
litmus-lite stream replay docs/stream/fixtures/*.ndjson -speed 5 -cors-origin http://localhost:5173
curl http://127.0.0.1:7070/v1/runs
curl -N http://127.0.0.1:7070/v1/runs/<runId>/events
```

## Re-record

```
docs/stream/fixtures/generate.sh [path/to/litmus-lite]
```

It needs python3, curl and go. It uses LaunchPad if it is already listening on `:8080` and `src/stub.py` otherwise. `ONLY=NAME` re-records one fixture. Recordings change on every run (timestamps, run ids), so only re-record when the format changes.
