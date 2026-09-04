"""Tests for the interactive clarity-request seam.

`task_loop.clarity.resolve_clarity` prompts the user with a developer's
`[needs-clarity]` request through a replaceable "user input" adapter and
appends a non-empty answer as a `user` reply to the request's thread. It
never fabricates an answer and never appends anything for cancelled,
unavailable, or blank input.
"""
import unittest
from unittest import mock

from task_loop.clarity import ClarityCancelledError, default_user_input_agent, resolve_clarity

from .support import temp_repo


class ThreadedReplyTests(unittest.TestCase):
    def test_a_non_empty_answer_is_appended_as_a_user_reply_to_the_thread(self):
        with temp_repo() as repo:
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.write_text(
                "-- Thread 1\n[developer] - 2026-01-01T00:00:00Z\n\n"
                "Which retry library should I use?\n"
            )

            answer = resolve_clarity(
                review_path,
                thread_id="1",
                message="Which retry library should I use?",
                agent=lambda prompt: "Use the standard library's urllib.",
            )

            self.assertEqual(answer, "Use the standard library's urllib.")
            content = review_path.read_text()
            self.assertIn("-- Reply to Thread 1", content)
            self.assertIn("[user]", content)
            self.assertIn("Use the standard library's urllib.", content)


class CancellationTests(unittest.TestCase):
    def test_a_cancelled_answer_stops_safely_without_appending_anything(self):
        with temp_repo() as repo:
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            prior_content = (
                "-- Thread 1\n[developer] - 2026-01-01T00:00:00Z\n\n"
                "Which retry library should I use?\n"
            )
            review_path.write_text(prior_content)

            with self.assertRaises(ClarityCancelledError):
                resolve_clarity(
                    review_path,
                    thread_id="1",
                    message="Which retry library should I use?",
                    agent=lambda prompt: None,
                )

            self.assertEqual(review_path.read_text(), prior_content)

    def test_a_blank_answer_stops_safely_without_appending_anything(self):
        with temp_repo() as repo:
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            prior_content = (
                "-- Thread 1\n[developer] - 2026-01-01T00:00:00Z\n\n"
                "Which retry library should I use?\n"
            )
            review_path.write_text(prior_content)

            with self.assertRaises(ClarityCancelledError):
                resolve_clarity(
                    review_path,
                    thread_id="1",
                    message="Which retry library should I use?",
                    agent=lambda prompt: "   ",
                )

            self.assertEqual(review_path.read_text(), prior_content)


class PromptContentTests(unittest.TestCase):
    def test_the_user_is_prompted_with_the_clarity_request_message(self):
        with temp_repo() as repo:
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.write_text(
                "-- Thread 1\n[developer] - 2026-01-01T00:00:00Z\n\n"
                "Which retry library should I use?\n"
            )

            captured = {}

            def capturing_agent(prompt):
                captured["prompt"] = prompt
                return "Use urllib."

            resolve_clarity(
                review_path,
                thread_id="1",
                message="Which retry library should I use?",
                agent=capturing_agent,
            )

            self.assertIn("Which retry library should I use?", captured["prompt"])


class DefaultUserInputAgentTests(unittest.TestCase):
    def test_unavailable_input_is_reported_as_cancellation_not_a_crash(self):
        with mock.patch("builtins.input", side_effect=EOFError):
            self.assertIsNone(default_user_input_agent("task-loop needs clarity: x?"))

    def test_interrupted_input_is_reported_as_cancellation_not_a_crash(self):
        with mock.patch("builtins.input", side_effect=KeyboardInterrupt):
            self.assertIsNone(default_user_input_agent("task-loop needs clarity: x?"))


if __name__ == "__main__":
    unittest.main()
