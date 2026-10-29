# Contributing

Thank you for helping. litmus-lite is small on purpose, so a change that makes it more correct, easier to understand or safer is worth more than one that makes it bigger.

## Before you start

- For anything bigger than a bug fix or a doc change, open an issue first and say what you want to do, so nobody builds something that cannot be merged.
- Read [`AGENTS.md`](AGENTS.md). It is written for AI coding agents but it is the plain statement of the design rules for everyone: scenarios are files, the CLI always works alone, MCP tools mirror CLI commands, and the engine, faults, probes, report, MCP and streaming code keep to their module boundaries.
- Read [How a run is judged](docs/concepts/hypotheses.md). A change that alters what a hypothesis or a probe means is a behaviour change and needs a release note.
- Security problems: do not open an issue. See [SECURITY.md](SECURITY.md).

## Build and test

```
go build -o litmus-lite ./cmd/litmus-lite
go vet ./...
gofmt -l .                  # prints nothing when the code is formatted
go test -race ./...
```

Go 1.22 or newer. Tests sit next to the code (`*_test.go`). A bug fix comes with a test that fails without the fix; a new fault kind, probe type or flag comes with tests for its refusals as well as for the case that works. Tests must not touch a real config directory, the network beyond loopback, or a real live-view service (`cmd/litmus-lite/main_test.go` isolates the live view).

## Conventions

- Run `gofmt` before you commit.
- Pass/fail predicates on a run are called **hypotheses**, not thresholds or scores.
- Docs for the community name no vendor. `move-to` adapters are `move-to -adapter <name>`.
- Do not add telemetry. Nothing leaves the machine unless the user configured the destination.
- Do not add behaviour to MCP that has no CLI equivalent.
- Commit messages are one line, in the present tense, and say why the change is made rather than listing commands.

## Sign your commits (DCO)

litmus-lite uses the [Developer Certificate of Origin](https://developercertificate.org/). It says you wrote the change or have the right to submit it under the project's licence (Apache-2.0). You state that by signing off every commit:

```
git commit -s -m "Refuse a probe expect key nothing reads"
```

`-s` adds a `Signed-off-by: Your Name <you@example.com>` line that matches your commit author. If you forgot, `git commit --amend -s` fixes the last commit and `git rebase --signoff main` fixes a branch.

## Pull requests

- Keep a pull request to one change. Small pull requests are reviewed faster.
- Say what changed, why, and how you tested it. The template asks.
- A change in behaviour that a user could notice goes under "Behaviour change" in the template, so it reaches the release notes.
- Be patient: reviewers are volunteers. A ping after a week is fine.

By taking part you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
