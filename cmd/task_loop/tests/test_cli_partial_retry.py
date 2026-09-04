"""CLI-level tracer tests proving [partial] retry runs through the public
`task-loop` entry point: progress is recorded, the same issue is retried
without re-running triage, the retried call sees accumulated review
context, partial work never advances to testing, and the shared iteration
budget is consumed and enforced exactly -- including when mixed with
needs-clarity retries.
"""
import unittest

from .support import passing_review_agent, run_cli, temp_repo, write_issue, write_prd


class CliRetriesAfterPartialTests(unittest.TestCase):
    def test_a_partial_response_retries_the_same_issue_without_re_triaging_and_eventually_completes(self):
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
                    return "[partial] Wrote the first failing test."
                return "[completed] All vertical slices are green."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                triage_agent=counting_triage_agent,
                development_agent=scripted_development_agent,
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(triage_calls), 1)
            self.assertEqual(len(development_calls), 2)
            self.assertIn(f"Selected issue: {issue_path}", stdout)
            self.assertIn("Developer: All vertical slices are green.", stdout)
            self.assertIn("Phase: testing", stdout)

            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn("Wrote the first failing test.", content)
            self.assertIn("All vertical slices are green.", content)

    def test_the_retried_call_receives_the_accumulated_review_context_including_the_prior_partial_message(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def scripted_development_agent(context):
                development_calls.append(context)
                if len(development_calls) == 1:
                    return "[partial] Wrote the first failing test."
                return "[completed] All vertical slices are green."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=scripted_development_agent,
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(len(development_calls), 2)
            self.assertIn(
                "Wrote the first failing test.", development_calls[1].review
            )


class CliPartialNeverAdvancesToTestingTests(unittest.TestCase):
    def test_partial_work_never_reports_phase_testing_or_marks_completion_even_at_the_cap(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            def always_partial(context):
                return "[partial] Still working on it."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "2"],
                development_agent=always_partial,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertNotIn("Phase: testing", stdout)
            review_path = repo / "review" / "01-first.md"
            self.assertNotIn("[completed]", review_path.read_text())


class CliPartialIterationBudgetTests(unittest.TestCase):
    def test_partial_retries_are_bounded_by_the_iteration_budget_exactly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def always_partial(context):
                development_calls.append(context)
                return f"[partial] Progress update {len(development_calls)}."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "3"],
                development_agent=always_partial,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(development_calls), 3)
            self.assertIn("iteration", stderr.lower())

            # Context recorded across every partial attempt is preserved,
            # not discarded, when the run stops at the cap.
            review_path = repo / "review" / "01-first.md"
            content = review_path.read_text()
            self.assertIn("Progress update 1.", content)
            self.assertIn("Progress update 2.", content)
            self.assertIn("Progress update 3.", content)


class CliSharedBudgetAcrossPartialAndClarityTests(unittest.TestCase):
    def test_partial_and_needs_clarity_retries_share_the_same_iteration_budget(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            development_calls = []

            def mixed_outcomes(context):
                development_calls.append(context)
                if len(development_calls) == 1:
                    return "[partial] First pass done."
                return "[needs-clarity] Which library should I use?"

            clarity_calls = []

            def counting_clarity_agent(prompt):
                clarity_calls.append(prompt)
                return "An answer."

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "2"],
                development_agent=mixed_outcomes,
                clarity_agent=counting_clarity_agent,
            )

            self.assertNotEqual(exit_code, 0)
            self.assertEqual(len(development_calls), 2)
            # The budget is already exhausted by the time the second
            # (needs-clarity) call lands, so no separate clarity budget
            # gives it a further retry.
            self.assertEqual(len(clarity_calls), 0)


if __name__ == "__main__":
    unittest.main()
