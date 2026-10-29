## What and why

<!-- What does this change, and why is it needed? Link the issue. -->

## Behaviour change

<!-- Can a user notice it (a flag, a default, a refusal, a verdict, the report or event format)? If so, say how, so it reaches the release notes. Otherwise write "none". -->

## How it was tested

<!-- `go test -race ./...`, and the test you added. For a fix: the test fails without it. -->

## Checklist

- [ ] Every commit is signed off (`git commit -s`, see CONTRIBUTING.md)
- [ ] `gofmt -l .` prints nothing and `go vet ./...` passes
- [ ] Tests added or updated, including the refusal cases
- [ ] Docs updated where the behaviour is described
- [ ] No telemetry, no vendor name in community docs, MCP has no behaviour the CLI lacks
