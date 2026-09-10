package taskloop

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func artifactJSON(t *testing.T, findings []map[string]any) string {
	t.Helper()
	if findings == nil {
		findings = []map[string]any{}
	}
	dimensions := make([]map[string]any, 0)
	for _, name := range RequiredDimensions {
		dimensions = append(dimensions, map[string]any{"dimension": name, "grade": 90, "evidence": []string{"ok"}})
	}
	data, err := json.Marshal(map[string]any{"schemaVersion": "1.0", "runId": "run", "dimensions": dimensions, "findings": findings})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func finding(id, dimension, severity, status string) map[string]any {
	return map[string]any{"id": id, "dimension": dimension, "severity": severity, "status": status, "summary": "A finding."}
}

func TestReviewScoringFormulaAndThresholds(t *testing.T) {
	tests := []struct {
		name     string
		findings []map[string]any
		load     float64
		score    float64
		passed   bool
	}{
		{"none", nil, 0, 100, true},
		{"medium", []map[string]any{finding("1", "security", "medium", "open")}, 5, 100 * math.Pow(.8, 5.0/20), true},
		{"nine low", repeatFindings(9), 9, 90.44623519256389, true},
		{"ten low", repeatFindings(10), 10, 89.44271909999159, false},
		{"critical", []map[string]any{finding("1", "security", "critical", "open")}, 20, 80, false},
		{"blocker", []map[string]any{finding("1", "security", "blocker", "open")}, 20, 80, false},
		{"info ignored", []map[string]any{finding("1", "security", "info", "open")}, 0, 100, true},
		{"addressed ignored", []map[string]any{finding("1", "security", "critical", "addressed")}, 0, 100, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, score, err := ParseAndScoreReview(artifactJSON(t, test.findings))
			if err != nil {
				t.Fatal(err)
			}
			dimension := score.Dimensions[0]
			if dimension.Load != test.load || math.Abs(dimension.Score-test.score) > 1e-10 || dimension.Passed != test.passed {
				t.Fatalf("got load=%v score=%v passed=%v", dimension.Load, dimension.Score, dimension.Passed)
			}
		})
	}
}

func repeatFindings(count int) []map[string]any {
	result := make([]map[string]any, count)
	for index := range result {
		result[index] = finding(string(rune('A'+index)), "security", "low", "open")
	}
	return result
}

func TestReviewStrictParsingRejectsMalformedArtifacts(t *testing.T) {
	valid := func() map[string]any {
		var object map[string]any
		if err := json.Unmarshal([]byte(artifactJSON(t, nil)), &object); err != nil {
			t.Fatal(err)
		}
		return object
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
		match  string
	}{
		{"schema", func(v map[string]any) { delete(v, "schemaVersion") }, "schemaVersion"},
		{"run", func(v map[string]any) { delete(v, "runId") }, "runId"},
		{"dimensions type", func(v map[string]any) { v["dimensions"] = "bad" }, "dimensions"},
		{"missing dimension", func(v map[string]any) { v["dimensions"] = v["dimensions"].([]any)[1:] }, "security"},
		{"duplicate", func(v map[string]any) { v["dimensions"] = append(v["dimensions"].([]any), v["dimensions"].([]any)[0]) }, "duplicate"},
		{"grade type", func(v map[string]any) { v["dimensions"].([]any)[0].(map[string]any)["grade"] = "90" }, "grade"},
		{"grade range", func(v map[string]any) { v["dimensions"].([]any)[0].(map[string]any)["grade"] = 101 }, "grade"},
		{"evidence empty", func(v map[string]any) { v["dimensions"].([]any)[0].(map[string]any)["evidence"] = []any{} }, "evidence"},
		{"findings type", func(v map[string]any) { v["findings"] = "bad" }, "findings"},
		{"duplicate ID", func(v map[string]any) {
			v["findings"] = []any{finding("DUP", "security", "low", "open"), finding("DUP", "security", "low", "open")}
		}, "DUP"},
		{"severity", func(v map[string]any) { v["findings"] = []any{finding("1", "security", "urgent", "open")} }, "urgent"},
		{"status", func(v map[string]any) { v["findings"] = []any{finding("1", "security", "low", "resolved")} }, "status"},
		{"location", func(v map[string]any) {
			f := finding("1", "security", "low", "open")
			f["location"] = map[string]any{"line": 1}
			v["findings"] = []any{f}
		}, "path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := valid()
			test.mutate(object)
			data, _ := json.Marshal(object)
			if _, _, err := ParseAndScoreReview(string(data)); err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("got error %v, want containing %q", err, test.match)
			}
		})
	}
	for _, malformed := range []string{`[]`, `{}`, artifactJSON(t, nil) + `{}`, artifactJSON(t, nil) + `x`} {
		if _, _, err := ParseAndScoreReview(malformed); err == nil {
			t.Errorf("accepted malformed response %q", malformed)
		}
	}
}

func TestReviewRenderingAndFailureMessageAreDeterministic(t *testing.T) {
	rawFinding := finding("SEC-1", "security", "critical", "open")
	rawFinding["summary"] = "Fix this."
	rawFinding["location"] = map[string]any{"path": "a.go", "line": 42, "column": 3}
	artifact, score, err := ParseAndScoreReview(artifactJSON(t, []map[string]any{rawFinding}))
	if err != nil {
		t.Fatal(err)
	}
	rendered := RenderReview(score, false)
	for _, wanted := range []string{"security: 80.0 [################----] FAILED", "Overall: FAILED", "architecture: 100.0 [####################] PASSED"} {
		if !strings.Contains(rendered, wanted) {
			t.Errorf("missing %q in %q", wanted, rendered)
		}
	}
	colored := RenderReview(score, true)
	if !strings.Contains(colored, "\x1b[31mfailed\x1b[0m") || !strings.Contains(colored, "\x1b[32mpassed\x1b[0m") {
		t.Fatal("missing deterministic ANSI tags")
	}
	message := BuildReviewFailureMessage(score, artifact.Findings)
	if !strings.Contains(message, "- security: 80.0 (below the 90 threshold)") || !strings.Contains(message, "[SEC-1] critical: Fix this. (a.go:42)") {
		t.Fatalf("unexpected message: %s", message)
	}
}
