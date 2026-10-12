# Roadmap

A living list of ideas that are intentionally **not** scheduled. Nothing here is a commitment.

## Open items

### Kernel-level packet faults (loss, corruption, reordering)

Not planned for the current phase. Proxy-level faults (latency, bandwidth limits, stalls, resets, refused connections, truncation) cover what most developers need on a laptop, work on every OS, and need no extra privileges.

Kernel-level traffic shaping would be a step up in permissions and complexity: Linux-only, opt-in, and with its own safety and portability questions. It will be revisited only if there is concrete demand. If you need it, please open an issue describing the scenario you are trying to test.
