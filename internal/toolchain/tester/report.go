package tester

import (
	"fmt"
	"sort"

	"doggo/internal/types"
	typeatom "doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type Report struct {
	Summary Summary  `json:"summary"`
	Cases   []Result `json:"cases"`
}

type Summary struct {
	Test       int     `json:"test"`
	Pass       int     `json:"pass"`
	Fail       int     `json:"fail"`
	Error      int     `json:"error"`
	DurationMS float64 `json:"duration-ms"`
}

type Result struct {
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

func DecodeReport(result types.Value) (*Report, error) {
	root, ok := result.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("run-tests result is not map")
	}

	summaryMap, err := mapField(root, "summary")
	if err != nil {
		return nil, err
	}
	summary := Summary{}
	if summary.Test, err = mapInt(summaryMap, "test"); err != nil {
		return nil, err
	}
	if summary.Pass, err = mapInt(summaryMap, "pass"); err != nil {
		return nil, err
	}
	if summary.Fail, err = mapInt(summaryMap, "fail"); err != nil {
		return nil, err
	}
	if summary.Error, err = mapInt(summaryMap, "error"); err != nil {
		return nil, err
	}
	if summary.DurationMS, err = mapFloat(summaryMap, "duration-ms"); err != nil {
		return nil, err
	}

	casesVal := root.Get(typeatom.NewKeyword("cases"))
	casesVec, ok := casesVal.(*coll.Vector)
	if !ok {
		return nil, fmt.Errorf("field :cases must be vector")
	}
	cases := make([]Result, 0, len(casesVec.Items))
	for _, item := range casesVec.Items {
		caseMap, ok := item.(*coll.Map)
		if !ok {
			return nil, fmt.Errorf("case entry must be map")
		}
		caseReport, err := decodeCase(caseMap)
		if err != nil {
			return nil, err
		}
		cases = append(cases, caseReport)
	}

	sort.Slice(cases, func(i, j int) bool {
		if cases[i].Namespace != cases[j].Namespace {
			return cases[i].Namespace < cases[j].Namespace
		}
		if cases[i].Name != cases[j].Name {
			return cases[i].Name < cases[j].Name
		}
		return cases[i].Status < cases[j].Status
	})

	return &Report{
		Summary: summary,
		Cases:   cases,
	}, nil
}

func decodeCase(m *coll.Map) (Result, error) {
	var err error
	out := Result{}

	if out.Namespace, err = mapSymbolString(m, "ns"); err != nil {
		return out, err
	}
	if out.Name, err = mapSymbolString(m, "name"); err != nil {
		return out, err
	}
	if out.Status, err = mapKeywordString(m, "status"); err != nil {
		return out, err
	}
	if out.DurationMS, err = mapFloat(m, "duration-ms"); err != nil {
		return out, err
	}
	out.Form = mapStringValue(m, "form")
	out.Message = mapDisplayString(m, "message")
	out.Expected = mapStringValue(m, "expected")
	out.Actual = mapStringValue(m, "actual")
	out.Error = mapDisplayString(m, "error")
	out.Contexts = mapStringVector(m, "contexts")
	return out, nil
}

func mapField(m *coll.Map, key string) (*coll.Map, error) {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil {
		return nil, fmt.Errorf("missing field :%s in test result", key)
	}
	fieldMap, ok := value.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("field :%s must be map", key)
	}
	return fieldMap, nil
}

func mapInt(m *coll.Map, key string) (int, error) {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil {
		return 0, fmt.Errorf("missing field :%s in test result", key)
	}
	number, ok := value.(types.Number)
	if !ok {
		return 0, fmt.Errorf("field :%s must be number", key)
	}
	return int(number.ToFloat64()), nil
}

func mapFloat(m *coll.Map, key string) (float64, error) {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil {
		return 0, fmt.Errorf("missing field :%s in test result", key)
	}
	number, ok := value.(types.Number)
	if !ok {
		return 0, fmt.Errorf("field :%s must be number", key)
	}
	return number.ToFloat64(), nil
}

func mapSymbolString(m *coll.Map, key string) (string, error) {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil {
		return "", fmt.Errorf("missing field :%s in case report", key)
	}
	name, ok := types.SymbolName(value)
	if !ok {
		return "", fmt.Errorf("field :%s must be symbol", key)
	}
	return name, nil
}

func mapKeywordString(m *coll.Map, key string) (string, error) {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil {
		return "", fmt.Errorf("missing field :%s in case report", key)
	}
	keyword, ok := value.(typeatom.Keyword)
	if !ok {
		return "", fmt.Errorf("field :%s must be keyword", key)
	}
	return string(keyword), nil
}

func mapStringValue(m *coll.Map, key string) string {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil || value == typeatom.NilValue {
		return ""
	}
	return value.String()
}

func mapDisplayString(m *coll.Map, key string) string {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil || value == typeatom.NilValue {
		return ""
	}
	if s, ok := value.(typeatom.String); ok {
		return string(s)
	}
	return value.String()
}

func mapStringVector(m *coll.Map, key string) []string {
	value := m.Get(typeatom.NewKeyword(key))
	if value == nil || value == typeatom.NilValue {
		return []string{}
	}
	vec, ok := value.(*coll.Vector)
	if !ok {
		return []string{value.String()}
	}
	out := make([]string, 0, len(vec.Items))
	for _, item := range vec.Items {
		if item == nil || item == typeatom.NilValue {
			continue
		}
		if s, ok := item.(typeatom.String); ok {
			out = append(out, string(s))
		} else {
			out = append(out, item.String())
		}
	}
	return out
}
