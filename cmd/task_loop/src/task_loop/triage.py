"""Triage seam for task-loop.

For a selected PRD, chooses exactly one issue from the feature's issue queue
and `progress.txt` context, and ensures the selected issue has a durable
`review/<issue-name>.md` document before development starts.

The actual choice is delegated to a small, replaceable "triage adapter"
callable (`TriageAgent`). This module owns the deep, testable policy --
discovering the issue queue, respecting `blocked-by` dependencies, and
prioritizing `status: review` issues -- so that a scripted adapter (tests) or
a future real agent only has to pick from an already-validated candidate set.
"""
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Optional, Sequence, Union

PathLike = Union[str, Path]

ACTIONABLE_STATUSES = ("ready-for-agent", "review")
INLINE_STATUS_PATTERN = re.compile(
    r"^[ \t]*(?:\*\*Status:\*\*|Status:)[ \t]*(.*?)[ \t]*$"
)


class NoActionableIssuesError(RuntimeError):
    """Raised when there are no actionable issues to triage for the PRD."""


class InvalidTriageResponseError(ValueError):
    """Raised when the triage response is not exactly one eligible issue reference."""


@dataclass(frozen=True)
class Issue:
    """A discovered issue file with the frontmatter fields triage depends on."""

    path: Path
    status: str
    blocked_by: Sequence[str]


@dataclass(frozen=True)
class TriageContext:
    """The PRD, prioritized candidate issues, and progress history for triage."""

    prd_path: Path
    candidates: Sequence[Issue]
    progress: str


TriageAgent = Callable[[TriageContext], str]


def _parse_frontmatter(text: str) -> dict:
    if not isinstance(text, str) or not text.startswith("---"):
        return {}
    end = text.find("\n---", 3)
    if end == -1:
        return {}
    lines = text[3:end].split("\n")
    result: dict = {}
    i = 0
    while i < len(lines):
        line = lines[i]
        if not line.strip() or ":" not in line:
            i += 1
            continue
        key, _, value = line.partition(":")
        key = key.strip()
        value = value.strip()
        if value and value != "[]":
            result[key] = value
            i += 1
            continue
        items = []
        j = i + 1
        while j < len(lines) and lines[j].strip().startswith("-"):
            items.append(lines[j].strip()[1:].strip())
            j += 1
        result[key] = items
        i = j
    return result


def _parse_inline_status(text: str) -> Optional[str]:
    """Read a legacy status line from the issue's metadata preamble."""
    if not isinstance(text, str):
        return None
    lines = text.splitlines()
    for line in lines:
        if line.startswith("## "):
            break
        match = INLINE_STATUS_PATTERN.fullmatch(line)
        if match:
            return match.group(1).strip()
    return None


def _parse_issue_metadata(text: str) -> dict:
    """Parse canonical YAML, filling absent fields from legacy inline metadata."""
    metadata = _parse_frontmatter(text)
    if "status" in metadata:
        return metadata
    inline_status = _parse_inline_status(text)
    if inline_status is None:
        return metadata
    return {**metadata, "status": inline_status}


def _dependency_references(metadata: dict) -> tuple:
    blocked_by = metadata.get("blocked-by", [])
    if isinstance(blocked_by, (list, tuple)):
        return tuple(blocked_by)
    if not blocked_by:
        return ()
    return (blocked_by,)


def _load_issue(path: Path) -> Issue:
    metadata = _parse_issue_metadata(path.read_text())
    return Issue(
        path=path,
        status=metadata.get("status", ""),
        blocked_by=_dependency_references(metadata),
    )


def _is_closed(path: Path) -> bool:
    if not path.is_file():
        return False
    try:
        return _load_issue(path).status == "closed"
    except OSError:
        return False


def _dependency_resolved(dep_ref: str, issues_dir: Path) -> bool:
    """A `blocked-by` reference resolves once the referenced issue is closed.

    Checks both the reference's original path and its `closed/` location,
    since closing an issue moves it there without rewriting dependents.
    """
    dep_path = Path(dep_ref)
    if _is_closed(dep_path):
        return True
    return _is_closed(issues_dir / "closed" / dep_path.name)


def _actionable_issues(issues_dir: Path) -> list:
    if not issues_dir.is_dir():
        return []
    issues = []
    for path in sorted(issues_dir.glob("*.md")):
        issue = _load_issue(path)
        if issue.status not in ACTIONABLE_STATUSES:
            continue
        if not all(
            _dependency_resolved(dep, issues_dir) for dep in issue.blocked_by
        ):
            continue
        issues.append(issue)
    return issues


def _eligible_issues(actionable: list) -> list:
    """Rank `status: review` issues ahead of otherwise actionable work."""
    review_issues = [issue for issue in actionable if issue.status == "review"]
    return review_issues if review_issues else actionable


def default_triage_agent(context: TriageContext) -> str:
    """Production stand-in triage adapter: picks the first eligible candidate.

    Replaceable -- a future real triage agent can be passed as `agent` to
    `select_issue` instead.
    """
    return str(context.candidates[0].path)


def _validate_response(response, candidates: Sequence[Issue]) -> Path:
    """Validate the response is exactly one non-empty, in-scope issue reference."""
    if not isinstance(response, str):
        raise InvalidTriageResponseError(
            f"triage response must be a single string, got {type(response).__name__}"
        )
    if len(response.splitlines()) > 1:
        raise InvalidTriageResponseError(
            "triage response must be exactly one issue reference, not multiple lines"
        )
    reference = response.strip()
    if not reference:
        raise InvalidTriageResponseError("triage response must not be empty")
    for candidate in candidates:
        if str(candidate.path) == reference:
            return candidate.path
    raise InvalidTriageResponseError(
        f"triage response is not an eligible issue reference: {reference!r}"
    )


def _ensure_review_doc(issue_path: Path) -> Path:
    """Create `review/<issue-name>.md` if absent; leave it untouched if present."""
    review_path = Path("review") / f"{issue_path.stem}.md"
    if not review_path.is_file():
        review_path.parent.mkdir(parents=True, exist_ok=True)
        review_path.touch()
    return review_path


def select_issue(
    prd_path: PathLike,
    agent: Optional[TriageAgent] = None,
) -> Path:
    """Choose exactly one issue for the PRD and ensure its review doc exists."""
    prd_path = Path(prd_path)
    issues_dir = prd_path.parent / "issues"
    progress_path = prd_path.parent / "progress.txt"
    progress = progress_path.read_text() if progress_path.is_file() else ""

    eligible = _eligible_issues(_actionable_issues(issues_dir))
    if not eligible:
        raise NoActionableIssuesError(
            f"no actionable issues found under {issues_dir}"
        )

    context = TriageContext(
        prd_path=prd_path, candidates=tuple(eligible), progress=progress
    )
    response = (agent or default_triage_agent)(context)
    issue_path = _validate_response(response, eligible)

    _ensure_review_doc(issue_path)

    return issue_path
