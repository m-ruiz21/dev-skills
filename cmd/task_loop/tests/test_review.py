"""Tests for `task_loop.review.run_review`: the replaceable review-agent
seam that builds context for one issue, delegates to a scripted or
production review adapter, strictly parses its response as the structured
review-diff JSON schema, and scores it. Agent prose never decides the
verdict -- only `score_review`'s arithmetic on the parsed JSON does.
"""
import unittest

from task_loop.messages import build_progress_update_instruction
from task_loop.review import (
    REQUIRED_DIMENSIONS,
    MalformedReviewDataError,
    ReviewAgentError,
    run_review,
)

from .support import temp_repo, write_issue, write_prd


def _empty_findings_json():
    import json

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


class RunReviewScoresTheAgentsStructuredResponseTests(unittest.TestCase):
    def test_a_well_formed_json_response_is_parsed_and_scored(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            captured_contexts = []

            def scripted_agent(context):
                captured_contexts.append(context)
                return _empty_findings_json()

            result = run_review(issue_path, prd_path, agent=scripted_agent)

            self.assertTrue(result.passed)
            self.assertEqual(len(captured_contexts), 1)
            self.assertIn(str(issue_path), captured_contexts[0].issue_path.as_posix())
            self.assertIn(
                build_progress_update_instruction(
                    prd_path.parent / "progress.txt", "reviewer"
                ),
                captured_contexts[0].instructions,
            )


class NonJsonResponseFailsExplicitlyTests(unittest.TestCase):
    def test_a_response_that_is_not_valid_json_raises_explicitly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            with self.assertRaises(MalformedReviewDataError) as ctx:
                run_review(
                    issue_path, prd_path, agent=lambda context: "not json at all"
                )
            self.assertIn("JSON", str(ctx.exception))


class ReviewAgentProcessFailureIsDistinctTests(unittest.TestCase):
    def test_a_review_agent_process_failure_is_reported_distinctly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            def crashing_agent(context):
                raise ReviewAgentError("agent process exited with status 1")

            with self.assertRaises(ReviewAgentError):
                run_review(issue_path, prd_path, agent=crashing_agent)


if __name__ == "__main__":
    unittest.main()
