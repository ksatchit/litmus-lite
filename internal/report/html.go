package report

import (
	"fmt"
	"html"
	"os"
	"strings"
	"time"
)

func WriteHTML(path string, res *Result) error {
	return os.WriteFile(path, []byte(RenderHTML(res)), 0o644)
}

func RenderHTML(res *Result) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>litmus-lite report — ")
	b.WriteString(html.EscapeString(res.Name))
	b.WriteString("</title><style>")
	b.WriteString(style)
	b.WriteString("</style></head><body><main>")
	fmt.Fprintf(&b, "<h1>litmus-lite report</h1><p class=\"meta\">%s · started %s · elapsed %s</p>",
		html.EscapeString(res.Name), res.StartedAt.Format("2006-01-02 15:04:05 MST"), res.Elapsed.Round(time.Millisecond))

	verdict, cls := "PASS", "pass"
	if !res.Passed {
		verdict, cls = "FAIL", "fail"
	}
	fmt.Fprintf(&b, "<p><span class=\"badge %s\">%s</span></p>", cls, verdict)

	b.WriteString("<h2>Phase comparison</h2><table><tr><th>phase</th><th>samples</th><th>failed</th><th>error rate</th><th>availability</th><th>p50</th><th>p95</th><th>rps</th></tr>")
	for _, p := range res.Phases {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>%d</td><td>%.2f%%</td><td>%.2f%%</td><td>%s</td><td>%s</td><td>%.2f</td></tr>",
			html.EscapeString(p.Name), p.Samples, p.Failed, p.ErrorRate*100, p.Availability*100,
			p.P50.Round(time.Millisecond), p.P95.Round(time.Millisecond), p.RPS)
	}
	fmt.Fprintf(&b, "</table><p class=\"meta\">recovery time-to-healthy: %s</p>", res.Recovery.Round(time.Millisecond))

	if len(res.Hypotheses) > 0 {
		b.WriteString("<h2>Hypotheses</h2><table><tr><th>result</th><th>name</th><th>metric</th><th>limit</th><th>observed</th></tr>")
		for _, h := range res.Hypotheses {
			w, c := "PASS", "pass"
			if !h.Passed {
				w, c = "FAIL", "fail"
			}
			fmt.Fprintf(&b, "<tr><td class=\"%s\">%s</td><td>%s</td><td>%s %s</td><td>%s</td><td>%s</td></tr>",
				c, w, html.EscapeString(h.Name), html.EscapeString(h.Metric), html.EscapeString(h.Operator),
				html.EscapeString(h.Limit), html.EscapeString(h.Observed))
		}
		b.WriteString("</table>")
	}

	b.WriteString("<h2>Fault log</h2><table><tr><th>name</th><th>kind</th><th>start</th><th>end</th><th>intensity</th><th>rollback</th></tr>")
	for _, f := range res.FaultLog {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%.0f</td><td>%s</td></tr>",
			html.EscapeString(f.Name), html.EscapeString(f.Kind), f.Start.Format(time.RFC3339), f.End.Format(time.RFC3339), f.Intensity, html.EscapeString(f.Rollback))
	}
	b.WriteString("</table>")

	if len(res.ObservedVsInjected) > 0 {
		b.WriteString("<h2>Observed vs injected</h2><table><tr><th>name</th><th>injected</th><th>observed</th></tr>")
		for _, kv := range res.ObservedVsInjected {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td></tr>", html.EscapeString(kv.Name), html.EscapeString(kv.Injected), html.EscapeString(kv.Observed))
		}
		b.WriteString("</table>")
	}

	fails := 0
	for _, s := range res.Probes {
		if !s.OK {
			fails++
		}
	}
	fmt.Fprintf(&b, "<h2>Probes</h2><p>%d samples, %d failed</p>", len(res.Probes), fails)

	if len(res.TimeSeries) > 0 {
		b.WriteString("<h2>Fault vs impact</h2>")
		b.WriteString(overlayChart(res.TimeSeries, res.FaultLog, res.StartedAt))
		b.WriteString(lineChart(res.TimeSeries, "availability", "Availability (probe success), 0–100%.", func(p Point) float64 { return p.Availability * 100 }, 100))
		b.WriteString(lineChart(res.TimeSeries, "rps", "Probe (or merged load) throughput, samples per second.", func(p Point) float64 { return p.RPS }, 0))
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "<p class=\"meta\">warning: %s</p>", html.EscapeString(w))
	}
	b.WriteString("</main></body></html>")
	return b.String()
}

const chartW, chartH, pad = 640.0, 180.0, 32.0

func overlayChart(series []Point, faults []FaultEvent, start time.Time) string {
	maxP := 1.0
	maxE := 1.0
	maxI := 1.0
	for _, p := range series {
		if p.P95Ms > maxP {
			maxP = p.P95Ms
		}
		if p.ErrorRate*100 > maxE {
			maxE = p.ErrorRate * 100
		}
		if p.Intensity > maxI {
			maxI = p.Intensity
		}
	}
	if maxE < 5 {
		maxE = 5
	}
	var bands strings.Builder
	dur := 1.0
	if n := len(series); n > 1 {
		dur = series[n-1].OffsetS
		if dur <= 0 {
			dur = 1
		}
	}
	innerW := chartW - pad
	for _, f := range faults {
		x1 := pad + innerW*f.Start.Sub(start).Seconds()/dur
		x2 := pad + innerW*f.End.Sub(start).Seconds()/dur
		if x2 < x1 {
			x2 = x1 + 2
		}
		fmt.Fprintf(&bands, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="currentColor" opacity="0.08"/>`, x1, 0.0, x2-x1, chartH-pad)
	}
	p95 := poly(series, maxP, func(p Point) float64 { return p.P95Ms })
	errp := poly(series, maxE, func(p Point) float64 { return p.ErrorRate * 100 })
	inj := poly(series, maxI, func(p Point) float64 { return p.Intensity })
	return fmt.Sprintf(`<figure><svg viewBox="0 0 %.0f %.0f" role="img">%s<polyline fill="none" stroke="currentColor" stroke-dasharray="4 3" stroke-width="1.5" points="%s"/><polyline fill="none" stroke="currentColor" stroke-width="2" points="%s"/><polyline fill="none" stroke="currentColor" stroke-width="1.2" opacity="0.7" points="%s"/><text x="%.0f" y="12" font-size="11">p95 ms / error %% / injected (dashed)</text></svg><figcaption>Shaded band is the inject window. Solid line is probe p95; lighter line is error rate; dashed is injected intensity. If p95 tracks the dashed line, the fault landed.</figcaption></figure>`,
		chartW, chartH, bands.String(), inj, p95, errp, pad)
}

func lineChart(series []Point, _ string, caption string, val func(Point) float64, fixedMax float64) string {
	maxV := fixedMax
	if maxV == 0 {
		maxV = 1
		for _, p := range series {
			if val(p) > maxV {
				maxV = val(p)
			}
		}
	}
	pts := poly(series, maxV, val)
	return fmt.Sprintf(`<figure><svg viewBox="0 0 %.0f %.0f" role="img"><polyline fill="none" stroke="currentColor" stroke-width="2" points="%s"/><text x="%.0f" y="12" font-size="11">%.1f</text></svg><figcaption>%s</figcaption></figure>`,
		chartW, chartH, pts, pad, maxV, html.EscapeString(caption))
}

func poly(series []Point, max float64, val func(Point) float64) string {
	if len(series) == 0 || max == 0 {
		return ""
	}
	innerW := chartW - pad
	innerH := chartH - pad
	var sb strings.Builder
	for i, p := range series {
		x := pad + innerW*float64(i)/float64(maxInt(len(series)-1, 1))
		y := pad + innerH*(1-clamp(val(p)/max))
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%.1f,%.1f", x, y)
	}
	return sb.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

const style = `
:root{--bg:#fff;--fg:#1d2433;--muted:#5a6477;--line:#d6dae3;--pass:#1a6b3a;--fail:#b3261e}
@media (prefers-color-scheme:dark){:root{--bg:#161a22;--fg:#e6e8ee;--muted:#9aa3b5;--line:#30384a;--pass:#5fd08a;--fail:#ff8a80}}
body{background:var(--bg);color:var(--fg);font-family:-apple-system,"Segoe UI",system-ui,sans-serif;margin:0;padding:24px}
main{max-width:760px;margin:0 auto}
h1{font-size:1.4rem;margin:0 0 4px}
h2{font-size:1.05rem;margin:24px 0 10px}
.meta{color:var(--muted);font-size:.85rem}
table{border-collapse:collapse;width:100%;font-size:.9rem;margin:0 0 16px}
th,td{border:1px solid var(--line);padding:6px 10px;text-align:left}
.badge{border:1px solid currentColor;border-radius:4px;padding:0 8px;font-size:.8rem;font-weight:600}
.badge.pass,.pass{color:var(--pass)}
.badge.fail,.fail{color:var(--fail)}
figure{margin:0 0 20px}
figcaption{color:var(--muted);font-size:.8rem;margin-top:4px}
svg{width:100%;height:auto;color:var(--fg)}
svg text{fill:currentColor}
`
