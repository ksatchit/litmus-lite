# Running in CI

A scenario is a file, so CI can check it like any other test. There are two levels:

1. **Validate every scenario on every pull request.** Cheap, needs no running service, and catches typos, unsafe settings and scenarios that cannot fail.
2. **Run scenarios against a live target.** Needs the service up in the job, and tells you whether it still copes.

## Validate on every pull request

=== "In Cursor"

    !!! quote ""
        Set up GitHub Actions to validate every chaos scenario in this repository with strict checking.

    The agent runs `litmus-lite ci github` in a shell (it is a plain CLI command), and shows you the workflow.

=== "In a terminal"

    ```bash
    litmus-lite ci github
    git add .github/workflows/litmus-lite.yml
    ```

The workflow downloads a litmus-lite release, checks it against `checksums.txt`, and runs `validate -strict` on every tracked `*.chaos.yaml`:

```yaml title=".github/workflows/litmus-lite.yml (abridged)"
- name: Install litmus-lite
  run: |
    gh release download --repo litmuschaos/litmus-lite --pattern 'litmus-lite_*_linux_amd64.tar.gz' --pattern checksums.txt
    sha256sum --check --ignore-missing checksums.txt
    ...
- name: Validate scenarios
  run: |
    git ls-files '*.chaos.yaml' | while IFS= read -r f; do
      litmus-lite validate -strict "$f" || exit 1
    done
```

`-version v0.2.0` pins a release instead of the latest; `-force` overwrites an existing workflow.

## Run chaos in CI

Start the service in the job, wait until it is healthy, then run:

```yaml
- name: Start the service
  run: |
    ./bin/checkout &
    for i in $(seq 1 50); do curl -fs 127.0.0.1:8080/healthz && break; sleep 0.2; done
- name: Chaos
  env:
    LITMUS_LITE_LIVE: "off"
  run: litmus-lite run -junit chaos.xml -out chaos.json checkout/http-latency.chaos.yaml
- uses: actions/upload-artifact@v4
  if: always()
  with:
    name: chaos-report
    path: |
      chaos.json
      chaos.html
      chaos.xml
```

- The exit code is the verdict: `0` passed, `3` a hypothesis failed (the job fails), `1` or `2` the scenario could not run.
- `-junit` writes one test case per hypothesis, which most CI systems display natively.
- Upload `chaos.html`: it is self-contained, so the artifact opens anywhere.
- No browser is opened when `CI` is set.

### Gate on a regression

Keep a baseline report (from `main`, say) and fail the job if a change makes things worse:

```bash
litmus-lite run -out candidate.json checkout/http-latency.chaos.yaml
litmus-lite compare baseline.json candidate.json   # exits 3 on a regression
```

## Other CI systems

Nothing is GitHub-specific except the generated workflow. Anywhere you can run a binary, download the release archive for Linux, check it against `checksums.txt`, and run the same commands.
