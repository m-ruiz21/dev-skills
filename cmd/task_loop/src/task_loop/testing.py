"""Testing seam for task-loop: runs the repository's existing tests for the
selected issue after a completed development pass.

Testing starts only after development returns a valid `[completed]` result.
The raw string response from a replaceable "testing adapter" callable
(`TestingAgent`) is strictly validated against the CLI's exactly-one-of-two
outcome contract: `[success]` or `[failure]`, with no other content
permitted. This module -- not the agent -- decides what happens next; the
agent only proposes an outcome string.

A `[failure]` response requires the agent to have already appended its
investigation findings as a `reviewer` message to the review document
through `task_loop.messages.add_message` -- the same production
message-writing interface `task-loop add-message` uses -- before returning.
`run_testing` verifies a new reviewer message actually landed and raises
explicitly, without retrying silently, when it did not.
"""
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Optional, Union

from .messages import build_progress_update_instruction

PathLike = Union[str, Path]

_VALID_RESPONSES = ("[success]", "[failure]")

# Matches the `[reviewer] - <timestamp>` header `task_loop.messages.add_message`
# writes for every reviewer message, whether a new thread or a reply.
_REVIEWER_MESSAGE_RE = re.compile(r"^\[reviewer\] - ", re.MULTILINE)

TEST_INVESTIGATION_INSTRUCTION = (
    "Run the repository's existing relevant tests for this issue (for "
    "example the project's `make test-*` targets) using the real test "
    "tooling; do not fabricate a result. If any test fails, investigate the "
    "cause and record actionable findings by running `task-loop add-message "
    "-file <review-path> -message <findings> -from reviewer` before you "
    "respond -- do not edit the review document directly."
)

RESPONSE_CONTRACT_INSTRUCTION = (
    "Respond with exactly one line and nothing else: '[success]' if every "
    "relevant test passes, or '[failure]' if any test fails. Do not add any "
    "other text, prose, or additional bracketed markers."
)


class TestingAgentError(RuntimeError):
    """Raised when the testing agent process itself fails to run.

    Distinct from `MalformedTestingResponseError`, which signals that the
    agent process ran successfully but returned a response that does not
    satisfy the strict `[success]`/`[failure]` contract.
    """


class MalformedTestingResponseError(ValueError):
    """Raised when the response is not exactly '[success]' or '[failure]'."""


class MissingInvestigationFindingsError(RuntimeError):
    """Raised when a `[failure]` response has no corresponding new
    `reviewer` message appended to the review document.
    """


@dataclass(frozen=True)
class TestingContext:
    """Everything a testing agent needs for one test run on one issue."""

    prd_path: Path
    prd: str
    issue_path: Path
    issue: str
    progress: str
    review_path: Path
    review: str
    instructions: str


TestingAgent = Callable[[TestingContext], str]


@dataclass(frozen=True)
class TestingResult:
    """The outcome of a testing pass.

    `outcome` is `"success"` (ready for automated review) or `"failure"`
    (retry development for the same issue on the next iteration).
    `next_phase` mirrors that distinction for callers that only care about
    what happens next.
    """

    issue_path: Path
    outcome: str
    next_phase: str


def _validate_response(response) -> str:
    """Strictly parse a `[success]`/`[failure]` response, or raise explicitly.

    Returns the bare outcome (`"success"` or `"failure"`) on success.
    """
    if not isinstance(response, str):
        raise MalformedTestingResponseError(
            f"testing response must be a single string, got {type(response).__name__}"
        )
    stripped = response.strip()
    if stripped not in _VALID_RESPONSES:
        raise MalformedTestingResponseError(
            "testing response must be exactly '[success]' or '[failure]' "
            f"with no other content: got {response!r}"
        )
    return stripped[1:-1]


def _count_reviewer_messages(content: str) -> int:
    return len(_REVIEWER_MESSAGE_RE.findall(content))


def _build_prompt(context: TestingContext) -> str:
    return (
        f"{context.instructions}\n\n"
        f"PRD ({context.prd_path}):\n{context.prd}\n\n"
        f"Issue ({context.issue_path}):\n{context.issue}\n\n"
        f"Progress so far:\n{context.progress}\n\n"
        f"Review thread ({context.review_path}):\n{context.review}\n"
    )


def default_testing_agent(context: TestingContext, binary: str = "copilot") -> str:
    """Production testing agent: invokes the Copilot CLI as a subprocess.

    Mirrors `task_loop.development.default_development_agent`'s replaceable
    process seam. Raises `TestingAgentError` for process-level failures (the
    binary is missing, the process cannot start, or it exits non-zero) --
    distinct from `MalformedTestingResponseError`, which `_validate_response`
    raises when the process runs successfully but its output does not
    satisfy the strict outcome contract.
    """
    prompt = _build_prompt(context)
    try:
        completed = subprocess.run(
            [binary, "--yolo", "-p", prompt],
            capture_output=True,
            text=True,
        )
    except OSError as exc:
        raise TestingAgentError(
            f"failed to start testing agent process: {exc}"
        ) from exc

    if completed.returncode != 0:
        raise TestingAgentError(
            f"testing agent process exited with status {completed.returncode}: "
            f"{completed.stderr.strip() or completed.stdout.strip()}"
        )

    return completed.stdout


def run_testing(
    issue_path: PathLike,
    prd_path: PathLike,
    agent: Optional[TestingAgent] = None,
) -> TestingResult:
    """Run the selected issue's tests through a testing agent."""
    issue_path = Path(issue_path)
    prd_path = Path(prd_path)
    review_path = Path("review") / f"{issue_path.stem}.md"
    progress_path = prd_path.parent / "progress.txt"

    review_before = review_path.read_text() if review_path.is_file() else ""

    context = TestingContext(
        prd_path=prd_path,
        prd=prd_path.read_text(),
        issue_path=issue_path,
        issue=issue_path.read_text(),
        progress=progress_path.read_text() if progress_path.is_file() else "",
        review_path=review_path,
        review=review_before,
        instructions=(
            f"{TEST_INVESTIGATION_INSTRUCTION}\n\n"
            f"{build_progress_update_instruction(progress_path, 'reviewer')}\n\n"
            f"{RESPONSE_CONTRACT_INSTRUCTION}"
        ),
    )
    response = (agent or default_testing_agent)(context)
    outcome = _validate_response(response)

    if outcome == "failure":
        review_after = review_path.read_text() if review_path.is_file() else ""
        if _count_reviewer_messages(review_after) <= _count_reviewer_messages(review_before):
            raise MissingInvestigationFindingsError(
                "a [failure] testing response must append investigation "
                "findings as a reviewer message via `task-loop add-message` "
                "before returning"
            )

    return TestingResult(
        issue_path=issue_path,
        outcome=outcome,
        next_phase="review" if outcome == "success" else "development",
    )
