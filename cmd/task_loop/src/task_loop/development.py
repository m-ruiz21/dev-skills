"""Development seam for task-loop: runs the selected issue through a TDD agent.

For a selected issue, builds the context a TDD agent needs (the PRD, the
issue, `progress.txt`, and the issue's review thread), delegates to a small,
replaceable "development adapter" callable (`DevelopmentAgent`), and strictly
validates the raw string response against the CLI's outcome contract. This
module -- not the agent -- decides whether a response counts as a valid
completion; the agent only proposes an outcome string.

A valid `[completed] message` response is recorded as a `developer` message
by reusing `task_loop.messages.add_message` -- the same production
message-writing interface `task-loop add-message` uses -- rather than
duplicating file-append behavior here.
"""
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Optional, Union

from .messages import add_message, build_progress_update_instruction

PathLike = Union[str, Path]

SUPPORTED_OUTCOMES = ("completed", "needs-clarity", "partial")

# next_phase for each supported outcome: "completed" advances to testing;
# "needs-clarity" pauses for an interactive answer before retrying the issue;
# "partial" automatically retries the same issue on the next iteration.
_NEXT_PHASE_BY_OUTCOME = {
    "completed": "testing",
    "needs-clarity": "needs-clarity",
    "partial": "partial",
}

TEST_FIRST_INSTRUCTION = (
    "Develop this issue using strict test-first vertical slices: for each "
    "observable behavior, write one failing test through a public interface, "
    "then only the minimal code to make it pass, before moving to the next "
    "behavior. Do not write multiple tests before implementing any of them."
)

NO_DIRECT_REVIEW_EDITS_INSTRUCTION = (
    "Do not edit the review document directly. Record findings and your "
    "final outcome only by running `task-loop add-message`."
)

# Anchored at the very start of the response: leading prose before the
# bracketed outcome is rejected, not merely ignored.
_LEADING_OUTCOME_RE = re.compile(
    r"^\[(?P<outcome>[a-zA-Z][a-zA-Z-]*)\](?P<after>.*)\Z", re.DOTALL
)
_SECOND_OUTCOME_RE = re.compile(r"^\s*\[[a-zA-Z][a-zA-Z-]*\]")


class DevelopmentAgentError(RuntimeError):
    """Raised when the development agent process itself fails to run.

    Distinct from `MalformedDevelopmentResponseError`, which signals that the
    agent process ran successfully but returned a response that does not
    satisfy the strict outcome contract.
    """


class MalformedDevelopmentResponseError(ValueError):
    """Raised when the agent's response is not a supported, well-formed outcome."""


@dataclass(frozen=True)
class DevelopmentContext:
    """Everything a development agent needs for one TDD pass on one issue."""

    prd_path: Path
    prd: str
    issue_path: Path
    issue: str
    progress: str
    review_path: Path
    review: str
    instructions: str


DevelopmentAgent = Callable[[DevelopmentContext], str]


@dataclass(frozen=True)
class DevelopmentResult:
    """The outcome of a successful development pass.

    `outcome` is `"completed"` (ready for testing), `"needs-clarity"` (the
    CLI must pause for an interactive answer before retrying the same
    issue), or `"partial"` (progress was recorded and the same issue is
    automatically retried on the next iteration, without pausing).
    `next_phase` mirrors that distinction for callers that only care about
    what happens next.
    """

    issue_path: Path
    outcome: str
    message: str
    thread_id: str
    next_phase: str


def _validate_response(response):
    """Strictly parse a `[<outcome>] message` response, or raise explicitly.

    Returns the `(outcome, message)` pair on success.
    """
    if not isinstance(response, str):
        raise MalformedDevelopmentResponseError(
            f"development response must be a single string, got {type(response).__name__}"
        )
    match = _LEADING_OUTCOME_RE.match(response)
    if not match:
        raise MalformedDevelopmentResponseError(
            "development response must start with a bracketed outcome, e.g. "
            f"'[completed] ...': got {response!r}"
        )
    outcome = match.group("outcome")
    after = match.group("after")
    if _SECOND_OUTCOME_RE.match(after):
        raise MalformedDevelopmentResponseError(
            f"development response must contain exactly one bracketed outcome: {response!r}"
        )
    if after and not after[:1].isspace():
        raise MalformedDevelopmentResponseError(
            f"development response outcome must be followed by whitespace: {response!r}"
        )
    message = after.strip()
    if not message:
        raise MalformedDevelopmentResponseError(
            "development response message must not be empty"
        )
    if outcome not in SUPPORTED_OUTCOMES:
        raise MalformedDevelopmentResponseError(
            f"unsupported development outcome: '[{outcome}]' "
            f"(only {SUPPORTED_OUTCOMES} is supported in this phase)"
        )
    return outcome, message


def _build_prompt(context: DevelopmentContext) -> str:
    return (
        f"{context.instructions}\n\n"
        f"PRD ({context.prd_path}):\n{context.prd}\n\n"
        f"Issue ({context.issue_path}):\n{context.issue}\n\n"
        f"Progress so far:\n{context.progress}\n\n"
        f"Review thread ({context.review_path}):\n{context.review}\n\n"
        "Respond with exactly one line: '[completed] <message>' once this "
        "vertical slice is fully implemented and its tests pass, "
        "'[partial] <message>' to record real progress and automatically "
        "retry this same issue on the next iteration, or "
        "'[needs-clarity] <question>' if you hit an unexpectedly large "
        "ticket, a broken seam, an undefined major decision, or "
        "low-confidence information that blocks safe progress."
    )


def default_development_agent(context: DevelopmentContext, binary: str = "copilot") -> str:
    """Production development agent: invokes the Copilot CLI as a subprocess.

    Raises `DevelopmentAgentError` for process-level failures (the binary is
    missing, the process cannot start, or it exits non-zero) -- distinct from
    `MalformedDevelopmentResponseError`, which `_validate_response` raises
    when the process runs successfully but its output does not satisfy the
    strict outcome contract.
    """
    prompt = _build_prompt(context)
    try:
        completed = subprocess.run(
            [binary, "--yolo", "-p", prompt],
            capture_output=True,
            text=True,
        )
    except OSError as exc:
        raise DevelopmentAgentError(
            f"failed to start development agent process: {exc}"
        ) from exc

    if completed.returncode != 0:
        raise DevelopmentAgentError(
            f"development agent process exited with status {completed.returncode}: "
            f"{completed.stderr.strip() or completed.stdout.strip()}"
        )

    return completed.stdout


def run_development(
    issue_path: PathLike,
    prd_path: PathLike,
    agent: Optional[DevelopmentAgent] = None,
) -> DevelopmentResult:
    """Run the selected issue through a development agent for one TDD pass."""
    issue_path = Path(issue_path)
    prd_path = Path(prd_path)
    review_path = Path("review") / f"{issue_path.stem}.md"
    progress_path = prd_path.parent / "progress.txt"

    context = DevelopmentContext(
        prd_path=prd_path,
        prd=prd_path.read_text(),
        issue_path=issue_path,
        issue=issue_path.read_text(),
        progress=progress_path.read_text() if progress_path.is_file() else "",
        review_path=review_path,
        review=review_path.read_text() if review_path.is_file() else "",
        instructions=(
            f"{TEST_FIRST_INSTRUCTION}\n\n"
            f"{NO_DIRECT_REVIEW_EDITS_INSTRUCTION}\n\n"
            f"{build_progress_update_instruction(progress_path, 'developer')}"
        ),
    )
    response = (agent or default_development_agent)(context)
    outcome, message = _validate_response(response)

    result = add_message(review_path, message, "developer")

    return DevelopmentResult(
        issue_path=issue_path,
        outcome=outcome,
        message=message,
        thread_id=result.thread_id,
        next_phase=_NEXT_PHASE_BY_OUTCOME[outcome],
    )
