# litmus-lite

Chaos testing from a YAML file, for a laptop or a CI job. You describe a failure, what healthy looks like and what must stay true. litmus-lite injects the failure, samples your service, judges the run and writes one self-contained HTML report. It is a single binary. There is no control plane, no cluster and no account.

```yaml
faults:
  - { name: slow-checkout, kind: http.latency, duration: 12s, params: { listen: "127.0.0.1:18080", upstream: "127.0.0.1:8080", delay: 800ms } }
hypotheses:
  - { name: errors stay bounded, metric: error_rate, operator: "<", value: 15% }
  - { name: recovers, metric: recovery, operator: "<=", value: 5s }
```

Scenarios are ordinary files (`*.chaos.yaml`) that you keep next to the service they test. An AI agent can write and run them through [MCP](#use-it-from-an-agent), but nothing needs one: every command works from a terminal.

Apache-2.0. Status: early. The CLI and the scenario format can still change before 1.0, and [what it does not do](#what-it-does-not-do) is listed below.

## Install

Download the archive for your system from the [releases page](https://github.com/ksatchit/litmus-lite/releases) (Linux, macOS and Windows, amd64 and arm64), check it against `checksums.txt`, and put `litmus-lite` on your `PATH`:

```
tar -xzf litmus-lite_*_darwin_arm64.tar.gz
sha256sum --check --ignore-missing checksums.txt   # shasum -a 256 -c on macOS
./litmus-lite version
```

The binaries are not signed or notarized. On macOS, a binary downloaded with a browser is quarantined; clear it with `xattr -d com.apple.quarantine litmus-lite`. Building from source needs Go 1.22 or newer: `go build -o litmus-lite ./cmd/litmus-lite`.

## Quick start: one green run, one red run

Five minutes, one terminal tab for the sample service and one for litmus-lite. You need the binary above, Go 1.22+ and a clone of this repository, because the sample service, `cashier`, lives in it. Nothing else is needed: no load generator, no Docker, no network service, and nothing leaves your machine.

`cashier` is a tiny HTTP service with a `GET /charge` route. It is healthy until a scenario arms a named failpoint in it ([walkthrough](docs/cookbooks/failpoints.md)).

**1. Start the sample service** (tab 1):

```
git clone https://github.com/ksatchit/litmus-lite && cd litmus-lite
LITMUS_LITE_FAILPOINT=charge,charge-slow LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo go run ./examples/cashier
```

**2. Check the first scenario** (tab 2, in the same clone):

```
litmus-lite validate -strict examples/cashier/charge.chaos.yaml
```

It prints `valid`. `validate` reads the file, applies the safety rules and warns about keys nothing reads, without touching anything.

**3. The green run.** The scenario makes the charge step fail for 8 seconds and says what must be true: the failure really landed (errors above 50%) and the service recovers within 5 seconds.

```
litmus-lite run examples/cashier/charge.chaos.yaml
```

```
cashier-charge passed=true hypotheses=2 report=report.json html=report.html
```

The exit code is 0. Open `report.html`: it shows the probes' latency and error rate through the baseline, the fault and the recovery.

**4. The red run.** The second scenario injects the same failure but asks for something `cashier` does not do: keep errors under 5% while charging is down, as a service with a fallback would.

```
litmus-lite run -out red.json examples/cashier/charge-must-survive.chaos.yaml
```

```
cashier-charge-must-survive passed=false hypotheses=2 report=red.json html=red.html
```

The exit code is 3, which means "the run finished and a hypothesis did not hold". That is a finding, not a tool failure. Ask what it found:

```
litmus-lite diagnose red.json
```

```
Hypothesis charge survives its own outage failed: error_rate < 5% (observed 100.00%).
```

(`diagnose` prints a few more lines about the fault and the recovery; the numbers differ from run to run.)

Exit codes: `0` passed, `3` ran and a hypothesis failed, `1` the scenario or the run could not proceed, `2` a usage error or a refusal by the safety rules, `130` interrupted (every fault is rolled back first).

**5. Write your own.** `litmus-lite hub list` shows the fault templates; `litmus-lite new -beside ./chaos -kind http.latency -name checkout-slow` writes one next to your service, ready to edit. The page [How a run is judged](docs/hypotheses.md) says exactly what probes, modes, units and hypotheses mean.

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

A report, the audit log (`~/.litmus-lite/audit.log`, one line per command) and any recording stay on disk. Details of the live view and the event format: [docs/streaming.md](docs/streaming.md).

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

## Use it from an agent

`litmus-lite init -editor cursor` registers an MCP server (`litmus-lite mcp serve`) and a rule in your project. Each MCP tool runs the same CLI command you can type, and the scenario the agent writes is a file in your repository. `litmus-lite mcp eval` checks the server against fixtures. [`AGENTS.md`](AGENTS.md) describes the design rules for changes to this repository.

## In CI

`litmus-lite ci github` writes `.github/workflows/litmus-lite.yml`, which installs a release (checked against its checksums) and runs `validate -strict` on every `*.chaos.yaml`. Running chaos in CI needs a target that is up; the workflow has a commented step for it. `-junit FILE` on `run` writes JUnit XML.

## More

- [Architecture](docs/architecture.md) and [internals](docs/internals.md): the parts, one run, the trust boundaries, and how to add a fault.
- [How a run is judged](docs/hypotheses.md): probes, modes, units, hypotheses, strict checking, report format.
- [Cookbooks](docs/cookbooks/): [with an agent](docs/cookbooks/with-an-agent.md), [a two-process app](docs/cookbooks/launchpad.md), [load and chaos](docs/cookbooks/load-and-chaos.md), [failpoints](docs/cookbooks/failpoints.md), and the [fault catalog](docs/cookbooks/fault-catalog.md).
- [Streaming a run](docs/streaming.md): events, live view, `serve`, replay.
- [Roadmap](docs/ROADMAP.md): what is deliberately not scheduled.
- Other commands: `compare BASE CAND` (phase regression between two reports), `move-to -adapter ir|litmus` (export a scenario to another tool's format), `doctor`.
- [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md) · [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) · [GOVERNANCE.md](GOVERNANCE.md) · [MAINTAINERS.md](MAINTAINERS.md)
