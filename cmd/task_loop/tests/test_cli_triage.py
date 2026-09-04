"""CLI-level tracer tests proving triage runs through the public `task-loop`
entry point, not only via the internal `task_loop.triage.select_issue` seam.
"""
import unittest

from .support import passing_review_agent, run_cli, temp_repo, write_issue, write_prd

_COMPLETED_DEVELOPMENT_AGENT = lambda context: "[completed] Implemented the thing."
_SUCCESS_TESTING_AGENT = lambda context: "[success]"


class CliRunsTriageTests(unittest.TestCase):
    def test_running_the_cli_selects_the_issue_and_creates_its_review_doc(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=_COMPLETED_DEVELOPMENT_AGENT,
                testing_agent=_SUCCESS_TESTING_AGENT,
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertIn(f"Selected issue: {issue_path}", stdout)
            review_path = repo / "review" / "01-first.md"
            self.assertTrue(review_path.is_file())

    def test_running_the_cli_preserves_prior_review_doc_content(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.write_text("Prior context.\n")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=_COMPLETED_DEVELOPMENT_AGENT,
                testing_agent=_SUCCESS_TESTING_AGENT,
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertTrue(review_path.read_text().startswith("Prior context.\n"))


class CliNoActionableIssuesTests(unittest.TestCase):
    def test_no_actionable_issues_fails_with_a_non_zero_exit_and_message(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-blocked.md", status="blocked")

            exit_code, stdout, stderr = run_cli([str(prd_path)])

            self.assertNotEqual(exit_code, 0)
            self.assertIn("no actionable issues", stderr)


class CliInvalidTriageResponseTests(unittest.TestCase):
    def test_an_invalid_triage_response_fails_with_a_non_zero_exit_and_message(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)], triage_agent=lambda context: ""
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("triage response", stderr)


if __name__ == "__main__":
    unittest.main()
