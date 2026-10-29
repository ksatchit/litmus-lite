# Your first scenario

Now test your own service. This page assumes an HTTP service on `127.0.0.1:8080` with a `/healthz` route and an API route; change the addresses to yours.

A scenario answers one question: **when this fails, does that stay true?** "This" is a fault. "That" is one or more hypotheses about what the probes see.

## Write it

=== "In Cursor"

    In an Agent chat, in your project (after `litmus-lite init`):

    !!! quote ""
        Add 800 ms of latency to the API on port 8080 for 12 seconds and tell me whether it copes and how fast it recovers. Write the scenario next to the service.

    The agent calls `list_faults` and `hub_import` (or `create_scenario`), edits the file, then `validate_scenario` and `run_test`. Expect:

    - a new `*.chaos.yaml` beside your service, which you can open and read;
    - probes pointed at the proxy's `listen` address, not at your service, so the latency actually reaches them;
    - an answer in terms of the hypotheses it wrote: which held, which did not, and how long recovery took.

    If the agent only describes a scenario in the chat, ask it to write the file. The file is the test; the chat is not.

=== "In a terminal"

    Start from a template:

    ```console
    $ litmus-lite hub list
    http.latency        HTTP latency
    http.status-inject  HTTP status inject
    http.timeout        HTTP timeout
    ...
    $ litmus-lite hub import http.latency -beside ./checkout
    ```

    This writes `checkout/http-latency.chaos.yaml`. (`litmus-lite new -beside ./checkout -kind http.latency -name checkout-slow` does the same with a file name of your choosing.) Open it and edit the addresses.

## Read it

Whoever wrote it, this is the file. Click the numbers for what each part does.

```yaml title="checkout/http-latency.chaos.yaml"
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata:
  name: checkout-slow
target:
  kind: http
  baseUrl: http://127.0.0.1:8080 # (1)!
steadyState: # (2)!
  - name: healthz
    type: http
    url: http://127.0.0.1:8080/healthz
    expect: { status: 200 }
    mode: SOT
faults:
  - name: slow-api
    kind: http.latency # (3)!
    duration: 12s
    params:
      listen: "127.0.0.1:18080" # (4)!
      upstream: "127.0.0.1:8080"
      delay: 800ms
      jitter: 100ms
probes:
  - name: list
    type: http
    url: http://127.0.0.1:18080/api/orders # (5)!
    interval: 200ms
    mode: continuous
    expect: { status: 200 }
hypotheses: # (6)!
  - name: errors-bounded
    metric: error_rate
    operator: <
    value: "25%"
  - name: recovers
    metric: recovery
    operator: <=
    value: 5s
rollback: always # (7)!
```

1. The system under test. A loopback address runs without any flag; anything else needs `-allow-target HOST`. See [Safety](../concepts/safety.md).
2. Checked before anything is injected. If the service is not healthy to begin with, the run stops without touching it.
3. The fault. `http.latency` starts a small proxy inside litmus-lite that adds delay to every request sent through it. [All fault kinds](../reference/faults.md).
4. The proxy listens here and forwards to `upstream`, your service.
5. **The probe calls the proxy, not the service.** A probe that calls `:8080` directly never sees the fault, and the run tells you so with a warning.
6. What must stay true. Each hypothesis is judged on what the probes saw while the fault was on, plus how long the service took to recover after. [How a run is judged](../concepts/hypotheses.md).
7. Every fault is undone when the run ends, fails or is interrupted. `always` is the only value.

## Check it and run it

=== "In Cursor"

    !!! quote ""
        Validate `checkout/http-latency.chaos.yaml` with strict checking and run it.

    The agent runs `validate_scenario` with `strict`, then `run_test`, and reports the verdict, the report path and anything the run warned about.

=== "In a terminal"

    ```bash
    litmus-lite validate -strict checkout/http-latency.chaos.yaml
    litmus-lite run checkout/http-latency.chaos.yaml
    ```

    `-strict` turns every warning into an error: a key nothing reads, a probe that bypasses the proxy, a scenario with no hypotheses. Use it in CI.

    The exit code is the verdict: `0` passed, `3` a hypothesis did not hold. `litmus-lite diagnose report.json` says which, and why.

## Keep it

Commit the file next to the service it tests. It is now a regression test anyone can run, with or without an agent:

- change a number (a longer delay, a tighter bound) and run it again;
- run every scenario's `validate -strict` in CI: `litmus-lite ci github` writes the workflow ([guide](../guides/ci.md));
- compare two runs of it before and after a change: `litmus-lite compare before.json after.json`.

## Where next

- [HTTP faults](../guides/http-faults.md): latency, errors and timeouts in more depth.
- [Failpoints](../guides/failpoints.md): fail a specific step inside your own code.
- [Working with an agent](../guides/agent.md): prompts that work, and the rules the agent follows.
- [Scenario reference](../reference/scenario.md): every key.
