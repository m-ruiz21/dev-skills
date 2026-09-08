"""Orchestration seam for task-loop runs.

Exposes PRD selection and the iteration budget through a small, testable
interface, and owns the bounded per-issue control flow: running the
selected issue through development, testing, and review, retrying it on
every actionable failure cause, and stopping cleanly on success or
exhaustion. The CLI (`task_loop.cli`) stays a thin layer over
`start_run` and `run_issue_loop`, so this control flow is testable and
does not grow further inside the argument-parsing entry point.
"""
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, Union

from .clarity import ClarityCancelledError, UserInputAgent, resolve_clarity
from .development import (
    DevelopmentAgent,
    DevelopmentAgentError,
    MalformedDevelopmentResponseError,
    run_development,
)
from .review import (
    PASS_THRESHOLD,
    MalformedReviewDataError,
    ReviewAgent,
    ReviewAgentError,
    ReviewScore,
    run_review,
)
from .testing import (
    MalformedTestingResponseError,
    MissingInvestigationFindingsError,
    TestingAgent,
    TestingAgentError,
    run_testing,
)

PathLike = Union[str, Path]

DEFAULT_MAX_ITERATIONS = 10


class InvalidPrdPathError(ValueError):
    """Raised when a PRD path is missing, unreadable, or not a PRD file."""


class InvalidMaxIterationsError(ValueError):
    """Raised when the requested iteration budget is not a positive integer."""


@dataclass(frozen=True)
class Run:
    """A selected PRD and the hard iteration budget for one task-loop invocation."""

    prd_path: Path
    max_iterations: int


def parse_max_iterations(value: Union[str, int, None]) -> int:
    """Parse and validate the iteration budget, defaulting when omitted."""
    if value is None:
        return DEFAULT_MAX_ITERATIONS
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        raise InvalidMaxIterationsError(
            f"--max-iterations must be an integer, got {value!r}"
        )
    if parsed < 1:
        raise InvalidMaxIterationsError(
            f"--max-iterations must be a positive integer, got {parsed}"
        )
    return parsed


def start_run(prd_path: PathLike, max_iterations: Union[str, int, None] = None) -> Run:
    """Validate the PRD path and iteration budget, returning an initialized run."""
    budget = parse_max_iterations(max_iterations)
    path = Path(prd_path)

    if not path.exists():
        raise InvalidPrdPathError(f"PRD path does not exist: {prd_path}")
    if not path.is_file():
        raise InvalidPrdPathError(f"PRD path is not a file: {prd_path}")
    if path.name.casefold() != "PRD.md".casefold():
        raise InvalidPrdPathError(
            f"PRD path must point to a PRD.md file, got: {prd_path}"
        )
    try:
        path.read_text()
    except OSError as exc:
        raise InvalidPrdPathError(f"PRD path is not readable: {prd_path} ({exc})")

    return Run(prd_path=path, max_iterations=budget)


def _review_failure_reason(score: ReviewScore) -> str:
    failing = ", ".join(
        f"{dimension.name} ({dimension.score:.1f})"
        for dimension in score.dimensions
        if not dimension.passed
    )
    return f"review failed: {failing} below the {PASS_THRESHOLD:.0f} threshold"


def _report_exhausted(run: Run, issue_path: Path, reason: str) -> int:
    """Print the shared non-success terminal message and return exit code 1.

    Names the selected issue and the latest failure reason so a run that
    exhausts its budget after any mix of partial, needs-clarity, test
    failure, or review failure retries reports a single, clear result --
    all four retry causes share this one message shape.
    """
    print(
        f"task-loop: error: max iterations ({run.max_iterations}) reached for "
        f"issue {issue_path}: {reason}",
        file=sys.stderr,
    )
    return 1


def run_issue_loop(
    run: Run,
    issue_path: PathLike,
    development_agent: Optional[DevelopmentAgent] = None,
    testing_agent: Optional[TestingAgent] = None,
    review_agent: Optional[ReviewAgent] = None,
    clarity_agent: Optional[UserInputAgent] = None,
) -> int:
    """Run the selected issue through its bounded development, testing,
    and review iterations, retrying automatically on every actionable
    failure cause.

    The same issue is retried on `[partial]`, `[needs-clarity]`, a failed
    test run, and a failed review -- always without re-running triage.
    All four retry causes share this single loop's iteration budget, so
    any chain or mix of them cannot run forever. A completed development
    pass plus its testing and review results belong to the same iteration
    that produced them; only a retry cause starts the next iteration.

    Returns the process exit code: `0` once every review dimension passes
    -- the run stops immediately and reports the work ready for external
    human review; it does not close the issue, collect approval, or
    select another issue -- or `1` when the iteration budget is exhausted
    (reporting the selected issue and the latest failure reason) or an
    agent/response error occurs.
    """
    issue_path = Path(issue_path)
    latest_reason: Optional[str] = None

    for iteration in range(1, run.max_iterations + 1):
        try:
            result = run_development(issue_path, run.prd_path, agent=development_agent)
        except DevelopmentAgentError as exc:
            print(f"task-loop: error: development agent failed: {exc}", file=sys.stderr)
            return 1
        except MalformedDevelopmentResponseError as exc:
            print(f"task-loop: error: {exc}", file=sys.stderr)
            return 1

        if result.next_phase == "testing":
            print(f"Developer: {result.message}")
            print(f"Phase: {result.next_phase}")

            try:
                testing_result = run_testing(
                    issue_path, run.prd_path, agent=testing_agent
                )
            except TestingAgentError as exc:
                print(
                    f"task-loop: error: testing agent failed: {exc}", file=sys.stderr
                )
                return 1
            except MalformedTestingResponseError as exc:
                print(f"task-loop: error: {exc}", file=sys.stderr)
                return 1
            except MissingInvestigationFindingsError as exc:
                print(f"task-loop: error: {exc}", file=sys.stderr)
                return 1

            if testing_result.next_phase == "review":
                print("Tests: success")
                print("Phase: review")

                try:
                    review_result = run_review(
                        issue_path,
                        run.prd_path,
                        agent=review_agent,
                        use_color=sys.stdout.isatty(),
                    )
                except ReviewAgentError as exc:
                    print(
                        f"task-loop: error: review agent failed: {exc}",
                        file=sys.stderr,
                    )
                    return 1
                except MalformedReviewDataError as exc:
                    print(f"task-loop: error: review: {exc}", file=sys.stderr)
                    return 1

                print(review_result.rendered)

                if review_result.passed:
                    print(
                        "Automated review passed for every dimension. "
                        "Stopping for external human review."
                    )
                    return 0

                latest_reason = _review_failure_reason(review_result.score)
                if iteration == run.max_iterations:
                    return _report_exhausted(run, issue_path, latest_reason)
                continue

            print("Tests: failure")
            latest_reason = (
                "tests failed; see the review thread for investigation findings"
            )
            if iteration == run.max_iterations:
                return _report_exhausted(run, issue_path, latest_reason)
            continue

        if result.next_phase == "partial":
            print(f"Developer (partial): {result.message}")
            latest_reason = f"partial development progress: {result.message}"
            if iteration == run.max_iterations:
                return _report_exhausted(run, issue_path, latest_reason)
            continue

        print(f"Developer needs clarity: {result.message}")
        latest_reason = f"awaiting clarity: {result.message}"
        if iteration == run.max_iterations:
            return _report_exhausted(run, issue_path, latest_reason)

        review_path = Path("review") / f"{issue_path.stem}.md"
        try:
            resolve_clarity(
                review_path, result.thread_id, result.message, agent=clarity_agent
            )
        except ClarityCancelledError as exc:
            print(f"task-loop: error: {exc}", file=sys.stderr)
            return 1

    return 1
