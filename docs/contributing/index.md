# Contributing

litmus-lite is small on purpose. A change that makes it more correct, easier to understand or safer is worth more than one that makes it bigger. Thank you for helping.

## Start here

- **[CONTRIBUTING.md](https://github.com/litmuschaos/litmus-lite/blob/main/CONTRIBUTING.md):** how to build and test, the conventions, signing your commits (DCO) and opening a pull request.
- **[AGENTS.md](https://github.com/litmuschaos/litmus-lite/blob/main/AGENTS.md):** the design rules, written for AI coding agents and binding for everyone.
- **[Architecture](architecture.md):** the parts, one run end to end, and the trust boundaries.
- **[Internals](internals.md):** how the engine, faults, probes and reports work, and how to add a fault kind.

## Build and test

```bash
git clone https://github.com/litmuschaos/litmus-lite && cd litmus-lite
go build -o litmus-lite ./cmd/litmus-lite
go vet ./...
gofmt -l .            # prints nothing when the code is formatted
go test -race ./...
```

Go 1.22 or newer.

## The design rules, in short

1. **Every test is a real file, never chat state.**
2. **The CLI always works alone.** A feature that only works through an agent is a bug.
3. **MCP tools mirror CLI commands.** No richer, agent-only path.

A proposal that breaks one of these needs a conversation in an issue first.

## Working on these docs

The site is built with [MkDocs](https://www.mkdocs.org/) and [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/) from the `docs/` folder and `mkdocs.yml`:

```bash
python3 -m venv .venv && . .venv/bin/activate
pip install -r docs/requirements.txt
mkdocs serve                 # http://127.0.0.1:8000, reloads as you edit
mkdocs build --strict        # what CI runs: fails on a broken link
```

Every page that shows a task shows it both ways, with content tabs named **In Cursor** and **In a terminal**: the tabs are linked, so a reader's choice follows them across the site. Include a whole example from `examples/` with a snippet (`--8<-- "examples/cashier/charge.chaos.yaml"`) rather than copying it, so it cannot drift; a test checks that every `examples/` path the docs mention exists.

## Community

- [Code of Conduct](https://github.com/litmuschaos/litmus-lite/blob/main/CODE_OF_CONDUCT.md)
- [Governance](https://github.com/litmuschaos/litmus-lite/blob/main/GOVERNANCE.md) and [maintainers](https://github.com/litmuschaos/litmus-lite/blob/main/MAINTAINERS.md)
- Security problems: do not open an issue; see [SECURITY.md](https://github.com/litmuschaos/litmus-lite/blob/main/SECURITY.md).
