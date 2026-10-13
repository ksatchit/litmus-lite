# litmus-lite

Chaos testing for agentic developers. Ask your agent for a failure. It writes a `*.chaos.yaml` next to your service and runs it — on a laptop or in CI, with a self-contained overlay report. No control plane.

Litmus-lite is the chaos twin of [VegaLoad](https://github.com/vegaload/vegaload). Demos use [LaunchPad](https://github.com/umamukkara/launchpad). How to change the code: [`AGENTS.md`](AGENTS.md).

Apache-2.0. Intended home: `github.com/litmuschaos/litmus-lite`.

## Status

Mode A Phase 0–2 is runnable: HTTP-proxy faults, process pause/kill, docker.pause, cpu/memory hog, disk.fill (capped, `-yes`, refuses to overwrite an existing file), overlay HTML/JSON, optional JUnit, ChaosHub (embedded + git pin), CLI ≡ MCP, load compose (`vegaload` by default, or `k6` / `command`), optional Prom, `move-to` IR and Litmus 4.0 file adapter (`-adapter harness` still stubbed). `process.pause` and `process.kill` signal only PIDs passed with `-allow-pid`. Mode B is a Go failpoint SDK: [`examples/cashier`](examples/cashier/) stays healthy until a `failpoint` scenario arms a named hook. Demo: [terminal](docs/cookbooks/demo-playbook.md) · [Cursor-first](docs/cookbooks/demo-playbook-cursor.md) · [VegaLoad](docs/cookbooks/demo-playbook-vegaload.md) · [failpoints](docs/cookbooks/demo-playbook-failpoint.md).

## Install

### Homebrew (macOS and Linux)

```
brew install --cask ksatchit/tap/litmus-lite
litmus-lite version
```

Upgrade when a new release is out, or remove:

```
brew update && brew upgrade --cask litmus-lite
brew uninstall --cask litmus-lite
```

Notes:

- It is a cask, so `--cask` is required. The tap (`ksatchit/homebrew-tap`) is added automatically on first install.
- Binaries are not signed or notarized; the cask clears the macOS quarantine flag on install so Gatekeeper does not block it.
- A local `go build` in the repo (`./litmus-lite`) does not conflict with the brew copy. `which -a litmus-lite` shows which one is on your `PATH`.
- `litmus-lite init` records the absolute path of the binary that ran it in the editor's MCP config. Re-run `init` with the brew copy to switch a project over; the brew path stays the same across upgrades.

### Prebuilt archives

Linux, macOS, and Windows archives are on the [releases page](https://github.com/ksatchit/litmus-lite/releases).

### From source

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
