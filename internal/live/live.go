// Package live is the optional "watch this run in your browser" feature of
// `run`.
//
// It is dormant until a destination is configured: nothing is sent, no browser
// is opened and no file is written on a machine that never configured one. A
// destination is the address of a live-view service that speaks the ingest and
// project-page protocol of docs/reference/streaming.md, given by -live-url, by
// LITMUS_LITE_LIVE_URL, or kept by `litmus-lite live enable URL`.
//
// Once there is one, the first use makes an identity on this machine: a
// project id, which is what the page URL contains, and a push token. The
// service creates the project when it first sees the id, so nobody has to
// visit it first.
//
// The identity is kept in the user's config directory (mode 0600), never in a
// scenario file, a report, the audit log or any event. The token is sent only
// as a bearer token to the live-view service. See docs/reference/streaming.md, "Live view".
package live

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Identity is what this machine sends to the live-view service.
type Identity struct {
	ProjectID string    `json:"projectId"`
	PushToken string    `json:"pushToken"`
	Base      string    `json:"base,omitempty"` // the destination kept by `live enable`
	CreatedAt time.Time `json:"createdAt"`

	Created bool   `json:"-"` // made by this call
	Path    string `json:"-"` // where it is kept
}

// Dir is the directory of the identity file: $LITMUS_LITE_CONFIG_DIR, or
// litmus-lite in the user's config directory.
func Dir() (string, error) {
	if d := os.Getenv("LITMUS_LITE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "litmus-lite"), nil
}

var b32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("live: no randomness: " + err.Error())
	}
	return b
}

func newIdentity() *Identity {
	return &Identity{
		// 128 bits: the page URL is the only thing that protects reading.
		ProjectID: "p-" + b32.EncodeToString(randomBytes(16)),
		PushToken: base64.RawURLEncoding.EncodeToString(randomBytes(32)),
		CreatedAt: time.Now().UTC(),
	}
}

// Load returns the identity of this machine, making and saving one when there
// is none yet (Created is then true). A file that cannot be read or written is
// an error: an identity that is not kept would make every run a new project.
func Load() (*Identity, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "live.json")
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		var id Identity
		if err := json.Unmarshal(b, &id); err != nil || id.ProjectID == "" || id.PushToken == "" {
			return nil, fmt.Errorf("%s is damaged; remove it, or run `litmus-lite live reset`", path)
		}
		id.Path = path
		return &id, nil
	case errors.Is(err, os.ErrNotExist):
		return save(path, newIdentity(), true)
	default:
		return nil, err
	}
}

// Reset replaces the identity with a new one. Runs of the old project stay
// where they are, under the old URL.
func Reset() (*Identity, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	var base string
	if old, err := Load(); err == nil {
		base = old.Base
	}
	id := newIdentity()
	id.Base = base
	return save(filepath.Join(dir, "live.json"), id, true)
}

func save(path string, id *Identity, created bool) (*Identity, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(id, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	id.Created, id.Path = created, path
	return id, nil
}

// Destination picks the live-view service: the flag, then
// LITMUS_LITE_LIVE_URL, then the address kept by `live enable`. It returns nil
// (and no error) when none is configured, which is the normal state of a
// machine that has not asked for a live view. It never makes or writes a file.
// The URL has no trailing slash, and one with credentials, a query or a
// fragment is refused.
func Destination(flag string) (*url.URL, error) {
	raw := flag
	if raw == "" {
		raw = os.Getenv("LITMUS_LITE_LIVE_URL")
	}
	if raw == "" {
		raw = keptBase()
	}
	if raw == "" {
		return nil, nil
	}
	return parseBase(raw)
}

// keptBase reads the address `live enable` kept, without making the file. An
// unreadable or damaged file counts as none here; Load reports damage.
func keptBase() string {
	dir, err := Dir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "live.json"))
	if err != nil {
		return ""
	}
	var id Identity
	if json.Unmarshal(b, &id) != nil {
		return ""
	}
	return id.Base
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("live view URL %q: want http:// or https:// with a host", strings.SplitN(raw, "?", 2)[0])
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("live view URL %s://%s: no credentials, query or fragment", u.Scheme, u.Host)
	}
	return u, nil
}

// Enable keeps base as this machine's live-view destination, making the
// identity if there is none yet. It is the one command that turns the feature
// on without a flag or an environment variable.
func Enable(raw string) (*Identity, *url.URL, error) {
	base, err := parseBase(raw)
	if err != nil {
		return nil, nil, err
	}
	id, err := Load()
	if err != nil {
		return nil, nil, err
	}
	id.Base = base.String()
	created := id.Created
	if _, err := save(id.Path, id, created); err != nil {
		return nil, nil, err
	}
	return id, base, nil
}

// Disable forgets the kept destination. The identity stays, so enabling the
// same service again returns to the same project. It reports whether there was
// one to forget.
func Disable() (bool, error) {
	dir, err := Dir()
	if err != nil {
		return false, err
	}
	path := filepath.Join(dir, "live.json")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	id, err := Load()
	if err != nil {
		return false, err
	}
	had := id.Base != ""
	id.Base = ""
	_, err = save(path, id, false)
	return had, err
}

// PushURL is where the events of this machine's project are sent.
func (id *Identity) PushURL(base *url.URL) string {
	return base.String() + "/v1/projects/" + id.ProjectID + "/ingest?source=litmus-lite"
}

// PageURL is the page that shows the latest run of this machine's project.
func (id *Identity) PageURL(base *url.URL) string {
	return base.String() + "/p/" + id.ProjectID
}

// Enabled reports whether a configured live view may be used by this run:
// not when -no-live or LITMUS_LITE_LIVE=off says no. It does not say there is
// a destination; Destination does. CI does not switch it off, because a
// destination in a CI job was put there on purpose; CI only keeps the
// browser closed (OpenEnabled).
func Enabled(forceOff bool) bool {
	return !forceOff && !envOff("LITMUS_LITE_LIVE")
}

// OpenEnabled reports whether the browser should be opened.
func OpenEnabled(noOpen bool) bool {
	if noOpen || envOff("LITMUS_LITE_OPEN") {
		return false
	}
	ci := strings.ToLower(os.Getenv("CI"))
	if ci != "" && ci != "0" && ci != "false" {
		return false
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return true
}

func envOff(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// Probe checks that a live-view service answers at base and accepts projects.
// It is quick (1.5 s at most) and follows no redirects, so a machine with no
// service, or no network, loses almost nothing.
func Probe(ctx context.Context, base *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/v1/health", nil)
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("not reachable (%v)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("answered %d to the health check", resp.StatusCode)
	}
	var h struct {
		Status   string `json:"status"`
		Projects bool   `json:"projects"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&h); err != nil || h.Status != "ok" {
		return errors.New("is not a live-view service")
	}
	if !h.Projects {
		return errors.New("is an older version without projects")
	}
	return nil
}

// Opener opens a URL in the user's browser. Tests replace it.
var Opener = openBrowser

func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
