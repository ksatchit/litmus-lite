# Scenario file

A scenario is a YAML file named `*.chaos.yaml`. This page lists every key. For what probes and hypotheses *mean* when a run is judged, see [How a run is judged](../concepts/hypotheses.md).

```yaml
apiVersion: litmus-lite.io/v1   # optional; the only version
kind: Scenario                  # optional
metadata:
  name: checkout-slow           # required
target:
  kind: http
  baseUrl: http://127.0.0.1:8080
baseline: 3s
recover: 5s
steadyState: [ ... ]            # probes, checked once before any fault
faults: [ ... ]                 # at least one
probes: [ ... ]                 # at least one probe here or in steadyState
hypotheses: [ ... ]
load: { ... }                   # optional
observability: { ... }          # optional
rollback: always
```

A key nothing reads is a warning with the closest name that exists (`intervall` → `interval`). `-strict` makes it an error.

## Top level

| Key | Default | Meaning |
| --- | --- | --- |
| `apiVersion` | `litmus-lite.io/v1` | the format version |
| `kind` | `Scenario` | |
| `metadata.name` | required | the run's name in reports and output |
| `target.kind` | | what the target is, such as `http` |
| `target.baseUrl` | | the service under test. A non-loopback host needs `-allow-target`. |
| `baseline` | `3s` | how long probes sample before any fault |
| `recover` | `5s` | how long probes sample after every fault is undone |
| `rollback` | `always` | the only accepted value: every fault is always undone |

## `faults`

| Key | Required | Meaning |
| --- | --- | --- |
| `name` | yes | your name for it, shown in reports |
| `kind` | yes | a fault kind: see [Faults](faults.md) |
| `duration` | yes | a Go duration, such as `12s` or `1m`. Over `60s` needs `-yes`. |
| `params` | | kind-specific: see [Faults](faults.md) |

All faults start together. The inject window lasts as long as the longest one.

## `probes` and `steadyState`

Both are lists of probes. `steadyState` probes default to `mode: SOT`: they run once before anything is injected, and a failure stops the run.

| Key | Default | Meaning |
| --- | --- | --- |
| `name` | | shown in the report |
| `type` | `http` | `http`, or `cmd` (also `command`) |
| `url` | | for `http`: the URL to call. Through the proxy's `listen` address for HTTP faults. |
| `method` | `GET` | for `http` |
| `command` | | for `cmd`: run with `sh -c` |
| `interval` | `200ms` | time between samples |
| `timeout` | `2s` | a sample that takes longer fails |
| `mode` | `continuous` | `continuous`, `on-chaos`, `SOT` or `EOT`: [modes](../concepts/hypotheses.md#probe-modes) |
| `expect` | | what a healthy sample looks like |

`expect` for an HTTP probe:

```yaml
expect:
  status: 200                      # default 200
  body:                            # optional; judges the first 1 MiB
    contains: paid                 # a string, or a list: all must be present
    notContains: [error, maintenance]
    matches: '"id":\s*[0-9]+'      # a Go (RE2) regular expression, or a list
```

`expect` for a command probe: exit status 0 is healthy, and `contains: TEXT` also requires that text in the output.

## `hypotheses`

```yaml
hypotheses:
  - name: errors stay bounded
    metric: error_rate
    operator: "<"
    value: 5%
    allowNoData: false
```

| Key | Meaning |
| --- | --- |
| `name` | how the verdict refers to it |
| `metric` | `error_rate`, `availability`, `p50`, `p95`, `p99`, `rps`, or `recovery` |
| `operator` | `<`, `<=`, `>` or `>=`. Quote it in YAML. |
| `value` | in the metric's unit: a percentage, a duration or a number. [Units](../concepts/hypotheses.md#hypotheses). |
| `allowNoData` | pass, rather than fail, when the window had no samples |

A scenario without hypotheses runs, but nothing is judged, so it passes trivially; `validate` warns about it, and `-strict` refuses it.

## `load`

Runs an external load generator alongside the fault, and merges its series into the report. Optional: a chaos-only run needs none. [Load and chaos guide](../guides/load-and-chaos.md).

| Key | Default | Meaning |
| --- | --- | --- |
| `tool` | `vegaload` | `vegaload`, `k6` or `command` |
| `start` | `inject` | `inject` (with the fault) or `baseline` (with the probes) |
| `scenario` | | the generator's script, relative to the scenario file. Required for `vegaload` and `k6`. |
| `command` | | for `tool: command`: the program to run. It prints a normalized time series as JSON. |
| `args` | | extra arguments passed to the tool as is |

The run ID is exported to the generator as `LITMUS_LITE_RUN_ID`.

## `observability`

```yaml
observability:
  prometheus:
    url: http://127.0.0.1:9090
```

After the run, litmus-lite asks that Prometheus for `rate(http_requests_total[5s])` over the run's time range, one point per second, and keeps the series in the report. If Prometheus is unreachable, the run warns and goes on. Optional.

`prometheus.queries` is accepted but not used yet: the query above is fixed.

## Validation

`litmus-lite validate FILE` (or the `validate_scenario` MCP tool) checks all of this without touching anything. It fails on:

- a missing `metadata.name`, no fault, or no probe;
- a fault without `name`, `kind` or a valid `duration`;
- an unknown fault kind, probe type, probe mode, metric or operator;
- a `value` that cannot be read in its metric's unit;
- a body assertion that cannot work (a bad regular expression, an unknown key);
- a fault that does not run on this OS (the process faults on Windows);
- the safety rules: see [Safety](../concepts/safety.md).

And it warns, or fails with `-strict`, on keys nothing reads, probes that bypass an HTTP fault's proxy, and a scenario with no hypotheses.
