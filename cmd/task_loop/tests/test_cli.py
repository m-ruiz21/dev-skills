import unittest

from .support import passing_review_agent, run_cli, temp_repo, write_issue, write_prd

_COMPLETED_DEVELOPMENT_AGENT = lambda context: "[completed] Implemented the thing."
_SUCCESS_TESTING_AGENT = lambda context: "[success]"


class SelectExplicitPrdTests(unittest.TestCase):
    def test_selecting_a_valid_prd_path_uses_the_default_iteration_budget(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path)],
                development_agent=_COMPLETED_DEVELOPMENT_AGENT,
                testing_agent=_SUCCESS_TESTING_AGENT,
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertIn(f"Selected PRD: {prd_path}", stdout)
            self.assertIn("Max iterations: 10", stdout)

    def test_max_iterations_flag_overrides_the_default_budget(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "5"],
                development_agent=_COMPLETED_DEVELOPMENT_AGENT,
                testing_agent=_SUCCESS_TESTING_AGENT,
                review_agent=passing_review_agent,
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertIn("Max iterations: 5", stdout)


if __name__ == "__main__":
    unittest.main()
