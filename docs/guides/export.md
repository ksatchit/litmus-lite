# Exporting a scenario

A scenario that proves useful on a laptop can move to other tools. `move-to` translates it; `export job` packages it to run in a Kubernetes cluster. Neither changes the scenario file.

## A neutral JSON form

The IR (intermediate representation) is the scenario as plain JSON, with parameters as `tunables`. Use it as the input to your own adapter or script.

=== "In Cursor"

    !!! quote ""
        Export `checkout/http-latency.chaos.yaml` to the IR and save it as `checkout/http-latency.ir.json`.

    There is no MCP tool for `move-to`; the agent runs the CLI command in a shell.

=== "In a terminal"

    ```bash
    litmus-lite move-to -adapter ir -out checkout/http-latency.ir.json checkout/http-latency.chaos.yaml
    ```

```json
{
  "apiVersion": "litmus-lite.io/v1",
  "kind": "Experiment",
  "name": "http-timeout",
  "target": { "kind": "http", "baseUrl": "http://127.0.0.1:8080" },
  "faults": [
    { "name": "stall", "kind": "http.timeout", "duration": "8s",
      "tunables": { "listen": "127.0.0.1:18080", "timeout": "3s", "upstream": "127.0.0.1:8080" } }
  ],
  "probes": [ ... ]
}
```

## A LitmusChaos experiment

```bash
litmus-lite move-to -adapter litmus checkout/http-latency.chaos.yaml
```

writes a LitmusChaos 4.0 `Experiment` document with the same target, faults, probes and hypotheses. The output is printed, and also written to `-out PATH` if you give one. Review it before you apply it to a cluster.

## A Kubernetes Job (experimental)

```bash
litmus-lite export job -image registry.example.com/litmus-lite:0.2.0 checkout/http-latency.chaos.yaml > job.yaml
```

writes a ConfigMap holding the scenario and a Job that runs it. The project publishes no container image: `-image` is required, and the image must have `litmus-lite` on its `PATH`. `-allow-target` and `-yes` are passed to the Job, and the export refuses a scenario the Job would refuse.

Inside a cluster, the target is usually a service name such as `checkout.shop.svc`, which needs both `-allow-target` and `-yes`. Review the Job before you apply it.
