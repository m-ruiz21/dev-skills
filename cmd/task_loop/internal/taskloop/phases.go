package taskloop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const TestFirstInstruction = "Develop this issue using strict test-first vertical slices: for each observable behavior, write one failing test through a public interface, then only the minimal code to make it pass, before moving to the next behavior. Do not write multiple tests before implementing any of them."
const NoDirectReviewEditsInstruction = "Do not edit the review document directly. Record findings and your final outcome only with the exact bundled add-message command below."
const TestInvestigationInstruction = "Run the repository's existing relevant tests for this issue (for example the project's `make test-*` targets) using the real test tooling; do not fabricate a result. If any test fails, investigate the cause and record actionable findings with the exact review findings command below before you respond."
const TestingResponseInstruction = "Respond with exactly one line and nothing else: '[success]' if every relevant test passes, or '[failure]' if any test fails. Do not add any other text, prose, or additional bracketed markers."
const ReviewResponseInstruction = "Respond with ONLY a single JSON object and nothing else -- no prose, no markdown code fences, no explanation before or after it. Produce exactly the structured artifact the `review-diff` skill documents: an object with a \"schemaVersion\" string, a \"runId\" string, a \"dimensions\" list, and a \"findings\" list. \"dimensions\" must contain exactly one entry for each of \"security\", \"testAdequacy\", \"planAlignment\", \"codeQuality\", and \"architecture\", each with a \"dimension\" name, an integer 0-100 \"grade\", and a non-empty \"evidence\" list of strings. \"findings\" is a list of finding objects, one per finding your review surfaced (an empty list if none); each must have a unique string \"id\", a \"dimension\" naming which of the five dimensions it belongs to, a \"severity\" that is exactly one of \"info\", \"low\", \"medium\", \"high\", \"critical\", or \"blocker\", a \"status\" that is exactly one of \"open\", \"addressed\", \"waived\", or \"invalid\", a non-empty \"summary\" string, and an optional \"location\" object with a \"path\" string and optional \"line\"/\"column\" integers. Do not include a pass/fail verdict yourself -- the CLI calculates it from your findings, not from your grades."

var leadingOutcomeRE = regexp.MustCompile(`(?s)^\[([a-zA-Z][a-zA-Z-]*)\](.*)$`)
var secondOutcomeRE = regexp.MustCompile(`^\s*\[[a-zA-Z][a-zA-Z-]*\]`)
var reviewerMessageRE = regexp.MustCompile(`(?m)^\[reviewer\] - `)

type DevelopmentContext struct {
	PRDPath, PRD, IssuePath, Issue, Progress, ReviewPath, Review, Instructions string
}

type DevelopmentResult struct {
	IssuePath string
	Outcome   DevelopmentOutcome
	Message   string
	ThreadID  string
}

type TestingContext struct {
	PRDPath, PRD, IssuePath, Issue, Progress, ReviewPath, Review, Instructions string
}

type TestingResult struct {
	IssuePath string
	Outcome   TestingOutcome
}

type ReviewContext struct {
	PRDPath, PRD, IssuePath, Issue, Progress, ReviewPath, Review, DeltaPath, Instructions string
}

type ReviewResult struct {
	IssuePath string
	Artifact  ReviewArtifact
	Score     ReviewScore
	Rendered  string
	Passed    bool
	ThreadID  string
	HasThread bool
}

type AgentProcessError struct {
	Phase  string
	Detail string
}

func (err AgentProcessError) Error() string { return err.Detail }

func RunDevelopment(ctx context.Context, root, issuePath, prdPath, commandPath string, store MessageStore, agent DevelopmentAgent) (DevelopmentResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	context, err := buildDevelopmentContext(root, issuePath, prdPath, commandPath, store)
	if err != nil {
		return DevelopmentResult{}, err
	}
	response, err := agent(ctx, context)
	if err != nil {
		return DevelopmentResult{}, AgentProcessError{Phase: "development", Detail: err.Error()}
	}
	if err := ctx.Err(); err != nil {
		return DevelopmentResult{}, AgentProcessError{Phase: "development", Detail: err.Error()}
	}
	outcome, message, err := parseDevelopmentResponse(response)
	if err != nil {
		return DevelopmentResult{}, err
	}
	thread, err := store.Add(context.ReviewPath, message, SenderDeveloper)
	if err != nil {
		return DevelopmentResult{}, err
	}
	return DevelopmentResult{IssuePath: issuePath, Outcome: outcome, Message: message, ThreadID: thread.ThreadID}, nil
}

func buildDevelopmentContext(root, issuePath, prdPath, commandPath string, store MessageStore) (DevelopmentContext, error) {
	paths, prd, issue, progress, review, err := readPhaseInputs(root, issuePath, prdPath)
	if err != nil {
		return DevelopmentContext{}, err
	}
	messagePath, err := store.PrepareMessageFile(MessagePurposeDevelopmentProgress)
	if err != nil {
		return DevelopmentContext{}, err
	}
	progressInstruction, err := BuildProgressUpdateInstruction(paths.Progress, messagePath, SenderDeveloper, commandPath)
	if err != nil {
		return DevelopmentContext{}, err
	}
	return DevelopmentContext{
		PRDPath: paths.PRD, PRD: prd, IssuePath: paths.Issue, Issue: issue,
		Progress: progress, ReviewPath: paths.ReviewReference, Review: review,
		Instructions: TestFirstInstruction + "\n\n" + NoDirectReviewEditsInstruction + "\n\n" + progressInstruction,
	}, nil
}

func parseDevelopmentResponse(response string) (DevelopmentOutcome, string, error) {
	match := leadingOutcomeRE.FindStringSubmatch(response)
	if match == nil {
		return "", "", fmt.Errorf("development response must start with a bracketed outcome, e.g. '[completed] ...': got %q", response)
	}
	if secondOutcomeRE.MatchString(match[2]) {
		return "", "", fmt.Errorf("development response must contain exactly one bracketed outcome: %q", response)
	}
	if match[2] != "" {
		first := match[2][0]
		if first != ' ' && first != '\t' && first != '\r' && first != '\n' && first != '\v' && first != '\f' {
			return "", "", fmt.Errorf("development response outcome must be followed by whitespace: %q", response)
		}
	}
	message := strings.TrimSpace(match[2])
	if message == "" {
		return "", "", fmt.Errorf("development response message must not be empty")
	}
	outcome := DevelopmentOutcome(match[1])
	switch outcome {
	case DevelopmentCompleted, DevelopmentNeedsClarity, DevelopmentPartial:
		return outcome, message, nil
	default:
		return "", "", fmt.Errorf("unsupported development outcome: '[%s]' (only ('completed', 'needs-clarity', 'partial') is supported in this phase)", outcome)
	}
}

func RunTesting(ctx context.Context, root, issuePath, prdPath, commandPath string, store MessageStore, agent TestingAgent) (TestingResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths, prd, issue, progress, reviewBefore, err := readPhaseInputs(root, issuePath, prdPath)
	if err != nil {
		return TestingResult{}, err
	}
	progressMessagePath, err := store.PrepareMessageFile(MessagePurposeTestingProgress)
	if err != nil {
		return TestingResult{}, err
	}
	progressInstruction, err := BuildProgressUpdateInstruction(paths.Progress, progressMessagePath, SenderReviewer, commandPath)
	if err != nil {
		return TestingResult{}, err
	}
	reviewMessagePath, err := store.PrepareMessageFile(MessagePurposeTestingReview)
	if err != nil {
		return TestingResult{}, err
	}
	reviewInstruction, err := BuildReviewFindingInstruction(paths.ReviewReference, reviewMessagePath, SenderReviewer, commandPath)
	if err != nil {
		return TestingResult{}, err
	}
	context := TestingContext{
		PRDPath: paths.PRD, PRD: prd, IssuePath: paths.Issue, Issue: issue,
		Progress: progress, ReviewPath: paths.ReviewReference, Review: reviewBefore,
		Instructions: TestInvestigationInstruction + "\n\n" + reviewInstruction + "\n\n" + progressInstruction + "\n\n" + TestingResponseInstruction,
	}
	response, err := agent(ctx, context)
	if err != nil {
		return TestingResult{}, AgentProcessError{Phase: "testing", Detail: err.Error()}
	}
	if err := ctx.Err(); err != nil {
		return TestingResult{}, AgentProcessError{Phase: "testing", Detail: err.Error()}
	}
	outcome, err := parseTestingResponse(response)
	if err != nil {
		return TestingResult{}, err
	}
	repositoryRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return TestingResult{}, err
	}
	reviewAfter, err := readOptionalConfined(repositoryRoot, paths.Review)
	if err != nil {
		return TestingResult{}, err
	}
	if outcome == TestingFailure && len(reviewerMessageRE.FindAllString(reviewAfter, -1)) <= len(reviewerMessageRE.FindAllString(reviewBefore, -1)) {
		return TestingResult{}, fmt.Errorf("a [failure] testing response must append investigation findings as a reviewer message via `task-loop add-message` before returning")
	}
	return TestingResult{IssuePath: issuePath, Outcome: outcome}, nil
}

func parseTestingResponse(response string) (TestingOutcome, error) {
	switch strings.TrimSpace(response) {
	case "[success]":
		return TestingSuccess, nil
	case "[failure]":
		return TestingFailure, nil
	default:
		return "", fmt.Errorf("testing response must be exactly '[success]' or '[failure]' with no other content: got %q", response)
	}
}

func RunReview(ctx context.Context, root, issuePath, prdPath, commandPath string, delta IssueDelta, store MessageStore, agent ReviewAgent, useColor bool) (ReviewResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths, prd, issue, progress, review, err := readPhaseInputs(root, issuePath, prdPath)
	if err != nil {
		return ReviewResult{}, err
	}
	if delta.Path == "" {
		return ReviewResult{}, fmt.Errorf("issue-owned review delta is missing")
	}
	deltaPath, err := canonicalIssueDeltaPath(paths.RunDirectory, delta.Path)
	if err != nil {
		return ReviewResult{}, err
	}
	defer os.Remove(deltaPath)
	messagePath, err := store.PrepareMessageFile(MessagePurposeReviewProgress)
	if err != nil {
		return ReviewResult{}, err
	}
	progressInstruction, err := BuildProgressUpdateInstruction(paths.Progress, messagePath, SenderReviewer, commandPath)
	if err != nil {
		return ReviewResult{}, err
	}
	reviewDiffInstruction := fmt.Sprintf("Use the `review-diff` skill to run a multi-dimensional review of only the issue-owned layered delta at %q, gathering findings for security, test adequacy, plan alignment, code quality, and architecture. The file labels independent run-start-to-current INDEX and EFFECTIVE WORKTREE deltas; identical layers are emitted once. Together they expose staged, unstaged, deleted, restored, and untracked issue changes while excluding state that predated the run. Do not use `git diff --staged` and do not review changes absent from this file.", deltaPath)
	context := ReviewContext{
		PRDPath: paths.PRD, PRD: prd, IssuePath: paths.Issue, Issue: issue,
		Progress: progress, ReviewPath: paths.ReviewReference, Review: review, DeltaPath: deltaPath,
		Instructions: reviewDiffInstruction + "\n\n" + progressInstruction + "\n\n" + ReviewResponseInstruction,
	}
	response, err := agent(ctx, context)
	if err != nil {
		return ReviewResult{}, AgentProcessError{Phase: "review", Detail: err.Error()}
	}
	if err := ctx.Err(); err != nil {
		return ReviewResult{}, AgentProcessError{Phase: "review", Detail: err.Error()}
	}
	artifact, score, err := ParseAndScoreReview(response)
	if err != nil {
		return ReviewResult{}, err
	}
	result := ReviewResult{IssuePath: issuePath, Artifact: artifact, Score: score, Rendered: RenderReview(score, useColor), Passed: score.Passed}
	if !score.Passed {
		thread, err := store.Add(paths.ReviewReference, BuildReviewFailureMessage(score, artifact.Findings), SenderReviewer)
		if err != nil {
			return ReviewResult{}, err
		}
		result.ThreadID, result.HasThread = thread.ThreadID, true
	}
	return result, nil
}

func readPhaseInputs(root, issuePath, prdPath string) (phasePaths, string, string, string, string, error) {
	paths, err := resolvePhasePaths(root, prdPath, issuePath)
	if err != nil {
		return phasePaths{}, "", "", "", "", err
	}
	prd, err := os.ReadFile(paths.PRD)
	if err != nil {
		return phasePaths{}, "", "", "", "", err
	}
	issue, err := os.ReadFile(paths.Issue)
	if err != nil {
		return phasePaths{}, "", "", "", "", err
	}
	progress, err := readOptionalConfined(paths.RunDirectory, paths.Progress)
	if err != nil {
		return phasePaths{}, "", "", "", "", err
	}
	repositoryRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return phasePaths{}, "", "", "", "", err
	}
	review, err := readOptionalConfined(repositoryRoot, paths.Review)
	if err != nil {
		return phasePaths{}, "", "", "", "", err
	}
	return paths, string(prd), string(issue), progress, review, nil
}

func issueStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func buildDevelopmentPrompt(context DevelopmentContext) string {
	return fmt.Sprintf("%s\n\nPRD (%s):\n%s\n\nIssue (%s):\n%s\n\nProgress so far:\n%s\n\nReview thread (%s):\n%s\n\nRespond with exactly one line: '[completed] <message>' once this vertical slice is fully implemented and its tests pass, '[partial] <message>' to record real progress and automatically retry this same issue on the next iteration, or '[needs-clarity] <question>' if you hit an unexpectedly large ticket, a broken seam, an undefined major decision, or low-confidence information that blocks safe progress.", context.Instructions, context.PRDPath, context.PRD, context.IssuePath, context.Issue, context.Progress, context.ReviewPath, context.Review)
}

func buildTestingPrompt(context TestingContext) string {
	return fmt.Sprintf("%s\n\nPRD (%s):\n%s\n\nIssue (%s):\n%s\n\nProgress so far:\n%s\n\nReview thread (%s):\n%s\n", context.Instructions, context.PRDPath, context.PRD, context.IssuePath, context.Issue, context.Progress, context.ReviewPath, context.Review)
}

func buildReviewPrompt(context ReviewContext) string {
	return fmt.Sprintf("%s\n\nPRD (%s):\n%s\n\nIssue (%s):\n%s\n\nProgress so far:\n%s\n\nReview thread (%s):\n%s\n", context.Instructions, context.PRDPath, context.PRD, context.IssuePath, context.Issue, context.Progress, context.ReviewPath, context.Review)
}
