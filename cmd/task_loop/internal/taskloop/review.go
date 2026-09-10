package taskloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const PassThreshold = 90.0

var RequiredDimensions = []string{"security", "testAdequacy", "planAlignment", "codeQuality", "architecture"}

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
	SeverityBlocker  Severity = "blocker"
)

type FindingStatus string

const (
	FindingOpen      FindingStatus = "open"
	FindingAddressed FindingStatus = "addressed"
	FindingWaived    FindingStatus = "waived"
	FindingInvalid   FindingStatus = "invalid"
)

type Location struct {
	Path      string
	Line      int
	Column    int
	HasLine   bool
	HasColumn bool
}

type Finding struct {
	ID          string
	Dimension   string
	Severity    Severity
	Status      FindingStatus
	Summary     string
	Location    Location
	HasLocation bool
}

type DimensionScore struct {
	Name   string
	Grade  int
	Counts map[Severity]int
	Load   float64
	Score  float64
	Passed bool
}

type ReviewScore struct {
	Dimensions []DimensionScore
	Passed     bool
}

type ReviewArtifact struct {
	SchemaVersion string
	RunID         string
	Grades        map[string]int
	Findings      []Finding
}

func ParseAndScoreReview(response string) (ReviewArtifact, ReviewScore, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(response))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return ReviewArtifact{}, ReviewScore{}, fmt.Errorf("review response is not valid JSON: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return ReviewArtifact{}, ReviewScore{}, fmt.Errorf("review response is not valid JSON: trailing content")
	} else if err != io.EOF {
		return ReviewArtifact{}, ReviewScore{}, fmt.Errorf("review response is not valid JSON: %v", err)
	}
	artifact, err := parseReviewArtifact(raw)
	if err != nil {
		return ReviewArtifact{}, ReviewScore{}, err
	}
	return artifact, ScoreReview(artifact), nil
}

func parseReviewArtifact(value any) (ReviewArtifact, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ReviewArtifact{}, fmt.Errorf("review data must be an object, got %T", value)
	}
	schemaVersion, err := nonEmptyString(object["schemaVersion"], "schemaVersion")
	if err != nil {
		return ReviewArtifact{}, err
	}
	runID, err := nonEmptyString(object["runId"], "runId")
	if err != nil {
		return ReviewArtifact{}, err
	}
	rawDimensions, ok := object["dimensions"].([]any)
	if !ok {
		return ReviewArtifact{}, fmt.Errorf("dimensions must be a list, got %T", object["dimensions"])
	}
	grades := make(map[string]int)
	for index, rawDimension := range rawDimensions {
		entry, ok := rawDimension.(map[string]any)
		if !ok {
			return ReviewArtifact{}, fmt.Errorf("dimensions[%d] must be an object, got %T", index, rawDimension)
		}
		name, err := nonEmptyString(entry["dimension"], fmt.Sprintf("dimensions[%d].dimension", index))
		if err != nil {
			return ReviewArtifact{}, err
		}
		if !isDimension(name) {
			return ReviewArtifact{}, fmt.Errorf("dimensions[%d].dimension is unsupported: %q", index, name)
		}
		if _, exists := grades[name]; exists {
			return ReviewArtifact{}, fmt.Errorf("dimensions contains a duplicate entry for %q", name)
		}
		grade, err := integer(entry["grade"], fmt.Sprintf("dimensions[%d] (%s).grade", index, name))
		if err != nil || grade < 0 || grade > 100 {
			if err != nil {
				return ReviewArtifact{}, err
			}
			return ReviewArtifact{}, fmt.Errorf("dimensions[%d] (%s).grade must be between 0 and 100, got %d", index, name, grade)
		}
		evidence, ok := entry["evidence"].([]any)
		if !ok || len(evidence) == 0 {
			return ReviewArtifact{}, fmt.Errorf("dimensions[%d] (%s).evidence must be a non-empty list", index, name)
		}
		for evidenceIndex, item := range evidence {
			if _, err := nonEmptyString(item, fmt.Sprintf("dimensions[%d] (%s).evidence[%d]", index, name, evidenceIndex)); err != nil {
				return ReviewArtifact{}, err
			}
		}
		grades[name] = grade
	}
	for _, name := range RequiredDimensions {
		if _, ok := grades[name]; !ok {
			return ReviewArtifact{}, fmt.Errorf("dimensions is missing required entries: %s", name)
		}
	}
	rawFindings, ok := object["findings"].([]any)
	if !ok {
		return ReviewArtifact{}, fmt.Errorf("findings must be a list, got %T", object["findings"])
	}
	findings := make([]Finding, 0, len(rawFindings))
	seen := make(map[string]int)
	for index, rawFinding := range rawFindings {
		finding, err := parseFinding(index, rawFinding, seen)
		if err != nil {
			return ReviewArtifact{}, err
		}
		findings = append(findings, finding)
	}
	return ReviewArtifact{SchemaVersion: schemaVersion, RunID: runID, Grades: grades, Findings: findings}, nil
}

func parseFinding(index int, value any, seen map[string]int) (Finding, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return Finding{}, fmt.Errorf("findings[%d] must be a finding object, got %T", index, value)
	}
	id, err := nonEmptyString(object["id"], fmt.Sprintf("findings[%d].id", index))
	if err != nil {
		return Finding{}, err
	}
	if first, exists := seen[id]; exists {
		return Finding{}, fmt.Errorf("findings[%d].id %q duplicates findings[%d].id", index, id, first)
	}
	seen[id] = index
	dimension, err := nonEmptyString(object["dimension"], fmt.Sprintf("findings[%d].dimension", index))
	if err != nil {
		return Finding{}, err
	}
	if !isDimension(dimension) {
		return Finding{}, fmt.Errorf("findings[%d].dimension is unsupported: %q", index, dimension)
	}
	severityText, err := nonEmptyString(object["severity"], fmt.Sprintf("findings[%d] (%s).severity", index, dimension))
	if err != nil {
		return Finding{}, err
	}
	severity, ok := parseSeverity(severityText)
	if !ok {
		return Finding{}, fmt.Errorf("findings[%d] (%s).severity is unsupported: %q", index, dimension, severityText)
	}
	statusText, ok := object["status"].(string)
	if !ok {
		return Finding{}, fmt.Errorf("findings[%d] (%s).status is invalid", index, dimension)
	}
	status, ok := parseFindingStatus(statusText)
	if !ok {
		return Finding{}, fmt.Errorf("findings[%d] (%s).status is invalid: %q", index, dimension, statusText)
	}
	summary, err := nonEmptyString(object["summary"], fmt.Sprintf("findings[%d] (%s).summary", index, dimension))
	if err != nil {
		return Finding{}, err
	}
	finding := Finding{ID: id, Dimension: dimension, Severity: severity, Status: status, Summary: summary}
	rawLocation, exists := object["location"]
	if !exists || rawLocation == nil {
		return finding, nil
	}
	locationObject, ok := rawLocation.(map[string]any)
	if !ok {
		return Finding{}, fmt.Errorf("findings[%d] (%s).location must be an object", index, dimension)
	}
	path, err := nonEmptyString(locationObject["path"], fmt.Sprintf("findings[%d] (%s).location.path", index, dimension))
	if err != nil {
		return Finding{}, err
	}
	location := Location{Path: path}
	if rawLine, exists := locationObject["line"]; exists && rawLine != nil {
		line, err := positiveInteger(rawLine, fmt.Sprintf("findings[%d] (%s).location.line", index, dimension))
		if err != nil {
			return Finding{}, err
		}
		location.Line, location.HasLine = line, true
	}
	if rawColumn, exists := locationObject["column"]; exists && rawColumn != nil {
		column, err := positiveInteger(rawColumn, fmt.Sprintf("findings[%d] (%s).location.column", index, dimension))
		if err != nil {
			return Finding{}, err
		}
		location.Column, location.HasColumn = column, true
	}
	finding.Location, finding.HasLocation = location, true
	return finding, nil
}

func ScoreReview(artifact ReviewArtifact) ReviewScore {
	lambda := -math.Log(0.8) / 20
	dimensions := make([]DimensionScore, 0, len(RequiredDimensions))
	passed := true
	for _, name := range RequiredDimensions {
		counts := map[Severity]int{SeverityLow: 0, SeverityMedium: 0, SeverityHigh: 0, SeverityCritical: 0, SeverityBlocker: 0}
		for _, finding := range artifact.Findings {
			if finding.Dimension == name && finding.Status == FindingOpen && finding.Severity != SeverityInfo {
				counts[finding.Severity]++
			}
		}
		load := float64(counts[SeverityLow] + counts[SeverityMedium]*5 + counts[SeverityHigh]*10 + counts[SeverityCritical]*20 + counts[SeverityBlocker]*20)
		score := 100 * math.Exp(-lambda*load)
		dimensionPassed := score >= PassThreshold
		passed = passed && dimensionPassed
		dimensions = append(dimensions, DimensionScore{Name: name, Grade: artifact.Grades[name], Counts: counts, Load: load, Score: score, Passed: dimensionPassed})
	}
	return ReviewScore{Dimensions: dimensions, Passed: passed}
}

func RenderReview(score ReviewScore, useColor bool) string {
	lines := make([]string, 0, len(score.Dimensions)+1)
	for _, dimension := range score.Dimensions {
		filled := int(math.RoundToEven(math.Max(0, math.Min(100, dimension.Score)) / 100 * 20))
		bar := "[" + strings.Repeat("#", filled) + strings.Repeat("-", 20-filled) + "]"
		lines = append(lines, fmt.Sprintf("%s: %.1f %s %s", dimension.Name, dimension.Score, bar, verdictTag(dimension.Passed, useColor)))
	}
	lines = append(lines, "Overall: "+verdictTag(score.Passed, useColor))
	return strings.Join(lines, "\n")
}

func verdictTag(passed, color bool) string {
	label := "failed"
	code := "\x1b[31m"
	if passed {
		label, code = "passed", "\x1b[32m"
	}
	if color {
		return code + label + "\x1b[0m"
	}
	return strings.ToUpper(label)
}

func BuildReviewFailureMessage(score ReviewScore, findings []Finding) string {
	lines := []string{"Automated review failed. Failing dimensions:"}
	for _, dimension := range score.Dimensions {
		if dimension.Passed {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %.1f (below the %.0f threshold)", dimension.Name, dimension.Score, PassThreshold))
		for _, finding := range findings {
			if finding.Dimension != dimension.Name || finding.Status != FindingOpen {
				continue
			}
			location := ""
			if finding.HasLocation {
				location = " (" + finding.Location.Path
				if finding.Location.HasLine {
					location += ":" + strconv.Itoa(finding.Location.Line)
				}
				location += ")"
			}
			lines = append(lines, fmt.Sprintf("  - [%s] %s: %s%s", finding.ID, finding.Severity, finding.Summary, location))
		}
	}
	return strings.Join(lines, "\n")
}

func nonEmptyString(value any, field string) (string, error) {
	text, ok := value.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("%s must be a non-empty string, got %v", field, value)
	}
	return text, nil
}

func integer(value any, field string) (int, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s must be an integer, got %v", field, value)
	}
	parsed, err := strconv.Atoi(string(number))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %v", field, value)
	}
	return parsed, nil
}

func positiveInteger(value any, field string) (int, error) {
	parsed, err := integer(value, field)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %v", field, value)
	}
	return parsed, nil
}

func isDimension(value string) bool {
	for _, dimension := range RequiredDimensions {
		if value == dimension {
			return true
		}
	}
	return false
}

func parseSeverity(value string) (Severity, bool) {
	severity := Severity(value)
	switch severity {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical, SeverityBlocker:
		return severity, true
	default:
		return "", false
	}
}

func parseFindingStatus(value string) (FindingStatus, bool) {
	status := FindingStatus(value)
	switch status {
	case FindingOpen, FindingAddressed, FindingWaived, FindingInvalid:
		return status, true
	default:
		return "", false
	}
}
