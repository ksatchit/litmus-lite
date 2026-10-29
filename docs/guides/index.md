# Guides

Task-focused pages. Each shows the agent-led way and the CLI way side by side.

## Working with litmus-lite

- **[Working with an agent](agent.md):** set up Cursor or another MCP client, prompts that work, what a good answer looks like, and the rules the agent follows.
- **[Reading a report](reports.md):** the HTML report, `diagnose`, and comparing two runs.
- **[Running in CI](ci.md):** validate every scenario on each pull request, and run chaos against a live target.

## Breaking things

- **[HTTP faults](http-faults.md):** latency, error responses and timeouts in front of a service.
- **[Failpoints](failpoints.md):** fail one step inside your own Go code.
- **[Processes and containers](processes-and-containers.md):** pause or kill a process, pause a container.
- **[CPU, memory and disk](resources.md):** contention on the machine your service runs on.
- **[Load and chaos together](load-and-chaos.md):** run a load generator through the fault, in one scenario.

## Going further

- **[Exporting a scenario](export.md):** to a neutral JSON form, a LitmusChaos experiment, or a Kubernetes Job.

## Walkthroughs

End to end, with a two-process sample app:

- **[In a terminal](launchpad.md):** latency, then freezing a dependency process.
- **[With an agent](agent-walkthrough.md):** the same, prompt by prompt, including consent.
