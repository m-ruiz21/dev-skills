"""CLI-level tracer tests proving automated review runs through the public
`task-loop` entry point only after a `[success]` testing result: a passing
review renders every dimension's score and stops the run cleanly for human
review, a failing review renders the verdict and retries the same issue
for development on the next iteration (see `test_cli_review_retry.py` for
the full retry contract issue 09 owns), malformed review data fails
explicitly, and a testing failure never invokes the review agent at all.
"""
import unittest

from .support import run_cli, temp_repo, write_issue, write_prd


def _artifact(findings=None):
    from task_loop.review import REQUIRED_DIMENSIONS

    return {
        "schemaVersion": "1.0",
        "runId": "test-run",
        "dimensions": [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ],
        "findings": findings if findings is not None else [],
    }


def _passing_review_json():
    import json

    return json.dumps(_artifact())


def _failing_review_json():
    import json

    return json.dumps(
        _artifact(
            findings=[
                {
                    "id": "SEC-001",
                    "dimension": "security",
                    "severity": "critical",
                    "status": "open",
                    "summary": "A critical security finding.",
                }
            ]
        )
    )


class ReviewRunsOnlyAfterSuccessAndRendersPassTests(unittest.TestCase):
    def test_a_passing_review_renders_every_dimension_and_stops_cleanly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            review_calls = []

            def scripted_review_agent(context):
                review_calls.append(context)
                return _passing_review_json()

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=scripted_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(review_calls), 1)
            self.assertIn("security", stdout)
            self.assertIn("testAdequacy", stdout)
            self.assertIn("planAlignment", stdout)
            self.assertIn("codeQuality", stdout)
            self.assertIn("architecture", stdout)
            self.assertIn("PASSED", stdout)
            self.assertIn("Overall: PASSED", stdout)
            self.assertIn("human review", stdout.lower())


class TestingFailureNeverInvokesReviewTests(unittest.TestCase):
    def test_a_failing_test_run_never_invokes_the_review_agent(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            review_calls = []

            def scripted_review_agent(context):
                review_calls.append(context)
                return _passing_review_json()

            def failing_testing_agent(context):
                from task_loop.messages import add_message

                add_message(context.review_path, "Investigated: broken fixture.", "reviewer")
                return "[failure]"

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=lambda context: "[completed] Attempt.",
                testing_agent=failing_testing_agent,
                review_agent=scripted_review_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(review_calls), 0)
            self.assertNotIn("Overall:", stdout)


class MalformedReviewDataFailsExplicitlyTests(unittest.TestCase):
    def test_malformed_review_data_fails_explicitly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=lambda context: "not valid json",
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("review", stderr.lower())
            self.assertNotIn("Overall:", stdout)


class FailingReviewRetriesTheSameIssueTests(unittest.TestCase):
    def test_a_failing_review_renders_the_verdict_and_retries_development_for_the_same_issue(self):
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
            self.assertEqual(len(review_calls), 2)
            self.assertIn("Overall: FAILED", stdout)
            self.assertIn("Overall: PASSED", stdout)


if __name__ == "__main__":
    unittest.main()
