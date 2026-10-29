# Processes and containers

What does your service do when a dependency stops responding, or disappears? These faults act on a local process or a Docker container. Each one signals only what you named, so they all need a person's explicit go-ahead.

## Freeze a process

`process.pause` sends `SIGSTOP`, waits for the fault's duration, then sends `SIGCONT`. The process keeps its state but makes no progress, like a long GC pause or a hung dependency. Linux and macOS only.

Find the PID first:

```bash
pgrep -fl engine
```

=== "In Cursor"

    !!! quote ""
        The engine process is PID 4242. Write a scenario that pauses it for 8 seconds, with probes on `GET /api/engine/status` through the API on port 8080 and an 800 ms probe timeout. Run it with `allow-pid` 4242 and tell me whether the API recovered.

    The MCP server refuses `allow-pid` unless you enabled it once:

    ```bash
    litmus-lite init -env LITMUS_LITE_MCP_ALLOW=allow-pid    # then reload the window
    ```

    Without that, the agent should give you the command to run yourself. That is the design: the agent cannot pick a process for you to signal.

=== "In a terminal"

    ```bash
    litmus-lite hub import process.pause -beside ./engine
    ```

    ```yaml
    faults:
      - name: freeze-engine
        kind: process.pause
        duration: 8s
        params:
          pid: 4242          # or: command: engine
    probes:
      - name: engine-status
        url: http://127.0.0.1:8080/api/engine/status
        timeout: 800ms
    ```

    ```bash
    litmus-lite run -allow-pid 4242 engine/process-pause.chaos.yaml
    ```

**What to expect:** calls that depend on the paused process time out, then succeed again once it continues. A complete example is [`examples/launchpad/pause-engine.chaos.yaml`](https://github.com/litmuschaos/litmus-lite/blob/main/examples/launchpad/pause-engine.chaos.yaml), walked through in the [terminal walkthrough](launchpad.md).

`command: NAME` finds the process by name instead of PID. The match must leave exactly one PID that is also in `-allow-pid`; otherwise the fault refuses and lists what it found.

!!! danger "If litmus-lite is killed during a pause"
    Ctrl-C and `SIGTERM` resume the process before litmus-lite exits. `SIGKILL` cannot be caught: resume it yourself with `kill -CONT PID`.

## Kill a process

`process.kill` sends `SIGTERM`, then `SIGKILL` after `sigkillAfter` (default `2s`). It is **not reversible**, so it needs `-yes` as well as `-allow-pid`. Use it to check that a supervisor (systemd, a process manager, your own restart logic) brings the process back, and that the service recovers when it does.

```bash
litmus-lite run -allow-pid 4242 -yes worker/process-kill.chaos.yaml
```

Never point it at a process you cannot restart.

## Pause a container

`docker.pause` runs `docker pause` on a container for the fault's duration, then `docker unpause`. It needs the `docker` CLI.

=== "In Cursor"

    !!! quote ""
        Pause the `redis` container for 8 seconds and tell me whether the API on port 8080 keeps serving from its cache.

=== "In a terminal"

    ```bash
    litmus-lite hub import docker.pause -beside ./api
    ```

    ```yaml
    faults:
      - name: freeze-redis
        kind: docker.pause
        duration: 8s
        params:
          name: redis
    ```

!!! warning "No allowlist"
    `docker.pause` pauses whatever container the scenario names, with no flag needed. Use it on disposable containers, and read a scenario you did not write before you run it. If litmus-lite is killed outright, run `docker unpause NAME`.

Parameters in full: [Faults reference](../reference/faults.md#process-faults).
