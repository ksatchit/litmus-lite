# Fault catalog: intent and what to expect

What each fault does, what a healthy run of it looks like, and what a failing one looks like. `litmus-lite hub list` prints the ids; `litmus-lite hub import ID -beside DIR` writes a template you can edit. Every scenario here is a file; none depends on an agent.

## Scenarios in `examples/`

### `examples/launchpad/ignite.chaos.yaml`: HTTP latency and sparse 500s

| | |
| --- | --- |
| **Intent** | Check that the list and ignite path of the [LaunchPad](https://github.com/umamukkara/launchpad) app stays usable when the HTTP hop in front of `missioncontrol` is slow and occasionally wrong. The inner-loop "slow dependency" test. |
| **How** | A userspace proxy on `:18080` forwards to `:8080`. 800 ms delay, 100 ms jitter and 5% HTTP 500 for 12 s. A continuous `GET /api/launches` goes through the **proxy**. |
| **Expect** | The start-of-test `/healthz` on `:8080` passes. During the fault, p95 rises toward about 800 ms plus jitter and the error rate stays under 25%. After the proxy stops, errors return to about 0 within 5 s. If Prometheus is down it is a warning, not a failure. |
| **Failing looks like** | A flat p95 (probes still on `:8080`), or the run hanging (nothing listening on `:8080`). |

### `examples/launchpad/pause-engine.chaos.yaml`: freeze a process

| | |
| --- | --- |
| **Intent** | See what happens when a **dependency process** stops making progress, as opposed to a slow HTTP hop. `missioncontrol` should show the outage and recover when the process continues. |
| **How** | `SIGSTOP` the process whose command matches `engine` for 8 s, then `SIGCONT`. The run takes `-allow-pid` for that PID and signals no other. Probes call `/api/engine/status` (not `/api/launches`, which is in-process), with an 800 ms timeout so a hung call becomes a failed sample. |
| **Expect** | The start-of-test health probe passes. During the fault, availability of engine status drops (502 or a timeout). After 8 s samples succeed again and `recovery <= 8s`. |
| **Failing looks like** | `validate` refuses the run when `-allow-pid` is missing. If the PID you named belongs to a different `engine`, stop and `kill -CONT` it; do not kill it. |

### `examples/cashier/*.chaos.yaml`: failpoints in application code

`charge.chaos.yaml` (the failure lands and the service recovers: green), `charge-must-survive.chaos.yaml` (the same failure against a hypothesis the sample service cannot meet: red) and `charge-slow.chaos.yaml` (a slow hook). See [failpoints.md](failpoints.md) and the [README quick start](../../README.md#quick-start-one-green-run-one-red-run).

## Hub faults

### `http.latency`

Add delay in front of any local HTTP service: the story of `ignite.chaos.yaml` without the 500s unless you add `statusOverride`. Expect at least `delay` extra on requests through `listen`; stopping the fault unbinds the port. Works on macOS, Linux and Windows. Point probes at `listen`, not `upstream`.

### `http.status-inject`

Return a percentage of HTTP error codes: clients, retries and error budgets, without touching application code. Roughly `percent` of proxied responses are `code`. One proxy can do latency and status together when both params are set, as `ignite.chaos.yaml` does.

### `http.timeout`

Stall responses so you see client-side timeouts instead of slow but complete calls. Requests through the proxy hang until the client, or the `timeout` param, gives up. The report shows a hole in availability rather than a clean p95 shift.

### `process.pause`

Freeze a local PID (or a command match) and continue it. Reversible. Unix only: on Windows `validate` refuses it rather than passing. No `-yes`, but `-allow-pid` must name the PID it may signal.

### `process.kill`

Terminate a local process: `SIGTERM`, then `SIGKILL` after `sigkillAfter`. Destructive and not reversible. **Always needs `-yes` and `-allow-pid`.** Unix only. Do not run it against a process you cannot restart.

### `docker.pause`

Pause a named container (`docker pause`, then `unpause`) through the `docker` CLI. If `docker` is missing, `validate` fails clearly, not as a silent pass. Reversible; no `-yes`. **There is no allowlist of container names**: it pauses whatever the scenario names, so use a disposable container.

### `cpu.hog`

Burn CPU **inside the litmus-lite process** (bounded workers, at most 16) to see whether the box degrades under contention. Workers stop on rollback. It stresses the machine litmus-lite runs on, so it only affects an app that shares that machine.

### `memory.hog`

Hold a bounded block of memory (cap 256 MB) and release it. Memory returns on stop. The same caveat as `cpu.hog` applies.

### `disk.fill`

Write a **capped** file (at most 256 MB), then delete it: a "disk full" check without filling the disk. The file exists during the fault and is gone after. **Always needs `-yes`.** It refuses to overwrite an existing file and does not follow symlinks. Do not point it at a real volume you care about.

### `failpoint`

Arm a named hook in your own Go service for the inject window and disarm it on rollback. See [failpoints.md](failpoints.md).
