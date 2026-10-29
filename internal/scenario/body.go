package scenario

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// BodyKeys are the assertions an HTTP probe reads under expect.body.
var BodyKeys = []string{"contains", "notContains", "matches"}

// MaxBodyBytes is how much of a response an HTTP probe reads when it has a
// body assertion. The assertion judges that much and no more.
const MaxBodyBytes = 1 << 20

// BodyChecks is expect.body of an HTTP probe, parsed. The zero value asserts
// nothing, and then the probe does not read the body at all.
type BodyChecks struct {
	Contains    []string
	NotContains []string
	Matches     []*regexp.Regexp
}

// Any reports whether there is anything to check.
func (b BodyChecks) Any() bool {
	return len(b.Contains)+len(b.NotContains)+len(b.Matches) > 0
}

// Check runs the assertions on body and returns the first that fails, as a
// reason that names the assertion and never quotes the body: a response can
// hold a token or a person's data, and the reason ends up in a report.
func (b BodyChecks) Check(body []byte) (string, bool) {
	text := string(body)
	for _, s := range b.Contains {
		if !strings.Contains(text, s) {
			return fmt.Sprintf("body does not contain %q", s), false
		}
	}
	for _, s := range b.NotContains {
		if strings.Contains(text, s) {
			return fmt.Sprintf("body contains %q, which it must not", s), false
		}
	}
	for _, re := range b.Matches {
		if !re.MatchString(text) {
			return fmt.Sprintf("body does not match %q", re.String()), false
		}
	}
	return "", true
}

// BodyExpect parses expect.body of an HTTP probe. A value that is not what the
// assertion needs (a number for contains, a regular expression that does not
// compile) is an error: a probe that quietly skipped the assertion would pass
// on a check the author thought it made. Each key takes a string, or a list of
// strings that must all hold.
func BodyExpect(expect map[string]any) (BodyChecks, error) {
	var out BodyChecks
	raw, ok := expect["body"]
	if !ok {
		return out, nil
	}
	m, ok := asMap(raw)
	if !ok {
		return out, fmt.Errorf("expect.body must hold %s, not %v", strings.Join(BodyKeys, ", "), raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals, err := stringList(m[k])
		if err != nil {
			return out, fmt.Errorf("expect.body.%s: %w", k, err)
		}
		switch k {
		case "contains":
			out.Contains = vals
		case "notContains":
			out.NotContains = vals
		case "matches":
			for _, v := range vals {
				re, err := regexp.Compile(v)
				if err != nil {
					return out, fmt.Errorf("expect.body.matches %q: %w", v, err)
				}
				out.Matches = append(out.Matches, re)
			}
		default:
			return out, fmt.Errorf("expect.body.%s is not an assertion; use %s", k, strings.Join(BodyKeys, ", "))
		}
	}
	return out, nil
}

func asMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	}
	return nil, false
}

func stringList(v any) ([]string, error) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil, fmt.Errorf("an empty string asserts nothing")
		}
		return []string{t}, nil
	case []any:
		if len(t) == 0 {
			return nil, fmt.Errorf("an empty list asserts nothing")
		}
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("every entry must be a non-empty string, got %v", e)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("must be a string or a list of strings, got %v", v)
}
