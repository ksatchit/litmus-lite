package faults

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

// ParamDeclarer is implemented by an injector that lists the params it reads,
// so that a key it does not read (a typo such as "dealy") is reported instead
// of silently doing nothing. A key with a dot is a key inside a map param:
// "statusOverride.percent" is "percent" inside "statusOverride".
//
// An injector that does not implement it is not checked. New kinds should
// implement it.
type ParamDeclarer interface {
	ParamKeys() []string
}

// UnknownParams returns the params of f that its injector does not read, as
// "name" or "map.name", sorted. It returns nil for a kind that is not
// registered or does not declare its params.
func UnknownParams(f scenario.Fault) []string {
	in, err := Lookup(f.Kind)
	if err != nil {
		return nil
	}
	d, ok := in.(ParamDeclarer)
	if !ok {
		return nil
	}
	top := map[string]bool{}
	nested := map[string]map[string]bool{}
	for _, k := range d.ParamKeys() {
		if outer, inner, isNested := strings.Cut(k, "."); isNested {
			if nested[outer] == nil {
				nested[outer] = map[string]bool{}
			}
			nested[outer][inner] = true
			top[outer] = true
			continue
		}
		top[k] = true
	}
	var out []string
	for k, v := range f.Params {
		if !top[k] {
			out = append(out, k)
			continue
		}
		inner, ok := nested[k]
		if !ok {
			continue
		}
		m, isMap := v.(map[string]any)
		if !isMap {
			continue
		}
		for ik := range m {
			if !inner[ik] {
				out = append(out, k+"."+ik)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Lint reports what is wrong with the faults of a scenario without stopping
// it from running: params the kind does not read. Each message names the
// fault, the kind, the key, and the closest key it does read when there is
// one. `run` and `validate` show them; -strict turns them into errors.
func Lint(sc *scenario.Scenario) []string {
	var out []string
	for _, f := range sc.Faults {
		in, err := Lookup(f.Kind)
		if err != nil {
			continue
		}
		d, ok := in.(ParamDeclarer)
		if !ok {
			continue
		}
		known := d.ParamKeys()
		for _, k := range UnknownParams(f) {
			msg := fmt.Sprintf("fault %q (%s): unknown param %q, it is ignored", f.Name, f.Kind, k)
			if s := closest(k, known); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			} else if len(known) > 0 {
				msg += "; known params: " + strings.Join(known, ", ")
			}
			out = append(out, msg)
		}
	}
	return append(out, lintTraffic(sc)...)
}

// closest returns the entry of known nearest to key when it is near enough to
// be a typo, and "" otherwise.
func closest(key string, known []string) string {
	best, bestD := "", 3
	for _, k := range known {
		if d := editDistance(strings.ToLower(key), strings.ToLower(k)); d < bestD {
			best, bestD = k, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
