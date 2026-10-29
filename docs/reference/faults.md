# Faults

Every fault kind, its parameters, and what a run of it looks like. `litmus-lite hub list` prints the template ids (they are the kind names); `litmus-lite hub import ID -beside DIR` writes a template to edit.

Every fault has the same three keys plus `params`:

```yaml
faults:
  - name: slow-api          # yours; appears in the report
    kind: http.latency      # one of the kinds below
    duration: 12s           # how long it stays on; over 60s needs -yes
    params: { ... }
```

| Kind | What it does | Undone by | Needs | OS |
| --- | --- | --- | --- | --- |
| [`http.latency`](#httplatency) | delays requests through a proxy | stopping the proxy | | all |
| [`http.status-inject`](#httpstatus-inject) | answers a share of requests with an error status | stopping the proxy | | all |
| [`http.timeout`](#httptimeout) | stalls requests through a proxy | stopping the proxy | | all |
| [`failpoint`](#failpoint) | arms a named hook in your code | disarming it | | all |
| [`process.pause`](#processpause) | `SIGSTOP`, then `SIGCONT` | `SIGCONT` | `-allow-pid` | Linux, macOS |
| [`process.kill`](#processkill) | `SIGTERM`, then `SIGKILL` | not reversible | `-allow-pid`, `-yes` | Linux, macOS |
| [`docker.pause`](#dockerpause) | `docker pause` | `docker unpause` | the `docker` CLI | all |
| [`cpu.hog`](#cpuhog) | burns CPU in the litmus-lite process | ending the fault | | all |
| [`memory.hog`](#memoryhog) | holds memory in the litmus-lite process | ending the fault | | all |
| [`disk.fill`](#diskfill) | writes a capped file | deleting it | `-yes` | all |

A parameter key nothing reads is a warning (an error with `-strict`). A value that cannot be read, such as `delay: 800` without a unit, is always an error.

## HTTP faults

The three HTTP kinds start a reverse proxy inside litmus-lite. It listens on `listen` and forwards to `upstream`. The proxy starts before the baseline, passes traffic through untouched, applies the fault only during the inject window, and stops at the end of the run.

!!! warning "Only traffic sent to `listen` is affected"
    Point your probes, your load generator or your client at the `listen` address. A probe that calls the service directly never sees the fault. `validate` warns when a probe bypasses the proxy, and `run` warns if the proxy saw no requests at all.

The proxy speaks HTTP/1.1 over plain TCP. gRPC, cleartext HTTP/2 (h2c) and an `https://` upstream are not supported yet.

Parameters shared by all three:

| Param | Required | Meaning |
| --- | --- | --- |
| `listen` | yes | `host:port` the proxy binds. Keep it on loopback. |
| `upstream` | yes | `host:port` or `http://host:port` of your service. A non-loopback upstream needs `-allow-target`. |
| `delay` | | a duration added to every request, such as `800ms` |
| `jitter` | | up to this much more, at random, per request |
| `timeout` | | hold each request this long before answering |
| `statusOverride` | | `{ percent: 5, code: 500 }`: answer this share of requests with this status |

One proxy can combine them, for example latency with sparse 500s.

### `http.latency`

Adds `delay` (plus up to `jitter`) to every request through the proxy. **Expect** p95 and p99 to rise by about the delay during the inject window and fall back after. **Failing looks like** a flat p95, which usually means the probes are not going through `listen`.

```yaml title="latency with sparse 500s, from examples/launchpad/ignite.chaos.yaml"
faults:
  - name: slow-api
    kind: http.latency
    duration: 12s
    params:
      listen: "127.0.0.1:18080"
      upstream: "127.0.0.1:8080"
      delay: 800ms
      jitter: 100ms
      statusOverride: { percent: 5, code: 500 }
probes:
  - name: list
    url: http://127.0.0.1:18080/api/launches   # through the proxy
```

### `http.status-inject`

Answers `percent` of requests (default 100) with `code` (default 500) instead of forwarding them. Use it to test retries, fallbacks and error budgets without touching application code.

| Param | Default | Meaning |
| --- | --- | --- |
| `percent` | `100` | share of requests to fail, 0 to 100 |
| `code` | `500` | the HTTP status to return |

### `http.timeout`

Holds each request for `timeout` (default: the fault's `duration`) before forwarding it, so clients see their own timeouts rather than slow but complete calls. **Expect** a drop in availability rather than a clean latency shift. Give your probes a `timeout` shorter than the stall.

## `failpoint`

Arms a named hook in your own service for the inject window and disarms it on rollback. The service must use the Go [`failpoint`](https://pkg.go.dev/github.com/litmuschaos/litmus-lite/failpoint) package and list the name in `LITMUS_LITE_FAILPOINT`. [Failpoints guide](../guides/failpoints.md).

| Param | Required | Meaning |
| --- | --- | --- |
| `url` | yes | the service's failpoint endpoint, such as `http://127.0.0.1:8091/_litmus/failpoint` |
| `name` | yes | the hook to arm |
| `token` | yes | must match the service's `LITMUS_LITE_FAILPOINT_TOKEN` |

If litmus-lite is killed outright while a failpoint is armed, it stays armed until the service restarts.

## Process faults

Both signal one local process, named by `pid` or found by `command`. The PID must be listed in `-allow-pid`; PIDs 0 and 1 and litmus-lite's own PID are always refused. Linux and macOS only: on Windows, `validate` refuses them.

| Param | Meaning |
| --- | --- |
| `pid` | the process ID to signal |
| `command` | a process name to look up instead (exact or substring). The match must leave exactly one PID that is also in `-allow-pid`. |

### `process.pause`

Sends `SIGSTOP` for the duration, then `SIGCONT`. The process stops making progress without losing state, like a long GC pause or a hung dependency. **Expect** calls to it to time out (set a probe `timeout`) and to succeed again once it continues.

If litmus-lite itself is killed with `SIGKILL` during the pause, resume the process with `kill -CONT PID`.

### `process.kill`

Sends `SIGTERM`, then `SIGKILL` after `sigkillAfter` (default `2s`; `0` sends `SIGKILL` at once). **Not reversible: it always needs `-yes`.** Use it to check that a supervisor restarts the process, and never against something you cannot restart.

| Param | Default | Meaning |
| --- | --- | --- |
| `sigkillAfter` | `2s` | the grace period between `SIGTERM` and `SIGKILL` |

## `docker.pause`

Runs `docker pause` on a container for the duration, then `docker unpause`. Needs the `docker` CLI; without it, `validate` fails.

| Param | Meaning |
| --- | --- |
| `name` | the container name |
| `id` | or its ID |

**There is no allowlist of containers**: it pauses whatever the scenario names. Use it on disposable containers. If litmus-lite is killed outright, run `docker unpause NAME`.

## Resource faults

These stress the machine litmus-lite runs on, from inside the litmus-lite process, so they only affect a service that shares that machine.

### `cpu.hog`

| Param | Default | Meaning |
| --- | --- | --- |
| `workers` | `1` | busy goroutines, at most 16 |

### `memory.hog`

| Param | Default | Meaning |
| --- | --- | --- |
| `size` | `32MB` | memory to hold, at most `256MB` (units `B`, `KB`, `MB`) |

### `disk.fill`

Writes a file of `size` for the duration, then deletes it: a "disk filling up" check without filling the disk. **Always needs `-yes`.** It never overwrites an existing file, does not follow symlinks and refuses a filesystem root.

| Param | Default | Meaning |
| --- | --- | --- |
| `size` | `16MB` | at most `256MB`; `GB` is refused |
| `path` | a new file under the system temp directory | where to write it; `~` is expanded |

## Examples in the repository

| Scenario | What it shows |
| --- | --- |
| [`examples/cashier/charge.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/cashier/charge.chaos.yaml) | a failpoint that lands, and recovery (green) |
| [`examples/cashier/charge-must-survive.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/cashier/charge-must-survive.chaos.yaml) | the same failure against a bar the service cannot meet (red) |
| [`examples/cashier/charge-slow.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/cashier/charge-slow.chaos.yaml) | a slow hook instead of a failing one |
| [`examples/launchpad/ignite.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/launchpad/ignite.chaos.yaml) | latency and sparse 500s through a proxy |
| [`examples/launchpad/pause-engine.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/launchpad/pause-engine.chaos.yaml) | freezing a dependency process |
| [`examples/launchpad/ignite-load.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/launchpad/ignite-load.chaos.yaml) | latency under load from a load generator |

## Adding a fault kind

A fault is a plugin: an `Injector` registered by name. [Internals](../contributing/internals.md) shows how to add one.
