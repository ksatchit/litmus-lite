# LaunchPad + litmus-lite

[LaunchPad](https://github.com/umamukkara/launchpad) is the shared demo app: `missioncontrol` (REST `:8080`) talks to `engine` (gRPC `:7070`).

Walkthroughs: [terminal](../../docs/cookbooks/demo-playbook.md) · [Cursor-first](../../docs/cookbooks/demo-playbook-cursor.md) · [with VegaLoad](../../docs/cookbooks/demo-playbook-vegaload.md).

## Files

| File | Beat |
| --- | --- |
| `ignite.chaos.yaml` | HTTP proxy latency + 5% 500s on `:18080` (chaos only) |
| `ignite-load.chaos.yaml` | Same fault + VegaLoad VUs on the proxy (`launches.vl.js`) |
| `pause-engine.chaos.yaml` | SIGSTOP the `engine` process; list-launches should suffer |
| `launches.vl.js` | VegaLoad script; must hit `:18080`, not `:8080` |

## Short path

```
# LaunchPad: go run ./cmd/engine  and  go run ./cmd/missioncontrol
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite run examples/launchpad/ignite.chaos.yaml
open report.html
./litmus-lite run examples/launchpad/pause-engine.chaos.yaml
```

Probes and VegaLoad must use the **proxy listen** address (`:18080`) for HTTP faults. Process pause talks to `:8080` (no proxy).
