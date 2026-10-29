# Reading a report

Every run writes two files in the folder you ran it from: `report.json` for tools and `report.html` for you. `-out results/checkout.json` puts them somewhere else (the HTML follows the JSON's name).

## Ask for the verdict

=== "In Cursor"

    The agent reads the report for you after `run_test`. To go deeper:

    !!! quote ""
        Diagnose `report.json`. Which hypothesis failed, by how much, and what should I look at first?

    The agent calls `diagnose_failure` (and `get_results` for the raw report) and answers in terms of the hypotheses and the phases.

=== "In a terminal"

    ```console
    $ litmus-lite diagnose red.json
    Run cashier-charge-must-survive failed hypotheses.
    Fault charge-fails (failpoint) injected hook armed. Probe p95 is not used to judge whether this kind landed.
    Error rate during inject is 97.6%. If this is unexpected, check timeouts, retries, and circuit breakers on the path under test.
    Recovery time-to-healthy: 199.250833ms.
    Hypothesis charge survives its own outage failed: error_rate < 5% (observed 97.56%).
    ```

## The HTML report

Open `report.html` in a browser. It is one file with no external requests, so you can attach it to a CI run or a pull request. From the top:

**The verdict.** Passed or failed, the scenario name and how long it took.

**Fault against impact.** A chart over the whole run: the injected intensity, the probes' p95 and their error rate. The inject window is shaded. If the fault landed, p95 or the error rate moves inside the band and comes back after it. A flat line inside the band means the fault did not reach the probes.

**Hypotheses.** One card per hypothesis: what it asked, what was observed, and whether it held.

**Phase comparison.** Samples, failures, error rate, availability, p50, p95 and requests per second for each phase. Compare the inject row with the steady row.

**Observed against injected.** For each fault, what was injected and what the probes saw.

**Fault log.** When each fault started and ended, its intensity, and whether its rollback succeeded.

## Warnings

A report keeps every warning the run printed. Read them before you trust a green run:

| Warning | It means |
| --- | --- |
| a key nothing reads | a typo; the setting did nothing. `-strict` makes it an error. |
| a probe bypasses the proxy | that probe never saw the HTTP fault |
| a fault saw no requests | nothing went through the proxy, so the hypotheses judged an undisturbed service |
| no hypotheses | nothing was judged; the run passes trivially |
| `prometheus: ...`, a load generator error | optional extras failed; the chaos result stands |

## Compare two runs

Keep the report from before a change and compare it with the one after:

=== "In Cursor"

    !!! quote ""
        Compare `baseline.json` and `report.json` phase by phase. Do not invent a score.

=== "In a terminal"

    ```bash
    litmus-lite run -out baseline.json checkout/http-latency.chaos.yaml
    # ... change the code ...
    litmus-lite run -out after.json checkout/http-latency.chaos.yaml
    litmus-lite compare baseline.json after.json
    ```

    ```text
    steady error_rate: baseline=0.0000 candidate=0.0000 ok
    steady p95_ms: baseline=12.0000 candidate=13.0000 ok
    inject error_rate: baseline=0.0200 candidate=0.1800 REGRESSED
    inject p95_ms: baseline=812.0000 candidate=1420.0000 REGRESSED
    ...
    compare: candidate is worse than baseline
    ```

A phase regressed when its error rate rose by more than one percentage point, or its p95 by more than 20%. `compare` exits `3` when any phase regressed, so it works as a CI gate.

The fields of `report.json` and the JUnit format are in the [report reference](../reference/report.md).
