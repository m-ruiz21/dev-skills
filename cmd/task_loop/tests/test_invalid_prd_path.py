import os
import unittest

from .support import run_cli, temp_repo, write_prd


class InvalidPrdPathTests(unittest.TestCase):
    def test_missing_prd_path_fails_with_an_actionable_message(self):
        with temp_repo() as repo:
            missing_path = repo / ".scratch" / "task-loop" / "PRD.md"

            exit_code, stdout, stderr = run_cli([str(missing_path)])

            self.assertNotEqual(exit_code, 0)
            self.assertIn("does not exist", stderr)

    def test_non_prd_path_fails_with_an_actionable_message(self):
        with temp_repo() as repo:
            not_a_prd = repo / "notes.md"
            not_a_prd.write_text("# Notes\n")

            exit_code, stdout, stderr = run_cli([str(not_a_prd)])

            self.assertNotEqual(exit_code, 0)
            self.assertIn("PRD.md", stderr)

    def test_unreadable_prd_path_fails_with_an_actionable_message(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            os.chmod(prd_path, 0o000)

            try:
                exit_code, stdout, stderr = run_cli([str(prd_path)])
            finally:
                os.chmod(prd_path, 0o644)

            self.assertNotEqual(exit_code, 0)
            self.assertIn("not readable", stderr)


if __name__ == "__main__":
    unittest.main()
