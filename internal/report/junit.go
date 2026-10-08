package report

import (
	"encoding/xml"
	"fmt"
	"os"
)

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name    string        `xml:"name,attr"`
	Class   string        `xml:"classname,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func JUnitXML(res *Result) []byte {
	s := junitSuite{Name: res.Name, Tests: len(res.Hypotheses)}
	if s.Tests == 0 {
		s.Tests = 1
		c := junitCase{Name: res.Name, Class: "litmus-lite"}
		if !res.Passed {
			s.Failures = 1
			c.Failure = &junitFailure{Message: "FAIL", Text: "scenario failed"}
		}
		s.Cases = []junitCase{c}
	} else {
		for _, h := range res.Hypotheses {
			c := junitCase{Name: h.Name, Class: "litmus-lite.hypothesis"}
			if !h.Passed {
				s.Failures++
				c.Failure = &junitFailure{
					Message: fmt.Sprintf("%s %s %s", h.Metric, h.Operator, h.Limit),
					Text:    fmt.Sprintf("observed %s", h.Observed),
				}
			}
			s.Cases = append(s.Cases, c)
		}
	}
	b, _ := xml.MarshalIndent(s, "", "  ")
	return append([]byte(xml.Header), append(b, '\n')...)
}

func WriteJUnit(path string, res *Result) error {
	return os.WriteFile(path, JUnitXML(res), 0o644)
}
