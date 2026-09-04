"""CLI-level tracer tests proving the testing phase runs through the public
`task-loop` entry point immediately after a completed development pass:
success advances to review, failure retries development for the same issue
with findings threaded into context, a failure lacking recorded findings
fails explicitly instead of silently retrying, and the shared iteration
budget accounts for a failed-test round exactly like the other retry paths.
"""
import unittest

from task_loop.testing import TestingAgentError

from .support import passing_review_agent, run_cli, temp_repo, write_issue, write_prd


class CliAdvancesToReviewOnSuccessTests(unittest.TestCase):
    def test_a_success_testing_response_advances_to_review_without_re_triaging(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            triage_calls = []

            def counting_triage_agent(context):
                triage_calls.append(context)
                return str(context.candidates[0].path)

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                triage_agent=counting_triage_agent,
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(triage_calls), 1)
            self.assertIn(f"Selected issue: {issue_path}", stdout)
            self.assertIn("Tests: success", stdout)
            self.assertIn("Phase: review", stdout)


class CliRetriesDevelopmentAfterFailureTests(unittest.TestCase):
    def test_a_failure_with_findings_retries_development_for_the_same_issue(self):
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
                    return "[completed] First attempt."
                return "[completed] Fixed after investigation."

            testing_calls = []

            def scripted_testing_agent(context):
                testing_calls.append(context)
                if len(testing_calls) == 1:
                    from task_loop.messages import add_message

                    add_message(
                        context.review_path,
                        "test_widget_render fails: missing fixture data.",
                        "reviewer",
                    )
                    return "[failure]"
                return "[success]"

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                triage_agent=counting_triage_agent,
                development_agent=scripted_development_agent,
                testing_agent=scripted_testing_agent,
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(triage_calls), 1)
            self.assertEqual(len(development_calls), 2)
            self.assertEqual(len(testing_calls), 2)
            self.assertIn(f"Selected issue: {issue_path}", stdout)
            self.assertIn("Tests: failure", stdout)
            self.assertIn("Tests: success", stdout)
            self.assertIn("Phase: review", stdout)

            # The retried development call must see the reviewer's finding
            # already threaded into the review context it receives.
            self.assertIn(
                "test_widget_render fails: missing fixture data.",
                development_calls[1].review,
            )

            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn("[reviewer]", content)
            self.assertIn("test_widget_render fails: missing fixture data.", content)
            self.assertIn("Fixed after investigation.", content)


class CliMissingFindingsFailsExplicitlyTests(unittest.TestCase):
    def test_a_failure_without_recorded_findings_fails_explicitly_and_does_not_retry(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return "[completed] Implemented the thing."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=scripted_development_agent,
                testing_agent=lambda context: "[failure]",
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("reviewer", stderr.lower())
            self.assertEqual(len(development_calls), 1)
            self.assertNotIn("Phase: review", stdout)


class CliMalformedTestingResponseTests(unittest.TestCase):
    def test_a_malformed_testing_response_fails_with_a_non_zero_exit(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "all good",
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("testing response", stderr)
            self.assertNotIn("Phase: review", stdout)


class CliTestingAgentProcessFailureTests(unittest.TestCase):
    def test_a_testing_agent_process_failure_is_reported_distinctly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            def crashing_agent(context):
                raise TestingAgentError("agent process exited with status 1")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=crashing_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("testing agent failed", stderr)
            self.assertNotIn("testing response", stderr)


class CliSharedIterationBudgetTests(unittest.TestCase):
    def test_a_completed_development_plus_its_failed_test_consumes_one_iteration(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return "[completed] Attempt."

            def failing_testing_agent(context):
                from task_loop.messages import add_message

                add_message(context.review_path, "Investigated: broken fixture.", "reviewer")
                return "[failure]"

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "1"],
                development_agent=scripted_development_agent,
                testing_agent=failing_testing_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(development_calls), 1)
            self.assertIn("iteration", stderr.lower())

    def test_a_failed_test_starts_the_next_iteration_and_budget_is_enforced_exactly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                return f"[completed] Attempt {len(development_calls)}."

            def always_failing_testing_agent(context):
                from task_loop.messages import add_message

                add_message(
                    context.review_path,
                    f"Investigated attempt {len(development_calls)}.",
                    "reviewer",
                )
                return "[failure]"

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "3"],
                development_agent=scripted_development_agent,
                testing_agent=always_failing_testing_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(development_calls), 3)


if __name__ == "__main__":
    unittest.main()
