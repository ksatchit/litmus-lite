package failpoint

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
)

const tokenHeader = "X-Litmus-Lite-Token"

// Handler is the local control endpoint a scenario posts to.
// Mount it on a loopback route the business handler does not use.
// Creating the handler turns on window mode.
func Handler() http.Handler {
	UseWindow()
	return http.HandlerFunc(serveControl)
}

type controlState struct {
	Allowed []string `json:"allowed"`
	Armed   []string `json:"armed"`
}

type armBody struct {
	Name  string `json:"name"`
	Armed *bool  `json:"armed"`
}

func serveControl(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(tokenHeader)) {
		http.Error(w, "failpoint token rejected", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeState(w)
	case http.MethodPost:
		var body armBody
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "failpoint body must be JSON name and armed", http.StatusBadRequest)
			return
		}
		arm := true
		if body.Armed != nil {
			arm = *body.Armed
		}
		if arm {
			if err := Arm(body.Name); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
		} else {
			Disarm(body.Name)
		}
		writeState(w)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeState(w http.ResponseWriter) {
	allowed := allowedList()
	names := armedList()
	sort.Strings(allowed)
	sort.Strings(names)
	if allowed == nil {
		allowed = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(controlState{Allowed: allowed, Armed: names})
}

func tokenMatches(got string) bool {
	want := os.Getenv("LITMUS_LITE_FAILPOINT_TOKEN")
	if want == "" || got == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func tokenConfigured() bool {
	return strings.TrimSpace(os.Getenv("LITMUS_LITE_FAILPOINT_TOKEN")) != ""
}
