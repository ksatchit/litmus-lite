# Demo playbook — LaunchPad + litmus-lite (terminal)

Repeatable **CLI** walkthrough. The Cursor-first wow (ask the agent, it writes the file) is [`demo-playbook-cursor.md`](demo-playbook-cursor.md). Load + chaos is [`demo-playbook-vegaload.md`](demo-playbook-vegaload.md). Scenario intent and expected outcomes live in the Cursor catalog plus the VegaLoad playbook; this file is the keyboard path.

Default machine: macOS or Linux, Go 1.22+, this repo and a LaunchPad checkout. VegaLoad and Prometheus are optional.

**Time:** 12–18 minutes if both repos are already cloned. **Do not** start the optional load or Prom beats until the HTTP overlay has landed.

## 0. Prep (before the audience is watching)

In two terminals, from the LaunchPad repo (`github.com/umamukkara/launchpad`):

```
# terminal A
go run ./cmd/engine

# terminal B
go run ./cmd/missioncontrol
```

Sanity:

```
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/healthz
curl -sS http://127.0.0.1:8080/api/launches | head
```

Expect `200` and a JSON list. If `engine` is down, ignite/list hangs or 5xx — fix that before you demo.

From the litmus-lite repo:

```
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite doctor
./litmus-lite validate examples/launchpad/ignite.chaos.yaml
```

`doctor` may warn that VegaLoad or Prom is missing. That is the point: chaos-only still runs.

Keep a browser tab ready for `report.html` (file://). Do not open Grafana.

## 1. The thesis (30 seconds)

Litmus-lite is chaos at the developer desk. The test is a `*.chaos.yaml` next to the service. The CLI works alone. MCP tools call that same CLI. The report is an overlay of **fault versus impact**, not a dashboard login.

## 2. Beat A — HTTP latency on LaunchPad (Phase 0, required)

**Intent:** slow + flaky HTTP hop in front of missioncontrol. **Expect:** p95 tracks ~800ms, errors under 15%, recover ≤ 5s. Full catalog: [Cursor playbook](demo-playbook-cursor.md#scenario-catalog--intent-and-expectation).

```
./litmus-lite run examples/launchpad/ignite.chaos.yaml
open report.html
```

What must be true:

- Probes hit **`:18080`** (the proxy), not `:8080`, during inject.
- Steady band: error ~0, p95 low.
- Inject band: p95 tracks ~800ms delay; a few 500s from `statusOverride`.
- Recover band: error back to 0.
- Hypotheses `errors-bounded` and `recovers` pass.
- If Prom `:9090` is down, the run still passes with a warning.

If p95 does not move, the client is still talking to `:8080`. Stop and fix that — do not improvise a new scenario on stage.

Optional one-liner after the HTML:

```
./litmus-lite diagnose report.json
```

## 3. Beat B — same file, agent path (30 seconds)

```
./litmus-lite mcp eval
./litmus-lite hub import http.latency -beside /tmp/lite-demo
```

Show that `mcp eval` exercises real CLI tools, and that import wrote a real file. Do not open a chat transcript as the source of truth.

## 4. Beat C — process.pause on engine (Phase 2, required)

**Intent:** engine process stops progressing. **Expect:** `/api/engine/status` fails/times out, then recovers after SIGCONT.

Confirm the engine process name (often `engine` when run as `go run ./cmd/engine`):

```
pgrep -fl engine
```

If the name is not `engine`, edit `examples/launchpad/pause-engine.chaos.yaml` `params.pid` to that PID, or set `params.command` to a unique substring from `pgrep`. Pass that same PID to `-allow-pid`. A command match may signal only a PID on that list; if the match is not the PID you named, the run fails before any signal.

```
PID=$(pgrep -n engine)
./litmus-lite validate examples/launchpad/pause-engine.chaos.yaml -allow-pid "$PID"
./litmus-lite run examples/launchpad/pause-engine.chaos.yaml -allow-pid "$PID"
open report.html
```

What must be true:

- SOT `healthz` passes (engine is up).
- During inject, `/api/engine/status` fails or times out (engine is SIGSTOP’d). `GET /api/launches` stays 200 — it does not call engine.
- After 8s, SIGCONT; recovery hypothesis passes.
- No `-yes` (pause is reversible). Do **not** run `process.kill` in the demo unless you have a disposable engine you can restart.
- `-allow-pid` is the PID you intend to freeze. The run will not signal any other PID.

If the run refuses the PID, abort. Show `validate` / the YAML; do not shotgun `kill`.

## 5. Beat D — compare two runs (Phase 1, 60 seconds)

Keep the HTML from Beat A as `baseline.json` (copy `report.json` after Beat A). After Beat C, or a second latency run:

```
cp report.json candidate.json   # if you just re-ran latency with a tweak
./litmus-lite compare baseline.json candidate.json
```

Talk to the phase table (steady / inject / recover), not a single score.

## 6. Optional beats (only if time and tools are already installed)

**VegaLoad:** stop here and switch to [`demo-playbook-vegaload.md`](demo-playbook-vegaload.md) (`ignite-load.chaos.yaml`). `load.tool` defaults to `vegaload`. The same block accepts `tool: k6` or `tool: command` (see that playbook); do not switch tools on stage. Do not uncomment `load:` on the chaos-only ignite file.

**move-to:** `./litmus-lite move-to -adapter litmus examples/launchpad/ignite.chaos.yaml` — show YAML, note there is no ChaosEngine string. `-adapter ir` is the vendor-neutral JSON.

**Hub list:** `./litmus-lite hub list` — HTTP, process, docker.pause, cpu.hog, memory.hog, disk.fill.

**Do not demo live:** `process.kill` on a shared engine, `disk.fill` on a real disk, or Docker pause unless a named throwaway container is already running.

## 7. Reset

```
# LaunchPad terminals: Ctrl-C, then start engine + missioncontrol again
rm -f report.json report.html baseline.json candidate.json junit.xml
```

## Faults you can import after the demo

```
./litmus-lite hub list
./litmus-lite hub import docker.pause -beside .
./litmus-lite hub import cpu.hog -beside .
./litmus-lite hub import memory.hog -beside .
./litmus-lite hub import disk.fill -beside .
# disk.fill and process.kill: run with -yes
# process.pause and process.kill: also pass -allow-pid <pid>
# disk.fill refuses to overwrite a file that already exists
```
