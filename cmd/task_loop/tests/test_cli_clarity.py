"""CLI-level tracer tests proving the needs-clarity pause runs through the
public `task-loop` entry point: the request is presented through an
injectable user-input seam, a non-empty answer is threaded back into the
review document, and the same issue is retried without re-running triage.
"""
import unittest

from .support import passing_review_agent, run_cli, temp_repo, write_issue, write_prd


class CliRetriesAfterClarityTests(unittest.TestCase):
    def test_a_needs_clarity_response_prompts_and_retries_the_same_issue(self):
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
                if len(development_calls) == 1:
                    return "[needs-clarity] Which retry library should I use?"
                return "[completed] Implemented using the approved library."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                triage_agent=counting_triage_agent,
                development_agent=scripted_development_agent,
                clarity_agent=lambda prompt: "Use the standard library's urllib.",
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(triage_calls), 1)
            self.assertEqual(len(development_calls), 2)
            self.assertIn(f"Selected issue: {issue_path}", stdout)
            self.assertIn(
                "Developer: Implemented using the approved library.", stdout
            )
            self.assertIn("Phase: testing", stdout)

            # The retried agent call must see the user's answer already
            # threaded into the review context it receives.
            self.assertIn(
                "Use the standard library's urllib.",
                development_calls[1].review,
            )

            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn("Which retry library should I use?", content)
            self.assertIn("-- Reply to Thread", content)
            self.assertIn("Use the standard library's urllib.", content)
            self.assertIn("Implemented using the approved library.", content)


class CliClarityCancellationTests(unittest.TestCase):
    def test_cancelled_clarity_input_stops_safely_without_marking_complete(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[needs-clarity] Which library?",
                clarity_agent=lambda prompt: None,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertNotIn("Phase: testing", stdout)
            self.assertNotIn("completed", stdout.lower())
            review_path = repo / "review" / "01-first.md"
            self.assertNotIn("[completed]", review_path.read_text())

    def test_a_blank_clarity_answer_stops_safely_without_marking_complete(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[needs-clarity] Which library?",
                clarity_agent=lambda prompt: "   ",
            )

            self.assertNotEqual(exit_code, 0)
            self.assertNotIn("Phase: testing", stdout)


class CliClarityIterationBudgetTests(unittest.TestCase):
    def test_needs_clarity_retries_are_bounded_by_the_iteration_budget(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def always_needs_clarity(context):
                development_calls.append(context)
                return "[needs-clarity] Still unclear, please clarify again."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "2"],
                development_agent=always_needs_clarity,
                clarity_agent=lambda prompt: "An answer that does not resolve it.",
            )

            self.assertNotEqual(exit_code, 0)
            self.assertLessEqual(len(development_calls), 2)
            self.assertIn("iteration", stderr.lower())


if __name__ == "__main__":
    unittest.main()
