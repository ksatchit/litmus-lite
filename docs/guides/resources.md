# CPU, memory and disk

These faults put pressure on the machine litmus-lite runs on, from inside the litmus-lite process. They answer "what happens to my service when it has to share this box with something greedy?", so they only tell you something about a service on the same machine.

All three are bounded and undone at the end of the fault.

## CPU

`cpu.hog` keeps `workers` goroutines busy (default 1, at most 16).

=== "In Cursor"

    !!! quote ""
        Burn 4 CPU workers for 10 seconds and tell me whether the API on port 8080 keeps its p95 under 300 ms.

=== "In a terminal"

    ```bash
    litmus-lite hub import cpu.hog -beside ./api
    ```

    ```yaml
    faults:
      - { name: burn, kind: cpu.hog, duration: 10s, params: { workers: 4 } }
    hypotheses:
      - { name: stays fast, metric: p95, operator: "<", value: 300ms }
    ```

## Memory

`memory.hog` allocates and holds `size` (default `32MB`, at most `256MB`) for the fault's duration.

```yaml
faults:
  - { name: eat-ram, kind: memory.hog, duration: 8s, params: { size: 128MB } }
```

## Disk

`disk.fill` writes a file of `size` (default `16MB`, at most `256MB`) and deletes it when the fault ends. It needs `-yes`, never overwrites an existing file, does not follow symlinks and refuses a filesystem root.

=== "In Cursor"

    !!! quote ""
        Fill 200 MB under `/tmp` for 8 seconds and tell me whether the upload endpoint still answers.

    `yes` is refused through MCP unless you enabled it with `LITMUS_LITE_MCP_ALLOW=yes`; the agent should give you the command instead.

=== "In a terminal"

    ```yaml
    faults:
      - name: fill-tmp
        kind: disk.fill
        duration: 8s
        params: { size: 200MB, path: /tmp/litmus-lite-fill.bin }
    ```

    ```bash
    litmus-lite run -yes api/disk-fill.chaos.yaml
    ```

Without `path`, the file goes in a new name under the system temp directory. Do not point it at a volume you care about.

Parameters in full: [Faults reference](../reference/faults.md#resource-faults).
