import unittest
from pathlib import Path

from task_loop.development import (
    NO_DIRECT_REVIEW_EDITS_INSTRUCTION,
    TEST_FIRST_INSTRUCTION,
    DevelopmentAgentError,
    DevelopmentContext,
    MalformedDevelopmentResponseError,
    default_development_agent,
    run_development,
)
from task_loop.messages import build_progress_update_instruction

from .support import temp_repo, write_issue, write_prd, write_progress


class CompletedOutcomeTests(unittest.TestCase):
    def test_a_completed_response_is_appended_as_a_developer_message(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            run_development(
                issue_path,
                prd_path,
                agent=lambda context: "[completed] Implemented the thing.",
            )

            content = review_path.read_text()
            self.assertIn("[developer]", content)
            self.assertIn("Implemented the thing.", content)


class NeedsClarityOutcomeTests(unittest.TestCase):
    def test_a_needs_clarity_response_is_appended_as_a_developer_message_and_pauses(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            result = run_development(
                issue_path,
                prd_path,
                agent=lambda context: "[needs-clarity] Which retry library should I use?",
            )

            self.assertEqual(result.outcome, "needs-clarity")
            self.assertEqual(result.next_phase, "needs-clarity")
            self.assertEqual(result.message, "Which retry library should I use?")
            content = review_path.read_text()
            self.assertIn("[developer]", content)
            self.assertIn("Which retry library should I use?", content)


class MalformedResponseTests(unittest.TestCase):
    def test_a_completed_outcome_with_an_empty_message_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path, prd_path, agent=lambda context: "[completed]   "
                )

            self.assertEqual(review_path.read_text(), "")

    def test_an_unsupported_outcome_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path,
                    prd_path,
                    agent=lambda context: "[bogus] Some progress.",
                )

            self.assertEqual(review_path.read_text(), "")

    def test_leading_prose_before_the_outcome_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path,
                    prd_path,
                    agent=lambda context: "Done! [completed] Implemented the thing.",
                )

            self.assertEqual(review_path.read_text(), "")

    def test_multiple_bracketed_outcomes_are_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path,
                    prd_path,
                    agent=lambda context: "[completed][partial] Mixed outcome.",
                )

            self.assertEqual(review_path.read_text(), "")

    def test_a_non_string_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path,
                    prd_path,
                    agent=lambda context: ["completed", "Implemented the thing."],
                )

            self.assertEqual(review_path.read_text(), "")

    def test_missing_whitespace_after_the_bracket_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path,
                    prd_path,
                    agent=lambda context: "[completed]Implemented the thing.",
                )

            self.assertEqual(review_path.read_text(), "")


class AgentProcessFailureTests(unittest.TestCase):
    def test_an_agent_process_failure_is_not_reported_as_a_malformed_response(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            def crashing_agent(context):
                raise DevelopmentAgentError("agent process exited with status 1")

            with self.assertRaises(DevelopmentAgentError):
                run_development(issue_path, prd_path, agent=crashing_agent)

            self.assertEqual(review_path.read_text(), "")


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
                "-- Thread 1\n[user] - 2026-01-01T00:00:00Z\n\nPlease be careful.\n"
            )

            captured = {}

            def capturing_agent(context):
                captured["prd"] = context.prd
                captured["issue"] = context.issue
                captured["progress"] = context.progress
                captured["review"] = context.review
                return "[completed] Implemented the thing."

            run_development(issue_path, prd_path, agent=capturing_agent)

            self.assertEqual(captured["prd"], prd_path.read_text())
            self.assertEqual(captured["issue"], issue_path.read_text())
            self.assertEqual(
                captured["progress"], "2026-01-01 prior work happened\n"
            )
            self.assertEqual(
                captured["review"],
                "-- Thread 1\n[user] - 2026-01-01T00:00:00Z\n\nPlease be careful.\n",
            )


class AgentInstructionsTests(unittest.TestCase):
    def test_the_agent_receives_instructions_requiring_test_first_vertical_slices(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            captured = {}

            def capturing_agent(context):
                captured["instructions"] = context.instructions
                return "[completed] Implemented the thing."

            run_development(issue_path, prd_path, agent=capturing_agent)

            self.assertIn(TEST_FIRST_INSTRUCTION, captured["instructions"])

    def test_the_agent_receives_instructions_prohibiting_direct_review_document_edits(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            captured = {}

            def capturing_agent(context):
                captured["instructions"] = context.instructions
                return "[completed] Implemented the thing."

            run_development(issue_path, prd_path, agent=capturing_agent)

            self.assertIn(NO_DIRECT_REVIEW_EDITS_INSTRUCTION, captured["instructions"])
            self.assertIn(
                build_progress_update_instruction(
                    prd_path.parent / "progress.txt", "developer"
                ),
                captured["instructions"],
            )


class DefaultDevelopmentAgentTests(unittest.TestCase):
    def test_the_default_agent_reports_a_process_start_failure_distinctly(self):
        with self.assertRaises(DevelopmentAgentError):
            default_development_agent(
                DevelopmentContext(
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
