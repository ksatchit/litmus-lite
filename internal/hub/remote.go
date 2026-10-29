package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/litmuschaos/litmus-lite/internal/hosts"
	"gopkg.in/yaml.v3"
)

// Remote hub model (read this before changing anything in this file)
//
// The remote hub is a git repository pinned by .litmus-lite/hub.lock
// (repo + ref + optional path) whose catalog.yaml lists importable scenario
// templates. It is the ONLY place "exec" templates (user-defined commands used
// as faults or probes) are distributed; the embedded catalog is native-only.
//
// Invariants to preserve:
//
//  1. Importing a template only writes a *.chaos.yaml file. Nothing in this
//     package may execute template content, ever.
//  2. A pin never executes anything while it is imported. A scenario file is
//     code, though: a probe of type command and load.tool: command run what
//     they say when the scenario is RUN, and there is no allowlist for them
//     yet (the planned exec.allow / -allow-exec of the spec, SR-EXEC-01..05, is
//     not built). Read an imported scenario before running it.
//  3. Embedded IDs always win over a remote item with the same ID, so a pinned
//     repo cannot shadow a native fault template.
//  4. Fetches are cached per (repo, ref, path); pin refs to a tag or commit to
//     make an audit meaningful.
//
// TODO(exec-templates): when parsing items, surface exec content and the
// template's declared requirements (binaries, docker, os) at import time so the
// human sees exactly which commands a template would run before allowlisting.
// TODO(exec-templates): document the pin format, trust model, and how to publish
// and audit a template in a cookbook (spec FR-EXEC-16).

// Lock is the pin in .litmus-lite/hub.lock. Absent lock means embedded-only (no git).
type Lock struct {
	Repo string `yaml:"repo" json:"repo"`
	Ref  string `yaml:"ref" json:"ref"`
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
}

type remoteCatalogFile struct {
	Items []remoteItem `yaml:"items"`
}

type remoteItem struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Kind     string   `yaml:"kind"`
	OS       []string `yaml:"os"`
	Summary  string   `yaml:"summary"`
	File     string   `yaml:"file"`
	Template string   `yaml:"template"`
}

type pinFile struct {
	Repo string `yaml:"repo"`
	Ref  string `yaml:"ref"`
	Path string `yaml:"path,omitempty"`
}

func ParseLock(b []byte) (*Lock, error) {
	var l Lock
	if err := yaml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("hub.lock: %w", err)
	}
	l.Repo = strings.TrimSpace(l.Repo)
	l.Ref = strings.TrimSpace(l.Ref)
	l.Path = strings.TrimSpace(l.Path)
	if l.Repo == "" {
		return nil, fmt.Errorf("hub.lock: repo is required")
	}
	if l.Ref == "" {
		l.Ref = "main"
	}
	return &l, nil
}

// checkLock refuses a pin that git would read as an option or that points out
// of the clone. hub.lock lives in the project, so whoever wrote it (a
// repository you just cloned, say) must not be able to pass `--upload-pack=...`
// as the "repo", which git would run as a command.
func checkLock(lock Lock) error {
	for name, v := range map[string]string{"repo": lock.Repo, "ref": lock.Ref} {
		if strings.HasPrefix(v, "-") {
			return fmt.Errorf("hub.lock: %s %q must not start with '-'", name, v)
		}
		if strings.ContainsAny(v, "\x00\r\n") {
			return fmt.Errorf("hub.lock: %s has a control character", name)
		}
	}
	if lock.Path != "" && !filepath.IsLocal(lock.Path) {
		return fmt.Errorf("hub.lock: path %q must stay inside the repository", lock.Path)
	}
	return nil
}

func fetchRemote(lock Lock) ([]Item, error) {
	if err := checkLock(lock); err != nil {
		return nil, err
	}
	dest, err := syncRepo(lock)
	if err != nil {
		return nil, err
	}
	root := dest
	if lock.Path != "" {
		root = filepath.Join(dest, lock.Path)
	}
	return loadRemoteCatalog(root)
}

func syncRepo(lock Lock) (string, error) {
	home := hosts.Dir()
	if home == "" {
		return "", fmt.Errorf("cannot resolve host dir for hub cache")
	}
	dest := filepath.Join(home, "hub", cacheKey(lock))
	if cached(dest, lock) {
		return dest, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git is required to fetch hub.lock repo: %w", err)
	}
	tmp := dest + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// "--" ends the options, so the repo is never read as one; ext:: (a
	// transport that runs a command) is switched off; git never asks for a
	// password on this terminal.
	args := []string{"-c", "protocol.ext.allow=never", "clone", "--depth", "1"}
	if lock.Ref != "" {
		args = append(args, "--branch", lock.Ref)
	}
	args = append(args, "--", lock.Repo, tmp)
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("git clone: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_ = os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	b, _ := yaml.Marshal(pinFile{Repo: lock.Repo, Ref: lock.Ref, Path: lock.Path})
	if err := os.WriteFile(filepath.Join(dest, ".litmus-lite-pin"), b, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func cached(dest string, lock Lock) bool {
	b, err := os.ReadFile(filepath.Join(dest, ".litmus-lite-pin"))
	if err != nil {
		return false
	}
	var p pinFile
	if yaml.Unmarshal(b, &p) != nil {
		return false
	}
	return p.Repo == lock.Repo && p.Ref == lock.Ref && p.Path == lock.Path
}

func cacheKey(lock Lock) string {
	sum := sha256.Sum256([]byte(lock.Repo + "\n" + lock.Ref + "\n" + lock.Path))
	return hex.EncodeToString(sum[:])[:16]
}

// loadRemoteCatalog parses catalog.yaml from a synced remote hub checkout.
// It only reads data; template text is returned verbatim as Item.Template and
// is never interpreted here (see the model notes at the top of this file).
func loadRemoteCatalog(root string) ([]Item, error) {
	b, err := os.ReadFile(filepath.Join(root, "catalog.yaml"))
	if err != nil {
		return nil, fmt.Errorf("remote hub catalog.yaml: %w", err)
	}
	var file remoteCatalogFile
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("remote hub catalog.yaml: %w", err)
	}
	var out []Item
	for _, raw := range file.Items {
		if strings.TrimSpace(raw.ID) == "" {
			continue
		}
		tmpl := raw.Template
		if raw.File != "" {
			if !filepath.IsLocal(raw.File) {
				return nil, fmt.Errorf("remote hub item %s: file %q must stay inside the repository", raw.ID, raw.File)
			}
			tb, err := os.ReadFile(filepath.Join(root, raw.File))
			if err != nil {
				return nil, fmt.Errorf("remote hub item %s: %w", raw.ID, err)
			}
			tmpl = string(tb)
		}
		out = append(out, Item{
			ID: raw.ID, Title: raw.Title, Kind: raw.Kind, OS: raw.OS,
			Summary: raw.Summary, Source: "git", Template: tmpl,
		})
	}
	return out, nil
}
