# Walkthrough: load and chaos in one run

A scenario can start a load generator alongside the fault, so one run, one file and one report answer two questions: does it stay fast under traffic, and does it stay correct when something breaks? The generator is an external program litmus-lite runs: `vegaload` by default ([VegaLoad](https://github.com/vegaload/vegaload)), `k6`, or any program of yours (`command`). It is never imported as a library, and **a chaos-only run never needs one**: if the program is missing, the run goes on without load and says so.

Do the [terminal walkthrough](launchpad.md) first, so the chaos-only overlay works. This one takes 6 to 8 minutes more.

## The scenario

### `examples/launchpad/ignite-load.chaos.yaml`

| | |
| --- | --- |
| **Intent** | The same slow and flaky HTTP hop as `ignite.chaos.yaml`, plus synthetic users, so the report shows the load's requests per second inside the fault band. Composition happens in the one scenario file; there is no separate load-test file and no separate MCP tool. |
| **How** | The same proxy (`:18080` to `:8080`, 800 ms, 5% 500s, 12 s). `load.scenario` points at `launches.vl.js`. `tool` is omitted, so the run executes `vegaload` from your `PATH`. The users call `/healthz` and `/api/launches` on **`:18080`**. The load's requests per second and error rate are merged onto the chaos timeline at the inject offset; probe p95 stays the latency signal. |
| **Expect** | The chaos hypotheses still pass (errors under 25%, recovery within 5 s). The HTML shows probe p95 tracking the delay **and** a throughput line during the fault. `report.json` has a `load` section. Prometheus stays optional. |
| **Failing looks like** | The warning `vegaload not on PATH; running chaos only`: you are back to the chaos-only run, with no load. Or healthy requests per second on `:8080` while p95 is flat: the users missed the proxy. Or the generator refused a host: keep the target on loopback, or pass `-allow-target HOST`. |

`examples/launchpad/launches.vl.js` is a small VegaLoad script that is a client of the **proxy**, not of `missioncontrol` directly. It must call `127.0.0.1:18080` only. `ignite.chaos.yaml` keeps its `load:` block commented, so a plain `run` never depends on a generator.

## 0. Prepare

```
which vegaload || echo "install vegaload first: this walkthrough needs it"
vegaload version
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/healthz    # LaunchPad up
litmus-lite doctor
litmus-lite validate examples/launchpad/ignite-load.chaos.yaml
```

`doctor` should show `vegaload` **ok**, not warn. Do one full run first to learn the flag names of your installed VegaLoad (`-vus`, `-duration`; `-allow-target` takes a host, and loopback needs none). If your build uses different flags, edit `load.args` in the scenario.

## 1. Run it

```
litmus-lite run examples/launchpad/ignite-load.chaos.yaml
```

Open `report.html` and look at:

1. The shaded inject window, as in the chaos-only run.
2. p95 rising with the 800 ms delay (the probes).
3. Requests per second during the fault (the generator's series, merged in).
4. The recover phase: probe errors fall, and the load process has exited.

Copy `report.json` if you want to `compare` it against a chaos-only run: more traffic, same hypotheses.

## 2. From an agent

> VegaLoad is on PATH. Using the litmus-lite MCP tools only: validate and run `examples/launchpad/ignite-load.chaos.yaml`. Do not invent a second tool. After the run, diagnose whether probe p95 tracked the 800 ms delay and whether load requests per second are on the overlay. If vegaload is missing, say so and continue chaos-only.

It should call `validate_scenario` and `run_test` on the same file; the warning in the output, if the generator failed, should not be hidden. See [with-an-agent.md](with-an-agent.md).

## Rules that keep it honest

- Do not send the users at `:8080` while probes use `:18080`.
- Do not require Prometheus to see load.
- `load.args` are passed to the generator as they are, so `-allow-target` cannot check what the generator calls.
- `load.tool: command` runs a program with your permissions. `validate` and `run` list it first; read a scenario you did not write.

## Other generators

`load.tool` chooses the program. Omit it for VegaLoad. `doctor -file FILE` checks the tool that file names; `doctor` alone reports whether `vegaload` is on `PATH`.

**k6.** litmus-lite adds `--out json=<file>` and turns `http_reqs` and `http_req_failed` into one-second requests per second and error rate. Point the script at the proxy.

```yaml
load:
  tool: k6
  scenario: ./script.js
  args: ["--vus", "5", "--duration", "12s"]
```

**Your own program.** `command` is the executable. An optional `scenario` is appended as the last argument. It must print only this JSON on stdout:

```yaml
load:
  tool: command
  command: ./scripts/our-load
  args: ["--rps", "50", "--duration", "12s"]
```

```json
{"time_series":[{"offset_s":0,"rps":10,"error_rate":0.01}]}
```

`offset_s` is seconds from when the generator started.

## Reset

```
rm -f report.json report.html
```

If a leftover proxy still holds `:18080`, the next run fails to listen: wait for the rollback or check `lsof -i :18080`.
