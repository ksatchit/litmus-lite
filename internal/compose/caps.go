package compose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// genCaps is what a generator can do, learned before it is run: the flags that
// its `run -h` lists. Detecting this up front (instead of running the tool and
// reading its error) keeps every run on one path, so the flags that matter for
// every run, such as keeping its own report out of the user's browser, are
// never lost on a fallback.
type genCaps struct {
	// known is false when the help text could not be read or listed no flags.
	// A caller then assumes the oldest behaviour it supports.
	known bool
	flags map[string]bool
}

func (c genCaps) has(flag string) bool { return c.known && c.flags[flag] }

// flagLine matches the start of a flag in a usage listing, as the Go flag
// package prints it ("  -name type") and as most CLIs do ("  -n, --name").
// Usage text under a flag is indented with a tab, so it never matches.
var flagLine = regexp.MustCompile(`(?m)^[ ]{1,8}(?:-[A-Za-z0-9],?[ ]+)?--?([A-Za-z0-9][A-Za-z0-9_-]*)`)

// parseFlags lists the flags named in a usage text.
func parseFlags(help string) genCaps {
	flags := map[string]bool{}
	for _, m := range flagLine.FindAllStringSubmatch(help, -1) {
		flags[m[1]] = true
	}
	return genCaps{known: len(flags) > 0, flags: flags}
}

// probeTimeout bounds how long the help of a generator is waited for. It is
// generous on purpose: the first start of a freshly installed binary can take
// seconds on macOS while the system checks it, and a probe that gives up too
// soon makes a capable generator look incapable.
const probeTimeout = 10 * time.Second

type capsKey struct {
	path string
	size int64
	mod  int64
}

var capsCache sync.Map // capsKey -> genCaps

// probeCaps runs `<path> run -h` and reads the flags it lists. The answer is
// kept per binary (path, size and modification time), so a run pays for it at
// most once. A binary that cannot be started or prints no flags is reported as
// not known.
func probeCaps(ctx context.Context, path string, env []string) genCaps {
	var key capsKey
	if st, err := os.Stat(path); err == nil {
		key = capsKey{path, st.Size(), st.ModTime().UnixNano()}
		if v, ok := capsCache.Load(key); ok {
			return v.(genCaps)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "run", "-h")
	cmd.Env = env
	// The exit status of -h differs between tools (0, or 2 from Go's flag
	// package); only the text matters.
	out, _ := cmd.CombinedOutput()
	caps := parseFlags(string(out))
	if key.path != "" && ctx.Err() == nil {
		capsCache.Store(key, caps)
	}
	return caps
}

// vegaLoadArgs is the command line of one vegaload run. Every run, streamed or
// not, keeps vegaload's own HTML report in the temporary folder and out of the
// browser: the page the user sees is litmus-lite's. A streamed run also asks
// for the per-second points in a file, when this vegaload can write one.
// streamFile is empty when no file is asked for; note says, once, what the
// user does not get. who names the copy ("vegaload 0.9.0 (~/bin/vegaload)").
func vegaLoadArgs(caps genCaps, who, dir, outFile string, streamed bool) (args []string, streamFile, note string) {
	args = []string{"run", "-output", "json", "-out", outFile}
	// Unknown capabilities (the help could not be read): -no-open and -report
	// exist in every vegaload that litmus-lite supports, so assume them.
	if !caps.known || caps.has("no-open") {
		args = append(args, "-no-open")
	} else {
		note = who + " has no -no-open; it may open its own report in the browser"
	}
	if !caps.known || caps.has("report") {
		args = append(args, "-report", filepath.Join(dir, "report.html"))
	}
	if streamed {
		if caps.has("stream-file") {
			streamFile = filepath.Join(dir, "stream.ndjson")
			args = append(args, "-stream-file", streamFile)
		} else if !caps.known {
			// Not the same as "has no -stream-file": its flags were never read.
			note = joinNotes(note, who+" did not list its flags (`run -h` gave nothing in time), so live load points are not available, the load series arrives when it finishes")
		} else if note == "" {
			note = who + " has no -stream-file; live load points are not available, the load series arrives when it finishes"
		} else {
			note += "; live load points are not available either"
		}
	}
	return args, streamFile, note
}

// joinNotes keeps every note of a generator run, once.
func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "" || a == b:
		return a
	}
	return fmt.Sprintf("%s; %s", a, b)
}
