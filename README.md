# litmus-lite

Chaos testing for agentic developers. Ask your agent for a failure. It writes a `*.chaos.yaml` next to your service and runs it — on a laptop or in CI, with a self-contained overlay report. No control plane.

Litmus-lite is the chaos twin of [VegaLoad](https://github.com/vegaload/vegaload). Demos use [LaunchPad](https://github.com/umamukkara/launchpad). How to change the code: [`AGENTS.md`](AGENTS.md).

Apache-2.0. Intended home: `github.com/litmuschaos/litmus-lite`.

## Status

Phase 0 is runnable: HTTP-proxy faults, `process.kill` (`-yes`), probes, hypotheses, overlay HTML/JSON, ChaosHub import (embedded, plus optional git pin), CLI ≡ MCP, VegaLoad compose (exec), optional Prometheus, generic `move-to` IR and Litmus 4.0 file adapter (`-adapter harness` still stubbed).

## Install

```
git clone https://github.com/ksatchit/litmus-lite
cd litmus-lite
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite version
```

Requires Go 1.22+.

## Quick start (with LaunchPad)

Start LaunchPad, then:

```
./litmus-lite run examples/launchpad/ignite.chaos.yaml
open report.html   # or xdg-open / start
```

```
./litmus-lite init -editor cursor
./litmus-lite hub list
./litmus-lite doctor
./litmus-lite mcp eval
```

## Commands

`new`, `validate`, `run`, `diagnose`, `hub`, `init`, `doctor`, `compare`, `mcp serve`, `mcp eval`, `move-to`, `ci github`, `export job`.

Every MCP tool wraps one of these. The CLI always works alone.

Optional remote hub: write `.litmus-lite/hub.lock` with `repo`, `ref`, and optional `path`. Embedded catalog IDs win on collision. No pin means no git.
