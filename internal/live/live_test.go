package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LITMUS_LITE_CONFIG_DIR", dir)
	t.Setenv("LITMUS_LITE_LIVE_URL", "")
	t.Setenv("LITMUS_LITE_LIVE", "")
	t.Setenv("LITMUS_LITE_OPEN", "")
	t.Setenv("CI", "")
	return dir
}

func TestFirstUseMakesAnIdentityAndKeepsIt(t *testing.T) {
	dir := isolate(t)
	a, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !a.Created || !strings.HasPrefix(a.ProjectID, "p-") || len(a.ProjectID) != 2+26 {
		t.Fatalf("project id %q", a.ProjectID)
	}
	// 128 bits of the id, 256 of the token: the id alone must not be guessable.
	if len(a.PushToken) < 40 || strings.Contains(a.ProjectID, a.PushToken) {
		t.Fatalf("token %q", a.PushToken)
	}
	if a.Path != filepath.Join(dir, "live.json") {
		t.Fatalf("path %s", a.Path)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(a.Path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v %v", st, err)
		}
	}
	b, err := Load()
	if err != nil || b.Created || b.ProjectID != a.ProjectID || b.PushToken != a.PushToken {
		t.Fatalf("second load %+v %v", b, err)
	}
}

func TestIdentitiesDiffer(t *testing.T) {
	isolate(t)
	a, _ := Load()
	b, _ := Reset()
	if a.ProjectID == b.ProjectID || a.PushToken == b.PushToken || !b.Created {
		t.Fatalf("reset kept something: %+v %+v", a, b)
	}
	c, _ := Load()
	if c.ProjectID != b.ProjectID {
		t.Fatal("reset was not saved")
	}
}

func TestDamagedFileIsAnErrorNotANewIdentity(t *testing.T) {
	dir := isolate(t)
	if err := os.WriteFile(filepath.Join(dir, "live.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("%v", err)
	}
	// It is left alone, so the user can look at it.
	if b, _ := os.ReadFile(filepath.Join(dir, "live.json")); string(b) != "{not json" {
		t.Fatal("the damaged file was overwritten")
	}
}

func TestDestinationPrecedenceAndRules(t *testing.T) {
	dir := isolate(t)

	// Nothing configured: no destination, and no file made by asking.
	u, err := Destination("")
	if u != nil || err != nil {
		t.Fatalf("nothing configured: %v %v", u, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "live.json")); err == nil {
		t.Fatal("asking for the destination made a file")
	}

	if _, _, err := Enable("https://from-file.example"); err != nil {
		t.Fatal(err)
	}
	u, _ = Destination("")
	if u == nil || u.String() != "https://from-file.example" {
		t.Fatalf("file: %v", u)
	}
	t.Setenv("LITMUS_LITE_LIVE_URL", "https://from-env.example/")
	u, _ = Destination("")
	if u.String() != "https://from-env.example" {
		t.Fatalf("env: %s", u)
	}
	u, _ = Destination("https://from-flag.example")
	if u.String() != "https://from-flag.example" {
		t.Fatalf("flag: %s", u)
	}

	for _, bad := range []string{"ftp://x", "nohost", "https://user:pw@x.example", "https://x.example/?k=v", "https://x.example/#f"} {
		if _, err := Destination(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		} else if strings.Contains(err.Error(), "pw") {
			t.Fatalf("the error repeats a credential: %v", err)
		}
	}

	id := &Identity{ProjectID: "p-abc"}
	base, _ := Destination("https://live.example")
	if got := id.PageURL(base); got != "https://live.example/p/p-abc" {
		t.Fatalf("page %s", got)
	}
	if got := id.PushURL(base); got != "https://live.example/v1/projects/p-abc/ingest?source=litmus-lite" {
		t.Fatalf("push %s", got)
	}
}

func TestEnableKeepsTheAddressAndDisableForgetsIt(t *testing.T) {
	isolate(t)
	if _, _, err := Enable("not a url"); err == nil {
		t.Fatal("a bad address was kept")
	}
	if u, _ := Destination(""); u != nil {
		t.Fatal("a refused address was kept")
	}

	id, base, err := Enable("https://live.example/")
	if err != nil || base.String() != "https://live.example" || !id.Created {
		t.Fatalf("enable: %+v %v %v", id, base, err)
	}
	again, _, _ := Enable("https://other.example")
	if again.Created || again.ProjectID != id.ProjectID || again.PushToken != id.PushToken {
		t.Fatalf("enabling again made a new identity: %+v", again)
	}
	if u, _ := Destination(""); u.String() != "https://other.example" {
		t.Fatalf("kept %v", u)
	}

	had, err := Disable()
	if err != nil || !had {
		t.Fatalf("disable: %v %v", had, err)
	}
	if u, _ := Destination(""); u != nil {
		t.Fatalf("still configured: %v", u)
	}
	back, _ := Load()
	if back.ProjectID != id.ProjectID {
		t.Fatal("disable lost the identity")
	}
	if had, _ := Disable(); had {
		t.Fatal("nothing was left to forget")
	}
}

func TestDisableMakesNoFile(t *testing.T) {
	dir := isolate(t)
	if had, err := Disable(); had || err != nil {
		t.Fatalf("%v %v", had, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "live.json")); err == nil {
		t.Fatal("disable made a file")
	}
}

func TestResetKeepsTheDestination(t *testing.T) {
	isolate(t)
	if _, _, err := Enable("https://live.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := Reset(); err != nil {
		t.Fatal(err)
	}
	if u, _ := Destination(""); u == nil || u.String() != "https://live.example" {
		t.Fatalf("reset dropped the address: %v", u)
	}
}

func TestEnabled(t *testing.T) {
	isolate(t)
	if !Enabled(false) {
		t.Fatal("not turned off, so usable")
	}
	if Enabled(true) {
		t.Fatal("-no-live")
	}
	t.Setenv("LITMUS_LITE_LIVE", "off")
	if Enabled(false) {
		t.Fatal("LITMUS_LITE_LIVE=off")
	}
	t.Setenv("LITMUS_LITE_LIVE", "")
	// A destination in a CI job was put there on purpose.
	t.Setenv("CI", "true")
	if !Enabled(false) {
		t.Fatal("CI must not switch a configured live view off")
	}
}

func TestOpenEnabled(t *testing.T) {
	isolate(t)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if !OpenEnabled(false) {
		t.Fatal("default is to open")
	}
	if OpenEnabled(true) {
		t.Fatal("-no-open")
	}
	t.Setenv("LITMUS_LITE_OPEN", "off")
	if OpenEnabled(false) {
		t.Fatal("LITMUS_LITE_OPEN=off")
	}
	t.Setenv("LITMUS_LITE_OPEN", "")
	t.Setenv("CI", "true")
	if OpenEnabled(false) {
		t.Fatal("never open a browser in CI")
	}
}

func TestProbe(t *testing.T) {
	serve := func(code int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
	}
	probe := func(s *httptest.Server) error {
		base, _ := Destination(s.URL)
		return Probe(context.Background(), base)
	}

	ok := serve(200, `{"status":"ok","projects":true}`)
	defer ok.Close()
	if err := probe(ok); err != nil {
		t.Fatalf("a good bridge: %v", err)
	}
	old := serve(200, `{"status":"ok"}`)
	defer old.Close()
	if err := probe(old); err == nil || !strings.Contains(err.Error(), "older version") {
		t.Fatalf("a bridge without projects: %v", err)
	}
	other := serve(200, `<html>hello</html>`)
	defer other.Close()
	if err := probe(other); err == nil {
		t.Fatal("something that is not a bridge")
	}
	down := serve(503, `{}`)
	defer down.Close()
	if err := probe(down); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("an unhealthy bridge: %v", err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ok.URL+"/v1/health", http.StatusFound)
	}))
	defer redirect.Close()
	if err := probe(redirect); err == nil {
		t.Fatal("a redirect must not be followed")
	}
	gone := serve(200, `{}`)
	gone.Close()
	err := probe(gone)
	if err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("a bridge that is not there: %v", err)
	}
	if strings.Contains(err.Error(), "/v1/health") {
		t.Fatalf("the error carries the URL: %v", err)
	}
}
