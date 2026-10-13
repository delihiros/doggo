package cli

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	toolchaintester "doggo/internal/toolchain/tester"
)

const (
	testReportFormatHuman = "human"
	testReportFormatJSON  = "json"
	testReportFormatJUnit = "junit"
)

type TestRunReport struct {
	Summary TestSummary      `json:"summary"`
	Cases   []TestCaseReport `json:"cases"`
}

type TestSummary struct {
	Test       int     `json:"test"`
	Pass       int     `json:"pass"`
	Fail       int     `json:"fail"`
	Error      int     `json:"error"`
	DurationMS float64 `json:"duration-ms"`
}

type TestCaseReport struct {
	Namespace  string   `json:"ns"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	DurationMS float64  `json:"duration-ms"`
	Form       string   `json:"form,omitempty"`
	Message    string   `json:"message,omitempty"`
	Contexts   []string `json:"contexts,omitempty"`
	Expected   string   `json:"expected,omitempty"`
	Actual     string   `json:"actual,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type TestSuiteReport struct {
	Namespace string
	Summary   TestSummary
	Cases     []TestCaseReport
}

func fromTesterReport(report *toolchaintester.Report) *TestRunReport {
	if report == nil {
		return nil
	}
	out := &TestRunReport{
		Summary: TestSummary{
			Test:       report.Summary.Test,
			Pass:       report.Summary.Pass,
			Fail:       report.Summary.Fail,
			Error:      report.Summary.Error,
			DurationMS: report.Summary.DurationMS,
		},
		Cases: make([]TestCaseReport, 0, len(report.Cases)),
	}
	for _, result := range report.Cases {
		out.Cases = append(out.Cases, TestCaseReport{
			Namespace:  result.Namespace,
			Name:       result.Name,
			Status:     result.Status,
			DurationMS: result.DurationMS,
			Form:       result.Form,
			Message:    result.Message,
			Contexts:   append([]string(nil), result.Contexts...),
			Expected:   result.Expected,
			Actual:     result.Actual,
			Error:      result.Error,
		})
	}
	return out
}

func renderHumanTestReport(report *TestRunReport) string {
	if report == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(formatTestSummaryLine(report.Summary))
	sb.WriteString("\n")

	for _, testCase := range report.Cases {
		switch testCase.Status {
		case "fail":
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("FAIL %s/%s (%.3f ms)\n", testCase.Namespace, testCase.Name, testCase.DurationMS))
			if testCase.Form != "" {
				sb.WriteString("  form: ")
				sb.WriteString(testCase.Form)
				sb.WriteString("\n")
			}
			if testCase.Message != "" {
				sb.WriteString("  message: ")
				sb.WriteString(testCase.Message)
				sb.WriteString("\n")
			}
			if len(testCase.Contexts) > 0 {
				sb.WriteString("  context: ")
				sb.WriteString(strings.Join(testCase.Contexts, " > "))
				sb.WriteString("\n")
			}
			if testCase.Expected != "" {
				sb.WriteString("  expected: ")
				sb.WriteString(testCase.Expected)
				sb.WriteString("\n")
			}
			if testCase.Actual != "" {
				sb.WriteString("  actual: ")
				sb.WriteString(testCase.Actual)
				sb.WriteString("\n")
			}
		case "error":
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("ERROR %s/%s (%.3f ms)\n", testCase.Namespace, testCase.Name, testCase.DurationMS))
			if testCase.Message != "" {
				sb.WriteString("  message: ")
				sb.WriteString(testCase.Message)
				sb.WriteString("\n")
			}
			if testCase.Error != "" {
				sb.WriteString("  error: ")
				sb.WriteString(testCase.Error)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

func formatTestSummaryLine(summary TestSummary) string {
	return fmt.Sprintf(
		"Ran %d tests. %d passed, %d failed, %d errors. (%.3f ms)",
		summary.Test, summary.Pass, summary.Fail, summary.Error, summary.DurationMS,
	)
}

func renderJSONTestReport(report *TestRunReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func mergeTestRunReports(reports []*TestRunReport) *TestRunReport {
	merged := &TestRunReport{
		Summary: TestSummary{},
		Cases:   []TestCaseReport{},
	}

	for _, report := range reports {
		if report == nil {
			continue
		}
		merged.Summary.Test += report.Summary.Test
		merged.Summary.Pass += report.Summary.Pass
		merged.Summary.Fail += report.Summary.Fail
		merged.Summary.Error += report.Summary.Error
		merged.Summary.DurationMS += report.Summary.DurationMS
		merged.Cases = append(merged.Cases, report.Cases...)
	}

	sort.Slice(merged.Cases, func(i, j int) bool {
		if merged.Cases[i].Namespace != merged.Cases[j].Namespace {
			return merged.Cases[i].Namespace < merged.Cases[j].Namespace
		}
		if merged.Cases[i].Name != merged.Cases[j].Name {
			return merged.Cases[i].Name < merged.Cases[j].Name
		}
		return merged.Cases[i].Status < merged.Cases[j].Status
	})

	return merged
}

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitError   `xml:"error,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",innerxml"`
}

type junitError struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",innerxml"`
}

func renderJUnitTestReport(report *TestRunReport) ([]byte, error) {
	suites := buildTestSuites(report)
	xmlSuites := junitTestSuites{
		Tests:    report.Summary.Test,
		Failures: report.Summary.Fail,
		Errors:   report.Summary.Error,
		Time:     formatJUnitTimeMS(report.Summary.DurationMS),
		Suites:   make([]junitTestSuite, 0, len(suites)),
	}

	for _, suite := range suites {
		xmlSuite := junitTestSuite{
			Name:     suite.Namespace,
			Tests:    suite.Summary.Test,
			Failures: suite.Summary.Fail,
			Errors:   suite.Summary.Error,
			Time:     formatJUnitTimeMS(suite.Summary.DurationMS),
			Cases:    make([]junitTestCase, 0, len(suite.Cases)),
		}
		for _, c := range suite.Cases {
			testCase := junitTestCase{
				Name:      c.Name,
				ClassName: c.Namespace,
				Time:      formatJUnitTimeMS(c.DurationMS),
			}
			switch c.Status {
			case "fail":
				testCase.Failure = &junitFailure{
					Message: c.Message,
					Type:    "assertion",
					Body:    wrapJUnitCDATA(formatCaseBody(c)),
				}
			case "error":
				testCase.Error = &junitError{
					Message: c.Message,
					Type:    "runtime",
					Body:    wrapJUnitCDATA(formatCaseBody(c)),
				}
			}
			xmlSuite.Cases = append(xmlSuite.Cases, testCase)
		}
		xmlSuites.Suites = append(xmlSuites.Suites, xmlSuite)
	}

	body, err := xml.MarshalIndent(xmlSuites, "", "  ")
	if err != nil {
		return nil, err
	}

	output := append([]byte(xml.Header), body...)
	return output, nil
}

func buildTestSuites(report *TestRunReport) []TestSuiteReport {
	byNamespace := make(map[string]*TestSuiteReport)
	for _, c := range report.Cases {
		suite := byNamespace[c.Namespace]
		if suite == nil {
			suite = &TestSuiteReport{
				Namespace: c.Namespace,
				Summary:   TestSummary{},
				Cases:     make([]TestCaseReport, 0, 4),
			}
			byNamespace[c.Namespace] = suite
		}

		suite.Cases = append(suite.Cases, c)
		suite.Summary.Test++
		suite.Summary.DurationMS += c.DurationMS
		switch c.Status {
		case "pass":
			suite.Summary.Pass++
		case "fail":
			suite.Summary.Fail++
		case "error":
			suite.Summary.Error++
		}
	}

	namespaces := make([]string, 0, len(byNamespace))
	for ns := range byNamespace {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	suites := make([]TestSuiteReport, 0, len(namespaces))
	for _, ns := range namespaces {
		suite := byNamespace[ns]
		sort.Slice(suite.Cases, func(i, j int) bool {
			if suite.Cases[i].Name != suite.Cases[j].Name {
				return suite.Cases[i].Name < suite.Cases[j].Name
			}
			return suite.Cases[i].Status < suite.Cases[j].Status
		})
		suites = append(suites, *suite)
	}
	return suites
}

func formatCaseBody(c TestCaseReport) string {
	lines := make([]string, 0, 6)
	if c.Form != "" {
		lines = append(lines, "form: "+c.Form)
	}
	if c.Expected != "" {
		lines = append(lines, "expected: "+c.Expected)
	}
	if c.Actual != "" {
		lines = append(lines, "actual: "+c.Actual)
	}
	if len(c.Contexts) > 0 {
		lines = append(lines, "contexts: "+strings.Join(c.Contexts, " > "))
	}
	if c.Error != "" {
		lines = append(lines, "error: "+c.Error)
	}
	return strings.Join(lines, "\n")
}

func wrapJUnitCDATA(body string) string {
	if body == "" {
		return ""
	}
	safe := strings.ReplaceAll(body, "]]>", "]]]]><![CDATA[>")
	return "<![CDATA[" + safe + "]]>"
}

func formatJUnitTimeMS(ms float64) string {
	seconds := ms / 1000.0
	return fmt.Sprintf("%.6f", seconds)
}
