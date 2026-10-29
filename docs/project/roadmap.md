# Roadmap

A living list of ideas that are intentionally **not** scheduled. Nothing here is a commitment.

## Open items

### Kernel-level packet faults (loss, corruption, reordering)

Not planned for the current phase. The HTTP proxy faults (latency, error responses, stalls) cover what most developers need on a laptop, work on every OS, and need no extra privileges.

Kernel-level traffic shaping would be a step up in permissions and complexity: Linux-only, opt-in, and with its own safety and portability questions. It will be revisited only if there is concrete demand. If you need it, please open an issue describing the scenario you are trying to test.

### Reaching the proxy from VMs, WSL2, local Kubernetes and LAN devices

The loopback proxy works for processes running on the same machine, and a sidecar proxy is planned for apps running in Docker. Apps in a VM, in WSL2, in a local Kubernetes cluster, or on another device on your network (for example a phone) cannot reach the host's loopback address, so they are not covered yet.

One idea is a guarded, opt-in bind to a non-loopback address: an explicit address, an explicit confirmation flag, and a mandatory list of allowed source networks enforced on every connection, with the listener removed as soon as the run ends. It is not scheduled. If you need it, please open an issue describing your environment.
