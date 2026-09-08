import json
import unittest
from pathlib import Path

from task_loop.discovery import discover_prds
from task_loop.orchestration import (
    DEFAULT_MAX_ITERATIONS,
    InvalidMaxIterationsError,
    InvalidPrdPathError,
    Run,
    run_issue_loop,
    start_run,
)
from task_loop.review import REQUIRED_DIMENSIONS

from .support import temp_repo, write_issue, write_prd


class OrchestrationInterfaceTests(unittest.TestCase):
    """Exercises the testable seam future issues build triage/TDD/review on."""

    def test_start_run_returns_a_run_exposing_prd_path_and_budget(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")

            run = start_run(prd_path, max_iterations=3)

            self.assertIsInstance(run, Run)
            self.assertEqual(run.prd_path, prd_path)
            self.assertEqual(run.max_iterations, 3)

    def test_start_run_defaults_to_ten_iterations(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")

            run = start_run(prd_path)

            self.assertEqual(run.max_iterations, DEFAULT_MAX_ITERATIONS)

    def test_start_run_raises_invalid_prd_path_error_for_a_missing_path(self):
        with temp_repo() as repo:
            missing_path = repo / ".scratch" / "task-loop" / "PRD.md"

            with self.assertRaises(InvalidPrdPathError):
                start_run(missing_path)

    def test_start_run_accepts_an_explicit_prd_filename_with_different_casing(self):
        with temp_repo() as repo:
            canonical_path = write_prd(repo, "task-loop")
            lowercase_path = canonical_path.with_name("prd.md")
            canonical_path.rename(lowercase_path)

            run = start_run(lowercase_path)

            self.assertEqual(run.prd_path, lowercase_path)

    def test_start_run_raises_invalid_max_iterations_error_for_zero(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")

            with self.assertRaises(InvalidMaxIterationsError):
                start_run(prd_path, max_iterations=0)

    def test_discover_prds_returns_paths_as_a_sorted_list(self):
        with temp_repo() as repo:
            write_prd(repo, "zeta-feature")
            write_prd(repo, "alpha-feature")

            prds = discover_prds(repo)

            self.assertEqual(
                prds,
                [
                    repo / ".scratch" / "alpha-feature" / "PRD.md",
                    repo / ".scratch" / "zeta-feature" / "PRD.md",
                ],
            )

    def test_discover_prds_returns_an_empty_list_when_none_exist(self):
        with temp_repo() as repo:
            self.assertEqual(discover_prds(repo), [])


class RunIssueLoopInterfaceTests(unittest.TestCase):
    """Exercises `run_issue_loop` directly as the CLI's orchestration seam
    for the bounded development/testing/review control flow, independent
    of `task_loop.cli`'s argument parsing and PRD/triage banner output.
    """

    def test_a_completed_pass_that_passes_testing_and_review_returns_zero(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = Path(
                write_issue(repo, "task-loop", "01-first.md")
            )
            run = start_run(prd_path, max_iterations=3)

            def passing_review_agent(context):
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

            exit_code = run_issue_loop(
                run,
                issue_path,
                development_agent=lambda context: "[completed] Done.",
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0)

    def test_repeated_partial_work_exhausts_the_budget_and_returns_one(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = Path(
                write_issue(repo, "task-loop", "01-first.md")
            )
            run = start_run(prd_path, max_iterations=2)

            development_calls = []

            def always_partial(context):
                development_calls.append(context)
                return "[partial] Still working."

            exit_code = run_issue_loop(
                run, issue_path, development_agent=always_partial
            )

            self.assertEqual(exit_code, 1)
            self.assertEqual(len(development_calls), 2)


if __name__ == "__main__":
    unittest.main()
