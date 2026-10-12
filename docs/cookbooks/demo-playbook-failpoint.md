# Demo playbook — Mode B failpoints (cashier)

Standalone **CLI** walkthrough. LaunchPad is the Mode A target (HTTP proxy, process pause, load). This beat uses **cashier**, a small Go service in this repo. It imports the failpoint SDK. LaunchPad does not.

**Language:** Go. The SDK is package `failpoint`.

**Time:** about 10 minutes once `litmus-lite` is built and you have rehearsed the curls. The error-hook run itself is about 15 seconds (3s steady, 8s inject, 4s recover).

Default machine: macOS, Linux, or Windows. Go 1.22+. No VegaLoad, Prometheus, or LaunchPad.

## What you are showing

Mode A injects from outside the process. Mode B is a line in the application: `failpoint.Fail("charge")`. With the process started in window mode, that line does nothing until the scenario arms `charge`, and it does nothing again after rollback. The report is the same overlay: shaded inject band, probe errors, recovery.

The hypothesis on this beat is that the hook **landed** (inject error rate above 50%) and that **rollback disarmed** it (recovery within 5s). Say “hypothesis,” not “threshold.”

## How a hook becomes active

All four have to be true. Miss one and `/charge` stays `200` with `{"status":"paid"}`.

1. **Allowlist.** The cashier process is started with `LITMUS_LITE_FAILPOINT=charge,charge-slow`. A name absent from that list cannot be armed.
2. **Token.** `LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo` on the process. The scenario sends the same string in `params.token` as `X-Litmus-Lite-Token`. An empty token rejects every arm.
3. **Window.** Cashier calls `failpoint.UseWindow` before it serves. The allowlist does not fire for the whole process lifetime. Hooks stay dark until an arm.
4. **Arm for the inject window only.** Kind `failpoint` POSTs `{"name":"charge","armed":true}` to `params.url` when inject starts, and `armed:false` on rollback. Baseline probes run before that POST. Recover probes run after the disarm.

`GET /charge` does two things, in order:

- `failpoint.Delay("charge-slow", 500ms)` — the 500ms lives in `examples/cashier/main.go`, not in the YAML.
- `failpoint.Fail("charge")` — when armed, the handler returns **503** and the body `litmus-lite failpoint charge`.

The control route is `POST /_litmus/failpoint`. Business traffic stays on `/charge` and `/healthz`. The control URL in the scenario must be loopback. Anything else needs `-allow-target`.

The demo token is the string `cashier-demo`. It is written in the scenario, so it also shows up under that fault’s tunables in `report.json`. It authorizes a loopback arm endpoint for this sample. It is not a credential for anything else.

`failpoint.Panic` exists for application code. Cashier does not call it. A panic exits the process, and the recover probes then have nothing to call. The live beats are `Fail` and `Delay`.

## Files

| File | What it does |
| --- | --- |
| `examples/cashier/main.go` | `GET /healthz`, `GET /charge`, control route. Loopback `:8091`. |
| `examples/cashier/charge.chaos.yaml` | Arms `charge` for 8s. Probe expects 200. Hypothesis: inject error rate > 50%, recovery ≤ 5s. |
| `examples/cashier/charge-slow.chaos.yaml` | Arms `charge-slow` for 6s. Hypothesis: inject p95 > 200ms, recovery ≤ 5s. |
| `failpoint/` | SDK a Go service imports. The fault plugin does not import it; it only POSTs. |

Hub id `failpoint` imports a template of the charge scenario (`litmus-lite hub import failpoint -beside DIR`). The rehearsed files are the ones under `examples/cashier/`.

## 0. Prep (before anyone is watching)

From the litmus-lite repo:

```
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite doctor
```

`doctor` may warn that VegaLoad is missing. This beat does not use it.

Terminal A — the app:

```
export LITMUS_LITE_FAILPOINT=charge,charge-slow
export LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo
go run ./examples/cashier
```

The log line must show the allowlist `charge,charge-slow`. If it says the token is empty, stop and export it in **this** terminal. `CASHIER_ADDR` overrides the listen address and must stay on loopback (`127.0.0.1`, `localhost`, or `::1`). Default is `127.0.0.1:8091`.

Terminal B — sanity, from the litmus-lite repo. Do **not** POST `"armed":true` during prep. That would light the hook before the run and the steady band would already be red.

```
curl -sS -o /dev/null -w "healthz %{http_code}\n" http://127.0.0.1:8091/healthz
curl -sS http://127.0.0.1:8091/charge; echo
curl -sS -o /dev/null -w "arm-without-token %{http_code}\n" \
  -X POST http://127.0.0.1:8091/_litmus/failpoint \
  -H 'Content-Type: application/json' \
  -d '{"name":"charge","armed":true}'
curl -sS -H "X-Litmus-Lite-Token: cashier-demo" http://127.0.0.1:8091/_litmus/failpoint; echo
```

Expect:

- `healthz 200`
- `{"status":"paid"}`
- `arm-without-token 401` (the hook stays dark; the 401 is the check)
- JSON with `"allowed":["charge","charge-slow"]` and `"armed":[]`

Then:

```
./litmus-lite validate examples/cashier/charge.chaos.yaml
./litmus-lite validate examples/cashier/charge-slow.chaos.yaml
```

Both print `valid`. Keep a browser tab ready for `report.html` (file://).

If port 8091 is taken, stop the other process. Do not point this scenario at LaunchPad’s `:8080`.

## 1. Thesis (20 seconds)

**Say:** “The failure is a function in the service, and it is off until this file arms it. Same CLI, same report. The app opted in with an allowlist and a token. Rollback turns the hook back off.”

## 2. Beat — `charge` returns an error (required)

**Intent:** `Fail("charge")` turns `GET /charge` into a 503 for the inject window, then back into a paid response.

**How:** `examples/cashier/charge.chaos.yaml`. SOT is `/healthz`. Continuous probe is `/charge` expecting 200. The fault arms hook `charge` for 8s.

```
./litmus-lite run examples/cashier/charge.chaos.yaml
open report.html
```

Optional, from a third terminal **while the command is still running** (the inject window is the middle ~8 seconds):

```
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8091/charge
```

You want a `503` in that middle stretch, then `200` again after the command returns. If you only have two terminals, skip the live curl and use the HTML.

**Point at the HTML in this order:**

1. Shaded inject band.
2. **Error-rate line** inside the band, near 100%. Steady before it and recover after it are near 0. That is the hook landing. Do not wait for p95 to follow the dashed intensity line. That caption is the Mode A latency reading. Here the dashed line is only the arm window.
3. Fault log: kind `failpoint`, rollback `ok`.
4. Hypotheses `hook-landed` and `recovers` both pass.

Then:

```
./litmus-lite diagnose report.json
```

The diagnose line for this fault says the hook was armed, and that probe p95 is not how this kind is judged. It also prints the inject error rate. The sentence about timeouts is the generic line for any run whose inject error rate is above 5%. On this beat that rate is the result you wanted (`hook-landed`). The last line is recovery time, here well under 5s.

Copy `report.json` to `charge.json` if you want it after the next beat overwrites `report.json`.

## 3. Beat — `charge` slows down (optional, one extra run)

Same process. Do not restart cashier. This arms the **other** name, so `/charge` stays `200` and takes about 500ms.

```
./litmus-lite run examples/cashier/charge-slow.chaos.yaml
open report.html
```

**Point at:** p95 inside the shaded band, up around 500ms. Error rate stays low (the handler still returns paid). Hypothesis `latency-landed` is p95 > 200ms. After rollback, p95 falls back. The 500ms is the `Delay` in the Go source. The YAML only names the hook.

If you run this beat first by mistake, the error-hook story is still intact: restart is unnecessary as long as `armed` is `[]` again. Check with the tokened GET from prep.

## 4. Cursor path (if MCP is already live)

The file already exists. The agent runs it. It does not invent a second tool and it does not edit LaunchPad.

**Paste:**

> Using litmus-lite MCP only: validate and run `examples/cashier/charge.chaos.yaml`. Cashier is already listening on 127.0.0.1:8091. Do not edit LaunchPad. Do not create a new scenario. After the run, diagnose `report.json`: did hook `charge` land (inject error rate well above 50%), and did recovery bring `/charge` back? Probe p95 is not the landing signal for kind `failpoint`.

**Expect:** a real run of that file, then diagnose text. If the agent tries to arm the hook by curling `armed:true` before `run`, stop it. That burns the steady band.

## 5. When it does not look right

| What you see | What it means |
| --- | --- |
| `validate` or `run` exits on steady-state `healthz` | Cashier is not listening. Start terminal A and curl `/healthz` before you retry. |
| Run fails immediately with `401` / `failpoint token rejected` | Token env and `params.token` differ, or the token env is unset in the cashier terminal. |
| Run fails with `not listed in LITMUS_LITE_FAILPOINT` | The name in the YAML is missing from the allowlist. Export `charge,charge-slow` and restart cashier. |
| Steady band already shows errors | Something armed `charge` before the run. GET the control route; `armed` must be `[]`. Restart cashier if you need a clean process. |
| Inject error rate stays ~0 and `hook-landed` fails | The probe is not `/charge`, or the arm POST never happened. Diagnose and the fault log come before any new YAML. |
| `charge-slow` p95 stays flat | That run armed `charge` (503s, not latency) or the probe timed out under 500ms. `charge-slow.chaos.yaml` uses a 2s probe timeout and hook name `charge-slow`. |
| Host is not loopback | Pass `-allow-target` for that host. The sample refuses a non-loopback `CASHIER_ADDR`. |
| Port in use | One cashier only. `8091` is reserved for this beat so it does not collide with LaunchPad on `8080`. |

Leave `process.kill`, `disk.fill`, and LaunchPad’s process pause for their own playbooks. They are different faults.

## 6. Reset

```
# terminal A: Ctrl-C
unset LITMUS_LITE_FAILPOINT LITMUS_LITE_FAILPOINT_TOKEN
rm -f report.json report.html charge.json
```

Start cashier again, with both exports, before a second rehearsal. A new process comes up dark: `armed` is empty, `/charge` returns paid.
