# FAQ and troubleshooting

## Questions

**Do I need Cursor, or any agent?**
No. Every MCP tool runs a CLI command, and every scenario is a file the CLI runs on its own. The agent is the fastest way to write a scenario, not a requirement to run one.

**Do I need Docker, Kubernetes, Prometheus or a load generator?**
No. A chaos-only run needs the binary and the service you are testing. Each of those is optional and used only if a scenario asks for it.

**Does anything leave my machine?**
Not by default. There is no telemetry. litmus-lite talks to the addresses in your scenario, plus streaming destinations or a live-view service only if you configure one. [What leaves your machine](../concepts/safety.md#what-leaves-your-machine).

**Is a green run proof my service is resilient?**
It is proof that the hypotheses you wrote held while the fault was on. Read the warnings, check the fault landed (the chart in `report.html` moves inside the shaded band), and make the hypotheses say what you actually care about. [How a run is judged](../concepts/hypotheses.md).

**Why is there no resilience score?**
A single number hides what matters: which property broke, by how much, and for how long. Hypotheses say that in words you chose.

**Can I test a service that is not on my machine?**
Yes, with `-allow-target HOST`. HTTP faults still need traffic to go through the proxy on your machine, so they suit a client you run locally; failpoints work wherever the service can be reached.

**Does it work on Windows?**
Yes, for HTTP faults, failpoints, `docker.pause` and the resource faults. The process faults need Unix signals, and command probes need `sh`; use WSL for those.

**How does this relate to LitmusChaos?**
litmus-lite is the laptop-and-CI end of the same idea, under the same organisation. `move-to -adapter litmus` exports a scenario as a LitmusChaos experiment.

## Troubleshooting

**`listen 127.0.0.1:18080: address already in use`**
Something else (often a previous run you interrupted, or another scenario) holds the proxy's port. Pick another `listen` port, or stop the other process (`lsof -i :18080`).

**The run passed, but p95 did not move during the fault.**
The probes call the service directly instead of the proxy. Point the probe `url` at the fault's `listen` address. The run also warns: "bypasses the proxy" or "saw no requests while it was armed".

**`steady-state probe "healthz" failed` and nothing was injected.**
The service was not healthy before the run started, so litmus-lite stopped. Check it is running and the URL is right. A "connection refused" means nothing is listening on that address.

**A hypothesis failed with `observed: "no data"`.**
No sample fell in the window it judges: probes that only run at the start or the end, an `interval` longer than the fault, or every sample in another phase. Shorten the interval, or set `allowNoData: true` if that is expected.

**`recovery` failed with `observed: "not recovered"`.**
The probes were not healthy three times in a row before the run ended. Lengthen `recover`, or that is your finding.

**`host "x" is not loopback; pass -allow-target x`.**
Working as intended. Name the host if you mean it: `-allow-target x`.

**The agent says an argument "is refused".**
Working as intended: an agent cannot pass `yes`, `allow-target` or `allow-pid` on its own. Run the command it gives you, or enable that one argument with `litmus-lite init -env LITMUS_LITE_MCP_ALLOW=...` and reload the window.

**The `litmus-lite` MCP tools do not show up in Cursor.**
Check **Settings > Tools & MCP**, toggle `litmus-lite` on or reload the window. If you moved or upgraded the binary, run `litmus-lite init` again: the entry records its absolute path. `litmus-lite doctor` checks the setup.

**macOS says the binary "cannot be opened".**
It is quarantined because it was downloaded with a browser and is not notarized: `xattr -d com.apple.quarantine litmus-lite`.

**A `cmd` probe fails on Windows.**
Command probes run through `sh -c`. Use an HTTP probe, or run under WSL.

**A process stayed paused after I killed litmus-lite.**
`SIGKILL` cannot be caught, so no rollback ran: `kill -CONT PID`. For a container: `docker unpause NAME`. A failpoint stays armed until the service restarts.

Still stuck? [Open an issue](https://github.com/litmuschaos/litmus-lite/issues) with `litmus-lite version`, the scenario and the output.
