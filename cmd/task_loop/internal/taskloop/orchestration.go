package taskloop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

func RunIssueLoop(ctx context.Context, root string, run Run, issuePath, commandPath string, store MessageStore, agents Agents) int {
	if ctx == nil {
		ctx = context.Background()
	}
	latestReason := ""
	for iteration := 1; iteration <= run.MaxIterations; iteration++ {
		development, err := RunDevelopment(ctx, root, issuePath, run.PRDPath, commandPath, store, agents.Develop)
		if err != nil {
			return reportPhaseError(agents.Stderr, "development", err)
		}
		switch development.Outcome {
		case DevelopmentPartial:
			fmt.Fprintf(agents.Stdout, "Developer (partial): %s\n", development.Message)
			latestReason = "partial development progress: " + development.Message
		case DevelopmentNeedsClarity:
			fmt.Fprintf(agents.Stdout, "Developer needs clarity: %s\n", development.Message)
			latestReason = "awaiting clarity: " + development.Message
			if iteration < run.MaxIterations {
				answer, available := readClarity(ctx, agents.Stdin, "task-loop needs clarity: "+development.Message)
				if !available || strings.TrimSpace(answer) == "" {
					fmt.Fprintln(agents.Stderr, "task-loop: error: clarity request was cancelled or received no answer")
					return 1
				}
				reviewPath := reviewReference(issuePath)
				if _, err := store.Reply(reviewPath, answer, SenderUser, development.ThreadID); err != nil {
					fmt.Fprintf(agents.Stderr, "task-loop: error: %v\n", err)
					return 1
				}
			}
		case DevelopmentCompleted:
			fmt.Fprintf(agents.Stdout, "Developer: %s\nPhase: testing\n", development.Message)
			testing, err := RunTesting(ctx, root, issuePath, run.PRDPath, commandPath, store, agents.Test)
			if err != nil {
				return reportPhaseError(agents.Stderr, "testing", err)
			}
			if testing.Outcome == TestingFailure {
				fmt.Fprintln(agents.Stdout, "Tests: failure")
				latestReason = "tests failed; see the review thread for investigation findings"
				break
			}
			fmt.Fprintln(agents.Stdout, "Tests: success")
			fmt.Fprintln(agents.Stdout, "Phase: review")
			if err := ctx.Err(); err != nil {
				return reportPhaseError(agents.Stderr, "review", AgentProcessError{Phase: "review", Detail: err.Error()})
			}
			delta, err := run.Baseline.PrepareDelta(ctx)
			if err != nil {
				return reportPhaseError(agents.Stderr, "review", err)
			}
			review, err := RunReview(ctx, root, issuePath, run.PRDPath, commandPath, delta, store, agents.Review, agents.UseColor)
			if err != nil {
				return reportPhaseError(agents.Stderr, "review", err)
			}
			fmt.Fprintln(agents.Stdout, review.Rendered)
			if review.Passed {
				fmt.Fprintln(agents.Stdout, "Automated review passed for every dimension. Stopping for external human review.")
				return 0
			}
			latestReason = reviewFailureReason(review.Score)
		}
		if iteration == run.MaxIterations {
			fmt.Fprintf(agents.Stderr, "task-loop: error: max iterations (%d) reached for issue %s: %s\n", run.MaxIterations, issuePath, latestReason)
			return 1
		}
	}
	return 1
}

type clarityRead struct {
	answer    string
	available bool
}

func readClarity(ctx context.Context, input UserInput, prompt string) (string, bool) {
	result := make(chan clarityRead, 1)
	go func() {
		answer, available := input.Read(prompt)
		result <- clarityRead{answer: answer, available: available}
	}()
	select {
	case read := <-result:
		return read.answer, read.available
	case <-ctx.Done():
		return "", false
	}
}

func reportPhaseError(stderr io.Writer, phase string, err error) int {
	var processError AgentProcessError
	if errors.As(err, &processError) {
		fmt.Fprintf(stderr, "task-loop: error: %s agent failed: %v\n", phase, err)
	} else if phase == "review" {
		fmt.Fprintf(stderr, "task-loop: error: review: %v\n", err)
	} else {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
	}
	return 1
}

func reviewFailureReason(score ReviewScore) string {
	failing := make([]string, 0)
	for _, dimension := range score.Dimensions {
		if !dimension.Passed {
			failing = append(failing, fmt.Sprintf("%s (%.1f)", dimension.Name, dimension.Score))
		}
	}
	return fmt.Sprintf("review failed: %s below the %.0f threshold", strings.Join(failing, ", "), PassThreshold)
}
