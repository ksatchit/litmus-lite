---
hide:
  - navigation
  - toc
---

<div class="ll-hero" markdown>

# litmus-lite

<p class="ll-tagline">Chaos testing for developers. Ask for it in Cursor, or run it from a terminal. One binary, scenarios as files, and a verdict you can trust.</p>

[Quick start](getting-started/quick-start.md){ .md-button .md-button--primary }
[Install](getting-started/install.md){ .md-button }

</div>

You describe a failure, what healthy looks like, and what must stay true. litmus-lite injects the failure, samples your service, judges the run against your hypotheses and writes one self-contained HTML report. There is no control plane, no cluster and no account.

=== "In Cursor"

    Ask the agent for the test you want:

    <div class="ll-prompt" markdown>
    Add 800 ms of latency to the checkout API for 12 seconds and tell me whether it copes and how fast it recovers.
    </div>

    It writes the scenario as a `*.chaos.yaml` file next to your service, validates it, runs it through litmus-lite's MCP tools, and answers from the hypotheses: which held, which did not, and how long recovery took.

=== "In a terminal"

    Write or import the scenario, then run it:

    ```bash
    litmus-lite hub import http.latency -beside ./checkout
    litmus-lite validate -strict ./checkout/http-latency.chaos.yaml
    litmus-lite run ./checkout/http-latency.chaos.yaml
    ```

    The exit code is the verdict: `0` every hypothesis held, `3` the run finished and one did not.

Either way, the test is the same file:

```yaml
faults:
  - { name: slow-checkout, kind: http.latency, duration: 12s, params: { listen: "127.0.0.1:18080", upstream: "127.0.0.1:8080", delay: 800ms } }
probes:
  - { name: checkout, type: http, url: http://127.0.0.1:18080/checkout }
hypotheses:
  - { name: errors stay bounded, metric: error_rate, operator: "<", value: 15% }
  - { name: recovers, metric: recovery, operator: "<=", value: 5s }
```

## Why litmus-lite

<div class="grid cards" markdown>

-   :material-file-document-outline:{ .lg .middle } **A test is a file**

    ---

    Scenarios are `*.chaos.yaml` files that live beside the service they test. Review them, commit them, run them in CI. Nothing exists only in a chat.

-   :material-robot-outline:{ .lg .middle } **Agent-native, not agent-mandatory**

    ---

    Cursor (or any MCP client) can write and run scenarios for you. Every MCP tool is a CLI command you can type, so the agent is optional the second time.

-   :material-scale-balance:{ .lg .middle } **Hypotheses, not thresholds**

    ---

    A run says which of your statements held: "errors under 5%", "healthy again within 10 s". No data is a failure, and a service that never came back fails as "not recovered".

-   :material-shield-check-outline:{ .lg .middle } **Safe by default**

    ---

    Loopback targets only, PIDs you name, `-yes` for anything destructive, and every fault rolled back on exit or Ctrl-C. An agent cannot grant itself those permissions.

-   :material-package-variant-closed:{ .lg .middle } **One static binary**

    ---

    Linux, macOS and Windows. No runtime, no daemon, no telemetry. Load generators (vegaload, k6 or your own command) are run, not linked.

-   :material-chart-timeline-variant:{ .lg .middle } **A report you can read**

    ---

    One HTML file with the probes' latency and errors across baseline, fault and recovery, plus JSON for tools and JUnit for CI.

</div>

## Where to go next

<div class="grid cards" markdown>

-   **New here?** Start with the [quick start](getting-started/quick-start.md): one green run and one red run against a sample service, in five minutes.
-   **Testing your own service?** [Your first scenario](getting-started/first-scenario.md) walks through writing one, then the [guides](guides/index.md) cover each kind of fault.
-   **Want to know what a pass means?** Read [how a run is judged](concepts/hypotheses.md).
-   **Looking something up?** The [reference](reference/index.md) has every scenario field, fault parameter, CLI flag and MCP tool.

</div>
