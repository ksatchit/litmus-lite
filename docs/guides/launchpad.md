# Walkthrough: chaos on a two-process app (LaunchPad)

A terminal walkthrough of the HTTP and process faults against a small app with two processes, [LaunchPad](https://github.com/umamukkara/launchpad): `missioncontrol` (REST on `:8080`) talks to `engine` (gRPC on `:7070`). It takes 12 to 18 minutes with both repositories cloned. Nothing here needs a load generator or Prometheus.

If you only want to see litmus-lite work, the [quick start](../getting-started/quick-start.md) needs nothing but the sample service in this repository. Related walkthroughs: [with an agent](agent-walkthrough.md), [load and chaos](load-and-chaos.md), [failpoints](failpoints.md). What each fault does: [fault reference](../reference/faults.md).

## 0. Prepare

You need `litmus-lite` on your `PATH` ([install](../getting-started/install.md)), Go 1.22+, and a clone of LaunchPad. In two terminals, from the LaunchPad repository:

```
go run ./cmd/engine           # terminal A
go run ./cmd/missioncontrol   # terminal B
```

Check that it is up:

```
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/healthz
curl -sS http://127.0.0.1:8080/api/launches | head
```

Expect `200` and a JSON list. If `engine` is down, the list call hangs or returns 5xx: fix that first.

From the litmus-lite repository:

```
litmus-lite doctor
litmus-lite validate examples/launchpad/ignite.chaos.yaml
```

`doctor` may warn that a load generator or Prometheus is missing. Neither is needed here.

## 1. HTTP latency in front of missioncontrol

A slow and flaky HTTP hop. The scenario puts a proxy on `:18080` in front of `:8080` and adds 800 ms of delay, 100 ms of jitter and 5% HTTP 500s for 12 seconds. Probes call `GET /api/launches` through the proxy.

```
litmus-lite run examples/launchpad/ignite.chaos.yaml
```

Open `report.html`. What should be true:

- The probes hit `:18080` (the proxy), not `:8080`, during the fault.
- Steady band: error rate near 0, p95 low.
- Inject band: p95 tracks the delay (about 800 ms plus jitter), with a few 500s.
- Recover band: error rate is back to 0 within 5 seconds.
- The hypotheses `errors-bounded` (errors under 25%) and `recovers` pass.
- If Prometheus on `:9090` is not running, the run still passes, with a warning.

If p95 does not move, the client is still talking to `:8080`. Point the probes at the proxy's `listen` address. To read the result in words:

```
litmus-lite diagnose report.json
```

## 2. The same file, from a catalog

```
litmus-lite hub import http.latency -beside ./chaos
litmus-lite mcp eval
```

`hub import` writes a real `*.chaos.yaml` you edit, and `mcp eval` runs the MCP server against fixtures that call the real CLI.

## 3. Freeze the engine process

`process.pause` sends `SIGSTOP` to a process, waits, then sends `SIGCONT`. While the engine is stopped, `GET /api/engine/status` fails or times out; `GET /api/launches` stays `200` because it does not call the engine.

Find the engine's PID:

```
pgrep -fl engine
```

If the process name is not `engine`, edit `params.pid` (or a unique substring in `params.command`) in `examples/launchpad/pause-engine.chaos.yaml`. A command match may signal only a PID on the `-allow-pid` list; if the match is not the PID you named, the run fails before any signal is sent.

```
PID=$(pgrep -n engine)
litmus-lite validate examples/launchpad/pause-engine.chaos.yaml -allow-pid "$PID"
litmus-lite run examples/launchpad/pause-engine.chaos.yaml -allow-pid "$PID"
```

What should be true:

- The start-of-test `healthz` probe passes (the engine is up).
- During the fault, `/api/engine/status` fails or times out. After 8 seconds the process is continued and the `recovery` hypothesis passes.
- No `-yes` is needed: pause is reversible. Do not run `process.kill` against an engine you cannot restart.
- If litmus-lite is killed with `SIGKILL` while the process is paused, nothing resumes it: run `kill -CONT PID`.

If the run refuses the PID, stop and check which process it matched. Do not work around it with a broader `-allow-pid`.

## 4. Compare two runs

Keep a report as `baseline.json` (copy `report.json` after step 1). After a second run, for example with a longer delay:

```
cp report.json candidate.json
litmus-lite compare baseline.json candidate.json
```

`compare` prints a table by phase (steady, inject, recover), not a single score.

## 5. Other things to try

- **Load and chaos together:** [load-and-chaos.md](load-and-chaos.md).
- **Export to another tool's format:** `litmus-lite move-to -adapter litmus examples/launchpad/ignite.chaos.yaml` prints a Litmus 4.0 experiment document; `-adapter ir` prints the neutral JSON.
- **Failpoints in application code:** [failpoints.md](failpoints.md).
- **Not on a shared machine:** `process.kill` on a process you cannot restart, `disk.fill` on a real disk, `docker.pause` on a container that is not disposable.

## 6. Reset

```
rm -f report.json report.html baseline.json candidate.json junit.xml
```

Restart the LaunchPad terminals if you paused the engine and did not let the run roll it back. If a proxy still holds `:18080`, the next run fails to listen: wait for the rollback, or check `lsof -i :18080`.

## Other faults to import

```
litmus-lite hub list
litmus-lite hub import docker.pause -beside .
litmus-lite hub import cpu.hog -beside .
litmus-lite hub import memory.hog -beside .
litmus-lite hub import disk.fill -beside .
```

`disk.fill` and `process.kill` need `-yes`. `process.pause` and `process.kill` also need `-allow-pid PID`. `disk.fill` refuses to overwrite a file that exists.
