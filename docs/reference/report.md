# Report format

Every run writes `report.json` and, beside it, `report.html` (`-out` and `-html` choose the paths). `-junit PATH` also writes JUnit XML. [Reading a report](../guides/reports.md) explains how to use them; this page lists the fields.

## `report.json`

```json
{
  "schemaVersion": 2,
  "name": "cashier-charge-must-survive",
  "startedAt": "2026-10-29T13:58:34.98+05:30",
  "elapsed_ns": 15200000000,
  "passed": false,
  "recovery_ns": 199250833,
  "phases": [ ... ],
  "hypotheses": [ ... ],
  "faultLog": [ ... ],
  "probes": [ ... ],
  "timeseries": [ ... ],
  "observedVsInjected": [ ... ],
  "warnings": [ ... ]
}
```

| Field | Meaning |
| --- | --- |
| `schemaVersion` | the report format version, currently `2` |
| `name` | `metadata.name` of the scenario |
| `startedAt`, `elapsed_ns` | when the run started, and how long it took (nanoseconds) |
| `passed` | `true` when every hypothesis held |
| `recovery_ns` | time from the end of the faults to healthy again |
| `phases` | one summary per phase: `steady`, `inject`, `recover` |
| `hypotheses` | each hypothesis with its verdict |
| `faultLog` | each fault: `name`, `kind`, `start`, `end`, `intensity`, `rollback` (`ok` or `fail: ...`) and its parameters as written (`tunables`) |
| `probes` | every probe sample |
| `timeseries` | per-second buckets for the charts |
| `observedVsInjected` | whether each fault visibly landed |
| `warnings` | everything the run warned about |
| `load`, `loadGenerator` | the load generator's output, when `load` is set |
| `prometheus` | the Prometheus series, when `observability.prometheus` is set |

Durations whose key ends in `_ns` are nanoseconds.

### A phase

```json
{
  "name": "inject",
  "from": "2026-10-29T13:58:37.98+05:30",
  "to": "2026-10-29T13:58:45.98+05:30",
  "errorRate": 0.9756,
  "availability": 0.0244,
  "p50_ns": 279000,
  "p95_ns": 353875,
  "p99_ns": 421834,
  "rps": 5.12,
  "samples": 41,
  "failed": 40
}
```

`errorRate` and `availability` are fractions (0 to 1) here; hypotheses state them as percentages.

### A hypothesis

```json
{
  "name": "charge survives its own outage",
  "metric": "error_rate",
  "operator": "<",
  "limit": "5%",
  "observed": "97.56%",
  "passed": false
}
```

`observed` is `"no data"` with `"noData": true` when the window had no samples, and `"not recovered"` for a `recovery` hypothesis whose service never came back. `allowNoData` is copied from the scenario.

## Versions

A report without `schemaVersion` was written by an older litmus-lite and is read as version 1. Later versions only add fields. litmus-lite refuses to read a report newer than it knows, and says so.

| Version | Change |
| --- | --- |
| 1 | no `schemaVersion` |
| 2 | adds `schemaVersion`, and `noData` and `allowNoData` on each hypothesis |

## `report.html`

One self-contained file with no external requests: open it from disk or attach it to a CI run. Sections: the verdict, fault against impact (a chart of the injected intensity, p95 and error rate over time, with the inject window shaded), hypotheses, a phase comparison table, observed against injected, the fault log and a probe summary.

## JUnit XML

One `testcase` per hypothesis (class `litmus-lite.hypothesis`), with a `failure` that carries the metric, operator, limit and observed value. A scenario with no hypotheses is one test case for the whole run.

```xml
<testsuite name="cashier-charge-must-survive" tests="2" failures="1">
  <testcase name="charge survives its own outage" classname="litmus-lite.hypothesis">
    <failure message="error_rate &lt; 5%">observed 97.56%</failure>
  </testcase>
  <testcase name="recovers" classname="litmus-lite.hypothesis"></testcase>
</testsuite>
```
