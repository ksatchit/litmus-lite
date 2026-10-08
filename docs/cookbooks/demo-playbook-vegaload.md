# Demo playbook — with VegaLoad (load + chaos)

Use this **after** the chaos-only overlay has landed ([terminal](demo-playbook.md) or [Cursor-first](demo-playbook-cursor.md)). The punchline is one file, one run, one report: VegaLoad asks “does it stay fast under traffic?”; litmus-lite asks “does it stay correct when something breaks?”

Do not lead the meeting with this beat. If VegaLoad is missing, chaos-only still runs — that is a feature, not a failure. This playbook is for when `vegaload` is already on PATH and you rehearsed once.

**Time:** 6–8 minutes on top of Beat A.

---

## Scenario — intent and expectation

### `examples/launchpad/ignite-load.chaos.yaml`

| | |
| --- | --- |
| **Intent** | Same slow/flaky HTTP hop as ignite, **plus** synthetic users so the overlay shows load RPS under the fault band. Proves composition: no third `*.rt.yaml`, no separate “resilience” MCP tool. |
| **How** | Identical proxy (`:18080` → `:8080`, 800ms, 5% 500s, 12s). `load.scenario` points at `launches.vl.js`. `litmus-lite run` execs `vegaload` from PATH (not a library). VUs GET `/healthz` and `/api/launches` on **`:18080`**. Load JSON RPS/error rate is merged onto the chaos timeseries at inject offset. |
| **Expect** | Chaos hypotheses still pass (errors under 15%, recover ≤ 5s). HTML shows probe p95 tracking delay **and** a throughput line during inject (VegaLoad RPS). `report.json` has a `load` blob. Prom still optional. |
| **Fail looks like** | Warning `vegaload not on PATH; running chaos only` — you are back on Beat A; do not pretend load landed. Or healthy RPS on `:8080` while p95 is flat — VUs missed the proxy. Or vegaload refused a non-allowlisted host — keep `-allow-target` and loopback. |

### `examples/launchpad/launches.vl.js`

| | |
| --- | --- |
| **Intent** | Tiny VegaLoad script that is a **client of the proxy**, not of missioncontrol directly. |
| **Expect** | `http.get` to `127.0.0.1:18080` only. If you point it at `:8080`, load will look fine while chaos probes (if on `:18080`) tell a different story — or vice versa. |

### Chaos-only `ignite.chaos.yaml`

Leave it as the no-VegaLoad default. Its `load:` block stays commented so `run` never depends on VegaLoad.

---

## 0. Prep

```
which vegaload || echo "install vegaload and stop — this playbook needs it"
vegaload version
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/healthz
# LaunchPad engine + missioncontrol already up
cd /path/to/litmus-lite
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite doctor
./litmus-lite validate examples/launchpad/ignite-load.chaos.yaml
```

`doctor` must show `vegaload` **ok**, not warn. Rehearse one full `run` so you know vegaload flag names on your installed version (`-vus`, `-duration`, `-allow-target`). If your VegaLoad build uses different flags, edit `load.args` in rehearsal, not on stage.

---

## 1. Thesis (15 seconds)

**Say:** “Chaos-only already showed the fault. Now we add traffic in the same file. One CLI. The agent does not get a richer `run_resilience_test` — `run` is enough because the YAML has `load:`.”

---

## 2. Terminal path

```
./litmus-lite run examples/launchpad/ignite-load.chaos.yaml
open report.html
```

**Point at:**

1. Shaded inject window (same as Beat A).
2. p95 rising with the 800ms delay (probes).
3. RPS / throughput during inject (merged VegaLoad series).
4. Recover: probe errors back down; load process has exited.

Copy `report.json` if you will `compare` against a chaos-only baseline — say “more traffic, same hypotheses,” not a score.

---

## 3. Cursor path (wow, if MCP is already live)

**Paste:**

> VegaLoad is on PATH. Using litmus-lite MCP only: validate and run `examples/launchpad/ignite-load.chaos.yaml`. Do not invent a second MCP tool. After the run, diagnose: did probe p95 track the 800ms delay, and do we have load RPS on the overlay? If vegaload is missing, say so and continue chaos-only — do not fail the product story.

**Watch for:** `validate_scenario` + `run_test` on **ignite-load** (same `run_test` as chaos-only). A warning in the tool output if VegaLoad failed; the agent should not hide it.

**If the agent uncomments `load:` on ignite.chaos.yaml instead:** fine, if `launches.vl.js` is resolved next to that file (`./launches.vl.js`). Prefer the committed `ignite-load.chaos.yaml` so you do not leave a dirty edit.

---

## 4. What you must not do

- Do not import VegaLoad as a Go library. Composition is exec + JSON merge.
- Do not add `run_resilience_test` to MCP.
- Do not send VUs at `:8080` while probes use `:18080`.
- Do not require Prometheus to “see” load. Grafana stays closed.
- Do not demo this as the first beat. Chaos-only is the wow for people without VegaLoad.

---

## 5. Reset

```
rm -f report.json report.html
# LaunchPad can stay up
```

If a leftover proxy still holds `:18080`, the next run will fail to listen — wait for rollback or check `lsof -i :18080`.
