package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ksatchit/litmus-lite/internal/report"
)

var DefaultQueries = []string{
	`rate(http_request_duration_seconds_count[5s])`,
	`rate(http_requests_total[5s])`,
}

func QueryRange(ctx context.Context, base string, start, end time.Time) ([]report.Point, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.Path = "/api/v1/query_range"
	q := u.Query()
	q.Set("query", DefaultQueries[1])
	q.Set("start", strconv.FormatFloat(float64(start.Unix()), 'f', 0, 64))
	q.Set("end", strconv.FormatFloat(float64(end.Unix()), 'f', 0, 64))
	q.Set("step", "1")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Data struct {
			Result []struct {
				Values [][]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var pts []report.Point
	if len(parsed.Data.Result) == 0 {
		return pts, nil
	}
	for _, pair := range parsed.Data.Result[0].Values {
		if len(pair) < 2 {
			continue
		}
		ts, _ := pair[0].(float64)
		vs, _ := pair[1].(string)
		v, _ := strconv.ParseFloat(vs, 64)
		t := time.Unix(int64(ts), 0)
		pts = append(pts, report.Point{T: t, OffsetS: t.Sub(start).Seconds(), RPS: v, Phase: "prometheus"})
	}
	return pts, nil
}
