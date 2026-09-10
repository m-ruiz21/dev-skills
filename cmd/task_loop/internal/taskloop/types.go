package taskloop

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

const DefaultMaxIterations = 10

type Sender string

const (
	SenderUser      Sender = "user"
	SenderReviewer  Sender = "reviewer"
	SenderDeveloper Sender = "developer"
)

func ParseSender(value string) (Sender, error) {
	sender := Sender(value)
	switch sender {
	case SenderUser, SenderReviewer, SenderDeveloper:
		return sender, nil
	default:
		return "", fmt.Errorf("-from must be one of ('user', 'reviewer', 'developer'), got %q", value)
	}
}

type DevelopmentOutcome string

const (
	DevelopmentCompleted    DevelopmentOutcome = "completed"
	DevelopmentNeedsClarity DevelopmentOutcome = "needs-clarity"
	DevelopmentPartial      DevelopmentOutcome = "partial"
)

type TestingOutcome string

const (
	TestingSuccess TestingOutcome = "success"
	TestingFailure TestingOutcome = "failure"
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type ProcessRunner interface {
	Run(context.Context, string, ...string) (stdout string, stderr string, exitCode int, err error)
}

type UserInput interface {
	Read(prompt string) (answer string, available bool)
}

type PRDOption struct {
	Path  string
	Label string
}

type PRDSelector interface {
	Select(context.Context, []PRDOption) (selected PRDOption, ok bool, err error)
}

type PRDSelectorFunc func(context.Context, []PRDOption) (PRDOption, bool, error)

func (selector PRDSelectorFunc) Select(ctx context.Context, options []PRDOption) (PRDOption, bool, error) {
	return selector(ctx, options)
}

type Dependencies struct {
	Runner          ProcessRunner
	Input           UserInput
	PRDSelector     PRDSelector
	Clock           Clock
	CaptureBaseline func(context.Context, string, string) (WorkspaceBaseline, error)
}

type CLI struct {
	Root string
	Deps Dependencies
}

type Run struct {
	RepositoryRoot string
	RunDirectory   string
	PRDPath        string
	MaxIterations  int
	Baseline       WorkspaceBaseline
}

type TriageAgent func(TriageContext) (string, error)
type DevelopmentAgent func(context.Context, DevelopmentContext) (string, error)
type TestingAgent func(context.Context, TestingContext) (string, error)
type ReviewAgent func(context.Context, ReviewContext) (string, error)

type Agents struct {
	Triage     TriageAgent
	Develop    DevelopmentAgent
	Test       TestingAgent
	Review     ReviewAgent
	Stdin      UserInput
	Interrupts <-chan os.Signal
	UseColor   bool
	Stdout     io.Writer
	Stderr     io.Writer
	CopilotBin string
}
