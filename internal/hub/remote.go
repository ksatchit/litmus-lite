package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/hosts"
	"gopkg.in/yaml.v3"
)

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

func fetchRemote(lock Lock) ([]Item, error) {
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
	args := []string{"clone", "--depth", "1"}
	if lock.Ref != "" {
		args = append(args, "--branch", lock.Ref)
	}
	args = append(args, lock.Repo, tmp)
	cmd := exec.Command("git", args...)
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
