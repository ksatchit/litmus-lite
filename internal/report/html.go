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
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>litmus-lite — ")
	b.WriteString(html.EscapeString(res.Name))
	b.WriteString("</title><style>")
	b.WriteString(style)
	b.WriteString("</style></head><body><main>")

	verdict, cls := "PASS", "pass"
	if !res.Passed {
		verdict, cls = "FAIL", "fail"
	}
	fmt.Fprintf(&b, `<header class="hero">
<div>
<p class="kicker">litmus-lite report</p>
<h1>%s</h1>
<p class="meta">%s · elapsed %s · recovery %s</p>
</div>
<p class="verdict %s" aria-label="verdict">%s</p>
</header>`,
		html.EscapeString(res.Name),
		html.EscapeString(res.StartedAt.Format("2006-01-02 15:04:05 MST")),
		res.Elapsed.Round(time.Millisecond),
		res.Recovery.Round(time.Millisecond),
		cls, verdict)

	for _, w := range res.Warnings {
		fmt.Fprintf(&b, `<p class="callout warn">%s</p>`, html.EscapeString(w))
	}

	if len(res.TimeSeries) > 0 {
		b.WriteString(`<section><h2>Fault vs impact</h2>`)
		b.WriteString(overlayChart(res.TimeSeries, res.FaultLog, res.StartedAt))
		b.WriteString(lineChart(res.TimeSeries, "avail", "Availability", "Probe success rate, 0–100%. A dip inside the inject band is impact.", func(p Point) float64 { return p.Availability * 100 }, 100, "var(--avail)"))
		b.WriteString(lineChart(res.TimeSeries, "rps", "Throughput", "Probe samples per second, or merged VegaLoad RPS when load: ran.", func(p Point) float64 { return p.RPS }, 0, "var(--rps)"))
		b.WriteString(`</section>`)
	}

	if len(res.Hypotheses) > 0 {
		b.WriteString(`<section><h2>Hypotheses</h2><div class="cards">`)
		for _, h := range res.Hypotheses {
			w, c := "PASS", "pass"
			if !h.Passed {
				w, c = "FAIL", "fail"
			}
			fmt.Fprintf(&b, `<article class="card %s"><p class="badge %s">%s</p><h3>%s</h3><p class="meta">%s %s %s</p><p>observed <strong>%s</strong></p></article>`,
				c, c, w, html.EscapeString(h.Name), html.EscapeString(h.Metric), html.EscapeString(h.Operator),
				html.EscapeString(h.Limit), html.EscapeString(h.Observed))
		}
		b.WriteString(`</div></section>`)
	}

	b.WriteString(`<section><h2>Phase comparison</h2><div class="table-wrap"><table><thead><tr><th>phase</th><th>samples</th><th>failed</th><th>error rate</th><th>availability</th><th>p50</th><th>p95</th><th>rps</th></tr></thead><tbody>`)
	for _, p := range res.Phases {
		fmt.Fprintf(&b, `<tr class="row-%s"><td><span class="pill %s">%s</span></td><td>%d</td><td>%d</td><td>%.2f%%</td><td>%.2f%%</td><td>%s</td><td>%s</td><td>%.2f</td></tr>`,
			html.EscapeString(p.Name), html.EscapeString(p.Name), html.EscapeString(p.Name),
			p.Samples, p.Failed, p.ErrorRate*100, p.Availability*100,
			p.P50.Round(time.Millisecond), p.P95.Round(time.Millisecond), p.RPS)
	}
	b.WriteString(`</tbody></table></div></section>`)

	if len(res.ObservedVsInjected) > 0 {
		b.WriteString(`<section><h2>Observed vs injected</h2><div class="table-wrap"><table><thead><tr><th>check</th><th>injected</th><th>observed</th></tr></thead><tbody>`)
		for _, kv := range res.ObservedVsInjected {
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td></tr>`, html.EscapeString(kv.Name), html.EscapeString(kv.Injected), html.EscapeString(kv.Observed))
		}
		b.WriteString(`</tbody></table></div></section>`)
	}

	b.WriteString(`<section><h2>Fault log</h2><div class="table-wrap"><table><thead><tr><th>name</th><th>kind</th><th>start</th><th>end</th><th>intensity</th><th>rollback</th></tr></thead><tbody>`)
	for _, f := range res.FaultLog {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%.0f</td><td>%s</td></tr>`,
			html.EscapeString(f.Name), html.EscapeString(f.Kind), f.Start.Format("15:04:05"), f.End.Format("15:04:05"), f.Intensity, html.EscapeString(f.Rollback))
	}
	b.WriteString(`</tbody></table></div></section>`)

	fails := 0
	for _, s := range res.Probes {
		if !s.OK {
			fails++
		}
	}
	fmt.Fprintf(&b, `<section><h2>Probes</h2><p class="meta">%d samples · %d failed</p></section>`, len(res.Probes), fails)

	b.WriteString("</main></body></html>")
	return b.String()
}

const (
	chartW = 720.0
	chartH = 240.0
	padL   = 54.0
	padR   = 16.0
	padT   = 18.0
	padB   = 36.0
)

func overlayChart(series []Point, faults []FaultEvent, start time.Time) string {
	maxP, maxE, maxI := 1.0, 5.0, 1.0
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
	dur := seriesDur(series)
	var bands strings.Builder
	for _, f := range faults {
		x1 := xAt(f.Start.Sub(start).Seconds(), dur)
		x2 := xAt(f.End.Sub(start).Seconds(), dur)
		if x2 < x1+4 {
			x2 = x1 + 4
		}
		fmt.Fprintf(&bands, `<rect class="band" x="%.1f" y="%.1f" width="%.1f" height="%.1f"/>`, x1, padT, x2-x1, chartH-padT-padB)
	}
	return fmt.Sprintf(`<figure class="chart">
%s
<svg viewBox="0 0 %.0f %.0f" role="img" aria-label="Fault versus impact overlay">
<defs><clipPath id="plot"><rect x="%.1f" y="%.1f" width="%.1f" height="%.1f"/></clipPath></defs>
%s
%s
<g clip-path="url(#plot)">
<polyline class="line inj" fill="none" stroke-dasharray="5 4" stroke-width="1.8" points="%s"/>
<polyline class="line p95" fill="none" stroke-width="2.4" points="%s"/>
<polyline class="line err" fill="none" stroke-width="1.6" points="%s"/>
</g>
</svg>
<figcaption>Shaded band is inject. If the p95 line tracks injected intensity, the fault landed.</figcaption>
</figure>`,
		legendHTML([]string{"inj:injected", "p95:p95 (ms)", "err:error rate %"}),
		chartW, chartH, padL, padT, chartW-padL-padR, chartH-padT-padB,
		bands.String(),
		axesSVG(dur, fmt.Sprintf("%.0f ms", maxP)),
		poly(series, maxI, func(p Point) float64 { return p.Intensity }),
		poly(series, maxP, func(p Point) float64 { return p.P95Ms }),
		poly(series, maxE, func(p Point) float64 { return p.ErrorRate * 100 }))
}

func lineChart(series []Point, id, title, caption string, val func(Point) float64, fixedMax float64, _ string) string {
	maxV := fixedMax
	if maxV == 0 {
		maxV = 1
		for _, p := range series {
			if val(p) > maxV {
				maxV = val(p)
			}
		}
	}
	dur := seriesDur(series)
	cls := "p95"
	if id == "avail" {
		cls = "avail"
	}
	if id == "rps" {
		cls = "rps"
	}
	return fmt.Sprintf(`<figure class="chart">
<h3>%s</h3>
<svg viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">
<defs><clipPath id="plot-%s"><rect x="%.1f" y="%.1f" width="%.1f" height="%.1f"/></clipPath></defs>
%s
<g clip-path="url(#plot-%s)"><polyline class="line %s" fill="none" stroke-width="2.4" points="%s"/></g>
</svg>
<figcaption>%s</figcaption>
</figure>`,
		html.EscapeString(title),
		chartW, chartH, html.EscapeString(title),
		id, padL, padT, chartW-padL-padR, chartH-padT-padB,
		axesSVG(dur, fmt.Sprintf("%.1f", maxV)),
		id, cls, poly(series, maxV, val),
		html.EscapeString(caption))
}

func legendHTML(items []string) string {
	var b strings.Builder
	b.WriteString(`<ul class="legend">`)
	for _, it := range items {
		k, v, _ := strings.Cut(it, ":")
		fmt.Fprintf(&b, `<li><i class="swatch %s"></i>%s</li>`, html.EscapeString(k), html.EscapeString(v))
	}
	b.WriteString(`</ul>`)
	return b.String()
}

func axesSVG(dur float64, yTop string) string {
	x0, y0 := padL, chartH-padB
	x1, y1 := chartW-padR, padT
	return fmt.Sprintf(`<g class="axes">
<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>
<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>
<text class="tick" x="%.1f" y="%.1f" text-anchor="end">%s</text>
<text class="tick" x="%.1f" y="%.1f" text-anchor="end">0</text>
<text class="tick" x="%.1f" y="%.1f" text-anchor="start">0s</text>
<text class="tick" x="%.1f" y="%.1f" text-anchor="end">%.0fs</text>
</g>`,
		x0, y0, x1, y0,
		x0, y0, x0, y1,
		x0-8, y1+10, html.EscapeString(yTop),
		x0-8, y0+4,
		x0, y0+18,
		x1, y0+18, dur)
}

func seriesDur(series []Point) float64 {
	if n := len(series); n > 1 && series[n-1].OffsetS > 0 {
		return series[n-1].OffsetS
	}
	return 1
}

func xAt(sec, dur float64) float64 {
	if dur <= 0 {
		dur = 1
	}
	if sec < 0 {
		sec = 0
	}
	if sec > dur {
		sec = dur
	}
	return padL + (chartW-padL-padR)*sec/dur
}

func poly(series []Point, max float64, val func(Point) float64) string {
	if len(series) == 0 || max == 0 {
		return ""
	}
	innerW := chartW - padL - padR
	innerH := chartH - padT - padB
	var sb strings.Builder
	den := float64(maxInt(len(series)-1, 1))
	for i, p := range series {
		x := padL + innerW*float64(i)/den
		y := padT + innerH*(1-clamp(val(p)/max))
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
:root{
  --bg:#f4f1ea;--ink:#1c1917;--muted:#57534e;--card:#fffdf8;--line:#e7e0d4;
  --pass:#166534;--pass-bg:#dcfce7;--fail:#991b1b;--fail-bg:#fee2e2;
  --inject:#f59e0b;--steady:#0ea5e9;--recover:#8b5cf6;
  --p95:#2563eb;--err:#dc2626;--inj:#7c3aed;--avail:#059669;--rps:#d97706;
  --warn-bg:#fef3c7;--warn-ink:#92400e
}
@media (prefers-color-scheme:dark){
  :root{
    --bg:#1c1917;--ink:#f5f5f4;--muted:#a8a29e;--card:#292524;--line:#44403c;
    --pass:#86efac;--pass-bg:#14532d;--fail:#fca5a5;--fail-bg:#7f1d1d;
    --warn-bg:#422006;--warn-ink:#fde68a
  }
}
*{box-sizing:border-box}
body{background:var(--bg);color:var(--ink);font-family:ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;margin:0;line-height:1.45}
main{max-width:880px;margin:0 auto;padding:28px 20px 48px}
.hero{display:flex;justify-content:space-between;gap:16px;align-items:flex-start;background:var(--card);border:1px solid var(--line);border-radius:16px;padding:20px 22px;margin-bottom:20px}
.kicker{margin:0;font-size:.75rem;letter-spacing:.08em;text-transform:uppercase;color:var(--muted)}
h1{font-size:1.55rem;margin:4px 0 8px;word-break:break-word}
h2{font-size:1.05rem;margin:0 0 12px}
h3{font-size:.95rem;margin:0 0 8px}
.meta{color:var(--muted);font-size:.88rem;margin:0}
.verdict{margin:0;font-size:1.1rem;font-weight:700;border-radius:999px;padding:8px 16px;white-space:nowrap}
.verdict.pass,.badge.pass,.card.pass{background:var(--pass-bg);color:var(--pass)}
.verdict.fail,.badge.fail,.card.fail{background:var(--fail-bg);color:var(--fail)}
.badge{display:inline-block;border-radius:999px;padding:2px 8px;font-size:.75rem;font-weight:700}
section{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px 20px;margin:0 0 16px}
.callout{border-radius:12px;padding:10px 14px;margin:0 0 16px;font-size:.88rem}
.callout.warn{background:var(--warn-bg);color:var(--warn-ink)}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px}
.card{border:1px solid var(--line);border-radius:12px;padding:12px 14px}
.card h3{margin:8px 0 4px}
.table-wrap{overflow-x:auto}
table{border-collapse:collapse;width:100%;font-size:.88rem}
th,td{border-bottom:1px solid var(--line);padding:8px 10px;text-align:left;white-space:nowrap}
th{color:var(--muted);font-weight:600}
.pill{display:inline-block;border-radius:999px;padding:2px 8px;font-size:.75rem;font-weight:600}
.pill.steady{background:#e0f2fe;color:#075985}
.pill.inject{background:#fef3c7;color:#92400e}
.pill.recover{background:#ede9fe;color:#5b21b6}
.row-inject{background:color-mix(in srgb,var(--inject) 8%,transparent)}
.chart{margin:0 0 22px}
.chart:last-child{margin-bottom:0}
.legend{display:flex;flex-wrap:wrap;gap:10px 16px;list-style:none;padding:0;margin:0 0 8px;font-size:.8rem;color:var(--muted)}
.legend i{display:inline-block;width:18px;height:3px;margin-right:6px;vertical-align:middle;border-radius:2px}
.swatch.p95{background:var(--p95)}.swatch.err{background:var(--err)}.swatch.inj{background:var(--inj)}
.swatch.avail{background:var(--avail)}.swatch.rps{background:var(--rps)}
figure svg{display:block;width:100%;height:auto;overflow:visible}
.band{fill:var(--inject);opacity:.22}
.line.p95{stroke:var(--p95)}.line.err{stroke:var(--err)}.line.inj{stroke:var(--inj)}
.line.avail{stroke:var(--avail)}.line.rps{stroke:var(--rps)}
.axes line{stroke:var(--line);stroke-width:1}
.tick{fill:var(--muted);font-size:11px;font-family:ui-sans-serif,system-ui,sans-serif}
figcaption{color:var(--muted);font-size:.82rem;margin-top:8px;max-width:62ch}
`
