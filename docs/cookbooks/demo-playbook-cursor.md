# Demo playbook — Cursor-first (the wow)

This is the agent-native walkthrough. The terminal playbook is [`demo-playbook.md`](demo-playbook.md). Use **this** one in the room if you want the “ask Cursor, it writes the file and runs it” beat. Keep the terminal guide as backup if MCP misbehaves.

**Same machine rules:** LaunchPad already running (`engine` `:7070`, `missioncontrol` `:8080`). VegaLoad and Prometheus are optional. Do not open Grafana.

**Time:** 12–16 minutes if `init` was done in rehearsal.

## What “wow” means here

1. You type a sentence in Cursor chat. You do not type `litmus-lite run`.
2. The agent calls MCP tools that **exec that same CLI**.
3. A real `*.chaos.yaml` appears on disk (beside the service, not only in chat).
4. A self-contained `report.html` shows fault versus impact.
5. You can leave Cursor and re-run the file in a terminal. If you cannot, the demo failed the product thesis.

---

## Scenario catalog — intent and expectation

Read this once. In the room, name the intent in one sentence before the agent runs.

### `examples/launchpad/ignite.chaos.yaml` — HTTP latency + sparse 500s

| | |
| --- | --- |
| **Intent** | Prove LaunchPad’s list/ignite path stays usable when the HTTP hop in front of missioncontrol is slow and occasionally wrong. This is the inner-loop “slow dependency” test. |
| **How** | Userspace proxy on `:18080` → `:8080`. Inject 800ms delay, 100ms jitter, 5% HTTP 500, for 12s. Continuous GET `/api/launches` through the **proxy**. |
| **Expect** | SOT `/healthz` on `:8080` is fine. During inject, probe p95 rises toward ~800ms+jitter; error rate stays under 15%. After the proxy disarms, error rate returns to ~0 within 5s. Overlay: shaded inject band, p95 tracks delay. Prom down is a warning, not a fail. |
| **Fail looks like** | Flat p95 (probes still on `:8080`), or the run hanging (nothing listening on `:8080`). |

### `examples/launchpad/pause-engine.chaos.yaml` — freeze the engine process

| | |
| --- | --- |
| **Intent** | Prove what happens when the **dependency process** stops making progress (not the HTTP hop). Missioncontrol should show the outage; it should recover when the process is continued. |
| **How** | SIGSTOP the process whose command matches `engine` for 8s, then SIGCONT. The run takes `-allow-pid` of that engine PID and will not signal any other PID. Probes hit `:8080` `/api/engine/status` (not `/api/launches`, which is in-process). Probe timeout 800ms so a hung gRPC call becomes a failed sample. |
| **Expect** | SOT healthz passes. During inject, engine-status availability drops (502 or timeout). After 8s, samples succeed again; `recovery <= 8s`. |
| **Fail looks like** | Validate refuses the run when `-allow-pid` is missing. If the PID you named is a different `engine` (Cursor / another `go run`), abort and `kill -CONT` that pid. Do not “just kill it.” |

### Hub: `http.latency`

| | |
| --- | --- |
| **Intent** | Add delay in front of any local HTTP service. Same story as ignite, without the 5% 500s unless you add `statusOverride`. |
| **Expect** | Extra latency ≥ `delay` on requests through `listen`. Stop unbinds the port. Works on macOS / Linux / Windows. |

### Hub: `http.status-inject`

| | |
| --- | --- |
| **Intent** | Inject a percentage of HTTP error codes (clients, retries, error budgets) without touching app code. |
| **Expect** | Roughly `percent` of proxied responses are `code`. Can share one proxy with latency when both params are set (as ignite does). |

### Hub: `http.timeout`

| | |
| --- | --- |
| **Intent** | Stall responses so you see client-side timeouts instead of slow-but-complete calls. |
| **Expect** | Requests through the proxy hang until the client or `timeout` param gives up. Overlay shows a hole in availability, not a clean p95 shift. |

### Hub: `process.pause`

| | |
| --- | --- |
| **Intent** | Freeze a local PID (or command match) and unfreeze it. Reversible. Unix only. |
| **Expect** | Target stops progressing; SIGCONT restores it. Windows: validate error (not a fake pass). No `-yes`. Requires `-allow-pid` of the PID it may signal. |

### Hub: `process.kill`

| | |
| --- | --- |
| **Intent** | Terminate a local process (SIGTERM, then SIGKILL after `sigkillAfter`). Destructive. |
| **Expect** | Process is gone. **Always `-yes` and `-allow-pid`.** Do **not** run this in the live demo on a shared engine. Unix only; Windows validate error. |

### Hub: `docker.pause`

| | |
| --- | --- |
| **Intent** | Pause a named local container (`docker pause` / `unpause`). No SDK. |
| **Expect** | Container frozen, then running again. If `docker` is missing, validate fails clearly — not a silent skip-as-success. Reversible; no `-yes`. Skip live unless a throwaway container is already up. |

### Hub: `cpu.hog`

| | |
| --- | --- |
| **Intent** | Burn CPU **inside the litmus-lite process** (bounded workers, max 16) to see if the app or the box degrades under contention. |
| **Expect** | Workers stop on rollback. This does **not** stress LaunchPad’s engine unless they share a starved machine. Weak live wow; mention in hub list only. |

### Hub: `memory.hog`

| | |
| --- | --- |
| **Intent** | Allocate a bounded heap block (cap 256MB) and release it. |
| **Expect** | Memory returns on Stop. Same caveat as CPU hog: it stresses the injector host, not LaunchPad by default. |

### Hub: `disk.fill`

| | |
| --- | --- |
| **Intent** | Write a **capped** file (max 256MB) then delete it. Safety test for “disk full” without filling the disk. |
| **Expect** | File exists during inject, gone after. **Always `-yes`.** Refuses to overwrite a file that already exists. Do not demo on a real volume. |

---

## 0. Prep (before the audience)

1. LaunchPad up; `curl` `:8080/healthz` → 200.
2. Build CLI once:

```
cd /path/to/litmus-lite
go build -o litmus-lite ./cmd/litmus-lite
```

3. **Cursor workspace:** prefer a **multi-root** window (LaunchPad + litmus-lite) or open LaunchPad and point MCP at the binary you just built.

4. One-time, from LaunchPad (or litmus-lite) root:

```
/path/to/litmus-lite/litmus-lite init -editor cursor
```

Confirm `.cursor/mcp.json` (or `.cursor/mcp.json` merge) has `litmus-lite` → `mcp serve`, and `.cursor/rules/litmus-lite.mdc` exists. Reload MCP if the tools do not appear (`list_faults`, `run_test`, …).

5. Rehearse once: Agent mode, the Beat A prompt below, so you know tool latency (~15s for ignite).

Keep `report.html` openable as a file. Do not start VegaLoad/Prom unless you already rehearsed Beat E.

---

## 1. Thesis (20 seconds, then type)

**Say:** “I am not going to write the chaos CLI by hand. I am going to ask the agent. The contract is: it must leave a file on disk, and that file must run without the agent.”

Open Agent chat. Do not open a terminal except to show LaunchPad is already up.

---

## 2. Beat A — ask for the slow-API test (required)

**Paste (adjust the LaunchPad path):**

> LaunchPad is running on localhost:8080. Using litmus-lite MCP only: import `http.latency` beside `cmd/missioncontrol` (or use `examples/launchpad/ignite.chaos.yaml` in the litmus-lite repo). Validate it. Point probes at the proxy listen address, not :8080. Run it. Then diagnose the report and tell me whether p95 tracked the injected delay and whether we recovered.

**Intent / expect:** see catalog — ignite (or hub `http.latency`). You want the overlay, not a chat summary alone.

**Watch for (the wow):**

- Tool calls: `hub_import` or the agent opens `ignite.chaos.yaml`, then `validate_scenario`, then `run_test`, then `diagnose_failure` / `get_results`.
- A `*.chaos.yaml` in the tree (beside missioncontrol or the examples path).
- `report.json` / `report.html` on disk.
- Agent says PASS/FAIL with **hypothesis names**, and mentions the inject band.

**You do:** open `report.html` yourself. Point at shaded inject, p95 vs delay, recover. Say: “This file is the test. I can re-run it in CI without Cursor.”

**If the agent only describes a scenario and writes nothing:** stop. That violates P2. Ask: “Write the file, then call `validate_scenario`.”

**If it runs against `:8080`:** stop. Edit listen/probes to `:18080` and re-run.

---

## 3. Beat B — prove CLI ≡ MCP (30 seconds)

**Paste:**

> Show me `list_faults`. Then run `doctor`. Do not add any tool that is not a CLI command.

**Expect:** the same ids as `litmus-lite hub list`. Doctor may warn about VegaLoad/Prom — leave it.

**Say:** “If a feature only works in chat, that is a bug.”

Optional: split the editor and run `./litmus-lite hub list` in the terminal. Same names.

---

## 4. Beat C — ask for the frozen engine (required)

**Paste:**

> The LaunchPad engine process is on this machine. Find its PID (`pgrep -fl engine`). Validate and run `examples/launchpad/pause-engine.chaos.yaml` with `-allow-pid` set to that PID. Explain the overlay: I expect GET /api/engine/status to fail or time out while the engine is SIGSTOP’d, then recover after SIGCONT. Do not probe /api/launches for this (it does not call engine). Do not use process.kill. Do not pass -yes.

**Intent / expect:** see `pause-engine` in the catalog.

**Watch for:** `validate_scenario` + `run_test` on that file. Agent must **not** invent `process.kill`.

**You do:** `pgrep -fl engine` in a sidebar if match fails. If the agent paused the wrong process: `kill -CONT <pid>`, then give an explicit pid in the next message.

---

## 5. Beat D — compare (60 seconds, if time)

After Beat A, rename `report.json` → `baseline.json` (or ask the agent to). After a second run:

**Paste:**

> Compare `baseline.json` and `report.json` with `compare_reports`. Talk through steady / inject / recover. Do not invent a resilience score.

**Expect:** phase table, not a single number.

---

## 6. Optional Cursor beats

**Co-locate in LaunchPad:**  
> Import `http.timeout` beside `cmd/missioncontrol` as `ignite-timeout.chaos.yaml`. Do not run it yet. Show me the file.

**Intent:** stall vs delay — timeouts, not slowness. **Expect:** file on disk, `kind: http.timeout`, probes on `listen`.

**VegaLoad (only if `vegaload` is on PATH and you rehearsed):** see [`demo-playbook-vegaload.md`](demo-playbook-vegaload.md). Prompt:

> Validate and run `examples/launchpad/ignite-load.chaos.yaml`. Same `run_test` tool. Diagnose p95 vs 800ms and whether load RPS landed on the overlay.

**move-to (no vendor name):**  
> Run the CLI equivalent of move-to -adapter litmus on the ignite file and show that the document is an Experiment, not a ChaosEngine.

MCP has no `move-to` tool (CLI-only is fine). The agent should shell `litmus-lite move-to` or you run it in the terminal. **Say:** “move-to is generic; adapters are names.”

**Do not ask the agent to:** `process.kill` the engine, `disk.fill` `/`, or pause a production-like container.

---

## 7. Close

**Say:** “Two files you can keep: the chaos YAML and the HTML. The agent is optional the second time.”

Reset: restart LaunchPad if you paused engine; delete `report.json` / `report.html` if you do not want them in the next clone.

---

## Prompt card (print this)

```
1) Import/use ignite (or http.latency). Validate. Probes on :18080. Run. Diagnose overlay.
2) list_faults + doctor.
3) pause-engine with -allow-pid <engine pid>. No kill. No -yes. Explain availability drop + recover.
4) compare_reports baseline vs candidate. Phase table only.
```

## MCP ↔ CLI (if someone asks)

| MCP | CLI |
| --- | --- |
| `create_scenario` | `new` |
| `validate_scenario` | `validate` |
| `run_test` | `run` |
| `get_results` | read report JSON |
| `diagnose_failure` | `diagnose` |
| `list_faults` | `hub list` |
| `hub_import` | `hub import` |
| `compare_reports` | `compare` |
| `doctor` | `doctor` |
