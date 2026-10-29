# Concepts

Four ideas carry the whole tool.

**A scenario is a file.** A `*.chaos.yaml` names the system under test, the faults to inject, the probes that watch it, and the hypotheses that must hold. It lives in your repository next to the service, like any other test. Whether you or an agent wrote it, it runs the same way.

**A run has three phases.** A baseline with no faults, an inject window with them, and a recovery window after. Probes sample throughout, so the report shows the service before, during and after. [How a run works](how-a-run-works.md).

**A verdict comes from hypotheses.** Not from a score. Each hypothesis is a plain statement, "the error rate stays under 5%", "it recovers within 5 seconds", and the run passes only when every one holds. [How a run is judged](hypotheses.md).

**Safety is opt-in per action.** Anything that leaves your machine, signals a process or cannot be undone needs a flag that a person gives. An agent cannot give it for you. [Safety](safety.md).

## Words used on this site

Scenario
:   A `*.chaos.yaml` file: one experiment.

Fault
:   Something litmus-lite does to the system for a fixed duration and then undoes: add latency, return errors, pause a process, arm a failpoint. [Fault reference](../reference/faults.md).

Probe
:   A check that litmus-lite runs repeatedly while the scenario runs: an HTTP request or a shell command, healthy or not.

Steady state
:   Probes checked once before anything is injected. If the service is not healthy to begin with, nothing is injected.

Hypothesis
:   A statement about what the probes saw: a metric, an operator and a value.

Report
:   `report.json` and `report.html`, written by every run.

Failpoint
:   A named hook in your own code that a scenario can arm, to fail one step from the inside. [Failpoints](../guides/failpoints.md).
