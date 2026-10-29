package main

import "flag"

// parseArgs is fs.Parse that also reads flags written after the positional
// arguments, as in `validate FILE -strict` or `hub import ID -beside DIR`.
// The flag package stops at the first positional argument, so such a flag
// used to be ignored without a word: -strict did not apply, -beside wrote into
// the current directory, and an unknown flag was accepted.
//
// Everything after "--" stays positional. The MCP server puts a file name
// there so that a name starting with "-" can never be read as a flag.
func parseArgs(fs *flag.FlagSet, args []string) error {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			pos = append(pos, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	return fs.Parse(append([]string{"--"}, pos...))
}
