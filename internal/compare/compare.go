package compare

import (
	"fmt"

	"github.com/ksatchit/litmus-lite/internal/report"
)

type Row struct {
	Phase     string  `json:"phase"`
	Metric    string  `json:"metric"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Regressed bool    `json:"regressed"`
}

func Phases(base, cand *report.Result) (rows []Row, failed bool) {
	bm := map[string]report.Phase{}
	for _, p := range base.Phases {
		bm[p.Name] = p
	}
	for _, c := range cand.Phases {
		b := bm[c.Name]
		er := Row{Phase: c.Name, Metric: "error_rate", Baseline: b.ErrorRate, Candidate: c.ErrorRate, Regressed: c.ErrorRate > b.ErrorRate+0.01}
		p95 := Row{Phase: c.Name, Metric: "p95_ms", Baseline: float64(b.P95.Milliseconds()), Candidate: float64(c.P95.Milliseconds()), Regressed: c.P95 > b.P95*12/10 && b.P95 > 0}
		rows = append(rows, er, p95)
		if er.Regressed || p95.Regressed {
			failed = true
		}
	}
	return rows, failed
}

func Text(rows []Row, failed bool) string {
	s := ""
	for _, r := range rows {
		mark := "ok"
		if r.Regressed {
			mark = "REGRESSED"
		}
		s += fmt.Sprintf("%s %s: baseline=%.4f candidate=%.4f %s\n", r.Phase, r.Metric, r.Baseline, r.Candidate, mark)
	}
	if failed {
		s += "compare: candidate is worse than baseline\n"
	}
	return s
}
