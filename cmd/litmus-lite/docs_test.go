package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The README and the docs site tell a reader to run files by path. A rename
// that leaves one of those paths behind sends a stranger's first five minutes
// into "no such file", so every examples/ path the docs mention must exist.
func TestDocsNameExamplesThatExist(t *testing.T) {
	root := filepath.Join("..", "..")
	docs := []string{"README.md", "SECURITY.md", "CONTRIBUTING.md"}
	more := []string{filepath.Join(root, "docs", "index.md")}
	for _, section := range []string{"getting-started", "concepts", "guides", "reference", "contributing", "project"} {
		pages, _ := filepath.Glob(filepath.Join(root, "docs", section, "*.md"))
		more = append(more, pages...)
	}
	for _, d := range docs {
		more = append(more, filepath.Join(root, d))
	}
	paths := regexp.MustCompile("examples/[A-Za-z0-9_./-]+\\.(?:chaos\\.yaml|js|go)")
	seen := 0
	for _, doc := range more {
		b, err := os.ReadFile(doc)
		if err != nil {
			continue
		}
		for _, p := range paths.FindAllString(string(b), -1) {
			seen++
			if _, err := os.Stat(filepath.Join(root, p)); err != nil {
				t.Errorf("%s mentions %s, which does not exist", doc, p)
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no example paths in the docs: the test is looking in the wrong place")
	}
}

// The quick start in the README runs two scenarios of examples/cashier. They
// must keep validating under -strict, and the red one must stay red for the
// reason the README gives (a hypothesis it cannot meet), not because the file
// stopped loading.
func TestQuickStartScenariosValidateStrict(t *testing.T) {
	for _, f := range []string{"charge.chaos.yaml", "charge-must-survive.chaos.yaml"} {
		path := filepath.Join("..", "..", "examples", "cashier", f)
		if code, out, errText := run2(t, "validate", "-strict", path); code != 0 {
			t.Errorf("%s: exit %d: %s%s", f, code, out, errText)
		}
	}
}
