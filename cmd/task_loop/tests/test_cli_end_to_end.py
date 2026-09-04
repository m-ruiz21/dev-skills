"""End-to-end scripted tests for the task-loop CLI (issue 09), exercising
the full bounded loop: PRD selection, triage, TDD development, a test
failure and its investigated retry, a failing automated review and its
retry, an eventual passing review that renders every dimension's score and
stops for external human review, and -- separately -- a run that exhausts
its iteration cap on repeated failure.
"""
import json
import unittest

from task_loop.review import REQUIRED_DIMENSIONS

from .support import run_cli, temp_repo, write_issue, write_prd


def _review_json(passed: bool):
    findings = []
    if not passed:
        findings.append(
            {
                "id": "SEC-001",
                "dimension": "security",
                "severity": "critical",
                "status": "open",
                "summary": "A critical security finding that must be resolved.",
            }
        )
    return json.dumps(
        {
            "schemaVersion": "1.0",
            "runId": "test-run",
            "dimensions": [
                {"dimension": name, "grade": 90, "evidence": ["ok"]}
                for name in REQUIRED_DIMENSIONS
            ],
            "findings": findings,
        }
    )


class EndToEndFailedTestThenFailedReviewThenSuccessTests(unittest.TestCase):
    def test_prd_selection_triage_tdd_a_test_failure_a_review_failure_and_successful_retries_stop_for_human_review(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            triage_calls = []

            def counting_triage_agent(context):
                triage_calls.append(context)
                self.assertEqual(len(context.candidates), 1)
                return str(context.candidates[0].path)

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return f"[completed] Attempt {len(development_calls)}."

            testing_calls = []

            def scripted_testing_agent(context):
                testing_calls.append(context)
                if len(testing_calls) == 1:
                    from task_loop.messages import add_message

                    add_message(
                        context.review_path,
                        "test_checkout fails: the retry policy raises on timeout.",
                        "reviewer",
                    )
                    return "[failure]"
                return "[success]"

            review_calls = []

            def scripted_review_agent(context):
                review_calls.append(context)
                if len(review_calls) == 1:
                    return _review_json(passed=False)
                return _review_json(passed=True)

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "10"],
                triage_agent=counting_triage_agent,
                development_agent=scripted_development_agent,
                testing_agent=scripted_testing_agent,
                review_agent=scripted_review_agent,
            )

            # Triage ran exactly once for the whole chain of retries.
            self.assertEqual(len(triage_calls), 1)

            # Development ran three times: the attempt that failed testing,
            # the attempt that then failed review, and the attempt that
            # finally passed both.
            self.assertEqual(len(development_calls), 3)
            self.assertEqual(len(testing_calls), 3)
            self.assertEqual(len(review_calls), 2)

            self.assertEqual(exit_code, 0, stderr)
            self.assertIn(f"Selected PRD: {prd_path}", stdout)
            self.assertIn(f"Selected issue: {issue_path}", stdout)
            self.assertIn("Tests: failure", stdout)
            self.assertIn("Tests: success", stdout)
            self.assertIn("Overall: FAILED", stdout)
            self.assertIn("Overall: PASSED", stdout)
            self.assertIn("security", stdout)
            self.assertIn("human review", stdout.lower())

            # The retried development call after the test failure saw the
            # reviewer's investigation finding threaded into its context.
            self.assertIn(
                "test_checkout fails: the retry policy raises on timeout.",
                development_calls[1].review,
            )
            # The retried development call after the review failure saw the
            # actionable reviewer summary threaded into its context.
            self.assertIn(
                "A critical security finding that must be resolved.",
                development_calls[2].review,
            )

            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn(
                "test_checkout fails: the retry policy raises on timeout.", content
            )
            self.assertIn("A critical security finding that must be resolved.", content)
            self.assertTrue(review_path.is_file())

            progress_path = repo / ".scratch" / "task-loop" / "progress.txt"
            # progress.txt is not required to exist for a run to succeed,
            # but the loop must never delete it when present.
            self.assertFalse(progress_path.is_file())


class EndToEndRepeatedFailureStopsAtTheIterationCapTests(unittest.TestCase):
    def test_repeated_review_failures_stop_exactly_at_the_configured_max_iterations(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            triage_calls = []

            def counting_triage_agent(context):
                triage_calls.append(context)
                return str(context.candidates[0].path)

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return f"[completed] Attempt {len(development_calls)}."

            testing_calls = []

            def always_success_testing_agent(context):
                testing_calls.append(context)
                return "[success]"

            review_calls = []

            def always_failing_review_agent(context):
                review_calls.append(context)
                return _review_json(passed=False)

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "4"],
                triage_agent=counting_triage_agent,
                development_agent=scripted_development_agent,
                testing_agent=always_success_testing_agent,
                review_agent=always_failing_review_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(triage_calls), 1)
            self.assertEqual(len(development_calls), 4)
            self.assertEqual(len(testing_calls), 4)
            self.assertEqual(len(review_calls), 4)

            self.assertIn(str(issue_path), stderr)
            self.assertIn("4", stderr)
            self.assertIn("security", stderr.lower())

            # Review and progress context recorded across every failing
            # pass are preserved, not discarded, when the run stops at the
            # cap.
            review_path = repo / "review" / "01-first.md"
            self.assertTrue(review_path.is_file())
            self.assertIn("SEC-001", review_path.read_text())


if __name__ == "__main__":
    unittest.main()
