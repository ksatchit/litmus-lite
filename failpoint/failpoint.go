// Package failpoint is the Mode B SDK for Go services.
//
// Hooks no-op unless LITMUS_LITE_FAILPOINT lists the name (or "*").
// That allowlist alone is enough for a unit test: Fail and Delay fire for
// the whole process.
//
// A service that should stay healthy until a scenario runs calls UseWindow
// (Handler does this). Allowlisted hooks then stay dark until Arm, and go
// dark again on Disarm. litmus-lite's failpoint fault does that over HTTP
// for the inject window only.
package failpoint

import (
	"os"
	"strings"
	"sync"
	"time"
)

var (
	mu     sync.Mutex
	window bool
	armed  = map[string]struct{}{}
)

// UseWindow keeps allowlisted hooks dark until Arm. Call it at process
// start when a scenario should own the inject window.
func UseWindow() {
	mu.Lock()
	window = true
	mu.Unlock()
}

// Arm marks name active. The name must be on the LITMUS_LITE_FAILPOINT allowlist.
func Arm(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errString("failpoint name is empty")
	}
	if !allowed(name) {
		return errString("failpoint " + name + " is not listed in LITMUS_LITE_FAILPOINT")
	}
	mu.Lock()
	armed[name] = struct{}{}
	mu.Unlock()
	return nil
}

// Disarm clears name. A name that was not armed is left unchanged.
func Disarm(name string) {
	name = strings.TrimSpace(name)
	mu.Lock()
	delete(armed, name)
	mu.Unlock()
}

// Delay sleeps d when name is active.
func Delay(name string, d time.Duration) {
	if active(name) && d > 0 {
		time.Sleep(d)
	}
}

// Fail returns an error when name is active.
func Fail(name string) error {
	if active(name) {
		return errFailed(name)
	}
	return nil
}

// Panic panics when name is active. In a service you want to keep up, use Fail: a panic
// exits the process, so the recovery probes have nothing left to call.
func Panic(name string) {
	if active(name) {
		panic(errFailed(name))
	}
}

func active(name string) bool {
	if !allowed(name) {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	if !window {
		return true
	}
	_, ok := armed[name]
	return ok
}

func allowed(name string) bool {
	raw := os.Getenv("LITMUS_LITE_FAILPOINT")
	if raw == "" || name == "" {
		return false
	}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == name || p == "*" {
			return true
		}
	}
	return false
}

func allowedList() []string {
	raw := os.Getenv("LITMUS_LITE_FAILPOINT")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func armedList() []string {
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, 0, len(armed))
	for name := range armed {
		out = append(out, name)
	}
	return out
}

type errString string

func (e errString) Error() string { return string(e) }

func errFailed(name string) error {
	return errString("litmus-lite failpoint " + name)
}

func resetState() {
	mu.Lock()
	window = false
	armed = map[string]struct{}{}
	mu.Unlock()
}
