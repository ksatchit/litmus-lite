# Getting started

Three short pages take you from nothing to a chaos test of your own service.

1. **[Install](install.md)** the single binary. Two minutes.
2. **[Quick start](quick-start.md):** one green run and one red run against a sample service that ships in the repository. Five minutes.
3. **[Your first scenario](first-scenario.md):** a test of your own service, written as a file you keep next to it.

## Two ways to work, one result

Every page shows both, in tabs. Pick one and the rest of the site follows your choice.

=== "In Cursor"

    You describe the test in an Agent chat. The agent writes the scenario file, validates it, runs it through litmus-lite's MCP tools and explains the result. You read and keep the file.

    This is the fastest way to start, and the way most people use litmus-lite day to day.

=== "In a terminal"

    You import a template, edit it, and run it with the CLI. The exit code is the verdict, so it drops straight into scripts and CI.

    This is also exactly what the agent does underneath: every MCP tool runs one of these commands.

Both produce the same `*.chaos.yaml` file and the same `report.json` and `report.html`. You can start in one and continue in the other: a scenario the agent wrote runs in CI without the agent, and a file you wrote by hand is one the agent can run and explain.
