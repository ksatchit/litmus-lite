package mcp

import "testing"

func TestEvalSuite(t *testing.T) {
	p, f, _ := Eval()
	if f != 0 || p < 2 {
		t.Fatalf("passed=%d failed=%d", p, f)
	}
}
