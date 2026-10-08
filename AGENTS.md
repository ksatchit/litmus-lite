# AGENTS.md — litmus-lite

This file tells any AI coding agent how to work in this repository. Read it before making a change.

## What this project is

Litmus-lite is a thin, open-source chaos testing tool: a single static binary that runs declarative `*.chaos.yaml` scenarios on a laptop or in CI, writes a self-contained overlay report, and speaks MCP by wrapping its own CLI. It is VegaLoad’s twin for failure injection.

Licence: Apache-2.0.

## The one design principle that overrides everything else

Litmus-lite is agent-native. It is not agent-mandatory.

1. **Every test is a real file, never chat state.** Scenarios an agent creates must be written as `*.chaos.yaml` on disk.
2. **The CLI always works alone.** If a feature only works through an agent, that is a bug.
3. **MCP tools mirror CLI commands.** Every MCP tool must invoke the same CLI a human can type. Do not build a richer, agent-only path.

If a proposed change breaks any of these, stop and flag it.

## Language and runtime

- Core: Go. Single static binary. Cross-compiles to Linux, macOS, Windows.
- Scenarios: YAML (`*.chaos.yaml`). Do not introduce JavaScript as the primary chaos authoring format.
- VegaLoad composition execs the `vegaload` binary; do not import VegaLoad as a library.
- MCP is Go, subcommand `litmus-lite mcp serve`, not a separate Node process.

## Module boundaries

- **Engine** (`internal/engine`): orchestration only. No MCP, no HTML, no move-to adapters.
- **Faults** (`internal/faults`): plugins. Adding a kind must not edit the orchestrator beyond registration.
- **Probes** (`internal/probe`): HTTP and command.
- **Report** (`internal/report`): JSON + HTML from a `Result` value.
- **Hub** (`internal/hub`): embedded catalog; optional git pin via `.litmus-lite/hub.lock`.
- **MCP** (`internal/mcp`): execs CLI, parses `-output json`. Must not call engine functions directly.
- **Hosts / doctor** (`internal/hosts`, `internal/doctor`): `init` and `doctor` share host paths.
- **Compose** (`internal/compose`): VegaLoad CLI exec + JSON merge.
- **move-to** (`internal/moveto`): IR and adapters. Core engine has zero awareness of Litmus SaaS or Harness.

## Conventions

- `gofmt` before commit.
- Tests live next to the code (`_test.go`).
- Pass/fail predicates on a chaos run are **hypotheses**, not “thresholds” and not “resilience score.”
- Community-facing copy never requires a vendor name for `move-to`.
- Commit messages: one line, present tense, why not a command log.
- Do not add telemetry.

## Commands an agent should know

- `litmus-lite new -beside DIR` — write a co-located scenario.
- `litmus-lite validate FILE`
- `litmus-lite run FILE` — `-out`, `-allow-target`, `-yes`, `-output json`
- `litmus-lite diagnose FILE.json`
- `litmus-lite hub list` / `hub import ID -beside DIR` — embedded catalog; optional `.litmus-lite/hub.lock` adds a git pin (embedded IDs win)
- `litmus-lite init` / `doctor` / `mcp serve` / `mcp eval`
- `litmus-lite compare BASE CAND`
- `litmus-lite move-to -adapter ir|litmus`
- `go test ./...`

## What not to do

- Do not add MCP behaviour that has no CLI equivalent.
- Do not generate ChaosEngine / ChaosExperiment manifests as the supported export (use IR).
- Do not make Prometheus or VegaLoad required for a chaos-only run.
- Do not put scenarios only in chat or only under `.litmus-lite/` — co-locate `*.chaos.yaml`.
- Do not name the commercial SaaS in Litmus-org getting-started docs; adapters are `move-to -adapter <name>`.

## Local-only files (do not commit)

`docs/PRD.md`, `docs/context/`, and `examples/launchpad/UPSTREAM.md` are gitignored. If they exist on disk, treat them as private working notes. Never add them to the repository.
