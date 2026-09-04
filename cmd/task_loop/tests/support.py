"""Shared test helpers for task-loop CLI tests.

Temp repos are created under this package's own ``tmp/`` directory (not the
system temp directory) so test artifacts stay inside the repository working
tree and are easy to clean up deterministically.
"""
import contextlib
import io
import json
import os
import tempfile
from pathlib import Path
from typing import Iterator, List, Optional, Tuple

TESTS_DIR = Path(__file__).resolve().parent
SCRATCH_ROOT = TESTS_DIR / "tmp"


def passing_review_agent(context) -> str:
    """A scripted review agent returning the real `review-diff` skill
    artifact shape with no findings, used by CLI tests that only care
    about reaching testing success and stopping cleanly -- not about
    review scoring itself, which `test_review*.py` cover directly.
    """
    from task_loop.review import REQUIRED_DIMENSIONS

    return json.dumps(
        {
            "schemaVersion": "1.0",
            "runId": "test-run",
            "dimensions": [
                {"dimension": name, "grade": 90, "evidence": ["ok"]}
                for name in REQUIRED_DIMENSIONS
            ],
            "findings": [],
        }
    )


@contextlib.contextmanager
def temp_repo() -> Iterator[Path]:
    """Create an isolated repo-like directory and chdir into it for the test."""
    SCRATCH_ROOT.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=SCRATCH_ROOT) as repo_dir:
        previous_cwd = os.getcwd()
        os.chdir(repo_dir)
        try:
            yield Path(repo_dir)
        finally:
            os.chdir(previous_cwd)


def write_prd(repo: Path, feature: str, contents: str = "# PRD\n") -> Path:
    prd_dir = repo / ".scratch" / feature
    prd_dir.mkdir(parents=True, exist_ok=True)
    prd_path = prd_dir / "PRD.md"
    prd_path.write_text(contents)
    return prd_path


def write_issue(
    repo: Path,
    feature: str,
    filename: str,
    status: str = "ready-for-agent",
    blocked_by: Optional[List[str]] = None,
    closed: bool = False,
    body: str = "## What to build\n\nDo the thing.\n",
) -> Path:
    """Write an issue file with YAML-style frontmatter under the feature's issues dir."""
    issues_dir = repo / ".scratch" / feature / "issues"
    if closed:
        issues_dir = issues_dir / "closed"
    issues_dir.mkdir(parents=True, exist_ok=True)

    frontmatter_lines = ["---", f"title: {filename}", f"status: {status}"]
    if blocked_by:
        frontmatter_lines.append("blocked-by:")
        frontmatter_lines.extend(f"  - {dep}" for dep in blocked_by)
    else:
        frontmatter_lines.append("blocked-by: []")
    frontmatter_lines.append("---")

    issue_path = issues_dir / filename
    issue_path.write_text("\n".join(frontmatter_lines) + "\n\n" + body)
    return issue_path


def write_progress(repo: Path, feature: str, contents: str) -> Path:
    progress_path = repo / ".scratch" / feature / "progress.txt"
    progress_path.parent.mkdir(parents=True, exist_ok=True)
    progress_path.write_text(contents)
    return progress_path


def run_cli(
    argv: Optional[List[str]],
    triage_agent=None,
    development_agent=None,
    clarity_agent=None,
    testing_agent=None,
    review_agent=None,
) -> Tuple[int, str, str]:
    """Invoke the task-loop CLI's public entry point and capture its output.

    ``triage_agent``, ``development_agent``, ``clarity_agent``,
    ``testing_agent``, and ``review_agent`` are optional scripted adapters
    injected through the CLI's replaceable agent seams, used by tests to
    exercise response validation without a real agent process or real
    terminal input.
    """
    from task_loop.cli import main

    stdout, stderr = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        exit_code = main(
            argv,
            triage_agent=triage_agent,
            development_agent=development_agent,
            clarity_agent=clarity_agent,
            testing_agent=testing_agent,
            review_agent=review_agent,
        )
    return exit_code, stdout.getvalue(), stderr.getvalue()
