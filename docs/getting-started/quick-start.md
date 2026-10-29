# Quick start: one green run, one red run

Five minutes. You run two scenarios against `cashier`, a tiny sample service in the repository: one that passes and one that fails on purpose, so you see both verdicts and how to read them.

You need [the binary](install.md) and Go 1.22 or newer (to run the sample service). No load generator, Docker or network service is needed, and nothing leaves your machine.

## 1. Start the sample service

`cashier` serves `GET /charge` on `127.0.0.1:8091`. It is healthy until a scenario arms a named [failpoint](../guides/failpoints.md) in its code.

```bash
git clone https://github.com/litmuschaos/litmus-lite && cd litmus-lite
LITMUS_LITE_FAILPOINT=charge,charge-slow LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo \
  go run ./examples/cashier
```

Leave it running and open a second terminal in the same clone.

## 2. Run the scenarios

=== "In Cursor"

    **Connect Cursor** (once per project):

    ```bash
    litmus-lite init
    ```

    Open the folder in Cursor, check **Settings > Tools & MCP** shows `litmus-lite` as on, and start a new Agent chat.

    **The green run.** Paste:

    !!! quote ""
        Using the litmus-lite tools, validate `examples/cashier/charge.chaos.yaml` with strict checking, run it, and tell me whether the failure landed and whether the service recovered.

    The agent calls `validate_scenario`, then `run_test`, and answers from the two hypotheses in the file: the failure really landed (`hook-landed`: errors above 50% while charging fails for 8 seconds) and the service recovered within 5 seconds (`recovers`). The run passes.

    **The red run.** Paste:

    !!! quote ""
        Now run `examples/cashier/charge-must-survive.chaos.yaml` and tell me why it failed.

    This scenario injects the same failure but asks for something `cashier` does not do: keep errors under 5% while charging is down, as a service with a fallback would. The agent calls `run_test`, sees the run did not pass, and calls `diagnose_failure`. Expect it to say that `charge survives its own outage` failed with an error rate near 100%.

    !!! tip "A failed hypothesis is a finding, not an error"
        The run finished and measured what happened; the service just did not meet the bar. The agent gets this result the same way as a pass.

=== "In a terminal"

    **Check the first scenario** without touching anything:

    ```bash
    litmus-lite validate -strict examples/cashier/charge.chaos.yaml
    ```

    It prints `valid`.

    **The green run:**

    ```console
    $ litmus-lite run examples/cashier/charge.chaos.yaml
    cashier-charge passed=true hypotheses=2 report=report.json html=report.html
    ```

    The exit code is `0`.

    **The red run:**

    ```console
    $ litmus-lite run -out red.json examples/cashier/charge-must-survive.chaos.yaml
    cashier-charge-must-survive passed=false hypotheses=2 report=red.json html=red.html
    ```

    The exit code is `3`: the run finished and a hypothesis did not hold. Ask why:

    ```console
    $ litmus-lite diagnose red.json
    Hypothesis charge survives its own outage failed: error_rate < 5% (observed 100.00%).
    ```

    `diagnose` prints a few more lines about the fault and the recovery. The numbers differ a little from run to run.

## 3. Open the report

Each run writes `report.json` (for tools) and `report.html` (for you) in the folder you ran it from. Open `report.html` in a browser: it shows the probes' latency and error rate through the three phases (baseline, fault, recovery), the fault window, and each hypothesis with what was observed.

[Reading a report](../guides/reports.md) explains every part.

## What you just ran

The green scenario, in full:

```yaml title="examples/cashier/charge.chaos.yaml"
--8<-- "examples/cashier/charge.chaos.yaml"
```

The red one differs in two places: the probe also expects the body to contain `paid`, and the hypothesis asks for an error rate below 5% instead of above 50%.

[How a run works](../concepts/how-a-run-works.md) walks through what litmus-lite did with this file, phase by phase.

## Next

[Your first scenario](first-scenario.md): write a test of your own service.
