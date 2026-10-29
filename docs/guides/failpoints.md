# Walkthrough: failpoints in your own code (cashier)

A CLI walkthrough that needs no load generator, Prometheus or other app. It uses **cashier**, a small Go service in this repository that imports the failpoint package (`failpoint`, Go). It takes about 10 minutes; the run of the error hook itself is about 15 seconds (3 s steady, 8 s fault, 4 s recover). Works on macOS, Linux and Windows with Go 1.22+. The shortest path is the [quick start](../getting-started/quick-start.md), which runs the scenarios below.

## What this is

The HTTP and process faults inject from outside the service. A failpoint is a line in the service: `failpoint.Fail("charge")`. With the process started in window mode, that line does nothing until a scenario arms `charge`, and does nothing again after rollback. The report is the same: a shaded fault band, probe errors, recovery.

The hypotheses in these scenarios are that the hook **landed** (error rate in the inject window above 50%) and that **rollback disarmed** it (recovery within 5 s).

## How a hook becomes active

All four must be true. If one is missing, `/charge` stays `200` with `{"status":"paid"}`.

1. **Allowlist.** The cashier process is started with `LITMUS_LITE_FAILPOINT=charge,charge-slow`. A name that is not in the list cannot be armed.
2. **Token.** `LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo` on the process. The scenario sends the same string in `params.token` as `X-Litmus-Lite-Token`. An empty token rejects every arm.
3. **Window.** Cashier calls `failpoint.UseWindow` before it serves. An allowlisted name does not fire for the whole life of the process: hooks stay dark until armed.
4. **Armed for the fault window only.** The `failpoint` fault POSTs `{"name":"charge","armed":true}` to `params.url` when the fault starts, and `armed:false` on rollback. Baseline probes run before the POST and recover probes after the disarm.

`GET /charge` does two things, in order:

- `failpoint.Delay("charge-slow", 500ms)`. The 500 ms is in `examples/cashier/main.go`, not in the YAML.
- `failpoint.Fail("charge")`. When armed, the handler returns **503** with the body `litmus-lite failpoint charge`.

The control route is `POST /_litmus/failpoint`. Business traffic stays on `/charge` and `/healthz`. The control URL in the scenario must be loopback; anything else needs `-allow-target`.

The sample token is the string `cashier-demo`. It is written in the scenario, so it also appears under the fault's parameters in `report.json`. It authorizes a loopback arm endpoint for this sample and is not a credential for anything else. **Do not put a real secret in a scenario.**

`failpoint.Panic` exists for application code. Cashier does not call it: a panic exits the process, and the recover probes then have nothing to call. If litmus-lite is killed with `SIGKILL` during a run, the hook stays armed until the service restarts.

## Files

| File | What it does |
| --- | --- |
| `examples/cashier/main.go` | `GET /healthz`, `GET /charge` and the control route, on loopback `:8091` |
| `examples/cashier/charge.chaos.yaml` | Arms `charge` for 8 s. The probe expects 200. Hypotheses: error rate above 50% (the hook landed), recovery within 5 s. **Passes.** |
| `examples/cashier/charge-must-survive.chaos.yaml` | The same fault, but the hypothesis is that errors stay under 5% while charging is down, and the probe also checks the body (`body: { contains: paid }`). Cashier has no fallback, so this **fails**, exit code 3. |
| `examples/cashier/charge-slow.chaos.yaml` | Arms `charge-slow` for 6 s. Hypotheses: p95 above 200 ms, recovery within 5 s. |
| `failpoint/` | The Go package a service imports. The fault does not import it; it only POSTs. |

The hub id `failpoint` imports a template of the charge scenario (`litmus-lite hub import failpoint -beside DIR`).

## 0. Prepare

Terminal A, the app (from a clone of this repository):

```
export LITMUS_LITE_FAILPOINT=charge,charge-slow
export LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo
go run ./examples/cashier
```

The log must show the allowlist `charge,charge-slow`. If it says the token is empty, export it in **this** terminal. `CASHIER_ADDR` overrides the listen address and must stay on loopback (`127.0.0.1`, `localhost` or `::1`); the default is `127.0.0.1:8091`.

Terminal B, a sanity check. Do **not** POST `"armed":true` now: it would light the hook before the run, and the steady band would already be red.

```
curl -sS -o /dev/null -w "healthz %{http_code}\n" http://127.0.0.1:8091/healthz
curl -sS http://127.0.0.1:8091/charge; echo
curl -sS -o /dev/null -w "arm-without-token %{http_code}\n" \
  -X POST http://127.0.0.1:8091/_litmus/failpoint \
  -H 'Content-Type: application/json' \
  -d '{"name":"charge","armed":true}'
curl -sS -H "X-Litmus-Lite-Token: cashier-demo" http://127.0.0.1:8091/_litmus/failpoint; echo
```

Expect `healthz 200`, `{"status":"paid"}`, `arm-without-token 401` (the hook stays dark; the 401 is the check) and JSON with `"allowed":["charge","charge-slow"]` and `"armed":[]`. Then:

```
litmus-lite validate -strict examples/cashier/charge.chaos.yaml
litmus-lite validate -strict examples/cashier/charge-slow.chaos.yaml
litmus-lite validate -strict examples/cashier/charge-must-survive.chaos.yaml
```

All print `valid`. If port 8091 is taken, stop the other process.

## 1. The error hook (green)

`Fail("charge")` turns `GET /charge` into a 503 for the fault window, then back into a paid response. `charge.chaos.yaml`: the start-of-test probe is `/healthz`, the continuous probe is `/charge` expecting 200, and the fault arms `charge` for 8 s.

```
litmus-lite run examples/cashier/charge.chaos.yaml
```

Optionally, from a third terminal while the command runs (the fault is the middle 8 seconds):

```
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8091/charge
```

You should see `503` in that stretch and `200` again afterwards. Open `report.html` and look at:

1. The shaded fault band.
2. The **error-rate line** inside the band, near 100%, and near 0 before and after. That is the hook landing. The dashed intensity line is the latency reading of the HTTP faults; here it only marks the armed window.
3. The fault log: kind `failpoint`, rollback `ok`.
4. The hypotheses `hook-landed` and `recovers` both pass.

```
litmus-lite diagnose report.json
```

`diagnose` says the hook was armed and that probe p95 is not how this kind is judged, and prints the inject error rate. The sentence about timeouts is the generic line for any run with an error rate above 5%; here that rate is the result you wanted. The last line is the recovery time, well under 5 s.

## 2. The same failure against a hypothesis the service cannot meet (red)

```
litmus-lite run -out red.json examples/cashier/charge-must-survive.chaos.yaml
litmus-lite diagnose red.json
```

The exit code is 3 and the hypothesis `charge survives its own outage` fails with an observed error rate of about 100%. That is a finding: a service with a fallback for the charge step would keep errors low, and this one does not. A red run is the tool working.

## 3. The slow hook (optional)

Same process; do not restart cashier. This arms the **other** name, so `/charge` stays `200` and takes about 500 ms.

```
litmus-lite run examples/cashier/charge-slow.chaos.yaml
```

Look at p95 inside the band, about 500 ms. The error rate stays low because the handler still returns paid. `latency-landed` is p95 above 200 ms. The 500 ms is the `Delay` in the Go source; the YAML only names the hook.

## 4. From an agent

The files already exist; the agent runs them and does not invent a second tool.

> Using the litmus-lite MCP tools only: validate and run `examples/cashier/charge.chaos.yaml`. Cashier is already listening on 127.0.0.1:8091. Do not create a new scenario. After the run, diagnose `report.json`: did hook `charge` land (error rate in the fault window well above 50%), and did recovery bring `/charge` back? Probe p95 is not the landing signal for kind `failpoint`.

If the agent arms the hook by calling the control route with `armed:true` before `run`, stop it: that burns the steady band. See the [agent walkthrough](agent-walkthrough.md).

## When it does not look right

| What you see | What it means |
| --- | --- |
| `validate` or `run` stops on the steady-state `healthz` probe | Cashier is not listening. Start terminal A and `curl` `/healthz` before you retry. |
| The run fails at once with `401` or `failpoint token rejected` | The token in the environment and `params.token` differ, or the variable is unset in the cashier terminal. |
| The run fails with `not listed in LITMUS_LITE_FAILPOINT` | The hook name in the YAML is missing from the allowlist. Export `charge,charge-slow` and restart cashier. |
| The steady band already shows errors | Something armed `charge` before the run. GET the control route: `armed` must be `[]`. Restart cashier for a clean process. |
| The error rate stays about 0 and `hook-landed` fails | The probe is not `/charge`, or the arm POST never happened. Read the diagnose output and the fault log before you write new YAML. |
| `charge-slow` p95 stays flat | That run armed `charge` (503s, not latency), or the probe timed out under 500 ms. `charge-slow.chaos.yaml` uses a 2 s timeout and the hook name `charge-slow`. |
| The host is not loopback | Pass `-allow-target` for that host. The sample refuses a non-loopback `CASHIER_ADDR`. |
| Port in use | Run one cashier only. `8091` is chosen so it does not collide with an app on `8080`. |

## Reset

```
# terminal A: Ctrl-C
unset LITMUS_LITE_FAILPOINT LITMUS_LITE_FAILPOINT_TOKEN
rm -f report.json report.html red.json red.html
```

A new cashier process comes up dark: `armed` is empty and `/charge` returns paid.
