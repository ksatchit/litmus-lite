# LaunchPad + litmus-lite

[LaunchPad](https://github.com/umamukkara/launchpad) is the shared demo app with VegaLoad: `missioncontrol` (REST/WS `:8080`) talks to `engine` (gRPC `:7070`).

## Demo (Phase 0)

1. Start LaunchPad from its repo (`go run ./cmd/engine` and `go run ./cmd/missioncontrol`, or `./deploy/run-local.sh` on macOS).
2. From this repo:

```
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite validate examples/launchpad/ignite.chaos.yaml
./litmus-lite run examples/launchpad/ignite.chaos.yaml
```

3. Open `report.html`. The shaded band is inject. The solid line is probe p95; dashed is injected delay. Prometheus at `:9090` is optional — if it is down, the run still finishes (warning only).

4. With VegaLoad installed, uncomment `load:` in `ignite.chaos.yaml` and re-run. Both tools must target `127.0.0.1:18080` (the proxy), not `:8080`.

5. `./litmus-lite diagnose report.json`

## Co-location

To write the hub template beside missioncontrol in a LaunchPad checkout:

```
./litmus-lite hub import -beside /path/to/launchpad/cmd/missioncontrol http.latency
```
