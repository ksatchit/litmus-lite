# docs

- [Architecture](architecture.md): the parts, one run, what is trusted with what, and why it is built this way.
- [Internals](internals.md): for contributors: the engine, the fault contract, the safety and MCP checks, streaming, and a checklist for adding a fault.
- [How a run is judged](hypotheses.md): probes, modes, hypotheses and their units, recovery, strict checking, the report format.
- [Cookbooks](cookbooks/): walkthroughs. Start with [http-latency](cookbooks/http-latency.md); the others are a two-process app in a terminal, working with an agent, load plus chaos, failpoints, and a [fault catalog](cookbooks/fault-catalog.md).
- [Streaming a run](streaming.md): the event format, push, pull, `serve`, replay, and the optional live view with what leaves your machine.
- [Roadmap](ROADMAP.md): what is deliberately not scheduled yet.
- Project: [README](../README.md) · [SECURITY](../SECURITY.md) · [CONTRIBUTING](../CONTRIBUTING.md) · [GOVERNANCE](../GOVERNANCE.md) · [AGENTS](../AGENTS.md)
