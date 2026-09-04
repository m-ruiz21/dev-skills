import unittest

from .support import run_cli, temp_repo, write_prd


class InvalidMaxIterationsTests(unittest.TestCase):
    def test_zero_max_iterations_is_rejected_with_non_zero_exit(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "0"]
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("positive integer", stderr)

    def test_negative_max_iterations_is_rejected_with_non_zero_exit(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "-3"]
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("positive integer", stderr)

    def test_malformed_max_iterations_is_rejected_with_non_zero_exit(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")

            exit_code, stdout, stderr = run_cli(
                [str(prd_path), "--max-iterations", "not-a-number"]
            )

            self.assertNotEqual(exit_code, 0)
            self.assertIn("integer", stderr)


if __name__ == "__main__":
    unittest.main()
