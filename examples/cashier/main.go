// Command cashier is the failpoint sample: one route, one instrumented call,
// healthy until a scenario arms the hook.
package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/litmuschaos/litmus-lite/failpoint"
)

const slowDelay = 500 * time.Millisecond

func main() {
	addr, err := listenAddr()
	if err != nil {
		log.Fatal(err)
	}
	failpoint.UseWindow()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/charge", handleCharge)
	mux.Handle("/_litmus/failpoint", failpoint.Handler())
	log.Printf("cashier http://%s", addr)
	log.Printf("failpoint allowlist %q", os.Getenv("LITMUS_LITE_FAILPOINT"))
	if os.Getenv("LITMUS_LITE_FAILPOINT_TOKEN") == "" {
		log.Print("LITMUS_LITE_FAILPOINT_TOKEN is empty; arm requests will be rejected")
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleCharge(w http.ResponseWriter, r *http.Request) {
	failpoint.Delay("charge-slow", slowDelay)
	if err := failpoint.Fail("charge"); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "paid"})
}

func listenAddr() (string, error) {
	addr := os.Getenv("CASHIER_ADDR")
	if addr == "" {
		return "127.0.0.1:8091", nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", errLoopback
	}
	return addr, nil
}

type loopbackError struct{}

func (loopbackError) Error() string { return "CASHIER_ADDR must be a loopback address" }

var errLoopback = loopbackError{}
