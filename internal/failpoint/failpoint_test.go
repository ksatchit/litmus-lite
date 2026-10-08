package failpoint

import (
	"testing"
	"time"
)

func TestOffByDefault(t *testing.T) {
	if err := Fail("x"); err != nil {
		t.Fatal(err)
	}
	Delay("x", 50*time.Millisecond)
}

func TestArmed(t *testing.T) {
	t.Setenv("LITMUS_LITE_FAILPOINT", "db")
	if err := Fail("db"); err == nil {
		t.Fatal("expected error")
	}
	if err := Fail("other"); err != nil {
		t.Fatal(err)
	}
}
