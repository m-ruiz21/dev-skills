import unittest

from .support import run_cli, temp_repo


class AddMessageCliTests(unittest.TestCase):
    def test_add_message_creates_a_thread(self):
        with temp_repo() as repo:
            target = repo / "review" / "01-issue.md"

            exit_code, stdout, stderr = run_cli(
                [
                    "add-message",
                    "-file",
                    str(target),
                    "-message",
                    "Starting work.",
                    "-from",
                    "developer",
                ]
            )

            self.assertEqual(exit_code, 0, stderr)
            self.assertEqual(stdout, "Thread 1\n")
            self.assertIn("Starting work.", target.read_text())

    def test_add_message_reports_validation_errors(self):
        with temp_repo() as repo:
            target = repo / "review" / "01-issue.md"

            exit_code, stdout, stderr = run_cli(
                [
                    "add-message",
                    "-file",
                    str(target),
                    "-message",
                    "Hello.",
                    "-from",
                    "admin",
                ]
            )

            self.assertEqual(exit_code, 1)
            self.assertEqual(stdout, "")
            self.assertIn("task-loop: error:", stderr)
            self.assertFalse(target.exists())


if __name__ == "__main__":
    unittest.main()
