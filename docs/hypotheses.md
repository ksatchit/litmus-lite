# How a run is judged

A run is a scenario file, a set of probes sampling the service, and hypotheses about what the samples must show. This page says exactly what each of those means, so that a green run is one you can trust.

## Phases

`baseline` (steady), the faults (inject), then `recover`. Each probe sample belongs to the phase in which the sample **started**. A request that starts just before the faults arm and fails because of them is counted as steady; keep probe timeouts short relative to the phases.

## Probe modes

| `mode` | Samples |
| --- | --- |
| `continuous` (default for `probes`) | every `interval`, from the start of the baseline to the end of recovery |
| `on-chaos` | every `interval`, only while the faults are armed; the first sample is taken as soon as they arm, so a window shorter than the interval is still sampled |
| `SOT` | once, before anything is injected (`steadyState` probes are `SOT`); if it fails the run stops and no fault is started |
| `EOT` | once, after recovery. It is recorded in the report, and the phase summaries do not include it |

An unknown mode is an error.

## What a probe checks

An **HTTP probe** (`type: http`, the default) sends one request per sample and is healthy when the status is the expected one (`expect.status`, default 200) and every body assertion holds:

```yaml
probes:
  - name: charge
    type: http
    url: http://127.0.0.1:8091/charge
    expect:
      status: 200
      body:
        contains: paid                 # a string, or a list: every entry must be present
        notContains: ["error", "maintenance"]
        matches: '"id":\s*[0-9]+'      # a Go (RE2) regular expression, or a list
```

A gateway that answers `200` with an error page, or a cache that serves stale data, passes on status alone; the body assertions are for those. Notes:

- Only the first 1 MiB of a response is read and judged, and only when there is a body assertion.
- The status is judged first. When an assertion fails, the sample records which one (`body does not contain "paid"`) and never quotes the body, because a response can carry a token or personal data and the sample ends up in the report.
- An assertion that cannot work is an error when the file is loaded, never a skipped check: a value that is not a string or a list of non-empty strings, a regular expression that does not compile, or a key other than `contains`, `notContains` and `matches`.
- Probes follow redirects.

A **command probe** (`type: cmd`) runs `sh -c command` and is healthy on exit status 0 and, if `expect.contains` is set, when the output contains that text. It needs `sh`, so it does not run on Windows. `validate` and `run` list every command a scenario runs before they run it, and `validate -output json` returns them as `runsCommands`; see [SECURITY.md](../SECURITY.md).

## Hypotheses

```yaml
hypotheses:
  - name: checkout stays up
    metric: availability      # error_rate | availability | p50 | p95 | p99 | rps | recovery
    operator: ">="            # < <= > >=
    value: 99%
```

`error_rate`, `availability`, `p50`, `p95`, `p99` and `rps` are measured over the **inject** window, over all probes together. `recovery` is measured from the end of the faults.

`value` is read in the unit of the metric, and a value that cannot be read is an error when the file is loaded (`validate`, `run`), never a silent 0:

| metric | value | a bare number is |
| --- | --- | --- |
| `error_rate`, `availability` | a percentage, `5` or `5%`, from 0 to 100 | percent |
| `p50`, `p95`, `p99` | a duration, `500ms`, `1.5s`, `2m` | milliseconds |
| `recovery` | a duration, `500ms`, `5s`, `1m` | seconds |
| `rps` | a number | requests per second |

### No data, and not recovered

A hypothesis about a window with no samples is not a pass. It fails with `observed: "no data"` and `noData: true` in the report. If that is what you expect (a probe that only runs at the end, say), set `allowNoData: true` on the hypothesis.

`recovery` is the time from the end of the faults to the first of three healthy samples in a row (with fewer than three samples, all of them healthy). If the service is not healthy again by the end of the run, the hypothesis fails with `observed: "not recovered"`, whatever its limit and whether or not `allowNoData` is set. A recovery window shorter than the limit never satisfies `recovery <= 30s` on its own.

## Strict checking

Most keys in a scenario are read by name, so a misspelled one is ignored: `dealy: 800ms` injects no delay, `intervall: 50ms` changes nothing. litmus-lite warns about each key nothing reads, with the closest name it does read, when you `validate` or `run` (the warnings are also in the report). `-strict` turns the warnings into an error, before anything is injected:

```
litmus-lite validate FILE -strict
litmus-lite run FILE -strict
```

Values that cannot be read (`delay: 800` without a unit, `percent: lots`) are errors without `-strict`.

## Report format

`report.json` has a `schemaVersion` (currently 2). A report without it was written by an older litmus-lite and is read as version 1. Later versions only add fields. A litmus-lite refuses to read a report with a version newer than it knows, and says so.

| version | change |
| --- | --- |
| 1 | no `schemaVersion` |
| 2 | adds `schemaVersion`, and `noData` and `allowNoData` on each hypothesis |
