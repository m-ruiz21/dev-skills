"""CLI-level tracer tests proving development runs through the public
`task-loop` entry point, immediately after triage selects an issue, without
triage running again.
"""
import unittest

from .support import passing_review_agent, run_cli, temp_repo, write_issue, write_prd


class CliRunsDevelopmentTests(unittest.TestCase):
    def test_a_completed_response_advances_to_the_testing_phase(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=lambda context: "[completed] Implemented the thing.",
                testing_agent=lambda context: "[success]",
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertIn("Developer: Implemented the thing.", stdout)
            self.assertIn("Phase: testing", stdout)
            review_path = repo / "review" / "01-first.md"
            self.assertIn("[developer]", review_path.read_text())

    def test_the_same_issue_is_used_without_running_triage_again(self):
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


class CliMalformedDevelopmentResponseTests(unittest.TestCase):
    def test_a_malformed_development_response_fails_with_a_non_zero_exit(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)], development_agent=lambda context: ""
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("development response", stderr)
            review_path = repo / "review" / "01-first.md"
            self.assertEqual(review_path.read_text(), "")


class CliDevelopmentAgentProcessFailureTests(unittest.TestCase):
    def test_an_agent_process_failure_is_reported_distinctly(self):
        from task_loop.development import DevelopmentAgentError

        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            def crashing_agent(context):
                raise DevelopmentAgentError("agent process exited with status 1")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)], development_agent=crashing_agent
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("development agent failed", stderr)
            self.assertNotIn("development response", stderr)


if __name__ == "__main__":
    unittest.main()
