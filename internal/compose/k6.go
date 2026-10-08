package compose

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/ksatchit/litmus-lite/internal/scenario"
)

func runK6(ctx context.Context, binOverride, baseDir string, load *scenario.LoadSpec) (Output, error) {
	path, err := resolveBin(baseDir, "k6", binOverride)
	if err != nil {
		return Output{Tool: "k6", Warn: "k6 not on PATH; running chaos only"}, nil
	}
	scen, err := resolveScenario(baseDir, load.Scenario)
	if err != nil {
		return Output{}, err
	}
	outFile := filepath.Join(os.TempDir(), fmt.Sprintf("litmus-lite-k6-%d.json", os.Getpid()))
	defer os.Remove(outFile)
	args := []string{"run", "--out", "json=" + outFile}
	args = append(args, load.Args...)
	args = append(args, scen)
	cmd := exec.CommandContext(ctx, path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	runErr := cmd.Run()
	raw, readErr := os.ReadFile(outFile)
	out := Output{Tool: "k6", Raw: asRaw(raw)}
	if readErr != nil {
		if runErr != nil {
			return out, fmt.Errorf("%w: %s", runErr, truncate(stderr.String(), 400))
		}
		return out, readErr
	}
	pts, perr := seriesFromK6(raw)
	if perr != nil {
		if runErr != nil {
			return out, fmt.Errorf("%v: %w: %s", perr, runErr, truncate(stderr.String(), 200))
		}
		out.Warn = perr.Error()
		return out, nil
	}
	out.Series = pts
	if runErr != nil {
		out.Warn = "k6 exited non-zero; merged samples anyway: " + truncate(stderr.String(), 200)
	}
	return out, nil
}

type k6Envelope struct {
	Type   string `json:"type"`
	Metric string `json:"metric"`
	Data   struct {
		Time  time.Time `json:"time"`
		Value float64   `json:"value"`
	} `json:"data"`
}

func seriesFromK6(raw []byte) ([]LoadPoint, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("k6 json output is empty")
	}
	type sample struct {
		t      time.Time
		metric string
		value  float64
	}
	var samples []sample
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env k6Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Type != "Point" || env.Data.Time.IsZero() {
			continue
		}
		if env.Metric != "http_reqs" && env.Metric != "http_req_failed" {
			continue
		}
		samples = append(samples, sample{t: env.Data.Time, metric: env.Metric, value: env.Data.Value})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("k6 json output has no http_reqs samples")
	}
	origin := samples[0].t
	for _, s := range samples {
		if s.t.Before(origin) {
			origin = s.t
		}
	}
	type agg struct {
		reqs   float64
		failed float64
	}
	buckets := map[int64]*agg{}
	for _, s := range samples {
		sec := int64(s.t.Sub(origin).Seconds())
		if sec < 0 {
			sec = 0
		}
		b := buckets[sec]
		if b == nil {
			b = &agg{}
			buckets[sec] = b
		}
		switch s.metric {
		case "http_reqs":
			b.reqs += s.value
		case "http_req_failed":
			b.failed += s.value
		}
	}
	if len(buckets) == 0 {
		return nil, fmt.Errorf("k6 json output has no http_reqs samples")
	}
	keys := make([]int64, 0, len(buckets))
	for k, b := range buckets {
		if b.reqs == 0 && b.failed == 0 {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	pts := make([]LoadPoint, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		var rate float64
		if b.reqs > 0 {
			rate = b.failed / b.reqs
		}
		pts = append(pts, LoadPoint{OffsetS: float64(k), RPS: b.reqs, ErrorRate: rate})
	}
	if len(pts) == 0 {
		return nil, fmt.Errorf("k6 json output has no http_reqs samples")
	}
	return pts, nil
}
