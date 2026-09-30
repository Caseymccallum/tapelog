// Package report renders scenario results for CI systems: JUnit XML
// (GitLab/Jenkins/Azure/GitHub actions all ingest it) and GitHub
// Actions workflow-command annotations.
package report

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Failure is one failed assertion.
type Failure struct {
	Check  string
	Detail string
}

// Result is one scenario's outcome.
type Result struct {
	Name     string
	File     string // scenario file path (annotation target)
	Asserts  int
	Failures []Failure
}

// WriteJUnit writes a JUnit XML report for the results.
func WriteJUnit(w io.Writer, results []Result) error {
	type tcFailure struct {
		XMLName xml.Name `xml:"failure"`
		Message string   `xml:"message,attr"`
		Body    string   `xml:",chardata"`
	}
	type testCase struct {
		XMLName    xml.Name   `xml:"testcase"`
		Name       string     `xml:"name,attr"`
		Classname  string     `xml:"classname,attr"`
		Assertions int        `xml:"assertions,attr"`
		Failures   []tcFailure `xml:"failure,omitempty"`
	}
	type testSuite struct {
		XMLName  xml.Name   `xml:"testsuite"`
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Cases    []testCase `xml:"testcase"`
	}
	type testSuites struct {
		XMLName xml.Name  `xml:"testsuites"`
		Tests   int       `xml:"tests,attr"`
		Fails   int       `xml:"failures,attr"`
		Suites  []testSuite `xml:"testsuite"`
	}

	total, fails := 0, 0
	suite := testSuite{Name: "tapelog test"}
	for _, r := range results {
		tc := testCase{Name: r.Name, Classname: r.File, Assertions: r.Asserts}
		for _, f := range r.Failures {
			tc.Failures = append(tc.Failures, tcFailure{
				Message: f.Check,
				Body:    f.Detail,
			})
			fails++
		}
		suite.Cases = append(suite.Cases, tc)
		total++
	}
	suite.Tests, suite.Failures = total, fails
	out := testSuites{Tests: total, Fails: fails, Suites: []testSuite{suite}}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// GitHubAnnotations renders `::error` workflow commands for every
// failure so they appear inline on pull requests. file may be empty.
func GitHubAnnotations(results []Result) []string {
	var out []string
	for _, r := range results {
		for _, f := range r.Failures {
			msg := f.Check + ": " + f.Detail
			loc := ""
			if r.File != "" {
				loc = "file=" + r.File + ","
			}
			out = append(out, fmt.Sprintf("::error %stitle=tapelog test::%s", loc, ghEscape(msg)))
		}
	}
	return out
}

// ghEscape applies the GitHub workflow-command escaping rules.
func ghEscape(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}
