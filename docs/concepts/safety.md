# Safety

litmus-lite injects faults with your permissions. Its rules are guard rails against mistakes: they make every risky action something a person asked for by name. They are not a sandbox against a hostile scenario, so **read a scenario you did not write before you run it**, as you would a shell script.

## What needs a flag

litmus-lite checks these before it injects anything, at `validate` and again at `run`:

| Action | Needs |
| --- | --- |
| a target, probe or proxy upstream that is not loopback (`localhost`, `127.0.0.0/8`, `::1`) | `-allow-target HOST` (comma-separated for several) |
| a Kubernetes service host (`*.svc`) | `-allow-target HOST` and `-yes` |
| `process.pause`, `process.kill` | `-allow-pid PID,PID` naming each process (never 0, 1 or litmus-lite itself) |
| `process.kill`, `disk.fill` (not reversible) | `-yes` |
| any fault longer than 60 seconds | `-yes` |

`0.0.0.0` and a bare `:port` are refused as targets. Without the flag, the run exits with code `2` and says which flag it needs.

## The agent cannot consent for you

=== "In Cursor"

    Through MCP, `yes`, `allow-target` and `allow-pid` are refused, before any command runs, unless you enabled them for the server. When the agent hits a refusal it should tell you, or give you the command to run yourself.

    To let the agent pass one of them, set it once, by hand:

    ```bash
    litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid    # then reload the Cursor window
    ```

    `LITMUS_LITE_MCP_ALLOW` takes a comma-separated list of `yes`, `allow-target` and `allow-pid`. Only the ones you list can be passed.

=== "In a terminal"

    You give the flags yourself, per run:

    ```bash
    litmus-lite run -allow-pid 4242 -yes scenarios/worker-kill.chaos.yaml
    litmus-lite run -allow-target staging.internal checkout/http-latency.chaos.yaml
    ```

## A scenario can run commands

A probe with `type: cmd` runs `sh -c COMMAND`, and `load.tool: command` runs a program, both with your permissions and without an allowlist. Nothing runs at `validate` or `hub import`; commands run only during `run`.

`validate` and `run` print every command a scenario will run, in quotes, before anything runs, and `validate -output json` lists them under `runsCommands`. litmus-lite does not ask for confirmation, so that it keeps working in CI. The MCP gate above does not cover commands: an agent that can write files can write a command probe. Review what it wrote.

## What leaves your machine

By default, nothing. There is no telemetry, no update check and no account. litmus-lite talks to the addresses your scenario names, plus the following, each only when you set it up:

| You set up | What is sent, and where |
| --- | --- |
| `-stream-push URL` | the run's events to that URL. Credentials in fault parameters are masked. |
| a live-view service (`live enable URL`, `-live-url`, `LITMUS_LITE_LIVE_URL`) | the same events to that service, and your browser opens its page |
| `.litmus-lite/hub.lock` | a `git` fetch of the repository you pinned |
| `observability.prometheus` | queries to that Prometheus |
| `load.tool` | whatever the load generator you named sends |

Reports, recordings and the audit log (`~/.litmus-lite/audit.log`, one line per command) stay on disk.

## What safety does not cover

- **An uncatchable kill is not rolled back.** If litmus-lite is killed with `SIGKILL` during `process.pause`, the process stays stopped (`kill -CONT PID`); during `docker.pause`, run `docker unpause NAME`. A failpoint stays armed until the service restarts.
- **`docker.pause` has no allowlist.** It pauses whichever container the scenario names.
- **HTTP probes follow redirects**, including to hosts `-allow-target` would refuse.
- **Fault parameters are written to the report as is.** Do not put real secrets in a scenario or in a probe URL's query string.

The full list, and how to report a vulnerability, is in [SECURITY.md](https://github.com/litmuschaos/litmus-lite/blob/main/SECURITY.md).
