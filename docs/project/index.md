# Project

litmus-lite is an Apache-2.0 project under the [LitmusChaos](https://litmuschaos.io) organisation. It is early: the CLI and the scenario format can still change before 1.0, and every change a user could notice is in the release notes.

- **[FAQ and troubleshooting](faq.md):** common questions, and what to do when a run does not behave.
- **[Roadmap](roadmap.md):** ideas that are deliberately not scheduled, and why.
- **[Releases](https://github.com/litmuschaos/litmus-lite/releases):** binaries, checksums and release notes.
- **[Issues](https://github.com/litmuschaos/litmus-lite/issues):** bugs and proposals.

## What litmus-lite is not

- **Not a Kubernetes chaos platform.** Faults act on a laptop, a process, a container or an HTTP hop. For chaos inside clusters at scale, see [LitmusChaos](https://litmuschaos.io); `move-to -adapter litmus` exports a scenario to it.
- **Not a load-testing tool.** It runs one you already have (`vegaload`, `k6`, or your own command) alongside a fault.
- **Not a sandbox.** A scenario runs with your permissions. Read one you did not write before you run it.
- **Not a score.** A run passes or fails on hypotheses you wrote down.
