package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Line struct {
	At      time.Time `json:"at"`
	Command string    `json:"command"`
	File    string    `json:"file,omitempty"`
	Passed  *bool     `json:"passed,omitempty"`
	// Stream names the sinks of a streamed run (file, pull, push:HOST). It never
	// holds a URL path, query or token.
	Stream string `json:"stream,omitempty"`
}

func Append(l Line) {
	if l.At.IsZero() {
		l.At = time.Now()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".litmus-lite")
	_ = os.MkdirAll(dir, 0o755) // shared with the hub cache (hosts.Dir)
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(l)
	_, _ = f.Write(append(b, '\n'))
}
