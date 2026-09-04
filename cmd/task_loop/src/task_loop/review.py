"""Review scoring seam for task-loop: turns the repository's real
`review-diff` skill artifact (see `skills/review-diff/SKILL.md`) into
per-dimension and overall pass/fail verdicts.

The CLI -- not the review agent, and not the skill's own subjective
`grade` -- owns the verdict. A replaceable review adapter (see
`task_loop.review.ReviewAgent`) invokes the `review-diff` skill and
returns raw text that this module parses as strict JSON, then validates
and scores against the skill's actual structured schema. Agent prose
never decides pass or fail; only this module's arithmetic on validated
`findings` does.

## Structured review data schema

This module consumes the exact artifact `review-diff` documents and
writes, not a parallel schema invented for task-loop. The parsed JSON
payload must be an object shaped like:

    {
      "schemaVersion": "1.0",
      "runId": "some-run-id",
      "dimensions": [
        {
          "dimension": "security",
          "grade": 90,
          "evidence": ["No changed code constructs commands from untrusted input."]
        },
        ...
      ],
      "findings": [
        {
          "id": "TEST-001",
          "dimension": "testAdequacy",
          "severity": "medium",
          "status": "open",
          "summary": "The timeout branch lacks a regression test.",
          "location": {"path": "src/example.cs", "line": 42, "column": 5}
        }
      ]
    }

- `schemaVersion` and `runId` (required, non-empty strings): preserved
  from the skill's artifact contract; validated for presence and shape
  only.
- `dimensions` (required, list): must contain exactly one entry for each
  of `REQUIRED_DIMENSIONS` -- no duplicates, no missing, no unknown
  dimension names. Each entry has:
  - `dimension` (required, string): one of `REQUIRED_DIMENSIONS`.
  - `grade` (required, integer 0-100): the review agent's own subjective
    grade for this dimension. **Validated for shape and range only --
    never used to compute pass/fail.** Findings and the PRD's exponential
    formula remain the sole authority on scoring, per the skill's own
    "Do not infer or declare Ralph's pass/fail result" instruction.
  - `evidence` (required, non-empty list of non-empty strings): per the
    skill, "Every dimension must appear exactly once and have non-empty
    evidence."
- `findings` (required, list, may be empty): each entry has:
  - `id` (required, non-empty string, unique across all findings): a
    stable finding identifier, per the skill's "Findings must retain
    stable identifiers across review passes."
  - `dimension` (required, string): one of `REQUIRED_DIMENSIONS`.
  - `severity` (required, string): one of `SUPPORTED_SEVERITIES`.
  - `status` (required, string): one of `info`, `open`, `addressed`,
    `waived`, or `invalid` -- see `_ALLOWED_STATUSES`.
  - `summary` (required, non-empty string).
  - `location` (optional, object): when present, must have a required
    non-empty string `path` and optional positive-integer `line` and
    `column`.

Any other shape -- a non-mapping payload, a missing/malformed
`schemaVersion` or `runId`, a missing/duplicate/unknown dimension, empty
or non-list evidence, a non-list `findings`, or a finding with a
malformed identity, dimension, severity, status, summary, or location --
raises `MalformedReviewDataError` explicitly instead of silently
coercing or ignoring the problem. There is no `count` extension: the
skill's artifact has none, and each finding object is exactly one
occurrence.

## Severity handling decisions

- `SUPPORTED_SEVERITIES` is exactly `info`, `low`, `medium`, `high`,
  `critical`, and `blocker` -- the six-value scale the skill instructs
  review subagents to normalize every dimension-specific severity label
  into ("Normalize all finding severities to `info`, `low`, `medium`,
  `high`, `critical`, or `blocker`.").
- `info` findings are valid and validated like any other finding, but
  never contribute to `L`: they carry no weight in `_SEVERITY_WEIGHTS`.
- `blocker` is a legitimate severity in the skill's own vocabulary (it is
  not translated away by the adapter). It is conservatively mapped to
  the same weight as `critical` (20) for scoring, since the PRD's formula
  has no `blocker` term of its own and a blocker is at least as severe as
  a critical finding. The raw `blocker` count is still reported on
  `DimensionScore.counts` for visibility.
- Only `status: "open"` findings count toward `L`. `addressed`, `waived`,
  and `invalid` findings are valid but always contribute zero, matching
  the skill's explicit statuses for resolved/dismissed/rejected findings.
"""
import json
import math
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Dict, List, Mapping, Optional, Sequence, Tuple, Union

from .messages import add_message, build_progress_update_instruction

PathLike = Union[str, Path]

REQUIRED_DIMENSIONS = (
    "security",
    "testAdequacy",
    "planAlignment",
    "codeQuality",
    "architecture",
)

SUPPORTED_SEVERITIES = ("info", "low", "medium", "high", "critical", "blocker")

# Severities that contribute to the load `L`, and their PRD weights.
# `blocker` is conservatively weighted the same as `critical` (see module
# docstring). `info` is intentionally absent -- it always contributes 0.
_SEVERITY_WEIGHTS = {"low": 1, "medium": 5, "high": 10, "critical": 20, "blocker": 20}

_ALLOWED_STATUSES = ("open", "addressed", "waived", "invalid")
_OPEN_STATUS = "open"

_MIN_GRADE = 0
_MAX_GRADE = 100

PASS_THRESHOLD = 90.0
_LAMBDA = -math.log(0.8) / 20


class MalformedReviewDataError(ValueError):
    """Raised when a structured review result cannot be validated or scored.

    Covers both a review agent's response failing to parse as JSON at all
    and a successfully parsed payload failing the schema validation
    described in the module docstring's "Structured review data schema"
    section -- both represent the same kind of problem: the review data
    itself is unusable, regardless of which step first detects it.
    """


class ReviewAgentError(RuntimeError):
    """Raised when the review agent process itself fails to run.

    Distinct from `MalformedReviewDataError`, which signals that the agent
    process ran successfully but its response does not parse as the
    structured review-diff JSON schema.
    """


@dataclass(frozen=True)
class DimensionScore:
    """The scored result for one review dimension.

    `grade` is the review agent's own subjective grade for this
    dimension, validated for shape only -- it never affects `score` or
    `passed`, which are computed solely from open findings via the PRD's
    exponential formula.
    """

    name: str
    grade: int
    counts: Dict[str, int]
    load: float
    score: float
    passed: bool


@dataclass(frozen=True)
class ReviewScore:
    """The scored result for an entire structured review result."""

    dimensions: Sequence[DimensionScore]
    passed: bool


@dataclass(frozen=True)
class ReviewContext:
    """Everything a review agent needs for one automated review pass."""

    prd_path: Path
    prd: str
    issue_path: Path
    issue: str
    progress: str
    review_path: Path
    review: str
    instructions: str


ReviewAgent = Callable[[ReviewContext], str]


@dataclass(frozen=True)
class ReviewResult:
    """The outcome of one automated review pass: its full per-dimension
    score plus the rendered report ready to print.

    `thread_id` is the review-document thread created for a failing
    review's actionable summary message (see `run_review`), and is
    `None` when the review passed and no failure message was appended.
    """

    issue_path: Path
    score: ReviewScore
    rendered: str
    passed: bool
    thread_id: Optional[str] = None


REVIEW_DIFF_SKILL_INSTRUCTION = (
    "Use the `review-diff` skill to run a multi-dimensional review of the "
    "staged diff for this issue (`git diff --staged`), gathering findings "
    "for security, test adequacy, plan alignment, code quality, and "
    "architecture."
)

RESPONSE_CONTRACT_INSTRUCTION = (
    "Respond with ONLY a single JSON object and nothing else -- no prose, "
    "no markdown code fences, no explanation before or after it. Produce "
    "exactly the structured artifact the `review-diff` skill documents: an "
    "object with a \"schemaVersion\" string, a \"runId\" string, a "
    "\"dimensions\" list, and a \"findings\" list. \"dimensions\" must "
    "contain exactly one entry for each of \"security\", \"testAdequacy\", "
    "\"planAlignment\", \"codeQuality\", and \"architecture\", each with a "
    "\"dimension\" name, an integer 0-100 \"grade\", and a non-empty "
    "\"evidence\" list of strings. \"findings\" is a list of finding "
    "objects, one per finding your review surfaced (an empty list if "
    "none); each must have a unique string \"id\", a \"dimension\" naming "
    "which of the five dimensions it belongs to, a \"severity\" that is "
    "exactly one of \"info\", \"low\", \"medium\", \"high\", \"critical\", "
    "or \"blocker\", a \"status\" that is exactly one of \"open\", "
    "\"addressed\", \"waived\", or \"invalid\", a non-empty \"summary\" "
    "string, and an optional \"location\" object with a \"path\" string "
    "and optional \"line\"/\"column\" integers. Do not include a pass/fail "
    "verdict yourself -- the CLI calculates it from your findings, not "
    "from your grades."
)


def _build_prompt(context: ReviewContext) -> str:
    return (
        f"{context.instructions}\n\n"
        f"PRD ({context.prd_path}):\n{context.prd}\n\n"
        f"Issue ({context.issue_path}):\n{context.issue}\n\n"
        f"Progress so far:\n{context.progress}\n\n"
        f"Review thread ({context.review_path}):\n{context.review}\n"
    )


def default_review_agent(context: ReviewContext, binary: str = "copilot") -> str:
    """Production review agent: invokes the Copilot CLI as a subprocess.

    Mirrors `task_loop.development.default_development_agent` and
    `task_loop.testing.default_testing_agent`'s replaceable process seam.
    Instructs the agent to use the `review-diff` skill and to respond with
    only the structured JSON schema the skill itself documents (see
    `RESPONSE_CONTRACT_INSTRUCTION`) -- the agent's own prose, and its
    subjective per-dimension grades, never decide pass or fail; only
    `score_review`'s arithmetic on validated open findings does. Raises
    `ReviewAgentError` for process-level failures (the binary is missing,
    the process cannot start, or it exits non-zero) -- distinct from
    `MalformedReviewDataError`, which `run_review` raises when the process
    runs successfully but its output does not parse as the required JSON
    schema.
    """
    prompt = _build_prompt(context)
    try:
        completed = subprocess.run(
            [binary, "--yolo", "-p", prompt],
            capture_output=True,
            text=True,
        )
    except OSError as exc:
        raise ReviewAgentError(
            f"failed to start review agent process: {exc}"
        ) from exc

    if completed.returncode != 0:
        raise ReviewAgentError(
            f"review agent process exited with status {completed.returncode}: "
            f"{completed.stderr.strip() or completed.stdout.strip()}"
        )

    return completed.stdout


def _parse_response(response: Any) -> Any:
    if not isinstance(response, str):
        raise MalformedReviewDataError(
            f"review response must be a single string, got {type(response).__name__}"
        )
    try:
        return json.loads(response)
    except json.JSONDecodeError as exc:
        raise MalformedReviewDataError(
            f"review response is not valid JSON: {exc}"
        ) from exc


def run_review(
    issue_path: PathLike,
    prd_path: PathLike,
    agent: Optional[ReviewAgent] = None,
    use_color: bool = False,
) -> ReviewResult:
    """Run the selected issue's automated review through a review agent."""
    issue_path = Path(issue_path)
    prd_path = Path(prd_path)
    review_path = Path("review") / f"{issue_path.stem}.md"
    progress_path = prd_path.parent / "progress.txt"

    context = ReviewContext(
        prd_path=prd_path,
        prd=prd_path.read_text(),
        issue_path=issue_path,
        issue=issue_path.read_text(),
        progress=progress_path.read_text() if progress_path.is_file() else "",
        review_path=review_path,
        review=review_path.read_text() if review_path.is_file() else "",
        instructions=(
            f"{REVIEW_DIFF_SKILL_INSTRUCTION}\n\n"
            f"{build_progress_update_instruction(progress_path, 'reviewer')}\n\n"
            f"{RESPONSE_CONTRACT_INSTRUCTION}"
        ),
    )
    response = (agent or default_review_agent)(context)
    data = _parse_response(response)
    score, findings = _validate_and_score(data)
    rendered = render_review(score, use_color=use_color)

    thread_id = None
    if not score.passed:
        message = _build_failure_message(score, findings)
        thread_id = add_message(review_path, message, "reviewer").thread_id

    return ReviewResult(
        issue_path=issue_path,
        score=score,
        rendered=rendered,
        passed=score.passed,
        thread_id=thread_id,
    )


def _score_for_load(load: float) -> float:
    return 100.0 * math.exp(-_LAMBDA * load)


def _require_non_empty_string(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value:
        raise MalformedReviewDataError(
            f"{field_name} must be a non-empty string, got {value!r}"
        )
    return value


def _validate_top_level_shape(data: Any) -> None:
    if not isinstance(data, Mapping):
        raise MalformedReviewDataError(
            f"review data must be an object, got {type(data).__name__}"
        )

    _require_non_empty_string(data.get("schemaVersion"), "schemaVersion")
    _require_non_empty_string(data.get("runId"), "runId")

    if not isinstance(data.get("dimensions"), list):
        raise MalformedReviewDataError(
            "dimensions must be a list, got "
            f"{type(data.get('dimensions')).__name__}"
        )

    if not isinstance(data.get("findings"), list):
        raise MalformedReviewDataError(
            f"findings must be a list, got {type(data.get('findings')).__name__}"
        )


def _validate_dimension_entry(index: int, entry: Any) -> None:
    if not isinstance(entry, Mapping):
        raise MalformedReviewDataError(
            f"dimensions[{index}] must be an object, got {type(entry).__name__}"
        )

    name = entry.get("dimension")
    if not isinstance(name, str) or not name:
        raise MalformedReviewDataError(
            f"dimensions[{index}].dimension must be a non-empty string, "
            f"got {name!r}"
        )
    if name not in REQUIRED_DIMENSIONS:
        raise MalformedReviewDataError(
            f"dimensions[{index}].dimension is unsupported: {name!r} "
            f"(only {list(REQUIRED_DIMENSIONS)} are supported)"
        )

    grade = entry.get("grade")
    if isinstance(grade, bool) or not isinstance(grade, int):
        raise MalformedReviewDataError(
            f"dimensions[{index}] ({name}).grade must be an integer, "
            f"got {grade!r}"
        )
    if grade < _MIN_GRADE or grade > _MAX_GRADE:
        raise MalformedReviewDataError(
            f"dimensions[{index}] ({name}).grade must be between "
            f"{_MIN_GRADE} and {_MAX_GRADE}, got {grade!r}"
        )

    evidence = entry.get("evidence")
    if not isinstance(evidence, list) or not evidence:
        raise MalformedReviewDataError(
            f"dimensions[{index}] ({name}).evidence must be a non-empty "
            f"list, got {evidence!r}"
        )
    for evidence_index, item in enumerate(evidence):
        if not isinstance(item, str) or not item:
            raise MalformedReviewDataError(
                f"dimensions[{index}] ({name}).evidence[{evidence_index}] "
                f"must be a non-empty string, got {item!r}"
            )


def _validate_dimensions_list(dimensions: List[Any]) -> Dict[str, Mapping]:
    by_name: Dict[str, Mapping] = {}
    for index, entry in enumerate(dimensions):
        _validate_dimension_entry(index, entry)
        name = entry["dimension"]
        if name in by_name:
            raise MalformedReviewDataError(
                f"dimensions contains a duplicate entry for {name!r}"
            )
        by_name[name] = entry

    missing = [name for name in REQUIRED_DIMENSIONS if name not in by_name]
    if missing:
        raise MalformedReviewDataError(
            f"dimensions is missing required entries: {missing}"
        )

    return by_name


def _validate_location(dimension: str, index: int, location: Any) -> None:
    if location is None:
        return
    if not isinstance(location, Mapping):
        raise MalformedReviewDataError(
            f"findings[{index}] ({dimension}).location must be an object, "
            f"got {type(location).__name__}"
        )

    _ = _require_non_empty_string(
        location.get("path"),
        f"findings[{index}] ({dimension}).location.path",
    )

    for field_name in ("line", "column"):
        value = location.get(field_name)
        if value is None:
            continue
        if isinstance(value, bool) or not isinstance(value, int) or value < 1:
            raise MalformedReviewDataError(
                f"findings[{index}] ({dimension}).location.{field_name} "
                f"must be a positive integer, got {value!r}"
            )


def _validate_finding(index: int, finding: Any, seen_ids: Dict[str, int]) -> Mapping:
    if not isinstance(finding, Mapping):
        raise MalformedReviewDataError(
            f"findings[{index}] must be a finding object, "
            f"got {type(finding).__name__}"
        )

    finding_id = finding.get("id")
    if not isinstance(finding_id, str) or not finding_id:
        raise MalformedReviewDataError(
            f"findings[{index}].id must be a non-empty string, "
            f"got {finding_id!r}"
        )
    if finding_id in seen_ids:
        raise MalformedReviewDataError(
            f"findings[{index}].id {finding_id!r} duplicates "
            f"findings[{seen_ids[finding_id]}].id"
        )
    seen_ids[finding_id] = index

    dimension = finding.get("dimension")
    if not isinstance(dimension, str) or not dimension:
        raise MalformedReviewDataError(
            f"findings[{index}].dimension must be a non-empty string, "
            f"got {dimension!r}"
        )
    if dimension not in REQUIRED_DIMENSIONS:
        raise MalformedReviewDataError(
            f"findings[{index}].dimension is unsupported: {dimension!r} "
            f"(only {list(REQUIRED_DIMENSIONS)} are supported)"
        )

    severity = finding.get("severity")
    if not isinstance(severity, str) or not severity:
        raise MalformedReviewDataError(
            f"findings[{index}] ({dimension}).severity must be a "
            f"non-empty string, got {severity!r}"
        )
    if severity not in SUPPORTED_SEVERITIES:
        raise MalformedReviewDataError(
            f"findings[{index}] ({dimension}).severity is unsupported: "
            f"{severity!r} (only {SUPPORTED_SEVERITIES} are supported)"
        )

    status = finding.get("status")
    if status not in _ALLOWED_STATUSES:
        raise MalformedReviewDataError(
            f"findings[{index}] ({dimension}).status is invalid: "
            f"{status!r} (only {_ALLOWED_STATUSES} are supported)"
        )

    summary = finding.get("summary")
    if not isinstance(summary, str) or not summary:
        raise MalformedReviewDataError(
            f"findings[{index}] ({dimension}).summary must be a "
            f"non-empty string, got {summary!r}"
        )

    _validate_location(dimension, index, finding.get("location"))

    return finding


def _validate_findings_list(findings: List[Any]) -> List[Mapping]:
    validated: List[Mapping] = []
    seen_ids: Dict[str, int] = {}
    for index, finding in enumerate(findings):
        validated.append(_validate_finding(index, finding, seen_ids))
    return validated


def _score_dimension(name: str, grade: int, findings: List[Mapping]) -> DimensionScore:
    counts = {severity: 0 for severity in _SEVERITY_WEIGHTS}
    for finding in findings:
        if finding["dimension"] != name:
            continue
        if finding["status"] != _OPEN_STATUS:
            continue
        severity = finding["severity"]
        if severity not in _SEVERITY_WEIGHTS:
            continue
        counts[severity] += 1

    load = sum(counts[severity] * weight for severity, weight in _SEVERITY_WEIGHTS.items())
    score = _score_for_load(load)
    return DimensionScore(
        name=name,
        grade=grade,
        counts=counts,
        load=load,
        score=score,
        passed=score >= PASS_THRESHOLD,
    )


_BAR_WIDTH = 20
_GREEN = "\x1b[32m"
_RED = "\x1b[31m"
_RESET = "\x1b[0m"


def _progress_bar(score: float, width: int = _BAR_WIDTH) -> str:
    clamped = max(0.0, min(100.0, score))
    filled = round(clamped / 100.0 * width)
    return "[" + "#" * filled + "-" * (width - filled) + "]"


def _tag(passed: bool, use_color: bool) -> str:
    label = "passed" if passed else "failed"
    if not use_color:
        return label.upper()
    color = _GREEN if passed else _RED
    return f"{color}{label}{_RESET}"


def _render_dimension(dimension: DimensionScore, use_color: bool) -> str:
    bar = _progress_bar(dimension.score)
    tag = _tag(dimension.passed, use_color)
    return f"{dimension.name}: {dimension.score:.1f} {bar} {tag}"


def render_review(score: ReviewScore, use_color: bool = False) -> str:
    """Render every dimension's score, progress bar, and pass/fail tag.

    Colors the `passed`/`failed` tag with ANSI green/red when `use_color`
    is true. When false, the tag is the plain, unambiguous uppercase word
    `PASSED`/`FAILED` -- readable with no color support at all. Rendering
    takes no runtime environment (TTY, `NO_COLOR`, etc.) into account
    itself, so it stays deterministic for tests; callers decide
    `use_color`.
    """
    lines = [_render_dimension(dimension, use_color) for dimension in score.dimensions]
    lines.append(f"Overall: {_tag(score.passed, use_color)}")
    return "\n".join(lines)


def _validate_and_score(data: Any) -> Tuple[ReviewScore, List[Mapping]]:
    """Validate the structured review payload and score every dimension.

    Shared by `score_review` (dimensions only) and `run_review` (which
    also needs the validated `findings` to compose a failing review's
    actionable summary message).
    """
    _validate_top_level_shape(data)

    dimensions_by_name = _validate_dimensions_list(data["dimensions"])
    findings = _validate_findings_list(data["findings"])

    dimensions: List[DimensionScore] = []
    for name in REQUIRED_DIMENSIONS:
        grade = dimensions_by_name[name]["grade"]
        dimensions.append(_score_dimension(name, grade, findings))

    score = ReviewScore(
        dimensions=tuple(dimensions),
        passed=all(dimension.passed for dimension in dimensions),
    )
    return score, findings


def score_review(data: Any) -> ReviewScore:
    """Validate and score the repository's real `review-diff` skill
    artifact (see the module docstring's "Structured review data schema"
    section).

    Grades are validated for shape only; only `findings` with
    `status: "open"` feed the PRD's exponential formula that determines
    `DimensionScore.score`/`.passed` and the overall `ReviewScore.passed`.
    """
    score, _ = _validate_and_score(data)
    return score


def _format_open_finding(finding: Mapping) -> str:
    location = finding.get("location")
    location_suffix = ""
    if location:
        location_suffix = f" ({location['path']}"
        if location.get("line") is not None:
            location_suffix += f":{location['line']}"
        location_suffix += ")"
    return f"[{finding['id']}] {finding['severity']}: {finding['summary']}{location_suffix}"


def _build_failure_message(score: ReviewScore, findings: Sequence[Mapping]) -> str:
    """Compose the actionable reviewer message appended for a failing
    review: every failing dimension's score plus its open findings,
    straight from the validated structured artifact -- never scraped from
    rendered terminal text. Still actionable when a failing dimension has
    only a single sparse open finding, since the exponential formula
    guarantees at least one contributed the load that failed it.
    """
    open_findings_by_dimension: Dict[str, List[Mapping]] = {
        name: [] for name in REQUIRED_DIMENSIONS
    }
    for finding in findings:
        if finding["status"] == _OPEN_STATUS:
            open_findings_by_dimension[finding["dimension"]].append(finding)

    lines = ["Automated review failed. Failing dimensions:"]
    for dimension in score.dimensions:
        if dimension.passed:
            continue
        lines.append(
            f"- {dimension.name}: {dimension.score:.1f} "
            f"(below the {PASS_THRESHOLD:.0f} threshold)"
        )
        for finding in open_findings_by_dimension[dimension.name]:
            lines.append(f"  - {_format_open_finding(finding)}")
    return "\n".join(lines)
