package failpoint

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func isolate(t *testing.T) {
	t.Helper()
	resetState()
	t.Cleanup(resetState)
}

func TestOffByDefault(t *testing.T) {
	isolate(t)
	if err := Fail("x"); err != nil {
		t.Fatal(err)
	}
	Delay("x", 50*time.Millisecond)
	Panic("x")
}

func TestArmedByEnv(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "db")
	if err := Fail("db"); err == nil {
		t.Fatal("expected error")
	}
	if err := Fail("other"); err != nil {
		t.Fatal(err)
	}
}

func TestWindowStaysDarkUntilArmed(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	UseWindow()
	if err := Fail("charge"); err != nil {
		t.Fatal(err)
	}
	if err := Arm("charge"); err != nil {
		t.Fatal(err)
	}
	if err := Fail("charge"); err == nil {
		t.Fatal("expected error")
	}
	Disarm("charge")
	if err := Fail("charge"); err != nil {
		t.Fatal(err)
	}
}

func TestArmRefusesUnlisted(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	if err := Arm("other"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDelayOnlyWhenActive(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge-slow")
	UseWindow()
	start := time.Now()
	Delay("charge-slow", 250*time.Millisecond)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("slept while disarmed")
	}
	if err := Arm("charge-slow"); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	Delay("charge-slow", 30*time.Millisecond)
	if time.Since(start) < 20*time.Millisecond {
		t.Fatal("did not sleep")
	}
}

func TestPanicWhenActive(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "boom")
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	Panic("boom")
}

func TestHandlerArmsAndDisarms(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge,charge-slow")
	t.Setenv("LITMUS_LITE_FAILPOINT_TOKEN", "cashier-demo")
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	if err := Fail("charge"); err != nil {
		t.Fatal(err)
	}
	res := postArm(t, srv.URL, "cashier-demo", `{"name":"charge","armed":true}`)
	if res.StatusCode != 200 {
		t.Fatalf("arm %d", res.StatusCode)
	}
	if err := Fail("charge"); err == nil {
		t.Fatal("expected error")
	}
	res = postArm(t, srv.URL, "cashier-demo", `{"name":"charge","armed":false}`)
	if res.StatusCode != 200 {
		t.Fatalf("disarm %d", res.StatusCode)
	}
	if err := Fail("charge"); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerRejectsTokenAndUnknownName(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	t.Setenv("LITMUS_LITE_FAILPOINT_TOKEN", "cashier-demo")
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	res := postArm(t, srv.URL, "nope", `{"name":"charge","armed":true}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token %d", res.StatusCode)
	}
	res = postArm(t, srv.URL, "cashier-demo", `{"name":"other","armed":true}`)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("name %d", res.StatusCode)
	}
	if err := Fail("charge"); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerRequiresTokenEnv(t *testing.T) {
	isolate(t)
	t.Setenv("LITMUS_LITE_FAILPOINT", "charge")
	t.Setenv("LITMUS_LITE_FAILPOINT_TOKEN", "")
	if tokenConfigured() {
		t.Fatal("token should be empty")
	}
	srv := httptest.NewServer(Handler())
	defer srv.Close()
	res := postArm(t, srv.URL, "cashier-demo", `{"name":"charge"}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func postArm(t *testing.T, url, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var st controlState
	if res.StatusCode == 200 {
		if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
			t.Fatal(err)
		}
		if st.Armed == nil || !strings.Contains(res.Header.Get("Content-Type"), "json") {
			t.Fatalf("state %+v", st)
		}
	}
	return res
}
