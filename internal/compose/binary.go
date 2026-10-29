package compose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// How a generator binary was found. It is part of what a report records, so a
// reader can tell which copy ran when there are several on one machine.
const (
	viaOverride = "explicit path"
	viaGiven    = "path in the scenario"
	viaPATH     = "PATH"
	viaBeside   = "next to litmus-lite"
)

// Status is what is known about the generator a load block will run, before it
// runs: where it is, which version it says it is, what it can do and whether
// something else on the machine looks like a better choice.
type Status struct {
	Tool    string // vegaload, k6 or command
	Bin     string // the name looked for
	Found   bool
	Path    string
	Via     string
	Version string // empty when the tool has no version command or says nothing
	Caps    genCaps
	// Warn is set when this copy is probably not the one the user wants, for
	// example an older vegaload on PATH while a newer one sits next to
	// litmus-lite. Empty means nothing to say.
	Warn string
}

// Inspect finds the generator of a load block and asks it a few cheap
// questions (its version and, for vegaload, its flags). It never runs a load.
// binOverride replaces the executable, as in Run.
func Inspect(ctx context.Context, baseDir string, load *scenario.LoadSpec, binOverride string) Status {
	st := Status{}
	if load == nil {
		return st
	}
	st.Tool = strings.ToLower(strings.TrimSpace(load.Tool))
	if st.Tool == "" {
		st.Tool = "vegaload"
	}
	st.Bin = st.Tool
	if st.Tool == "command" {
		st.Bin = strings.TrimSpace(load.Command)
	}
	path, via, err := locate(baseDir, st.Bin, binOverride)
	if err != nil {
		return st
	}
	st.Found, st.Path, st.Via = true, path, via
	env := childEnv(st.Tool, RunOptions{})
	switch st.Tool {
	case "vegaload":
		st.Caps = probeCaps(ctx, path, env)
		st.Version = probeVersion(ctx, path, env)
		if via == viaPATH {
			st.Warn = staleCopy(ctx, st, env)
		}
	case "k6":
		st.Version = probeVersion(ctx, path, env)
	}
	return st
}

// Name is the tool and its version, for a sentence: "vegaload 0.9.0".
func (s Status) Name() string {
	if s.Version == "" {
		return s.Tool
	}
	return s.Tool + " " + s.Version
}

// Message is the one line `doctor` shows for a generator that was found.
func (s Status) Message() string {
	msg := fmt.Sprintf("%s at %s (%s)", s.Name(), tilde(s.Path), s.Via)
	if s.Tool == "vegaload" && !s.Caps.known {
		msg += "; its flags could not be read, so live load points are not available"
	} else if s.Tool == "vegaload" && !s.Caps.has("stream-file") {
		msg += "; it has no -stream-file, so live load points are not available"
	}
	if s.Warn != "" {
		msg += "; " + s.Warn
	}
	return msg
}

// Degraded reports whether `doctor` should warn about a found generator.
func (s Status) Degraded() bool {
	return s.Warn != "" || (s.Tool == "vegaload" && !s.Caps.has("stream-file"))
}

// Generator is the record of this copy for a report. The path is the local
// file; the stream never carries it (see stream.BuildFinished).
func (s Status) Generator() *report.LoadGenerator {
	if !s.Found {
		return nil
	}
	return &report.LoadGenerator{Tool: s.Tool, Version: s.Version, Path: s.Path, Via: s.Via}
}

// locate is where a generator is looked for: an explicit path, then a path
// given in the scenario, then PATH, then the folder of this executable. It
// returns how it was found.
func locate(baseDir, name, override string) (path, via string, err error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", "", err
		}
		return override, viaOverride, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("empty load command")
	}
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		cand := name
		if !filepath.IsAbs(name) && baseDir != "" {
			cand = filepath.Join(baseDir, name)
		}
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, viaGiven, nil
		}
		if filepath.IsAbs(name) {
			return "", "", fmt.Errorf("%s: not found", name)
		}
	}
	p, lerr := exec.LookPath(name)
	if lerr == nil {
		return p, viaPATH, nil
	}
	// Not on PATH: look next to this executable. A GUI app such as an editor
	// often starts the MCP server with a short PATH, and two tools installed
	// into one folder should find each other either way.
	if !strings.ContainsAny(name, `/\`) {
		if p := besideExecutable(name); p != "" {
			return p, viaBeside, nil
		}
	}
	return "", "", lerr
}

// besideExecutable is the file called name (or name.exe) in the folder of the
// running litmus-lite, or "".
func besideExecutable(name string) string {
	dir := executableDir()
	if dir == "" {
		return ""
	}
	for _, cand := range []string{name, name + ".exe"} {
		p := filepath.Join(dir, cand)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// staleCopy says so when the vegaload that PATH gave is probably the wrong one:
// another copy sits next to litmus-lite and is newer, or can do what this one
// cannot (write a stream file). The order of the lookup is not changed: PATH
// still wins, because a user may pin a version there on purpose. The user is
// told once per run, in the run's warnings.
func staleCopy(ctx context.Context, used Status, env []string) string {
	other := besideExecutable(used.Bin)
	if other == "" {
		return ""
	}
	a, aerr := os.Stat(used.Path)
	b, berr := os.Stat(other)
	if aerr == nil && berr == nil && os.SameFile(a, b) {
		return "" // the same file, reached through a link
	}
	otherVer := probeVersion(ctx, other, env)
	otherCaps := probeCaps(ctx, other, env)
	why := ""
	if c, ok := compareVersions(used.Version, otherVer); ok && c < 0 {
		why = "newer"
	} else if !used.Caps.has("stream-file") && otherCaps.has("stream-file") {
		why = "able to stream live load points"
	}
	if why == "" {
		return ""
	}
	return fmt.Sprintf("%s from PATH is used, but the copy next to litmus-lite (%s, %s) is %s. Put that one first on PATH or remove the older one",
		used.Name(), versionOrUnknown(otherVer), tilde(other), why)
}

func versionOrUnknown(v string) string {
	if v == "" {
		return "version unknown"
	}
	return "version " + v
}

// tilde shortens a path under the home folder, so that a warning that travels
// with a report or an event does not carry the user's name more than needed.
func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

var versionRE = regexp.MustCompile(`(?i)\bv?(\d+\.\d+\.\d+[0-9A-Za-z.+-]*)`)

type versionKey struct {
	path string
	size int64
	mod  int64
}

var versionCache sync.Map // versionKey -> string

// probeVersion runs `<path> version` and reads the first version number in its
// first line ("vegaload 0.9.0", "k6 v0.49.0 (go1.21)"). It returns "" when the
// tool does not answer or says no number. The answer is kept per binary.
func probeVersion(ctx context.Context, path string, env []string) string {
	var key versionKey
	if st, err := os.Stat(path); err == nil {
		key = versionKey{path, st.Size(), st.ModTime().UnixNano()}
		if v, ok := versionCache.Load(key); ok {
			return v.(string)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout/2)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	cmd.Env = env
	out, _ := cmd.CombinedOutput()
	ver := parseVersion(string(out))
	if key.path != "" && ctx.Err() == nil {
		versionCache.Store(key, ver)
	}
	return ver
}

func parseVersion(out string) string {
	first := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if m := versionRE.FindStringSubmatch(first); m != nil {
		return m[1]
	}
	return ""
}

// compareVersions orders two release versions (-1, 0 or 1). It says not ok
// for a version that is missing, a development build or a pre-release: those
// cannot be ordered and the caller must not guess.
func compareVersions(a, b string) (int, bool) {
	pa, oka := releaseParts(a)
	pb, okb := releaseParts(b)
	if !oka || !okb {
		return 0, false
	}
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1, true
		case pa[i] > pb[i]:
			return 1, true
		}
	}
	return 0, true
}

func releaseParts(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false // "0-dev", "1-rc1", "2+build": not a plain release
		}
		out[i] = n
	}
	return out, true
}
