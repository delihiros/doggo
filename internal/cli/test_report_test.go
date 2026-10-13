package cli

import (
	"encoding/json"
	"strings"
	"testing"

	toolchaintester "doggo/internal/toolchain/tester"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestTesterDecodeReportConversion(t *testing.T) {
	raw := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("summary"), coll.NewMapFromPairs([]types.Value{
			atom.NewKeyword("test"), atom.NewNumberInt(2),
			atom.NewKeyword("pass"), atom.NewNumberInt(1),
			atom.NewKeyword("fail"), atom.NewNumberInt(1),
			atom.NewKeyword("error"), atom.NewNumberInt(0),
			atom.NewKeyword("duration-ms"), atom.NewNumberFloat(3.5),
		}),
		atom.NewKeyword("cases"), coll.NewVector(
			coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("ns"), types.NewSymbol("alpha.tests"),
				atom.NewKeyword("name"), types.NewSymbol("ok"),
				atom.NewKeyword("status"), atom.NewKeyword("pass"),
				atom.NewKeyword("duration-ms"), atom.NewNumberFloat(1.2),
				atom.NewKeyword("contexts"), coll.NewVector(),
			}),
			coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("ns"), types.NewSymbol("alpha.tests"),
				atom.NewKeyword("name"), types.NewSymbol("ng"),
				atom.NewKeyword("status"), atom.NewKeyword("fail"),
				atom.NewKeyword("duration-ms"), atom.NewNumberFloat(2.3),
				atom.NewKeyword("form"), coll.NewList(types.NewSymbol("="), atom.NewNumberInt(1), atom.NewNumberInt(2)),
				atom.NewKeyword("expected"), atom.NewNumberInt(1),
				atom.NewKeyword("actual"), atom.NewNumberInt(2),
				atom.NewKeyword("message"), atom.NewString("should be equal"),
				atom.NewKeyword("contexts"), coll.NewVector(atom.NewString("arithmetic")),
			}),
		),
	})

	testerReport, err := toolchaintester.DecodeReport(raw)
	if err != nil {
		t.Fatalf("DecodeReport failed: %v", err)
	}
	report := fromTesterReport(testerReport)
	if report.Summary.Test != 2 || report.Summary.Fail != 1 || report.Summary.Pass != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if len(report.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(report.Cases))
	}

	var failCase *TestCaseReport
	for i := range report.Cases {
		if report.Cases[i].Name == "ng" && report.Cases[i].Status == "fail" {
			failCase = &report.Cases[i]
			break
		}
	}
	if failCase == nil {
		t.Fatalf("fail case not found: %+v", report.Cases)
	}
	if failCase.Expected != "1" || failCase.Actual != "2" {
		t.Fatalf("expected/actual decode mismatch: %+v", *failCase)
	}
}

func TestRenderJSONTestReport(t *testing.T) {
	report := sampleDecodedReport()
	body, err := renderJSONTestReport(report)
	if err != nil {
		t.Fatalf("renderJSONTestReport failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, string(body))
	}
	if _, ok := decoded["summary"]; !ok {
		t.Fatalf("summary field missing: %v", decoded)
	}
	if _, ok := decoded["cases"]; !ok {
		t.Fatalf("cases field missing: %v", decoded)
	}
}

func TestRenderJUnitTestReport(t *testing.T) {
	report := sampleDecodedReport()
	xmlBody, err := renderJUnitTestReport(report)
	if err != nil {
		t.Fatalf("renderJUnitTestReport failed: %v", err)
	}
	xmlText := string(xmlBody)
	if !strings.Contains(xmlText, `<testsuite name="alpha.tests"`) {
		t.Fatalf("missing alpha testsuite:\n%s", xmlText)
	}
	if !strings.Contains(xmlText, `<testsuite name="beta.tests"`) {
		t.Fatalf("missing beta testsuite:\n%s", xmlText)
	}
	if !strings.Contains(xmlText, "<failure") {
		t.Fatalf("missing failure node:\n%s", xmlText)
	}
	if !strings.Contains(xmlText, "<error") {
		t.Fatalf("missing error node:\n%s", xmlText)
	}
}

func TestRenderHumanTestReport(t *testing.T) {
	report := sampleDecodedReport()
	human := renderHumanTestReport(report)

	if !strings.Contains(human, "FAIL alpha.tests/ng") {
		t.Fatalf("missing failure headline:\n%s", human)
	}
	if !strings.Contains(human, "expected: 1") {
		t.Fatalf("missing expected output:\n%s", human)
	}
	if !strings.Contains(human, "actual: 2") {
		t.Fatalf("missing actual output:\n%s", human)
	}
	if !strings.Contains(human, "ERROR beta.tests/boom") {
		t.Fatalf("missing error headline:\n%s", human)
	}
}

func TestMergeTestRunReports(t *testing.T) {
	a := &TestRunReport{
		Summary: TestSummary{Test: 1, Pass: 1, Fail: 0, Error: 0, DurationMS: 1.0},
		Cases: []TestCaseReport{
			{Namespace: "b.tests", Name: "b", Status: "pass", DurationMS: 1.0},
		},
	}
	b := &TestRunReport{
		Summary: TestSummary{Test: 1, Pass: 0, Fail: 1, Error: 0, DurationMS: 2.0},
		Cases: []TestCaseReport{
			{Namespace: "a.tests", Name: "a", Status: "fail", DurationMS: 2.0},
		},
	}

	merged := mergeTestRunReports([]*TestRunReport{a, b})
	if merged.Summary.Test != 2 || merged.Summary.Fail != 1 || merged.Summary.Pass != 1 {
		t.Fatalf("unexpected merged summary: %+v", merged.Summary)
	}
	if len(merged.Cases) != 2 {
		t.Fatalf("unexpected merged cases length: %d", len(merged.Cases))
	}
	if merged.Cases[0].Namespace != "a.tests" || merged.Cases[0].Name != "a" {
		t.Fatalf("cases are not sorted by namespace/name: %+v", merged.Cases)
	}
}

func sampleDecodedReport() *TestRunReport {
	return &TestRunReport{
		Summary: TestSummary{
			Test:       3,
			Pass:       1,
			Fail:       1,
			Error:      1,
			DurationMS: 12.5,
		},
		Cases: []TestCaseReport{
			{
				Namespace:  "alpha.tests",
				Name:       "ok",
				Status:     "pass",
				DurationMS: 2.0,
			},
			{
				Namespace:  "alpha.tests",
				Name:       "ng",
				Status:     "fail",
				DurationMS: 4.0,
				Form:       "(= 1 2)",
				Message:    "should be equal",
				Contexts:   []string{"math", "basic"},
				Expected:   "1",
				Actual:     "2",
			},
			{
				Namespace:  "beta.tests",
				Name:       "boom",
				Status:     "error",
				DurationMS: 6.5,
				Error:      "{:type :runtime, :message \"boom\"}",
			},
		},
	}
}
