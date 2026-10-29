# Install

litmus-lite is one static binary for Linux, macOS and Windows, on amd64 and arm64. It needs no runtime, daemon or account.

## Download a release

Download the archive for your system from the [releases page](https://github.com/litmuschaos/litmus-lite/releases), check it against `checksums.txt`, and put `litmus-lite` on your `PATH`.

=== "macOS"

    ```bash
    tar -xzf litmus-lite_*_darwin_arm64.tar.gz      # darwin_amd64 on Intel
    shasum -a 256 -c --ignore-missing checksums.txt
    xattr -d com.apple.quarantine litmus-lite        # the binary is not notarized
    sudo mv litmus-lite /usr/local/bin/
    litmus-lite version
    ```

    A binary downloaded with a browser is quarantined by Gatekeeper; `xattr` clears that.

=== "Linux"

    ```bash
    tar -xzf litmus-lite_*_linux_amd64.tar.gz        # linux_arm64 on ARM
    sha256sum --check --ignore-missing checksums.txt
    sudo mv litmus-lite /usr/local/bin/
    litmus-lite version
    ```

=== "Windows"

    ```powershell
    Expand-Archive litmus-lite_*_windows_amd64.zip -DestinationPath .
    (Get-FileHash litmus-lite.exe -Algorithm SHA256).Hash   # compare with checksums.txt
    .\litmus-lite.exe version
    ```

    Move `litmus-lite.exe` to a folder on your `PATH`. The process faults need Unix signals, and command probes (`type: cmd`) run through `sh -c`, so on Windows use HTTP probes and the other faults, or run under WSL.

!!! note "The binaries are not signed"
    Releases are built in GitHub Actions from a tag, and `checksums.txt` is published with them. They are not code-signed or notarized.

## Build from source

You need Go 1.22 or newer.

```bash
go install github.com/litmuschaos/litmus-lite/cmd/litmus-lite@latest
```

or, from a clone:

```bash
git clone https://github.com/litmuschaos/litmus-lite && cd litmus-lite
go build -o litmus-lite ./cmd/litmus-lite
```

A binary built this way reports a development version from `litmus-lite version`; release binaries report the release.

## Connect your editor (optional)

To use litmus-lite from an agent, run this once in each project:

=== "In Cursor"

    ```bash
    cd path/to/your/project
    litmus-lite init
    ```

    `init` adds a `litmus-lite` MCP server (`litmus-lite mcp serve`) to `.cursor/mcp.json` and writes a rule, `.cursor/rules/litmus-lite.mdc`, that tells the agent the conventions. It also writes `.mcp.json` for Claude Code. It records the absolute path of the binary that ran it, so re-run `init` after you move or upgrade the binary.

    Then, in Cursor: **Settings > Tools & MCP**, check that `litmus-lite` is on. If it is not listed, toggle it or reload the window.

=== "In a terminal"

    Nothing to set up. Check your environment with:

    ```bash
    litmus-lite doctor
    ```

    It checks the binary, your `PATH`, the optional load generator and the editor setup. Warnings about optional pieces are fine: a chaos-only run needs none of them.

Other MCP clients work too: point them at `litmus-lite mcp serve` over stdio. `litmus-lite init -editor claude-code` sets up Claude Code.

## Next

[Quick start](quick-start.md): one green run and one red run, in five minutes.
