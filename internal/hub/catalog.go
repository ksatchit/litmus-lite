package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Catalog merges the embedded hub with an optional git pin from .litmus-lite/hub.lock.
// Embedded IDs always win over a remote item with the same id.
type Catalog struct {
	root      string
	lock      *Lock
	remote    []Item
	fetched   bool
	remoteErr error
}

func Open(root string) *Catalog {
	if root == "" {
		root, _ = os.Getwd()
	}
	c := &Catalog{root: root}
	if lock, err := FindLock(root); err == nil {
		c.lock = lock
	}
	return c
}

func (c *Catalog) List() ([]Item, error) {
	out := List()
	if err := c.ensureRemote(); err != nil {
		return out, err
	}
	seen := map[string]struct{}{}
	for _, it := range out {
		seen[it.ID] = struct{}{}
	}
	for _, it := range c.remote {
		if _, ok := seen[it.ID]; ok {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (c *Catalog) Get(id string) (Item, error) {
	if it, err := Get(id); err == nil {
		return it, nil
	}
	if err := c.ensureRemote(); err != nil {
		return Item{}, err
	}
	for _, it := range c.remote {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("unknown hub id %q", id)
}

func (c *Catalog) Search(q string) ([]Item, error) {
	items, err := c.List()
	if err != nil {
		return items, err
	}
	q = strings.ToLower(q)
	var out []Item
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.ID+" "+it.Title+" "+it.Kind+" "+it.Summary), q) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (c *Catalog) Import(id, beside, name string) (string, error) {
	it, err := c.Get(id)
	if err != nil {
		return "", err
	}
	return writeImport(it, beside, name)
}

func (c *Catalog) ensureRemote() error {
	if c.fetched {
		return c.remoteErr
	}
	c.fetched = true
	if c.lock == nil {
		return nil
	}
	items, err := fetchRemote(*c.lock)
	c.remote = items
	c.remoteErr = err
	return err
}

func FindLock(start string) (*Lock, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 16; i++ {
		p := filepath.Join(dir, ".litmus-lite", "hub.lock")
		if b, err := os.ReadFile(p); err == nil {
			return ParseLock(b)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, fmt.Errorf("no .litmus-lite/hub.lock")
}
