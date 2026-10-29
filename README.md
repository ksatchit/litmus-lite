<p align="center">
  <img src="docs/assets/pip.svg" alt="Pip, the litmus-lite mascot" width="160">
</p>

<h1 align="center">litmus-lite</h1>

<p align="center">
  <a href="https://litmuschaos.github.io/litmus-lite/"><b>Documentation</b></a> ·
  <a href="https://litmuschaos.github.io/litmus-lite/getting-started/quick-start/">Quick start</a> ·
  <a href="https://github.com/litmuschaos/litmus-lite/releases">Releases</a>
</p>

Chaos testing you ask for in [Cursor](https://cursor.com). Tell the agent what should break and what must stay true:

> Add 800 ms of latency to the checkout API for 12 seconds and tell me whether it copes and how fast it recovers.

The agent writes the scenario as a `*.chaos.yaml` file next to your service, validates it, runs it through litmus-lite's MCP tools, and answers in terms of the hypotheses it wrote: which held, which did not, and how long recovery took. litmus-lite injects the failure, samples your service, judges the run and writes one self-contained HTML report. It is a single binary. There is no control plane, no cluster and no account.

```yaml
faults:
  - { name: slow-checkout, kind: http.latency, duration: 12s, params: { listen: "127.0.0.1:18080", upstream: "127.0.0.1:8080", delay: 800ms } }
hypotheses:
  - { name: errors stay bounded, metric: error_rate, operator: "<", value: 15% }
  - { name: recovers, metric: recovery, operator: "<=", value: 5s }
```

The agent is the quickest way in, not a requirement. The test is the file, in your repository, and it runs the same without the editor, in a terminal or in CI. Every MCP tool runs a command you can type yourself. Cursor is set up by `litmus-lite init`; any other MCP client works the same way (`litmus-lite mcp serve`), and `init -editor claude-code` is there for Claude Code.

Apache-2.0. Status: early. The CLI and the scenario format can still change before 1.0, and [what it does not do](#what-it-does-not-do) is listed below.

## Install

Download the archive for your system from the [releases page](https://github.com/litmuschaos/litmus-lite/releases) (Linux, macOS and Windows, amd64 and arm64), check it against `checksums.txt`, and put `litmus-lite` on your `PATH`:

```
tar -xzf litmus-lite_*_darwin_arm64.tar.gz
sha256sum --check --ignore-missing checksums.txt   # shasum -a 256 -c on macOS
./litmus-lite version
```

The binaries are not signed or notarized. On macOS, a binary downloaded with a browser is quarantined; clear it with `xattr -d com.apple.quarantine litmus-lite`. Building from source needs Go 1.22 or newer: `go build -o litmus-lite ./cmd/litmus-lite`.

## Quick start: one green run, one red run

Five minutes, with Cursor open on a clone of this repository and one terminal for the sample service. You need the binary above and Go 1.22+. The sample service, `cashier`, lives in the repository. Nothing else is needed: no load generator, no Docker, no network service, and nothing leaves your machine.

`cashier` is a tiny HTTP service with a `GET /charge` route. It is healthy until a scenario arms a named failpoint in it ([walkthrough](https://litmuschaos.github.io/litmus-lite/guides/failpoints/)).

**1. Start the sample service** (terminal):

```
git clone https://github.com/litmuschaos/litmus-lite && cd litmus-lite
LITMUS_LITE_FAILPOINT=charge,charge-slow LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo go run ./examples/cashier
```

**2. Connect Cursor** (a second terminal, in the same clone):

```
litmus-lite init
```

This adds `litmus-lite mcp serve` to `.cursor/mcp.json` and writes a rule, `.cursor/rules/litmus-lite.mdc`, that tells the agent the conventions: scenarios are files, probes go through the proxy, and the safety flags are yours to give. It also writes a `.mcp.json`, which Claude Code reads. Open the folder in Cursor, go to Settings > Tools & MCP, and check that `litmus-lite` is on (toggle it, or reload the window, if it is not listed). Start a new Agent chat.

**3. The green run.** Paste:

> Using the litmus-lite tools, validate `examples/cashier/charge.chaos.yaml` with strict checking, run it, and tell me whether the failure landed and whether the service recovered.

The agent calls `validate_scenario` and `run_test`, then answers from the two hypotheses in the file: the failure really landed (`hook-landed`: errors above 50% while the charge step fails for 8 seconds) and the service recovers within 5 seconds (`recovers`). The run passes. `report.json` and `report.html` are written in the project folder; open `report.html` for the probes' latency and error rate through the baseline, the fault and the recovery.

**4. The red run.** Paste:

> Now run `examples/cashier/charge-must-survive.chaos.yaml` and tell me why it failed.

It injects the same failure but asks for something `cashier` does not do: keep errors under 5% while charging is down, as a service with a fallback would. The run finishes and a hypothesis does not hold, which is a finding and not a tool error; the agent gets the result the same way as for a pass, and calls `diagnose_failure`. Expect it to say `charge survives its own outage` failed, with an error rate near 100%.

**5. Ask for your own.** Paste:

> Add 800 ms of latency to the cashier's `/charge` route for 12 seconds and tell me whether it copes and how fast it recovers.

Expect a new `*.chaos.yaml` beside the service, validated, run and judged against hypotheses the agent chose, with the probes pointed at the proxy's listen address and not at the service. Read the file: it is the test. Keep it, commit it, change a number, and run it again without the agent. If the agent only describes a scenario, tell it to write the file. [Working with an agent](#working-with-an-agent) has more prompts and what to expect.

### The same by hand

Everything above is the CLI underneath, so you can do it without Cursor. In the second terminal, in the same clone:

**Check the first scenario.**

```
litmus-lite validate -strict examples/cashier/charge.chaos.yaml
```

It prints `valid`. `validate` reads the file, applies the safety rules and warns about keys nothing reads, without touching anything.

**The green run.**

```
litmus-lite run examples/cashier/charge.chaos.yaml
```

```
cashier-charge passed=true hypotheses=2 report=report.json html=report.html
```

The exit code is 0.

**The red run.**

```
litmus-lite run -out red.json examples/cashier/charge-must-survive.chaos.yaml
```

```
cashier-charge-must-survive passed=false hypotheses=2 report=red.json html=red.html
```

The exit code is 3, which means "the run finished and a hypothesis did not hold". Ask what it found:

```
litmus-lite diagnose red.json
```

```
Hypothesis charge survives its own outage failed: error_rate < 5% (observed 100.00%).
```

(`diagnose` prints a few more lines about the fault and the recovery; the numbers differ from run to run.)

Exit codes: `0` passed, `3` ran and a hypothesis failed, `1` the scenario or the run could not proceed, `2` a usage error or a refusal by the safety rules, `130` interrupted (every fault is rolled back first).

**Write your own by hand.** `litmus-lite hub list` shows the fault templates; `litmus-lite new -beside ./chaos -kind http.latency -name checkout-slow` writes one next to your service, ready to edit. The page [How a run is judged](https://litmuschaos.github.io/litmus-lite/concepts/hypotheses/) says exactly what probes, modes, units and hypotheses mean.

## Working with an agent

The MCP server is `litmus-lite mcp serve`; each tool runs the CLI command beside it in this table, and there is no agent-only path.

| MCP tool | CLI |
| --- | --- |
| `create_scenario` | `new` |
| `validate_scenario` | `validate` |
| `run_test` | `run` |
| `get_results` | read the report JSON |
| `diagnose_failure` | `diagnose` |
| `list_faults` | `hub list` |
| `hub_import` | `hub import` |
| `compare_reports` | `compare` |
| `doctor` | `doctor` |

Three rules keep it honest, and they are a good way to check an agent is following them:

1. **A test is a file, never chat state.** Every scenario the agent makes is a `*.chaos.yaml` on disk, beside the service. If it only describes one, ask it to write the file.
2. **The CLI works alone.** You can leave the editor and run the file in a terminal or in CI.
3. **The agent cannot consent for you.** `yes`, `allow-target` and `allow-pid` confirm a destructive or out-of-scope action, so the MCP server refuses them unless you enabled them once for the server: `litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid`, then reload the window. If a call is refused, the agent should tell you, or give you the command to run yourself. See [SECURITY.md](SECURITY.md#mcp).

Prompts that work, each answered from the hypotheses and the report:

- *Add 800 ms of latency to the API for 12 seconds and tell me whether it copes and how fast it recovers.*
- *Make the API return 503 for half the requests for 10 seconds and tell me whether errors stay bounded. Validate with strict checking first.*
- *Now do both at once: send the same traffic through the API while it is being slowed down, and tell me what those users experienced.* (needs a load generator; see [load and chaos](https://litmuschaos.github.io/litmus-lite/guides/load-and-chaos/))
- *Compare `baseline.json` and `report.json` and go through steady, inject and recover. Do not invent a score.*

`litmus-lite mcp eval` checks the server against fixtures. The full walkthrough, with a second app, process faults and consent, is [with an agent](https://litmuschaos.github.io/litmus-lite/guides/agent-walkthrough/). [`AGENTS.md`](AGENTS.md) holds the design rules for changes to this repository.

## Faults

| Kind | What it does | Reversible by |
| --- | --- | --- |
| `http.latency`, `http.status-inject`, `http.timeout` | a proxy in front of your service adds delay, returns chosen status codes or hangs. Point probes at the proxy's `listen` address | stopping the proxy |
| `failpoint` | arms a named hook in your own code (the Go package [`failpoint`](failpoint/failpoint.go)) | disarming it |
| `process.pause` | `SIGSTOP`/`SIGCONT` on a PID you list with `-allow-pid` (not on Windows) | `SIGCONT` |
| `process.kill` | kills a PID you list with `-allow-pid`; needs `-yes` (not on Windows) | not reversible |
| `docker.pause` | `docker pause` on a named container | `docker unpause` |
| `cpu.hog`, `memory.hog` | burns CPU or holds memory in the litmus-lite process, bounded | ending the run |
| `disk.fill` | writes a capped file; needs `-yes`; refuses to overwrite a file | deleting the file |

## Safety, and what it does not cover

litmus-lite refuses before it injects anything:

- A target that is not loopback needs `-allow-target HOST`. A Kubernetes service name (`*.svc`) needs `-allow-target` and `-yes`.
- Process faults signal only PIDs you name with `-allow-pid`.
- `process.kill`, `disk.fill` and any fault longer than 60 seconds need `-yes`.
- Every fault is rolled back when the run ends, fails or is interrupted with Ctrl-C or SIGTERM.
- An agent cannot grant these for you. Through MCP, `yes`, `allow-target` and `allow-pid` are refused unless a person put them in `LITMUS_LITE_MCP_ALLOW` ([how](SECURITY.md#mcp)).

A scenario can also run commands: `cmd` probes and `load.tool: command` execute a shell command or program with your permissions, and nothing limits what it does. `validate` and `run` print every command a scenario will run before it runs, in quotes. **Read a scenario you did not write before you run it.** More in [SECURITY.md](SECURITY.md).

## What leaves your machine

By default, nothing. litmus-lite has no telemetry and makes no network request of its own accounts, updates or usage. It talks only to the addresses your scenario names (the service under test and its probes), plus these, each only when you set it up:

| You set up | What is sent, and to whom |
| --- | --- |
| `-stream-push URL` | the run's events to that URL: scenario name, target and probe URLs, fault kinds and names, labels and results. Credentials in fault parameters are masked. |
| a live-view service (`live enable URL`, `-live-url`, `LITMUS_LITE_LIVE_URL`) | the same events to that service, and your browser opens its page. With none configured, nothing is sent, opened or written. |
| `.litmus-lite/hub.lock` | a `git` fetch from the repository you pinned |
| `observability.prometheus` in a scenario | queries to that Prometheus |
| `load.tool` | runs the generator you named; what it sends is its own business |
| `ci github` | nothing from litmus-lite; the workflow it writes downloads a release from GitHub when your CI runs |

A report, the audit log (`~/.litmus-lite/audit.log`, one line per command) and any recording stay on disk. Details of the live view and the event format: [Streaming](https://litmuschaos.github.io/litmus-lite/reference/streaming/).

## What it does not do

Read this before you rely on a green run.

- **It is not a Kubernetes chaos platform.** Faults act on a laptop, a process, a container or an HTTP hop. `export job` can run a scenario as a Job, but it is experimental and the project publishes no image.
- **A probe sees what it asks.** A green run says the probes you wrote stayed healthy, nothing more. An HTTP probe judges the status and, if you add `expect.body`, the first 1 MiB of the body. It follows redirects.
- **A probe's sample belongs to the phase where it started.** A request that starts just before a fault and fails because of it counts as steady.
- **An `EOT` probe is recorded, not judged.** A failing end-of-test probe does not change the verdict. Judge the end state with a hypothesis.
- **Command probes need `sh`.** They do not run on Windows. HTTP probes, proxies and failpoints do.
- **`docker.pause` has no allowlist.** It pauses whatever container name the scenario gives.
- **An interrupt you cannot catch is not rolled back.** `SIGKILL` of litmus-lite (or a power cut) during `process.pause` leaves that process stopped (resume it with `kill -CONT PID`), and leaves a `failpoint` armed until the service restarts.
- **HTTP faults only touch traffic sent to them.** `http.latency`, `http.status-inject` and `http.timeout` are a proxy in the litmus-lite process: only requests to `listen` are affected, and a probe that calls the service directly sees nothing. `validate` and `run` warn when that happens. The proxy speaks HTTP/1.1 on plain TCP; gRPC and cleartext HTTP/2 are not supported.
- **A report keeps what a probe URL says.** A failed sample's error text can include the probe's URL, with any query string. Do not put a secret in a probe URL query.
- **Hypotheses are about one window.** They judge the inject window over all probes together; there is no per-probe or per-route breakdown yet.

## In CI

`litmus-lite ci github` writes `.github/workflows/litmus-lite.yml`, which installs a release (checked against its checksums) and runs `validate -strict` on every `*.chaos.yaml`. Running chaos in CI needs a target that is up; the workflow has a commented step for it. `-junit FILE` on `run` writes JUnit XML.

## More

The [documentation site](https://litmuschaos.github.io/litmus-lite/) has the full story. Good places to start:

- [Getting started](https://litmuschaos.github.io/litmus-lite/getting-started/): install, the quick start, and your first scenario, each in Cursor and in a terminal.
- [Working with an agent](https://litmuschaos.github.io/litmus-lite/guides/agent/): set-up, prompts that work, and what a good answer looks like.
- [How a run works](https://litmuschaos.github.io/litmus-lite/concepts/how-a-run-works/) and [how it is judged](https://litmuschaos.github.io/litmus-lite/concepts/hypotheses/): phases, probes, modes, units and hypotheses.
- [Faults](https://litmuschaos.github.io/litmus-lite/reference/faults/), [scenario file](https://litmuschaos.github.io/litmus-lite/reference/scenario/), [CLI](https://litmuschaos.github.io/litmus-lite/reference/cli/) and [MCP tools](https://litmuschaos.github.io/litmus-lite/reference/mcp/) reference.
- [Streaming a run](https://litmuschaos.github.io/litmus-lite/reference/streaming/): events, live view, `serve`, replay.
- [Architecture](https://litmuschaos.github.io/litmus-lite/contributing/architecture/) and [internals](https://litmuschaos.github.io/litmus-lite/contributing/internals/): the parts, one run, the trust boundaries, and how to add a fault.
- [Roadmap](https://litmuschaos.github.io/litmus-lite/project/roadmap/): what is deliberately not scheduled.
- Other commands: `compare BASE CAND` (phase regression between two reports), `move-to -adapter ir|litmus` (export a scenario to another tool's format), `doctor`.
- [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md) · [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) · [GOVERNANCE.md](GOVERNANCE.md) · [MAINTAINERS.md](MAINTAINERS.md)
