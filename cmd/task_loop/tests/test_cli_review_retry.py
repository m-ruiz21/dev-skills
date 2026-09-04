"""CLI-level tracer tests proving a failing automated review, per issue 09,
appends an actionable reviewer message through `task-loop add-message` (built
from the validated structured review artifact -- failing dimensions, their
scores, and their open findings -- rather than scraped rendered terminal
text) and retries the same issue's development on the next iteration,
consuming the shared iteration budget exactly like partial, needs-clarity,
and test-failure retries.
"""
import json
import unittest

from task_loop.review import REQUIRED_DIMENSIONS

from .support import run_cli, temp_repo, write_issue, write_prd


def _artifact(findings=None):
    return {
        "schemaVersion": "1.0",
        "runId": "test-run",
        "dimensions": [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ],
        "findings": findings if findings is not None else [],
    }


def _failing_review_json(finding_overrides=None):
    finding = {
        "id": "SEC-001",
        "dimension": "security",
        "severity": "critical",
        "status": "open",
        "summary": "A critical security finding.",
        "location": {"path": "src/example.py", "line": 42},
    }
    finding.update(finding_overrides or {})
    return json.dumps(_artifact(findings=[finding]))


def _sparse_high_finding_json():
    """A single `high` open finding is enough load (L=10) to fail its
    dimension's score just below the 90 threshold -- the "sparse findings"
    case: still exactly one finding, still must produce an actionable
    summary.
    """
    finding = {
        "id": "ARCH-001",
        "dimension": "architecture",
        "severity": "high",
        "status": "open",
        "summary": "A module now reaches across three layers of abstraction.",
    }
    return json.dumps(_artifact(findings=[finding]))


def _passing_review_json():
    return json.dumps(_artifact())


class ActionableReviewerMessageTests(unittest.TestCase):
    def test_a_failing_review_appends_an_actionable_reviewer_message_with_dimension_and_finding(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            review_calls = []

            def scripted_review_agent(context):
                review_calls.append(context)
                if len(review_calls) == 1:
                    return _failing_review_json()
                return _passing_review_json()

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=scripted_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn("[reviewer]", content)
            self.assertIn("security", content)
            self.assertIn("SEC-001", content)
            self.assertIn("A critical security finding.", content)

    def test_a_sparse_single_finding_still_produces_an_actionable_summary(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            review_calls = []

            def scripted_review_agent(context):
                review_calls.append(context)
                if len(review_calls) == 1:
                    return _sparse_high_finding_json()
                return _passing_review_json()

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=scripted_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn("architecture", content)
            self.assertIn("ARCH-001", content)
            self.assertIn(
                "A module now reaches across three layers of abstraction.",
                content,
            )

    def test_the_retried_development_call_receives_the_reviewer_finding_in_context(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return "[completed] Implemented the thing."

            review_calls = []

            def scripted_review_agent(context):
                review_calls.append(context)
                if len(review_calls) == 1:
                    return _failing_review_json()
                return _passing_review_json()

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=scripted_development_agent,
                testing_agent=lambda context: "[success]",
                review_agent=scripted_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(development_calls), 2)
            self.assertIn(
                "A critical security finding.", development_calls[1].review
            )


class ReviewFailureIterationBudgetTests(unittest.TestCase):
    def test_review_failure_retries_are_bounded_by_the_iteration_budget_exactly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return f"[completed] Attempt {len(development_calls)}."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "3"],
                development_agent=scripted_development_agent,
                testing_agent=lambda context: "[success]",
                review_agent=lambda context: _failing_review_json(),
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(development_calls), 3)
            self.assertIn("iteration", stderr.lower())
            self.assertIn(str(issue_path), stderr)
            self.assertIn("security", stderr.lower())

    def test_a_completed_development_plus_its_failed_review_consumes_one_iteration(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return "[completed] Attempt."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=scripted_development_agent,
                testing_agent=lambda context: "[success]",
                review_agent=lambda context: _failing_review_json(),
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(development_calls), 1)


class ReviewAndProgressFilesSurviveTerminationTests(unittest.TestCase):
    def test_the_review_file_remains_available_after_an_exhausted_review_failure_run(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=lambda context: "[completed] Attempt.",
                testing_agent=lambda context: "[success]",
                review_agent=lambda context: _failing_review_json(),
            )

            self.assertNotEqual(exit_code, 0)
            review_path = repo / "review" / "01-first.md"
            self.assertTrue(review_path.is_file())
            self.assertIn("SEC-001", review_path.read_text())


if __name__ == "__main__":
    unittest.main()
