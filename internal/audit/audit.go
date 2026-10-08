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
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(l)
	_, _ = f.Write(append(b, '\n'))
}
