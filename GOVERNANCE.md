# Governance

litmus-lite is a small open-source project under the Apache-2.0 licence. This page says how decisions are made, as it works today, so that it can grow with the project.

## Roles

- **Users** use the tool and tell us what works and what does not, in issues and discussions.
- **Contributors** send pull requests, review them, improve the docs or answer questions. Every contribution is signed off under the DCO ([CONTRIBUTING.md](CONTRIBUTING.md)).
- **Maintainers** ([MAINTAINERS.md](MAINTAINERS.md)) can merge, cut releases and decide the roadmap. They are accountable for the project's safety and correctness rules in [`AGENTS.md`](AGENTS.md) and [SECURITY.md](SECURITY.md).

## Decisions

- Most changes are decided in the pull request: a maintainer approves and merges, or says why not.
- A change that touches the scenario format, a safety rule, the report format or the MCP/CLI contract is discussed in an issue first. Maintainers look for consensus; if there is none after a reasonable discussion, a maintainer decides and writes the reason down in the issue.
- Behaviour changes that a user could notice go in the release notes.
- Security reports are handled privately ([SECURITY.md](SECURITY.md)).

## Becoming a maintainer

A contributor who has made several non-trivial contributions over time, reviews other people's work well and follows the project's principles can be nominated by any maintainer in an issue. The nomination is accepted when the existing maintainers agree and the nominee accepts. A maintainer who is no longer active can step down, or be moved to emeritus after a period of inactivity, by the same process.

## Vendor neutrality

The project is not tied to a vendor. Community-facing docs do not require any commercial product, and the core has no dependency on one. Integrations (`move-to` adapters, load generators) are optional and named by their own adapter or tool name.

## Changing this document

By a pull request approved by the maintainers. With more than one maintainer, a change needs the approval of a majority.
