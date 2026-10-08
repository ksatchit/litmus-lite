// Package failpoint is the Mode B scaffold: named hooks that no-op unless
// LITMUS_LITE_FAILPOINT lists the name. Application code may call these later;
// Mode A does not require them.
package failpoint

import (
	"os"
	"strings"
	"time"
)

func enabled(name string) bool {
	raw := os.Getenv("LITMUS_LITE_FAILPOINT")
	if raw == "" || name == "" {
		return false
	}
	for _, p := range strings.Split(raw, ",") {
		if strings.TrimSpace(p) == name || strings.TrimSpace(p) == "*" {
			return true
		}
	}
	return false
}

func Delay(name string, d time.Duration) {
	if enabled(name) {
		time.Sleep(d)
	}
}

func Fail(name string) error {
	if enabled(name) {
		return errFailed(name)
	}
	return nil
}

type fpError string

func (e fpError) Error() string { return string(e) }

func errFailed(name string) error {
	return fpError("litmus-lite failpoint " + name)
}
