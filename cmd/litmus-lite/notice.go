package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// maxNoticeCommand is how much of a command the notice shows. The scenario
// file has the rest; the notice is a prompt to read it.
const maxNoticeCommand = 200

// commandsToRun lists what a scenario runs on this machine as a command, in
// the words the notice shows: a probe of type cmd or command, and a load tool
// of type command. Nothing limits what those do, so a scenario from somewhere
// else must be read before it is run. (The faults have their own guards, and a
// load tool of type vegaload or k6 runs that tool, not a line from the file.)
//
// Each line is quoted, so a control character or an escape sequence in a
// command cannot rewrite what the terminal shows.
func commandsToRun(sc *scenario.Scenario) []string {
	var out []string
	for _, p := range append(append([]scenario.Probe(nil), sc.SteadyState...), sc.Probes...) {
		switch strings.ToLower(p.Type) {
		case "cmd", "command":
			out = append(out, fmt.Sprintf("probe %s: sh -c %s", quoteShort(p.Name, 60), quoteShort(p.Command, maxNoticeCommand)))
		}
	}
	if sc.Load != nil && sc.Load.Tool == "command" {
		line := "load: " + quoteShort(sc.Load.Command, maxNoticeCommand)
		if len(sc.Load.Args) > 0 {
			line += " with " + quoteShort(strings.Join(sc.Load.Args, " "), maxNoticeCommand)
		}
		out = append(out, line)
	}
	return out
}

func quoteShort(s string, max int) string {
	r := []rune(s)
	suffix := ""
	if len(r) > max {
		r, suffix = r[:max], "..."
	}
	q := strconv.Quote(string(r))
	if suffix != "" {
		q = q[:len(q)-1] + suffix + `"`
	}
	return q
}

// printCommandNotice writes the notice to w, if there is anything to say.
func printCommandNotice(w io.Writer, cmds []string) {
	if len(cmds) == 0 {
		return
	}
	fmt.Fprintln(w, "notice: running this scenario runs these commands on this machine, with your permissions:")
	for _, c := range cmds {
		fmt.Fprintln(w, "  "+c)
	}
	fmt.Fprintln(w, "  litmus-lite does not limit what they do. Read them before you run a scenario you did not write.")
}
