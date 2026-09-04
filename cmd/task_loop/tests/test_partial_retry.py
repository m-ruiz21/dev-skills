import unittest

from task_loop.development import MalformedDevelopmentResponseError, run_development

from .support import temp_repo, write_issue, write_prd


class PartialOutcomeTests(unittest.TestCase):
    def test_a_partial_response_is_appended_as_a_developer_message_and_does_not_advance(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            result = run_development(
                issue_path,
                prd_path,
                agent=lambda context: "[partial] Wrote the first failing test.",
            )

            self.assertEqual(result.outcome, "partial")
            self.assertNotEqual(result.next_phase, "testing")
            content = review_path.read_text()
            self.assertIn("[developer]", content)
            self.assertIn("Wrote the first failing test.", content)


class MalformedPartialResponseTests(unittest.TestCase):
    def test_a_partial_outcome_with_an_empty_message_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.touch()

            with self.assertRaises(MalformedDevelopmentResponseError):
                run_development(
                    issue_path, prd_path, agent=lambda context: "[partial]   "
                )

            self.assertEqual(review_path.read_text(), "")


if __name__ == "__main__":
    unittest.main()
