package scenario

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// number is what a bare hypothesis value must look like: digits with an
// optional fraction. No sign, no exponent, no "NaN" or "Inf" (ParseFloat
// accepts those, and a limit of NaN makes every comparison false or, worse,
// every "!=" true).
var number = regexp.MustCompile(`^(\d+(\.\d*)?|\.\d+)$`)

// Limit is the value of the hypothesis in the unit its metric is measured in:
//
//	error_rate, availability   percent, 0 to 100 (a trailing % is allowed)
//	p50, p95, p99              milliseconds (a bare number is milliseconds;
//	                           otherwise a duration such as 500ms, 1.5s, 2m)
//	recovery                   seconds (a bare number is seconds; otherwise a
//	                           duration such as 500ms, 5s, 1m)
//	rps                        requests per second, a bare number
//
// A value that is not valid for the metric is an error and never 0: a limit
// that silently became 0 made "availability >= abc" pass.
func (h Hypothesis) Limit() (float64, error) {
	raw := strings.TrimSpace(h.Value)
	bad := func(want string) (float64, error) {
		return 0, fmt.Errorf("hypothesis %q: value %q is not valid for %s (want %s)", h.Name, h.Value, h.Metric, want)
	}
	switch h.Metric {
	case "error_rate", "availability":
		n := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
		if !number.MatchString(n) {
			return bad("a percentage such as 5 or 5%")
		}
		f, _ := strconv.ParseFloat(n, 64)
		if f > 100 {
			return bad("a percentage between 0 and 100")
		}
		return f, nil
	case "p50", "p95", "p99":
		d, ok := durationOrNumber(raw, time.Millisecond)
		if !ok {
			return bad("a duration such as 500ms or 1.5s, or a bare number of milliseconds")
		}
		return float64(d) / float64(time.Millisecond), nil
	case "recovery":
		d, ok := durationOrNumber(raw, time.Second)
		if !ok {
			return bad("a duration such as 5s or 500ms, or a bare number of seconds")
		}
		return d.Seconds(), nil
	case "rps":
		if !number.MatchString(raw) {
			return bad("a number of requests per second")
		}
		f, _ := strconv.ParseFloat(raw, 64)
		return f, nil
	default:
		return 0, fmt.Errorf("unknown hypothesis metric %q", h.Metric)
	}
}

// durationOrNumber reads raw as a bare number of unit, or as a Go duration.
// A negative or non-finite duration is refused.
func durationOrNumber(raw string, unit time.Duration) (time.Duration, bool) {
	if number.MatchString(raw) {
		f, _ := strconv.ParseFloat(raw, 64)
		if math.IsInf(f, 0) || math.IsNaN(f) || f*float64(unit) > float64(math.MaxInt64) {
			return 0, false
		}
		return time.Duration(f * float64(unit)), true
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0, false
	}
	return d, true
}
