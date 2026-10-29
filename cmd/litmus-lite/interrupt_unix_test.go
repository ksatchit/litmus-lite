//go:build unix

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// procState is the state letter of a process as ps shows it ("T" is stopped).
func procState(pid int) string {
	out, err := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// A fault must never be left in place because the user pressed Ctrl-C. This
// runs the real CLI, with no -stream flag and no live view (so the default
// path of `run`), pauses a real process, interrupts the run while it is
// paused, and checks the process was resumed and the run ended as interrupted.
func TestInterruptRollsBackFaultsWithoutAnyStreamFlag(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			victim := exec.Command("sleep", "120")
			if err := victim.Start(); err != nil {
				t.Skip("no sleep command:", err)
			}
			t.Cleanup(func() {
				_ = victim.Process.Signal(syscall.SIGCONT)
				_ = victim.Process.Kill()
				_, _ = victim.Process.Wait()
			})
			pid := victim.Process.Pid

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
			t.Cleanup(srv.Close)
			dir := t.TempDir()
			scenario := filepath.Join(dir, "pause.chaos.yaml")
			yaml := fmt.Sprintf(`
apiVersion: litmus-lite.io/v1
kind: Scenario
metadata: { name: interrupted }
target: { kind: http, baseUrl: %[1]s }
baseline: 300ms
recover: 300ms
steadyState:
  - { name: healthz, type: http, url: %[1]s/healthz, expect: { status: 200 } }
faults:
  - name: pause
    kind: process.pause
    duration: 40s
    params: { pid: %[2]d }
probes:
  - { name: list, type: http, url: "%[1]s/", interval: 100ms, timeout: 2s, expect: { status: 200 } }
hypotheses:
  - { name: recovers, metric: recovery, operator: "<=", value: 5s }
`, srv.URL, pid)
			if err := os.WriteFile(scenario, []byte(yaml), 0o644); err != nil {
				t.Fatal(err)
			}

			cli := exec.Command(os.Args[0], "run", "-allow-pid", fmt.Sprint(pid), "-out", filepath.Join(dir, "report.json"), scenario)
			cli.Env = append(os.Environ(), "LITMUS_LITE_TEST_AS_CLI=1", "HOME="+dir, "USERPROFILE="+dir)
			cli.Dir = dir
			if err := cli.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cli.Wait() }()
			t.Cleanup(func() { _ = cli.Process.Kill() })

			// Wait until the fault is really in place.
			deadline := time.Now().Add(15 * time.Second)
			for !strings.HasPrefix(procState(pid), "T") {
				if time.Now().After(deadline) {
					t.Fatalf("the process was never paused (state %q)", procState(pid))
				}
				time.Sleep(50 * time.Millisecond)
			}

			if err := cli.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				ee, ok := err.(*exec.ExitError)
				if !ok || ee.ExitCode() != 130 {
					t.Fatalf("run ended with %v, want exit code 130 (interrupted, not killed by the signal)", err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("the run did not end after the signal")
			}
			if st := procState(pid); strings.HasPrefix(st, "T") {
				t.Fatalf("the process is still paused after the run ended (state %q): the fault was not rolled back", st)
			}
			if _, err := os.Stat(filepath.Join(dir, "report.json")); err == nil {
				t.Fatal("an interrupted run wrote a report")
			}
		})
	}
}
