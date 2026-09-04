import unittest
from pathlib import Path

from task_loop.messages import add_message

from task_loop.testing import (
    RESPONSE_CONTRACT_INSTRUCTION,
    TEST_INVESTIGATION_INSTRUCTION,
    MalformedTestingResponseError,
    MissingInvestigationFindingsError,
    TestingAgentError,
    TestingContext,
    default_testing_agent,
    run_testing,
)
from task_loop.messages import build_progress_update_instruction

from .support import temp_repo, write_issue, write_prd, write_progress


class SuccessOutcomeTests(unittest.TestCase):
    def test_a_success_response_advances_to_the_review_phase(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            result = run_testing(
                issue_path, prd_path, agent=lambda context: "[success]"
            )

            self.assertEqual(result.outcome, "success")
            self.assertEqual(result.next_phase, "review")


class InvestigatedFailureTests(unittest.TestCase):
    def test_a_failure_response_with_a_new_reviewer_finding_retries_development(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            def investigating_agent(context):
                add_message(
                    context.review_path,
                    "test_widget_render fails: missing fixture data.",
                    "reviewer",
                )
                return "[failure]"

            result = run_testing(issue_path, prd_path, agent=investigating_agent)

            self.assertEqual(result.outcome, "failure")
            self.assertEqual(result.next_phase, "development")
            content = review_path.read_text()
            self.assertIn("[reviewer]", content)
            self.assertIn("missing fixture data.", content)


class MissingFindingsTests(unittest.TestCase):
    def test_a_failure_response_with_no_new_reviewer_finding_fails_explicitly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MissingInvestigationFindingsError):
                run_testing(issue_path, prd_path, agent=lambda context: "[failure]")

    def test_a_prior_reviewer_finding_does_not_satisfy_a_later_failure_without_a_new_one(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()
            add_message(review_path, "An earlier, stale finding.", "reviewer")

            with self.assertRaises(MissingInvestigationFindingsError):
                run_testing(issue_path, prd_path, agent=lambda context: "[failure]")


class MalformedResponseTests(unittest.TestCase):
    def _assert_rejected(self, repo, prd_path, issue_path, response):
        with self.assertRaises(MalformedTestingResponseError):
            run_testing(issue_path, prd_path, agent=lambda context: response)

    def test_extra_prose_around_the_outcome_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            self._assert_rejected(
                repo, prd_path, issue_path, "[success] all tests passed"
            )

    def test_an_unknown_outcome_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            self._assert_rejected(repo, prd_path, issue_path, "[skipped]")

    def test_an_empty_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            self._assert_rejected(repo, prd_path, issue_path, "")

    def test_multiple_bracketed_markers_are_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            self._assert_rejected(
                repo, prd_path, issue_path, "[success][failure]"
            )

    def test_a_non_string_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            self._assert_rejected(repo, prd_path, issue_path, ["success"])


class AgentProcessFailureTests(unittest.TestCase):
    def test_an_agent_process_failure_is_not_reported_as_a_malformed_response(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            def crashing_agent(context):
                raise TestingAgentError("agent process exited with status 1")

            with self.assertRaises(TestingAgentError):
                run_testing(issue_path, prd_path, agent=crashing_agent)


class AgentContextTests(unittest.TestCase):
    def test_the_agent_receives_the_prd_issue_progress_and_review_thread_content(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop", contents="# PRD\n\nBuild the thing.\n")
            issue_path = write_issue(
                repo,
                "task-loop",
                "01-first.md",
                body="## What to build\n\nDo the thing.\n",
            )
            write_progress(repo, "task-loop", "2026-01-01 prior work happened\n")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.write_text(
                "-- Thread 1\n[developer] - 2026-01-01T00:00:00Z\n\nImplemented the thing.\n"
            )

            captured = {}

            def capturing_agent(context):
                captured["prd"] = context.prd
                captured["issue"] = context.issue
                captured["progress"] = context.progress
                captured["review"] = context.review
                return "[success]"

            run_testing(issue_path, prd_path, agent=capturing_agent)

            self.assertEqual(captured["prd"], prd_path.read_text())
            self.assertEqual(captured["issue"], issue_path.read_text())
            self.assertEqual(
                captured["progress"], "2026-01-01 prior work happened\n"
            )
            self.assertEqual(
                captured["review"],
                "-- Thread 1\n[developer] - 2026-01-01T00:00:00Z\n\nImplemented the thing.\n",
            )

    def test_the_agent_receives_instructions_to_investigate_and_record_findings(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            captured = {}

            def capturing_agent(context):
                captured["instructions"] = context.instructions
                return "[success]"

            run_testing(issue_path, prd_path, agent=capturing_agent)

            self.assertIn(TEST_INVESTIGATION_INSTRUCTION, captured["instructions"])
            self.assertIn(RESPONSE_CONTRACT_INSTRUCTION, captured["instructions"])
            self.assertIn(
                build_progress_update_instruction(
                    prd_path.parent / "progress.txt", "reviewer"
                ),
                captured["instructions"],
            )


class DefaultTestingAgentTests(unittest.TestCase):
    def test_the_default_agent_reports_a_process_start_failure_distinctly(self):
        with self.assertRaises(TestingAgentError):
            default_testing_agent(
                TestingContext(
                    prd_path=Path("PRD.md"),
                    prd="",
                    issue_path=Path("issue.md"),
                    issue="",
                    progress="",
                    review_path=Path("review.md"),
                    review="",
                    instructions="",
                ),
                binary="task-loop-nonexistent-binary",
            )


if __name__ == "__main__":
    unittest.main()
