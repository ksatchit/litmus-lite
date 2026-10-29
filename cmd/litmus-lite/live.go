package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/litmuschaos/litmus-lite/internal/live"
	"github.com/litmuschaos/litmus-lite/internal/stream"
)

// cmdLive manages the optional live view of `run`. It is dormant until a
// destination is configured, and this is the one command that configures it
// for good (the other two ways are -live-url and LITMUS_LITE_LIVE_URL).
//
//	litmus-lite live                 print the project page URL and where the identity is kept
//	litmus-lite live -open           also open it in the browser
//	litmus-lite live enable URL      keep URL as the live-view service of this machine
//	litmus-lite live disable         forget it again (the identity stays)
//	litmus-lite live reset           start a new project (the old runs stay under the old URL)
func cmdLive(args []string) int {
	fs := flag.NewFlagSet("live", flag.ContinueOnError)
	open := fs.Bool("open", false, "open the project page in the browser")
	url := fs.String("live-url", "", "live-view service (also LITMUS_LITE_LIVE_URL, or `live enable URL`)")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	sub := ""
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	usage := func() int {
		fmt.Fprintln(os.Stderr, "live [-open] [-live-url URL] | live enable URL | live disable | live reset")
		return 2
	}
	switch {
	case sub == "enable" && fs.NArg() == 2:
		return liveEnable(fs.Arg(1))
	case sub == "disable" && fs.NArg() == 1:
		had, err := live.Disable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "live:", err)
			return 1
		}
		if had {
			fmt.Fprintln(os.Stderr, "live: the kept live-view address is forgotten. LITMUS_LITE_LIVE_URL or -live-url can still turn it on for a run.")
		} else {
			fmt.Fprintln(os.Stderr, "live: no live-view address was kept")
		}
		return 0
	case sub == "reset" && fs.NArg() == 1:
	case sub == "" && fs.NArg() == 0:
	default:
		return usage()
	}
	reset := sub == "reset"

	base, err := live.Destination(*url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "live:", err)
		return 2
	}
	if base == nil {
		fmt.Fprintln(os.Stderr, "live: the live view is not set up, so nothing is sent anywhere. To use a live-view service: `litmus-lite live enable URL`, or set LITMUS_LITE_LIVE_URL, or pass -live-url to `run` (docs/reference/streaming.md, \"Live view\").")
		return 1
	}

	var id *live.Identity
	if reset {
		id, err = live.Reset()
	} else {
		id, err = live.Load()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "live:", err)
		return 1
	}
	switch {
	case reset:
		fmt.Fprintln(os.Stderr, "live: made a new project; runs of the old one stay under its old URL")
	case id.Created:
		fmt.Fprintln(os.Stderr, "live: made a project for this machine")
	}
	fmt.Println(id.PageURL(base))
	fmt.Fprintf(os.Stderr, "identity kept in %s (mode 0600). The push token in it is never shown or sent anywhere but to %s://%s.\n", id.Path, base.Scheme, base.Host)
	if *open {
		if err := live.Opener(id.PageURL(base)); err != nil {
			fmt.Fprintf(os.Stderr, "live: could not open the browser (%v)\n", err)
			return 1
		}
	}
	return 0
}

func liveEnable(raw string) int {
	// Keep nothing that a run would refuse: a remote http:// address would be
	// saved here and then rejected, with the project's token, on every run.
	if u, err := live.Destination(raw); err == nil && u != nil {
		if _, err := stream.ValidatePushURL(u.String()+"/v1/projects/p/ingest", false); err != nil {
			fmt.Fprintln(os.Stderr, "live:", err)
			return 2
		}
	}
	id, base, err := live.Enable(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "live:", err)
		return 2
	}
	fmt.Println(id.PageURL(base))
	fmt.Fprintf(os.Stderr, "live: runs now stream to %s://%s (not wanted for one run? -no-live or LITMUS_LITE_LIVE=off). Identity kept in %s (mode 0600).\n", base.Scheme, base.Host, id.Path)
	return 0
}
