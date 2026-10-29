# Streaming a run

Status: S0, S1, S2 and S3 are built. `litmus-lite run` can write its events to a file (`-stream-file`), push them to an ingest URL (`-stream-push`) and serve them as SSE while it runs (`-stream-pull`), with `-run-id`, `-label`, `-stream-samples` and `-stream-scenario`. `litmus-lite stream receive` is a reference ingest endpoint, `doctor -stream` checks a destination, and `run_test` over MCP takes the same arguments. You can also convert a finished report into an event stream, validate recordings, and serve recordings over HTTP. `litmus-lite serve` is a standing, read-only data source over a directory of run files (it follows a run in progress), and `stream replay` can push a recording to an ingest URL. Each section below says which phase it belongs to when that is not S0, S1, S2 or S3. **Event format v1 is frozen** as of the end of S2 (see "Compatibility"); one consumer has run against it by push and by pull, from real runs.

**Live view (optional, off until you configure it).** If you point `litmus-lite run` at a live-view service (`-live-url`, `LITMUS_LITE_LIVE_URL`, or `litmus-lite live enable URL`), it also pushes the events there and opens the page for the run in your browser. With none of those, nothing is sent, no browser is opened and no file is written. See "Live view" below, including what leaves your machine.

Litmus-lite can emit the events of a chaos run as it happens, so an application can show a run live, replay it, or collect it. Streaming is **additive**: `report.json`, `report.html`, JUnit output, stdout and exit codes are the same with or without it. Nothing is sent anywhere unless you pass a streaming flag or configure a live-view service.

Litmus-lite is a data source. It does not know about, or depend on, any particular dashboard. An application consumes one documented event format and chooses how the events reach it.

## What you can do with it

| You want to | Use |
| --- | --- |
| Keep a machine-readable record of a run (CI artifact, replay, tests) | `-stream-file` |
| Let an application connect to litmus-lite and watch | `-stream-pull` (during a run) or `litmus-lite serve` (any time) |
| Have litmus-lite send events to an application that exposes an ingest endpoint | `-stream-push` |
| Show an old run, or work without running chaos | `litmus-lite stream from-report` and `litmus-lite stream replay` |
| Watch a run in your browser | A live-view service, configured once: `litmus-lite live enable URL` (see "Live view") |

You can combine them. All sinks of a run receive the same events with the same sequence numbers.

## Modes of operation

### Push

Litmus-lite sends events to a URL you control.

```
export LITMUS_LITE_STREAM_TOKEN=...
litmus-lite run checkout.chaos.yaml -stream-push https://app.example.com/ingest/litmus-lite
```

- `POST` with `Content-Type: application/x-ndjson`. The body is a batch of events, one JSON object per line.
- Batches are sent about every 500 ms, or every 100 events, whichever comes first. A final flush runs when the run ends.
- Headers: `Authorization: Bearer $LITMUS_LITE_STREAM_TOKEN` (when set), `X-Litmus-Lite-Run-Id`, `X-Litmus-Lite-Seq-From`, `X-Litmus-Lite-Seq-To`, `User-Agent: litmus-lite/<version>`.
- `2xx` acknowledges the batch. `5xx`, `408`, `429` and network errors are retried with exponential backoff (500 ms doubling to 10 s, with 20% jitter, and `Retry-After` honoured up to 30 s). Other `4xx` responses drop the batch and add a warning. A redirect is not followed and counts as a failure, because following it could send the token somewhere else.
- When the run is over, the sink stops doubling its backoff and retries at the shortest interval until the exit window (five seconds) is used up, because what is left includes `run.finished`. A destination that is down does not hold up the run itself, only its exit, by at most that window. Events that were not delivered are named in a warning (`gave up at exit; the last events (from N) were not delivered`).
- Warnings about a destination never contain the URL path, the query or the token. Only the host is named.
- Delivery is at least once. A receiver deduplicates on `(runId, seq)`.
- One batch is in flight per destination at a time, so a receiver sees batches in order. A retried batch is sent again with the same sequence range.
- HTTPS is required unless the host is loopback or `-stream-insecure` is passed.

#### What an ingest endpoint must do

1. Check the bearer token if it requires one. Answer `401` if it is wrong.
2. Read the NDJSON body. Answer `400` if a line is not valid JSON and `413` if the body is too large. Neither is retried.
3. Store events, skipping any `(runId, seq)` already seen, so a retried batch is harmless.
4. Answer `2xx` promptly. Do the heavy work afterwards, because the sender waits for the answer before sending the next batch.

`litmus-lite stream receive` is a reference implementation of exactly this (see Utilities). It answers `200` with `{"accepted": N, "duplicates": M, "ignored": K}` (`ignored` counts events that arrive after the run's terminal event). An invalid line rejects the whole batch with `400`, so a batch is stored completely or not at all.

### Pull

An application connects to litmus-lite and reads events as Server-Sent Events (SSE).

```
litmus-lite run checkout.chaos.yaml -stream-pull 127.0.0.1:7070
# GET http://127.0.0.1:7070/v1/events        (SSE, the current run)
```

- Served only while the process is alive, plus `-stream-pull-linger` (default 15s) after the run ends, so a late client still reads the final event. Ctrl-C ends the linger early. Over MCP, the tool call returns only after the linger, so give `stream_pull` with care.
- The server is the same read-only `/v1` API as `serve` and `stream replay`: `GET /v1/health`, `/v1/info`, `/v1/runs`, `/v1/runs/{id}/events` and `/v1/runs/{id}/result`. `GET /v1/events` is a shortcut for the current run (`503` until the run has started).
- Each SSE message has `id: <seq>`, `event: <type>` and `data: <event JSON>`.
- A client that connects late gets the run from the start. A client that reconnects with `Last-Event-ID` gets exactly the events after it, with no gap and no duplicate. A client that already has the last event of a finished run gets `204`, which stops `EventSource` from reconnecting.
- The run is held in memory, capped at 50,000 events. Past the cap the oldest `probe.sample` events go first, then `point` events. Lifecycle events are kept. At most 32 clients are served at once.
- The listener binds to loopback unless you give another address together with `LITMUS_LITE_STREAM_TOKEN`. A non-loopback bind without a token is refused. Browsers need `-stream-cors-origin ORIGIN` to read it from a page on another origin.
- If the address cannot be bound, the run goes on without the pull sink and says so in the warnings.

### Persistent pull: `litmus-lite serve` (S3)

Pull during a run does not survive the run. For a data source that is always there, run:

```
litmus-lite serve -dir .litmus-lite/runs -listen 127.0.0.1:7070
```

`serve` is read-only. It does not start runs and it never writes. It reads the NDJSON files in `-dir`: the ones `-stream-file` writes (`-stream-file auto` puts them in `.litmus-lite/runs/`) and the ones `stream receive` stores. Litmus-lite and `serve` never talk to each other, so the CLI keeps working with no server running, and stopping `serve` in the middle of a run changes nothing about that run.

| Endpoint | Returns |
| --- | --- |
| `GET /v1/info` | Litmus-lite version, supported schema versions, capabilities (`mode` is `serve`) |
| `GET /v1/runs` | Run index, newest first. Filter with `?label=key=value` and `?status=running\|passed\|failed\|errored` |
| `GET /v1/runs/{id}/events` | SSE for that run, with `Last-Event-ID` replay. A finished run replays and closes. A run in progress is followed until its last event. |
| `GET /v1/runs/{id}/result` | The `report.json` content for a finished run |
| `GET /v1/health` | Readiness |

How it behaves:

- **It follows a run in progress.** A client that connects mid-run gets what has been written, then the rest as it is written, and the stream ends with the terminal event. The file is read from where the last request stopped, so nothing is parsed twice. A half-written last line is never served; it waits for the writer to finish it.
- **The directory can be empty or missing.** `serve` starts anyway and picks runs up as files appear.
- **A run that never ended** (the process was killed, so there is no `run.finished` or `run.failed`) is shown as `running` until its file has not been written to for `-stale` (default 2 minutes; a live run writes at least a point per second). After that the index shows status `errored` with `"incomplete": true`, followed clients are released, and a resume past its last event gets `204`.
- **A file that is replaced** (a new run with the same `-stream-file` truncates it) releases clients that were following the old one.
- **Files that are not runs** are skipped with one line on stderr per file: not JSON, another schema, a first event that is not `run.started`, more than one run id. Only regular files named `*.ndjson` directly inside `-dir` are read; symlinks and subdirectories are ignored.
- **Two files with the same run id:** the one written last is served, with a warning.
- At most 32 clients follow at once (`503` with `Retry-After` beyond that). An idle stream gets a comment line every 15 seconds so proxies keep it open.
- The directory is looked at on every request (a stat per file). That is fine for thousands of runs; a very large history should be rotated or split into directories.

`serve` binds to loopback by default. Any other address needs a token and, for browser clients, an explicit `-cors-origin`.

### File

```
litmus-lite run checkout.chaos.yaml -stream-file run.ndjson
litmus-lite run checkout.chaos.yaml -stream-file auto      # .litmus-lite/runs/<runId>.ndjson
```

The file holds one event per line. It is flushed per event, so `tail -f` works and `serve` can follow a run in progress.

- The file is created (with its directories) and truncated at the start of the run. If it cannot be written, the run goes on without it and the problem is a line in the run warnings.
- `auto` and the environment variables work from S1. `LITMUS_LITE_RUN_ID` is also used when the flag is absent.
- A run with `-stream-file` prints `streaming run <id> to <path>` on stderr. Stdout, `report.json`, `report.html`, JUnit output and the exit code are the same as without it (a sink problem can only add a line to the warnings).

### What a live run emits

- `run.started` first, then `phase.changed` (`steady`) when the probes start, `phase.changed` (`inject`) and `fault.started` when the fault is injected, `phase.changed` (`recover`) and `fault.ended` when it is rolled back, and `run.finished` last.
- `point` once per second, as each one-second bucket closes. Points are computed by the same function that builds `timeseries` in `report.json`, so for probe-derived fields (`offsetS`, `phase`, `p95Ms`, `errorRate`, `availability`, `intensity`) a live point equals the matching point of the final series. A bucket is held back while a probe that started inside it is still running, so a slow probe is counted in the right bucket.
- `rps` and `errorRate` in a live `point` come from the probes. When a load generator is used, the final series replaces them with the generator's numbers in the seconds it covers, so use `run.finished.timeseries` once it arrives.
- `warning` for each line added to the run warnings while the run is going (load generator problems, Prometheus).
- `probe.sample` only with `-stream-samples`.
- Exactly one terminal event. A run that stops early ends with `run.failed` and a `reason`: a steady-state probe that failed (`steady-state probe "healthz" failed`), a fault that could not start, a bad scenario value, or `interrupted`. Faults that had started are rolled back first and get their `fault.ended`, so `run.failed` is always the last event. `run.failed.faults` lists the faults that had been injected. A run interrupted after the recover window carries the partial `result`.
- Ctrl-C and SIGTERM: with a streaming flag, `run` stops, rolls faults back, ends the stream with `run.failed` (`interrupted`), writes no report and exits 130. Without a streaming flag, signals are handled exactly as before.

### When a sink has trouble

The engine never waits on a sink. Each sink has its own bounded queue and writer, and a failing sink only produces warnings:

- A write error stops that sink and is reported in the run warnings. The run, its reports and its exit code are unchanged.
- Under pressure `probe.sample` events are dropped first, then `point` events. Lifecycle events are never dropped. A drop leaves a gap in `seq` for that sink, and a `warning` event reports how many events were dropped (at most once a second).
- At exit, sinks are flushed after the reports are written, for up to five seconds in total. A push sink retries tightly during that window (see Push). A sink that is still stuck after that is abandoned and named in a warning.
- Dropped events leave a gap in `seq` for that sink. A receiver does not require `seq` to be contiguous, but `stream validate` does, so a recording from a sink that dropped events fails validation by design. The authoritative event is `run.finished`.

## Live view

The live view shows a run in your browser as it happens. It is a normal opt-in feature of `run`: **until you configure a live-view service it does nothing at all.** No request is made, no browser is opened, no identity file is written and `run` does not even mention it.

A service is configured in one of three ways, strongest first:

1. `-live-url URL` on a `run`.
2. `LITMUS_LITE_LIVE_URL` in the environment. For an editor's MCP server, `litmus-lite init -env LITMUS_LITE_LIVE_URL=URL` writes it into the server entry.
3. `litmus-lite live enable URL`, which keeps the address in `live.json` for later runs. `litmus-lite live disable` forgets it.

The service is any program that implements "What the service must do" below; litmus-lite does not ship one and has no default address.

What happens on a run once a service is configured:

1. litmus-lite reads `live.json` from your config directory (`$LITMUS_LITE_CONFIG_DIR`, or `litmus-lite/` under the OS user config directory). The first time there is none, so it **makes one**: a random project id (`p-` and 26 characters, 128 bits) and a random push token. The file is mode 0600.
2. It asks the service whether it is there (`GET /v1/health`, 1.5 s at most, no redirects).
3. It pushes the run to `<service>/v1/projects/<project id>/ingest` with the token as a bearer token. The service **creates the project when it first sees the id**, so nobody has to visit the service first or copy anything back.
4. It prints `live view: <service>/p/<project id>` on stderr and **opens that page in the default browser** (not when `CI` is set, `-no-open` or `LITMUS_LITE_OPEN=off` is used, or there is no display on Linux). The page shows the latest run of the project and moves to the next run by itself.

**One project for several tools.** Two tools that keep a `live.json` of this format (the file is just `projectId`, `pushToken` and an optional `base`) share a project when they read the same folder. `litmus-lite init -env LITMUS_LITE_CONFIG_DIR=DIR` (and the other tool's equivalent) writes that folder into each MCP server entry; the directory is made absolute, because an editor starts the server from a directory of its own. Runs started from the editor then share a project; runs from a terminal use the default folder unless that variable is exported there too. Re-running `init` without `-env` keeps what is already in the entry, and `-env KEY=` removes a variable. `init` refuses a variable whose name looks like a credential (`token`, `secret`, `password`, `api_key`) and a URL with a password in it: the MCP file is written into your project and is often committed.

Two values, two jobs: the project id is in the page URL, so whoever has the URL can read the project's runs. The push token never appears in a URL; the service keeps its hash from the first push and requires it for every later push, so knowing the URL is not enough to add runs to someone else's project.

### When it is used

| Situation | Live view |
| --- | --- |
| no `-live-url`, no `LITMUS_LITE_LIVE_URL`, nothing kept by `live enable` | **off: nothing is sent, opened or written** |
| a service is configured | on |
| `-no-live`, or `LITMUS_LITE_LIVE=off` | off |
| any `-stream-file`, `-stream-push` or `-stream-pull` (flag or variable) | off: you chose where the run goes |
| `-live` (with a service configured) | on, in addition to the destinations you chose |
| `-live` with no service configured | refused, exit 2 |
| `CI` set | on if configured (a destination in a CI job was put there on purpose), but the browser is never opened |
| `-no-open`, `LITMUS_LITE_OPEN=off`, or no display on Linux | on, but the browser is not opened |

Commands other than `run` never send anything to it. MCP `run_test` follows the same rules as the CLI (it execs it), with the `live`, `no-live` and `no-open` arguments.

If the service cannot be reached, answers something that is not a live-view service, or is an older version without projects, the run goes on without it and says so on stderr. A bad address (credentials, a query, a non-https remote) is refused the same way. Nothing here changes a run's result, reports or exit code. A non-loopback `http://` address is refused.

### What leaves your machine

Nothing, unless you configured a service. Then: the same events as any push (see "Event format"): the scenario name, the target and probe URLs and names, fault kinds and names, labels, and the results. Credentials in fault parameters are masked as always, and the scenario YAML is not included unless you also pass `-stream-scenario`. **Only the service you named receives it**; the audit log records `live:HOST` and nothing else. The push token and the project id are never written to a report, an event or the audit log. litmus-lite prints one line on stderr on every run that uses it, saying where the page is and how to turn it off.

### `litmus-lite live`

```
litmus-lite live enable URL # keep URL as the live-view service of this machine
litmus-lite live disable    # forget it (the identity stays)
litmus-lite live            # print the project page URL, and where the identity is kept
litmus-lite live -open      # also open it
litmus-lite live reset      # start a new project; runs of the old one stay under the old URL
```

With no service configured, `live`, `live -open` and `live reset` say so and exit 1 without making any file.

### What the service must do

For anyone implementing the other side:

- `GET /v1/health` answers `200 {"status":"ok","projects":true}`.
- `POST /v1/projects/{id}/ingest` takes the NDJSON batches described in "What an ingest endpoint must do", with `Authorization: Bearer <push token>`. An id that is new, with a token of at least 16 characters, creates the project and pins that token. An existing project needs the same token (`401` otherwise, also for a missing token, so a response does not say whether a project exists). Ids must be 16 to 64 characters of `a-z 0-9 -`.
- The page is `GET /p/{id}`.

### Design notes

1. **Opt-in.** The live view was first on by default; it is now dormant until a service is configured, because a run's target URLs and labels leave the machine when it is on. There is no built-in address.
2. **The browser opens on every run** once a service is configured. A later refinement is to open it on the first run of a project only, or only when no viewer is connected (the service could report that in the response to a push).
3. **Run retention is the service's business.** A service should say how many runs it keeps and answer `410` with `"code":"purged"` for a purged run.
4. **MCP runs follow the CLI.** `run_test` opens the page when a service is configured, so an agent that starts many runs opens many tabs; it passes `no-open` or `no-live` to avoid that.

## Flags

All flags belong to `run` and are optional. Each has an environment variable so MCP and CI can set it once. A flag wins over the variable.

| Flag | Env | Meaning |
| --- | --- | --- |
| `-stream-file PATH\|auto` | `LITMUS_LITE_STREAM_FILE` | Write NDJSON (S1) |
| `-stream-push URL` (repeatable) | `LITMUS_LITE_STREAM_PUSH` | Push to an endpoint; the variable takes a comma-separated list (S2) |
| `-stream-pull ADDR` | `LITMUS_LITE_STREAM_PULL` | Serve SSE during the run (S2) |
| `-stream-pull-linger DUR` | | Keep serving after the run (default 15s) (S2) |
| `-stream-insecure` | | Allow non-loopback `http://` push (S2) |
| `-stream-cors-origin ORIGIN` | | Allow this browser origin to read `-stream-pull` (S2) |
| `-run-id ID` | `LITMUS_LITE_RUN_ID` | Use this run id (default: 12 random hex digits). 1 to 64 characters from `A-Z a-z 0-9 . _ -` (S1) |
| `-label key=value` (repeatable) | | Attach a label to every event (S1) |
| `-stream-samples` | | Also emit every probe sample (off by default; high volume) (S1) |
| `-stream-scenario` | | Include the scenario YAML in `run.started`, redacted (off by default) (S2) |
| | `LITMUS_LITE_STREAM_TOKEN` | Bearer token for push and for a non-loopback pull or `serve` |
| `-live` | | Also use the live view when another destination is chosen (needs a service configured) |
| `-no-live` | `LITMUS_LITE_LIVE=off` | Do not use the live view, even if one is configured |
| `-no-open` | `LITMUS_LITE_OPEN=off` | Use the live view but do not open the browser |
| `-live-url URL` | `LITMUS_LITE_LIVE_URL` | The live-view service. Setting it is what turns the live view on; there is no default |
| | `LITMUS_LITE_CONFIG_DIR` | Where `live.json` is kept |

Destinations are never read from the scenario file. A checked-in `*.chaos.yaml` cannot send data anywhere.

## Event format

Every event is a JSON object:

```json
{
  "schema": "litmus-lite.io/stream/v1",
  "runId": "9f2c4e1a7b3d",
  "seq": 42,
  "ts": "2026-10-14T08:21:07.412Z",
  "offsetS": 7.0,
  "labels": { "service": "checkout" },
  "type": "point",
  "data": { }
}
```

- `seq` increases by one per event within a run and is the same in every sink.
- `ts` is the wall-clock time. `offsetS` is seconds since the run started. Consumers that combine streams from independent tools should align on `ts`.
- Unknown fields must be ignored by consumers. Unknown `type` values must be skipped.

### Types

| `type` | `data` | When |
| --- | --- | --- |
| `run.started` | `name`, `target`, `baseline`, `recover`, `faults[]`, `probes[]`, `hypotheses[]`, `load` (tool and start), `version`, optional `scenario` | First event |
| `phase.changed` | `phase` (`steady`, `inject`, `recover`) | At each phase boundary |
| `fault.started` | `name`, `kind`, `intensity`, `tunables` (redacted) | Fault armed |
| `fault.ended` | `name`, `kind`, `rollback` (`ok` or `fail: …`) | Fault rolled back |
| `point` | `offsetS`, `phase`, `p95Ms`, `errorRate`, `availability`, `rps`, `intensity` | Once per second, after each one-second bucket closes |
| `probe.sample` | `name`, `ok`, `latencyMs`, `status`, `error` | Per probe, only with `-stream-samples` |
| `load.started` | `tool`, `startOffsetS` (seconds from the run's time zero) | Load generator started |
| `load.point` | `offsetS` (run clock, like `point.offsetS`), `rps`, `errorRate`, optional `p50Ms`, `p95Ms`, `p99Ms`, `requests`, `failed` | Generator progress, about once a second, when the tool can report it. Today only vegaload does |
| `load.finished` | `series[]` (`offsetS`, `rps`, `errorRate`, on the run clock; can be empty), `error` only on failure | Generator exited |
| `warning` | `message` | Anything that goes to `res.Warnings`, including dropped events |
| `run.finished` | The `report.json` content plus `diagnosis` (the `diagnose` text). Raw `probes` only with `-stream-samples`. Credentials in `faultLog` tunables are redacted. A composed run adds an optional `loadGenerator` (`tool`, `version`, `via`): which generator binary ran. Its local `path` is in `report.json` only, never in the stream. | Last event of a run that completed |
| `run.failed` | `reason` (for example a failed steady-state probe, or `interrupted`), partial result if any | Last event of a run that did not complete |

Every stream ends with exactly one of `run.finished` or `run.failed`, including on Ctrl-C.

### Rules for consumers

1. **`run.finished` is authoritative.** Replace any live series with `data.timeseries`. The final series includes load data that may only arrive late.
2. **Live `point.rps` comes from the probe rate.** When `load.point` events exist, prefer them: they carry the generator's own rate, error rate and (for vegaload) latency percentiles, so a consumer can draw the load next to the probes. Their `offsetS` is on the run's clock, not the generator's. `load.finished.series` is the generator's whole series and fills seconds a consumer missed. The final series merges both, as `report.json` does today.
3. **Verdicts come only from `run.finished`.** Hypotheses are judged on the whole inject window, so there are no live pass or fail verdicts.
4. **A stream is self-sufficient.** A chaos stream is complete without a load stream, and the reverse.

## Chaos and load together or apart

The two usual shapes:

- **Composed.** The scenario has a `load:` block and litmus-lite starts the generator. One stream carries the chaos events and, when the tool allows, `load.*` events.
- **Independent.** The load generator and litmus-lite are started separately, for example from two terminals. Each produces its own stream.

To let an application join independent streams, give both the same identifier:

```
litmus-lite run checkout.chaos.yaml -run-id demo-42 -stream-pull 127.0.0.1:7070
vegaload run checkout.vl.js -run-id demo-42 ...      # when the generator supports it
```

When litmus-lite streams a composed run with vegaload, it starts vegaload with `-stream-file` and `-no-open`, reads the per-second points as vegaload writes them and re-emits them as `load.point` in its own stream, so the whole run is one stream with one `runId`. It also sets `VEGALOAD_LIVE=off` for the generator in every composed run, streamed or not: litmus-lite owns the view, and the generator must not open its own page or push a second run. Whether vegaload can write a stream file is read from its `run -h` before it starts, so every run takes one path: `-no-open` and a temporary `-report` are always passed, `-stream-file` only when listed. A vegaload without `-stream-file` still runs; its points arrive in `load.finished` at the end, with one warning that names the copy. The `k6` and `command` tools emit `load.started` and `load.finished` only.

Litmus-lite also exports `LITMUS_LITE_RUN_ID` to a generator it starts (and `VEGALOAD_RUN_ID` to vegaload). A consumer joins on `runId` when present, otherwise on a shared label plus overlapping `ts` ranges. Any generator can contribute by emitting `load.point` and `load.finished` events in this format.

### When the load starts

Today the generator starts when the fault is injected. A new optional field starts it with the baseline, which is how most people run chaos with load:

```yaml
load:
  start: baseline      # or inject (default)
  scenario: ./checkout.vl.js
  args: ["-vus", "180", "-duration", "23s"]
```

The default stays `inject`, so existing scenarios behave as before. With `baseline`, the generator starts at the same moment as the probes, and its series is aligned to that start instead of the inject start. Set the generator's own duration to cover baseline, inject and recover. The generator is stopped if the run ends early.

## Safety and privacy

- Streaming is off unless you pass a flag, set a variable or configure a live-view service (see "Live view"), and nothing is sent anywhere on its own. A configured live view goes only to that service, and is turned off for a run by `-no-live`, `LITMUS_LITE_LIVE=off`, or by choosing a destination yourself.
- Fault `tunables` are redacted before they are emitted. Keys that match `token`, `secret`, `password` or `key` (case-insensitive) become `"***"`. The failpoint `token` is one example.
- Response bodies, request headers and probe command output are never streamed.
- `-stream-scenario` adds the scenario YAML to `run.started` for consumers that want to show it. It is redacted on a best-effort basis: fault parameters (as above), credentials in URLs (user info and query keys such as `token`, `api_key`, `password`), the values after secret-looking flags in load arguments, and `name=value` pairs in command text. Redacting free text cannot be complete, so it is off by default. Do not put a secret in a scenario and rely on this.
- Pull and `serve` refuse a non-loopback bind without a token. Push refuses plain `http://` to a non-loopback host unless `-stream-insecure` is given. Both refusals end the run before it starts, with exit code 2.
- The token comes from the environment, never from the scenario file, and is never written to `report.json` or the audit log. The audit log records only that streaming was used and the kind of each destination (`file`, `pull`, `push:HOST`, `live:HOST`), never a path, query or token. The live view's project id and push token are kept in `live.json` (mode 0600) and nowhere else.
- A streaming problem never changes the result of a run. A slow, unreachable or rejecting sink adds a `warning` event and a line in the run warnings. The run, its reports and its exit code are unaffected.
- The engine never waits on a sink. Each sink has a bounded queue. Under pressure, `probe.sample` events are dropped first, then `point` events. Lifecycle events (`run.*`, `phase.changed`, `fault.*`, `load.started`, `load.finished`, `warning`) are never dropped. Drops are reported as a `warning`.
- `serve` is a read-only data endpoint. It is not a dashboard, and the HTML report still renders offline.

## Utilities

Available now (S0, S1 and S2):

```
litmus-lite run checkout.chaos.yaml -stream-file run.ndjson -label service=checkout
litmus-lite stream from-report report.json -out run.ndjson
litmus-lite stream validate run.ndjson
litmus-lite stream replay docs/stream/fixtures/*.ndjson -speed 5 -cors-origin http://localhost:5173
```

### `stream from-report`

Turns a finished `report.json` into the events a live run would have produced. Flags: `-out FILE` (default stdout), `-run-id ID`, `-label key=value` (repeatable), `-samples`.

- Every event has `"synthetic": true` on the envelope. The report holds less than a live run knew, so `run.started` has no target, probe interval or scenario text. A live run does not set `synthetic`.
- The run id is derived from the run name and start time, so converting the same report twice gives the same id. `-run-id` overrides it.
- One `point` per second of `timeseries`, emitted when its bucket closes. No `probe.sample` events unless `-samples` is given.
- No `load.*` events. The merged load series is in `run.finished.timeseries`.
- Credentials in fault parameters are redacted everywhere, including `run.finished.faultLog`. `report.json` itself keeps them as written.
- `run.finished` is the report content plus `diagnosis`. It leaves out `probes` unless `-samples` is given. Arrays that `report.json` writes as `null` (for example `hypotheses` when a scenario has none) stay `null`; treat `null` as empty.

### `stream validate`

Checks the structure of recordings: the schema string, one run id, a `seq` that starts at 1 and rises by one, `ts` and `data` present, `run.started` first, and nothing after a terminal event. It exits 1 if any file fails. Use it in CI for recordings you produce or keep.

### `stream replay`

Serves recordings over HTTP so a consumer can be built and demoed without running chaos. It serves the read-only surface described under `serve`, backed by the files you give it:

| Endpoint | Returns |
| --- | --- |
| `GET /v1/info` | `{"name","version","mode":"replay","schemas":[…],"capabilities":[…]}` |
| `GET /v1/runs` | Run index, newest first. Filter with `?status=` and `?label=key=value`. Each entry has `runId`, `name`, `labels`, `status`, `startedAt`, `finishedAt`, `events`, `synthetic`. |
| `GET /v1/runs/{id}/events` | SSE: `id: <seq>`, `event: <type>`, `data: <the event line>` |
| `GET /v1/runs/{id}/result` | The `run.finished` data. `404` if the run did not finish. |
| `GET /v1/health` | `{"status":"ok"}` |

`status` is `passed` or `failed` (the run completed, with every hypothesis holding or not), `errored` (it ended with `run.failed`), or `running` (no terminal event yet).

Playback:

- Events are sent with the gaps they were recorded with, divided by `-speed` (default 1; 0 sends everything at once). `-max-gap` caps a single pause. `?speed=N` on the request overrides `-speed`.
- Every connection plays the run from its start, so any number of clients can watch the same recording independently. `Last-Event-ID` (or `?after=N`) resumes after that event.
- When a client asks to resume a finished run after its last event, the answer is `204 No Content`. A browser `EventSource` treats that as "stop reconnecting". Without it the page would replay the run every few seconds.
- The stream closes after the terminal event.

Flags: `-stream-pull ADDR` (default `127.0.0.1:7070`), `-speed`, `-max-gap`, `-cors-origin ORIGIN`. `LITMUS_LITE_STREAM_TOKEN`, when set, is required as a bearer token on everything except `/v1/health`. An address that is not loopback is refused unless the token is set.

The recordings in [`stream/fixtures/`](stream/fixtures/) cover a passed run, a run with a failed hypothesis, a run with a load series, a run with a warning, redacted credentials, and a run that errored.

#### Replay through push (S3)

```
litmus-lite stream replay run.ndjson -stream-push https://app.example.com/ingest/litmus-lite
litmus-lite stream replay docs/stream/fixtures/*.ndjson -stream-push http://127.0.0.1:9000/v1/ingest -speed 20 -fresh
```

With `-stream-push`, `replay` sends the recordings to an ingest endpoint instead of serving them, through the same push sink as a real run (batching, retry, token from `LITMUS_LITE_STREAM_TOKEN`, the https rule and `-stream-insecure`). Use it to test a push endpoint without running chaos.

- Events are sent exactly as recorded: same `seq`, `ts` and data. Timestamps are the original ones, not shifted to now. Pace is the recorded pace divided by `-speed` (`-speed 0` sends everything at once; `-max-gap` caps a pause).
- A receiver deduplicates on `(runId, seq)`, so pushing the same recording twice stores it once. `-fresh` gives each recording a new run id so it is stored again; `-run-id ID` pushes one recording under a chosen id. Only the run id changes.
- Nothing is dropped. The process exits 0 when every event was delivered, 1 when a destination rejected or never took some of them (the summary says how many), 2 for a usage error such as plain `http://` to a remote host, and 130 on Ctrl-C.
- Without `-stream-pull`, `replay` pushes and exits. With both flags it pushes, then serves until Ctrl-C.
- Only an explicit `-stream-push` pushes. `replay` does not read `LITMUS_LITE_STREAM_PUSH`.

### Reference receiver

```
export LITMUS_LITE_STREAM_TOKEN=demo
litmus-lite stream receive -dir .litmus-lite/runs -listen 127.0.0.1:9000
litmus-lite run checkout.chaos.yaml -stream-push http://127.0.0.1:9000/v1/ingest
```

- Accepts pushed batches at `POST /v1/ingest`, checks the token, and drops duplicate `(runId, seq)` events.
- Writes one NDJSON file per run into `-dir`, in the same layout as `-stream-file auto`. `litmus-lite serve -dir` reads that directory, so pushed runs can be viewed through the pull endpoints.
- `-print` echoes a one-line summary of each event to stderr.
- It is a small tool for developing and testing a push integration, and a worked example of the ingest rules above. It is not meant to be a production collector.
- For testing a sender, two development flags make it misbehave on purpose: `-delay DUR` waits before answering, and `-fail-percent N` answers `503` for N% of batches. They exist to exercise retry and backpressure.

### `doctor -stream`

```
litmus-lite doctor -stream https://app.example.com/ingest/litmus-lite
litmus-lite doctor -stream 127.0.0.1:7070
```

A URL is checked by POSTing an empty batch with `X-Litmus-Lite-Doctor: 1` and the token from the environment. The receiver must answer `2xx`, so an ingest endpoint should accept an empty body. An address is checked against the bind rule and by listening on it briefly. The same URL rules apply as for a run (`http://` to a remote host fails unless `-stream-insecure`).

### From an agent (MCP)

`run_test` follows the same rules as the CLI: with no `stream_*` argument and a live-view service configured (for example `litmus-lite init -env LITMUS_LITE_LIVE_URL=URL` wrote it into the server's environment), it uses the live view, so a run started from an agent also opens the page for the run. With no service configured it does nothing extra. It forwards three arguments for this: `live` (`-live`), `no-live` (`-no-live`) and `no-open` (`-no-open`: push to the live view, but do not open a browser). An agent that must not open pages passes `no-live` or `no-open`.

The tool result keeps stdout and stderr apart. The first text item is the run's JSON, as before. The second text item, present only when the run wrote anything to stderr, holds those notes, among them the line `live view: URL`. An agent should tell the user that URL. The host rule that `litmus-lite hosts` writes says so. A failed run reports both streams in the error text.

`run_test` also accepts `stream_file`, `stream_push` (comma-separated), `stream_pull`, `run_id` and `labels` (comma-separated `key=value`) and forwards them as the flags above. There is no argument for a token or for `-stream-insecure`: the token comes from the environment of the MCP server, and the https rule cannot be relaxed from a tool call. With `stream_pull`, the tool call returns after the run plus the linger.

## Compatibility

Event format `litmus-lite.io/stream/v1` is frozen at the end of S2. Inside `v1`:

- New optional fields and new event types may be added, so consumers must ignore unknown fields and skip unknown event types (`load.*` arrive in S4 this way).
- Existing fields are not renamed, removed or retyped, and the meaning of an existing event type does not change. A change like that is a new `schema` value (`v2`), and litmus-lite would emit v2 only when asked.
- Known behaviours that are part of v1 as it stands: delivery is at least once; consumers dedupe on `(runId, seq)`; `seq` can have gaps in a sink that dropped events; `run.finished` is authoritative over what came before it (including the `fault.ended` rollback status of faults that arm and disarm, and the `rps` of a live `point` when a load generator is used).

## Not in scope

- Starting, stopping or scheduling runs over the stream or `serve`. Use the CLI, or MCP, which calls the CLI.
- Storing runs beyond the NDJSON files you choose to keep.
- A hosted service, accounts or a control plane. A live-view service is a separate program.
- Telemetry. litmus-lite collects nothing about how it is used and sends nothing by default. A live-view service you configure gets a run's events, as described above; that is the run, not usage data.

## Using it from an application

Treat litmus-lite as one data source behind a small interface and keep the view a pure function of events:

```
list()                  -> run summaries
subscribe(runId)        -> ordered events (SSE, or a replayed file)
get(runId)              -> final result
```

- Pull configuration: `{ kind: "litmus-lite", mode: "pull", baseUrl, token }`
- Push configuration: the application exposes an ingest URL and token, and the user passes them to `-stream-push` and `LITMUS_LITE_STREAM_TOKEN`
- The JSON Schema for the event format lives at `docs/stream/schema/v1.json` (v1, frozen), together with recorded fixtures under `docs/stream/fixtures/`. This repository owns the contract. The schema is also attached to every release, so a consumer can pin an exact version.

### TypeScript types

Litmus-lite does not ship a TypeScript package. The schema is the source of truth, and a consumer generates types from it:

```
npx json-schema-to-typescript --unreachableDefinitions -i docs/stream/schema/v1.json -o src/litmus-lite-stream.d.ts
```

- The generator produces the envelope and one interface per `data` shape (`Point`, `RunFinished`, and so on). It does not link a `type` string to its `data` interface, because that link is an `if`/`then` in the schema. Write that map yourself in a few lines (`'point': Point`, `'run.finished': RunFinished`, …) and build the discriminated union from it. Without `--unreachableDefinitions` you get the envelope only.
- Pin the schema to a release tag or a commit, and regenerate when you bump it. Check the generated file into the consumer.
- Validate the consumer against the fixtures in CI. A fixture that fails to parse is a contract break.
- The schema allows unknown fields, so a newer litmus-lite does not break an older consumer inside `v1`.
- If several applications end up generating the same types, publish one small shared package from this repository then. Until there is a second consumer it would only add Node tooling to a Go project.

## Phases

Delivered in order. Each phase leaves the tool fully working without any streaming flag.

| Phase | Adds |
| --- | --- |
| S0 (done) | Schema, fixtures, `stream from-report`, `stream validate`, `stream replay` over HTTP |
| S1 (done) | Engine emitter, live `point` events, `-stream-file`, `-run-id`, `-label`, `-stream-samples`, `run.failed`, `load.start: baseline` |
| S2 (done) | `-stream-push`, `-stream-pull`, queueing and backpressure, redaction (including `-stream-scenario`), token and TLS rules, MCP arguments, `doctor -stream`, `stream receive`. Schema v1 frozen |
| S3 (done) | `litmus-lite serve` over a directory (follows a run in progress), `stream replay` through push |
| Live view (done) | Optional live view for `run`, dormant until a service is configured: `internal/live`, `live.json` identity, project push, `-live`, `-no-live`, `-no-open`, `-live-url`, `litmus-lite live enable\|disable\|reset`, `init -env`. Built after S3, before S4 |
| S4 | `LITMUS_LITE_RUN_ID` to generators, live `load.*` events |
| S5 | Compatibility policy, CI example that uploads the NDJSON file |
