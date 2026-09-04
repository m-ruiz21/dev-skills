"""CLI-level tracer tests proving the bounded task loop's shared
termination contract (issue 09): every retry cause -- partial development,
needs-clarity, a failed test run, and a failed review -- shares the exact
same iteration budget and, on exhaustion, produces one clear non-success
terminal message naming the selected issue and the latest failure reason.
A passing review stops immediately for external human review without
closing the issue, collecting approval, or selecting another issue.
"""
import json
import unittest

from task_loop.review import REQUIRED_DIMENSIONS

from .support import run_cli, temp_repo, write_issue, write_prd


def _failing_review_json():
    return json.dumps(
        {
            "schemaVersion": "1.0",
            "runId": "test-run",
            "dimensions": [
                {"dimension": name, "grade": 90, "evidence": ["ok"]}
                for name in REQUIRED_DIMENSIONS
            ],
            "findings": [
                {
                    "id": "SEC-001",
                    "dimension": "security",
                    "severity": "critical",
                    "status": "open",
                    "summary": "A critical security finding.",
                }
            ],
        }
    )


class ExhaustionNamesTheIssueAndLatestReasonTests(unittest.TestCase):
    def test_exhaustion_after_repeated_partial_work_names_the_issue_and_the_latest_partial_message(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            def always_partial(context):
                return "[partial] Still refactoring the seam."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "2"],
                development_agent=always_partial,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn(str(issue_path), stderr)
            self.assertIn("Still refactoring the seam.", stderr)

    def test_exhaustion_while_awaiting_clarity_names_the_issue_and_the_latest_question(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            def always_needs_clarity(context):
                return "[needs-clarity] Which retry policy is approved?"

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=always_needs_clarity,
                clarity_agent=lambda prompt: "An answer.",
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn(str(issue_path), stderr)
            self.assertIn("Which retry policy is approved?", stderr)

    def test_exhaustion_after_repeated_test_failures_names_the_issue_and_a_test_failure_reason(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            def failing_testing_agent(context):
                from task_loop.messages import add_message

                add_message(context.review_path, "Investigated: broken fixture.", "reviewer")
                return "[failure]"

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=lambda context: "[completed] Attempt.",
                testing_agent=failing_testing_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn(str(issue_path), stderr)
            self.assertIn("test", stderr.lower())

    def test_exhaustion_after_repeated_review_failures_names_the_issue_and_the_failing_dimensions(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=lambda context: "[completed] Attempt.",
                testing_agent=lambda context: "[success]",
                review_agent=lambda context: _failing_review_json(),
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn(str(issue_path), stderr)
            self.assertIn("security", stderr.lower())


class SuccessDoesNotCloseIssueOrSelectAnotherTests(unittest.TestCase):
    def test_a_successful_run_leaves_the_issue_file_untouched_and_never_invokes_triage_twice(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            triage_calls = []

            def counting_triage_agent(context):
                triage_calls.append(context)
                return str(context.candidates[0].path)

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

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                triage_agent=counting_triage_agent,
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(triage_calls), 1)
            # The issue's own frontmatter status is untouched -- the CLI
            # neither closes it nor moves it, and it does not remain in a
            # "closed/" location either.
            self.assertIn("status: ready-for-agent", issue_path.read_text())
            self.assertFalse((repo / ".scratch" / "task-loop" / "issues" / "closed").exists())


if __name__ == "__main__":
    unittest.main()
