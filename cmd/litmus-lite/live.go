package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ksatchit/litmus-lite/internal/live"
)

// cmdLive shows or resets the live-view identity of this machine.
//
//	litmus-lite live           print the project page URL and where it is kept
//	litmus-lite live -open     also open it in the browser
//	litmus-lite live reset     start a new project (the old runs stay under the old URL)
func cmdLive(args []string) int {
	fs := flag.NewFlagSet("live", flag.ContinueOnError)
	open := fs.Bool("open", false, "open the project page in the browser")
	url := fs.String("live-url", "", "live view service (default: LITMUS_LITE_LIVE_URL, or "+live.DefaultBase+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reset := fs.NArg() == 1 && fs.Arg(0) == "reset"
	if fs.NArg() > 1 || (fs.NArg() == 1 && !reset) {
		fmt.Fprintln(os.Stderr, "live [-open] [-live-url URL] | live reset")
		return 2
	}

	var id *live.Identity
	var err error
	if reset {
		id, err = live.Reset()
	} else {
		id, err = live.Load()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "live:", err)
		return 1
	}
	base, err := id.BaseURL(*url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "live:", err)
		return 2
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
